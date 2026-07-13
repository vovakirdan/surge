package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/driver"
)

func TestLLVMTransportPayloadGuard(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name  string
		src   string
		codes []diag.Code
	}{
		{
			name: "owned shard movable capture",
			src: `
@shard_movable
type Movable = { id: int };

fn use(m: own Movable) -> int { return m.id; }

async fn start(dst: Placement, m: own Movable) -> far Task<int> {
    return spawn on dst { ret use(own m); };
}
`,
			codes: []diag.Code{diag.FutCrossingPayloadNotShippable},
		},
		{
			name: "heap backed result and await",
			src: `
async fn start(dst: Placement) -> far Task<string> {
    return spawn on dst { ret "heap-owned"; };
}

async fn wait(t: far Task<string>) -> TaskResult<string> {
    return t.await();
}
`,
			codes: []diag.Code{diag.FutCrossingPayloadNotShippable},
		},
		{
			name: "captured far task lease",
			src: `
async fn start(dst: Placement, task: far Task<int>) -> far Task<int> {
    return spawn on dst {
        let _cancelled: TaskResult<nothing> = task.cancel();
        ret 1;
    };
}
`,
			codes: []diag.Code{diag.FutCrossingPayloadNotShippable},
		},
		{
			name: "heap element channel mint",
			src: `
async fn produce(dst: Placement) -> int {
    let ch: far Channel<string> = channel_on::<string>(dst, 4);
    let _ = ch;
    return 0;
}
`,
			codes: []diag.Code{diag.FutCrossingPayloadNotShippable},
		},
		{
			name: "union reply through anchored block",
			src: `
async fn take(ch: far Channel<int>) -> TaskResult<Option<int>> {
    return on ch { ret ch.recv(); };
}
`,
			codes: []diag.Code{diag.FutCrossingPayloadNotShippable},
		},
		{
			name: "captured far task lease in anchored body",
			src: `
async fn f(ch: far Channel<int>, task: far Task<int>) -> TaskResult<nothing> {
    return on ch {
        ch.send(1);
        let _cancelled: TaskResult<nothing> = task.cancel();
        ret nothing;
    };
}
`,
			codes: []diag.Code{diag.FutCrossingPayloadNotShippable},
		},
		{
			name: "owned shard movable capture into anchored body",
			src: `
@shard_movable
type Movable = { id: int };

fn use(m: own Movable) -> int { return m.id; }

async fn f(ch: far Channel<int>, m: own Movable) -> TaskResult<nothing> {
    return on ch {
        ch.send(1);
        let _ = use(own m);
        ret nothing;
    };
}
`,
			codes: []diag.Code{diag.FutCrossingPayloadNotShippable},
		},
		{
			name: "share stays guarded until its lowering lands",
			src: `
async fn fan_out(ch: far Channel<int>) -> far Channel<int> {
    return ch.share();
}
`,
			codes: []diag.Code{diag.FutChannelShareBackendUnavailable},
		},
		{
			name: "synchronous share names the missing async context",
			src: `
fn fan_out(ch: far Channel<int>) -> far Channel<int> {
    return ch.share();
}
`,
			codes: []diag.Code{diag.FutCrossingSyncContext},
		},
		{
			name: "synchronous site names the missing async context",
			src: `
fn start(dst: Placement) -> far Task<int> {
    return spawn on dst { ret 7; };
}
`,
			codes: []diag.Code{diag.FutCrossingSyncContext},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.sg")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}
			res, err := Compile(context.Background(), &CompileRequest{
				TargetPath: path, Backend: BackendLLVM, MaxDiagnostics: 200,
			})
			if err == nil || res.MIR != nil {
				t.Fatal("representation-unsafe crossing must stop before MIR")
			}
			if res.Diagnose == nil || res.Diagnose.Bag == nil {
				t.Fatalf("missing diagnostics: %v", err)
			}
			for _, code := range tc.codes {
				if findDiagnostic(res.Diagnose.Bag.Items(), code) == nil {
					t.Errorf("missing %s; got %s", code.ID(), summarizeCodes(res.Diagnose.Bag.Items()))
				}
			}
		})
	}
}

