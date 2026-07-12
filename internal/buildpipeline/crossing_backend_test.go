package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("buildpipeline: cannot locate test source file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func compileFixtureDiagnostics(t *testing.T, fixture string, backend Backend) []*diag.Diagnostic {
	t.Helper()
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	res, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     filepath.Join(testRepoRoot(t), fixture),
		Backend:        backend,
		MaxDiagnostics: 200,
	})
	if res.Diagnose == nil || res.Diagnose.Bag == nil {
		if err != nil {
			t.Fatalf("compile %s with backend %q: %v", fixture, backend, err)
		}
		return nil
	}
	return res.Diagnose.Bag.Items()
}

func findDiagnostic(diags []*diag.Diagnostic, code diag.Code) *diag.Diagnostic {
	for _, d := range diags {
		if d.Code == code {
			return d
		}
	}
	return nil
}

func TestCrossingBackendUnavailableMessages(t *testing.T) {
	// Executable async shapes on a transportless backend: only there does the
	// generic backend message survive. The synchronous variants of the same
	// programs are pinned as FUT7019 by the crossinggate golden fixtures.
	cases := []struct {
		name    string
		src     string
		code    diag.Code
		message string
	}{
		{
			name: "on placement",
			src: `async fn run(n: int) -> TaskResult<int> {
    return on pool { ret n; };
}`,
			code:    diag.FutOnBackendUnavailable,
			message: "`on` placement crossing cannot be executed: no available backend supports cross-shard transport",
		},
		{
			name: "on far handle",
			src: `async fn send_job(ch: far Channel<int>) -> TaskResult<nothing> {
    return on ch { ch.send(1); ret nothing; };
}`,
			code:    diag.FutOnBackendUnavailable,
			message: "`on` placement crossing cannot be executed: no available backend supports cross-shard transport",
		},
		{
			name: "spawn on",
			src: `async fn start(n: int) -> far Task<int> {
    return spawn on distributed { ret n; };
}`,
			code:    diag.FutSpawnOnBackendUnavailable,
			message: "`spawn on` remote spawn cannot be executed: no available backend supports cross-shard transport",
		},
		{
			name: "far task await",
			src: `async fn wait_result(t: far Task<int>) -> TaskResult<int> {
    return t.await();
}`,
			code:    diag.FutFarTaskAwaitBackendUnavailable,
			message: "`far Task<T>.await()` cannot be executed: no available backend supports remote task transport",
		},
		{
			name: "far task cancel",
			src: `async fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
    return t.cancel();
}`,
			code:    diag.FutFarTaskCancelBackendUnavailable,
			message: "`far Task<T>.cancel()` cannot be executed: no available backend supports remote task transport",
		},
	}
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.sg")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}
			res, _ := Compile(context.Background(), &CompileRequest{
				TargetPath:     path,
				Backend:        BackendVM,
				MaxDiagnostics: 200,
			})
			if res.Diagnose == nil || res.Diagnose.Bag == nil {
				t.Fatal("missing diagnostics bag")
			}
			diags := res.Diagnose.Bag.Items()
			got := findDiagnostic(diags, tc.code)
			if got == nil {
				t.Fatalf("expected %s, got %s", tc.code.ID(), summarizeCodes(diags))
			}
			if got.Message != tc.message {
				t.Fatalf("message mismatch:\nwant: %s\n got: %s", tc.message, got.Message)
			}
			if strings.Contains(got.Message, "Phase") || strings.Contains(got.Message, "Epic") {
				t.Fatalf("message leaks internal planning wording: %q", got.Message)
			}
		})
	}
}

func TestCrossingBackendGuardsAreDefaultClosed(t *testing.T) {
	// Executable async shapes stay guarded with the generic backend code on
	// every backend WITHOUT the transport capability. LLVM is deliberately
	// absent: these forms are open there and the open behavior is pinned by
	// the capability tests and the e2e suites.
	cases := []struct {
		name string
		src  string
		code diag.Code
	}{
		{
			name: "on placement",
			src: `async fn run(n: int) -> TaskResult<int> {
    return on pool { ret n; };
}`,
			code: diag.FutOnBackendUnavailable,
		},
		{
			name: "on far handle",
			src: `async fn send_job(ch: far Channel<int>) -> TaskResult<nothing> {
    return on ch { ch.send(1); ret nothing; };
}`,
			code: diag.FutOnBackendUnavailable,
		},
		{
			name: "spawn on",
			src: `async fn start(n: int) -> far Task<int> {
    return spawn on distributed { ret n; };
}`,
			code: diag.FutSpawnOnBackendUnavailable,
		},
		{
			name: "far task await",
			src: `async fn wait_result(t: far Task<int>) -> TaskResult<int> {
    return t.await();
}`,
			code: diag.FutFarTaskAwaitBackendUnavailable,
		},
		{
			name: "far task cancel",
			src: `async fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
    return t.cancel();
}`,
			code: diag.FutFarTaskCancelBackendUnavailable,
		},
	}
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, backend := range []Backend{BackendVM, Backend("future_backend")} {
				t.Run(string(backend), func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "main.sg")
					if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
						t.Fatalf("write source: %v", err)
					}
					res, _ := Compile(context.Background(), &CompileRequest{
						TargetPath:     path,
						Backend:        backend,
						MaxDiagnostics: 200,
					})
					if res.Diagnose == nil || res.Diagnose.Bag == nil {
						t.Fatal("missing diagnostics bag")
					}
					diags := res.Diagnose.Bag.Items()
					if got := findDiagnostic(diags, tc.code); got == nil {
						t.Fatalf("expected default-closed %s, got %s", tc.code.ID(), summarizeCodes(diags))
					}
				})
			}
		})
	}
}

func TestCrossingBackendGuardDoesNotMaskSemaErrors(t *testing.T) {
	cases := []struct {
		name       string
		fixture    string
		semaCode   diag.Code
		futureCode diag.Code
	}{
		{
			name:       "on",
			fixture:    "testdata/golden/crossing/block02/invalid/on_negative_integer_destination.sg",
			semaCode:   diag.SemaOnDestNotPlacement,
			futureCode: diag.FutOnBackendUnavailable,
		},
		{
			name:       "spawn on",
			fixture:    "testdata/golden/crossing/block03/invalid/spawn_on_negative_nonplacement_literal.sg",
			semaCode:   diag.SemaSpawnOnDestNotPlacement,
			futureCode: diag.FutSpawnOnBackendUnavailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := compileFixtureDiagnostics(t, tc.fixture, BackendVM)
			if got := findDiagnostic(diags, tc.semaCode); got == nil {
				t.Fatalf("expected semantic diagnostic %s, got %s", tc.semaCode.ID(), summarizeCodes(diags))
			}
			if got := findDiagnostic(diags, tc.futureCode); got != nil {
				t.Fatalf("unexpected backend-unavailable diagnostic on sema-invalid crossing: %s", got.Code.ID())
			}
		})
	}
}

func TestCrossingBackendGuardsCoverImportedModules(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))

	dir, err := os.MkdirTemp(".", "rv2-crossing-xmod-")
	if err != nil {
		t.Fatalf("mkdir temp project: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	remoteDir := filepath.Join(dir, "remote")
	if mkdirErr := os.MkdirAll(remoteDir, 0o755); mkdirErr != nil {
		t.Fatalf("mkdir remote module: %v", mkdirErr)
	}
	writeFile := func(path, body string) {
		t.Helper()
		if writeErr := os.WriteFile(path, []byte(body), 0o600); writeErr != nil {
			t.Fatalf("write %s: %v", path, writeErr)
		}
	}
	writeFile(filepath.Join(remoteDir, "remote.sg"), `
pragma module::remote;

pub fn remote_score() -> TaskResult<int> {
	return on pool {
		ret 7;
	};
}

pub fn start_remote() -> far Task<int> {
	return spawn on pool {
		ret 41;
	};
}

pub fn cancel_remote() -> TaskResult<nothing> {
	let task: far Task<int> = spawn on pool {
		ret 1;
	};
	return task.cancel();
}

pub fn close_remote(ch: far Channel<int>) -> TaskResult<nothing> {
	return on ch {
		ch.close();
		ret nothing;
	};
}
`)
	mainPath := filepath.Join(dir, "main.sg")
	writeFile(mainPath, `
import remote::remote_score;
import remote::start_remote;
import remote::cancel_remote;
import remote::close_remote;

fn caller() -> TaskResult<int> {
	let _score = remote_score();
	let _cancelled = cancel_remote();
	let task = start_remote();
	return task.await();
}
`)

	// Every imported crossing sits in a synchronous function, so the guard
	// names the missing async context — the same finding on every backend.
	// The count proves the guard walked the DEPENDENCY module: at least six
	// findings live in remote.sg (on placement, spawn on, spawn+cancel, and
	// the on far-handle form), beyond the caller's own sites.
	for _, backend := range []Backend{BackendVM, BackendLLVM, Backend("future_backend")} {
		t.Run(string(backend), func(t *testing.T) {
			res, compileErr := Compile(context.Background(), &CompileRequest{
				TargetPath:     mainPath,
				Backend:        backend,
				MaxDiagnostics: 200,
			})
			if compileErr == nil {
				t.Fatal("expected compile to stop on imported crossing constructs before lowering")
			}
			if res.MIR != nil {
				t.Fatal("expected no MIR for a guarded crossing compile")
			}
			if res.Diagnose == nil || res.Diagnose.Bag == nil {
				t.Fatalf("missing diagnostics bag: %v", compileErr)
			}
			diags := res.Diagnose.Bag.Items()
			if got := countDiagnostics(diags, diag.FutCrossingSyncContext); got < 6 {
				t.Errorf("expected the guard to reach the imported module's crossings, got %d sync-context findings: %s",
					got, summarizeCodes(diags))
			}
		})
	}
}

func TestLLVMTransportCapabilityOpensAsyncSpawnOn(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	source := `
async fn run(dst: Placement, n: int) -> far Task<int> {
    return spawn on dst {
        ret n + 1;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	enabled, enabledErr := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		Backend:        BackendLLVM,
		MaxDiagnostics: 200,
	})
	if enabledErr != nil {
		t.Fatalf("production LLVM async spawn_on should compile to MIR: %v", enabledErr)
	}
	if enabled.MIR == nil {
		t.Fatal("production LLVM async spawn_on produced no MIR")
	}
	if enabled.Diagnose != nil && enabled.Diagnose.Bag != nil {
		if got := findDiagnostic(enabled.Diagnose.Bag.Items(), diag.FutSpawnOnBackendUnavailable); got != nil {
			t.Fatalf("production LLVM capability must suppress FUT7015, got %s", got.Code.ID())
		}
	}
}

func TestSpawnOnCrossingOverrideDoesNotOpenOtherForms(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	source := `
fn direct_on(dst: Placement) -> TaskResult<int> {
    return on dst {
        ret 1;
    };
}

fn wait_remote(t: far Task<int>) -> TaskResult<int> {
    return t.await();
}

fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
    return t.cancel();
}

async fn remote_child(dst: Placement) -> far Task<int> {
    return spawn on dst {
        ret 1;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	res, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		Backend:        BackendLLVM,
		MaxDiagnostics: 200,
		CrossingFormsForTest: map[sema.CrossingLoweringKind]bool{
			sema.CrossingLoweringSpawnOn: true,
		},
	})
	if err == nil {
		t.Fatal("spawn-only override must not open other crossing forms")
	}
	if res.MIR != nil {
		t.Fatal("compile with still-guarded crossing forms must not produce MIR")
	}
	if res.Diagnose == nil || res.Diagnose.Bag == nil {
		t.Fatalf("missing diagnostics from guarded compile: %v", err)
	}
	diags := res.Diagnose.Bag.Items()
	// The three non-spawn forms sit in synchronous functions, so the guard
	// names the missing async context for each; the compile still stops.
	if got := countDiagnostics(diags, diag.FutCrossingSyncContext); got < 3 {
		t.Fatalf("expected the non-spawn forms to stay guarded with spawn-only override, got %d findings: %s",
			got, summarizeCodes(diags))
	}
	if got := findDiagnostic(diags, diag.FutSpawnOnBackendUnavailable); got != nil {
		t.Fatalf("spawn-only override should suppress FUT7015, got %s", summarizeCodes(diags))
	}
}