// TestCrossingPayloadDiagnosticNamesTheField pins the kindness contract:
// a nested heap-owning payload names the exact offending field path, and
// the anchored union reply names the in-body unwrap fix.
func TestCrossingPayloadDiagnosticNamesTheField(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "nested field path",
			src: `
type Meta = { name: string };
type Report = { id: int, meta: Meta };

async fn start(dst: Placement) -> far Task<Report> {
    return spawn on dst { ret Report{ id: 1, meta: Meta{ name: "x" } }; };
}
`,
			contains: []string{"`Report`", "field `meta.name` owns heap memory"},
		},
		{
			name: "anchored union reply names the unwrap fix",
			src: `
async fn take(ch: far Channel<int>) -> TaskResult<Option<int>> {
    return on ch { ret ch.recv(); };
}
`,
			contains: []string{"`Option<int>`", "unwrap it inside the block before `ret`"},
		},
		{
			name: "shard movable capture names the binding",
			src: `
@shard_movable
type Movable = { id: int };

fn use(m: own Movable) -> int { return m.id; }

async fn f(ch: far Channel<int>, m: own Movable) -> TaskResult<nothing> {
    return on ch { ch.send(1); let _ = use(own m); ret nothing; };
}
`,
			contains: []string{"capture `m`", "moves owned data across shards"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.sg")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}
			res, _ := Compile(context.Background(), &CompileRequest{
				TargetPath: path, Backend: BackendLLVM, MaxDiagnostics: 200,
			})
			if res.Diagnose == nil || res.Diagnose.Bag == nil {
				t.Fatal("missing diagnostics bag")
			}
			found := findDiagnostic(res.Diagnose.Bag.Items(), diag.FutCrossingPayloadNotShippable)
			if found == nil {
				t.Fatalf("missing FUT7020, got %s", summarizeCodes(res.Diagnose.Bag.Items()))
			}
			for _, want := range tc.contains {
				if !strings.Contains(found.Message, want) {
					t.Fatalf("message %q does not contain %q", found.Message, want)
				}
			}
		})
	}
}

func TestCompileOnlyCrossingDoesNotReportBackendUnavailable(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	sources := []string{
		`fn start(dst: Placement) -> far Task<int> {
    return spawn on dst { ret 7; };
}`,
		`async fn start(dst: Placement) -> far Task<string> {
    return spawn on dst { ret "heap-owned"; };
}`,
	}
	for i, source := range sources {
		path := filepath.Join(t.TempDir(), "main.sg")
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatalf("write source %d: %v", i, err)
		}
		res, err := driver.Diagnose(context.Background(), path, driver.DiagnoseStageSema, 200)
		if err != nil || res == nil || res.Sema == nil || res.Bag == nil {
			t.Fatalf("compile-only diagnose %d failed: %v", i, err)
		}
		if len(res.Sema.CrossingLowering) == 0 {
			t.Fatalf("compile-only source %d recorded no crossing", i)
		}
		addSpawnOnBackendErrors(&CompileRequest{}, res)
		for _, code := range []diag.Code{
			diag.FutSpawnOnBackendUnavailable,
			diag.FutFarTaskAwaitBackendUnavailable,
			diag.FutFarTaskCancelBackendUnavailable,
		} {
			if findDiagnostic(res.Bag.Items(), code) != nil {
				t.Errorf("compile-only source %d unexpectedly reported %s", i, code.ID())
			}
		}
	}
}

func TestLLVMTransportPayloadGuardCoversImportedModules(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir, err := os.MkdirTemp(".", "rv2-payload-xmod-")
	if err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	remoteDir := filepath.Join(dir, "remote")
	if err := os.MkdirAll(remoteDir, 0o755); err != nil {
		t.Fatalf("mkdir remote: %v", err)
	}
	remote := `
pragma module::remote;

pub async fn start(dst: Placement) -> far Task<string> {
    return spawn on dst { ret "heap-owned"; };
}

pub async fn wait_remote(t: far Task<string>) -> TaskResult<string> {
    return t.await();
}
`
	if err := os.WriteFile(filepath.Join(remoteDir, "remote.sg"), []byte(remote), 0o600); err != nil {
		t.Fatalf("write remote: %v", err)
	}
	mainPath := filepath.Join(dir, "main.sg")
	if err := os.WriteFile(mainPath, []byte("import remote::start;\nimport remote::wait_remote;\n"), 0o600); err != nil {
		t.Fatalf("write main: %v", err)
	}
	res, compileErr := Compile(context.Background(), &CompileRequest{
		TargetPath: mainPath, Backend: BackendLLVM, MaxDiagnostics: 200,
	})
	if compileErr == nil || res.MIR != nil {
		t.Fatal("unsafe imported crossing must stop before MIR")
	}
	if res.Diagnose == nil || res.Diagnose.Bag == nil {
		t.Fatalf("missing diagnostics: %v", compileErr)
	}
	// Both imported crossings carry a heap-backed `string` payload, so the
	// guard names the payload cause for each — proving the payload walk
	// reaches dependency modules.
	if got := countDiagnostics(res.Diagnose.Bag.Items(), diag.FutCrossingPayloadNotShippable); got < 2 {
		t.Errorf("expected imported payload findings, got %d: %s",
			got, summarizeCodes(res.Diagnose.Bag.Items()))
	}
}