func TestSpawnOnCrossingOverrideCoversImportedModules(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir, err := os.MkdirTemp(".", "rv2-crossing-override-xmod-")
	if err != nil {
		t.Fatalf("mkdir temp project: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	remoteDir := filepath.Join(dir, "remote")
	err = os.MkdirAll(remoteDir, 0o755)
	if err != nil {
		t.Fatalf("mkdir remote module: %v", err)
	}
	err = os.WriteFile(filepath.Join(remoteDir, "remote.sg"), []byte(`
pragma module::remote;

pub async fn remote_child(dst: Placement, n: int) -> far Task<int> {
    return spawn on dst {
        ret n + 1;
    };
}
`), 0o600)
	if err != nil {
		t.Fatalf("write remote module: %v", err)
	}
	mainPath := filepath.Join(dir, "main.sg")
	err = os.WriteFile(mainPath, []byte(`
import remote::remote_child;

@entrypoint
fn main() -> int {
    return 0;
}
`), 0o600)
	if err != nil {
		t.Fatalf("write main source: %v", err)
	}

	res, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     mainPath,
		Backend:        BackendLLVM,
		MaxDiagnostics: 200,
		CrossingFormsForTest: map[sema.CrossingLoweringKind]bool{
			sema.CrossingLoweringSpawnOn: true,
		},
	})
	if err != nil {
		t.Fatalf("spawn_on override should apply during imported-module HIR merge: %v", err)
	}
	if res.MIR == nil {
		t.Fatal("imported spawn_on override compile produced no MIR")
	}
}

func countDiagnostics(diags []*diag.Diagnostic, code diag.Code) int {
	count := 0
	for _, d := range diags {
		if d.Code == code {
			count++
		}
	}
	return count
}

func summarizeCodes(diags []*diag.Diagnostic) string {
	if len(diags) == 0 {
		return "(no diagnostics)"
	}
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code.ID())
	}
	return strings.Join(codes, ", ")
}
