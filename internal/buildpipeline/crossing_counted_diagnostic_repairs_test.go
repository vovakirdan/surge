package buildpipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/backend/llvm"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/types"
)

const countedOnCaptureSource = `
fn use(ch: Channel<$N>) -> bool { let held: Channel<$N> = ch; return true; }
async fn probe(dst: Placement, ch: Channel<$N>) -> TaskResult<bool> {
    return on dst { ret use(ch); };
}
`

const countedBlockingCaptureSource = `
fn use(ch: Channel<$N>) -> bool { let held: Channel<$N> = ch; return true; }
async fn probe(ch: Channel<$N>) -> bool {
    let job: Task<bool> = blocking { ret use(ch); };
    return compare job.await() { Success(v) => v; Cancelled() => false; };
}
`

const countedChannelCreateSource = `
async fn probe(dst: Placement) -> far Channel<Channel<$N>> {
    return channel_on::<Channel<$N>>(dst, 1:uint);
}
`

const countedReplySource = `
async fn probe(dst: Placement) -> far Task<Channel<$N>> {
    return spawn on dst { ret Channel::<$N>::new(1:uint); };
}
`

func countedDiagnosticCompile(t *testing.T, src string) (CompileResult, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.sg")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return Compile(context.Background(), &CompileRequest{
		TargetPath: path, Backend: BackendLLVM, Analysis: true, MaxDiagnostics: 200,
	})
}

func requireCountedDiagnostic(t *testing.T, src string, code diag.Code) string {
	t.Helper()
	res, err := countedDiagnosticCompile(t, src)
	if err == nil || res.MIR != nil || res.Diagnose == nil || res.Diagnose.Bag == nil {
		t.Fatalf("expected %s before MIR: err=%v result=%+v", code.ID(), err, res)
	}
	var messages []string
	for _, item := range res.Diagnose.Bag.Items() {
		if item.Severity != diag.SevError {
			continue
		}
		if item.Code != code {
			t.Fatalf("unrelated diagnostic %s: %s (wanted %s)", item.Code.ID(), item.Message, code.ID())
		}
		messages = append(messages, item.Message)
	}
	if len(messages) == 0 {
		t.Fatalf("missing %s: %v", code.ID(), err)
	}
	return strings.Join(messages, "\n")
}

func requireCountedRepairEmits(t *testing.T, src, site string) {
	t.Helper()
	res, err := countedDiagnosticCompile(t, src)
	if err != nil || res.MIR == nil || res.Diagnose == nil || res.Diagnose.Sema == nil ||
		res.Diagnose.Bag == nil || res.Diagnose.Bag.HasErrors() {
		t.Fatalf("suggested width repair does not compile: %v", err)
	}
	ir, err := llvm.EmitModule(res.MIR, res.Diagnose.Sema.TypeInterner, res.Diagnose.Symbols.Table, res.Diagnose.FileSet)
	if err != nil {
		t.Fatalf("suggested width repair does not emit: %v", err)
	}
	semaRes := res.Diagnose.Sema
	if site == "blocking_capture" {
		if len(semaRes.BlockingCaptures) == 0 || !strings.Contains(ir, "call ptr @rt_blocking_submit(") {
			t.Fatal("repair removed the actual captured blocking operation")
		}
		return
	}
	forms := map[string]sema.CrossingLoweringKind{
		"on_capture":     sema.CrossingLoweringOnPlacement,
		"channel_create": sema.CrossingLoweringChannelCreate,
		"reply":          sema.CrossingLoweringSpawnOn,
		"far_await":      sema.CrossingLoweringFarTaskAwait,
	}
	want, ok := forms[site]
	if !ok {
		t.Fatalf("unknown proof site %q", site)
	}
	for _, record := range semaRes.CrossingLowering {
		if record.Kind != want || !record.Expr.IsValid() || !record.SuspendCapable {
			continue
		}
		if site == "on_capture" {
			for _, capture := range record.Captures {
				if capture.Name == "ch" && capture.Type != types.NoTypeID {
					return
				}
			}
		} else if record.PayloadType != types.NoTypeID {
			return
		}
	}
	t.Fatalf("repair lost its real %s crossing record", site)
}

func TestCountedBlockDiagnosticRepairsAtAllSites(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	sites := []struct {
		name string
		src  string
		code diag.Code
	}{
		{"on_capture", countedOnCaptureSource, diag.SemaCrossNotShardMovable},
		{"blocking_capture", countedBlockingCaptureSource, diag.SemaCrossNotShardMovable},
		{"channel_create", countedChannelCreateSource, diag.FutCrossingPayloadNotShippable},
		{"reply", countedReplySource, diag.FutCrossingPayloadNotShippable},
	}
	for _, site := range sites {
		for _, number := range []string{"int", "uint", "float"} {
			t.Run(site.name+"_"+number, func(t *testing.T) {
				message := requireCountedDiagnostic(t, strings.ReplaceAll(site.src, "$N", number), site.code)
				for _, want := range []string{
					fmt.Sprintf("`%s` at `payload[0]`", number),
					fmt.Sprintf("replace `%s` with `%s64`", number, number),
				} {
					if !strings.Contains(message, want) {
						t.Fatalf("diagnostic %q lacks %q", message, want)
					}
				}
				for _, other := range []string{"int", "uint", "float"} {
					if other != number && strings.Contains(message, "`"+other+"` with `"+other+"64`") {
						t.Fatalf("repair names a numeric kind the payload does not hold: %s", message)
					}
				}
				if strings.Contains(message, "values themselves") {
					t.Fatalf("unproved alternative repair survived: %s", message)
				}
				requireCountedRepairEmits(t, strings.ReplaceAll(site.src, "$N", number+"64"), site.name)
			})
		}
	}
}

func TestCountedBlockDiagnosticRecursiveRepairs(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	rows := []struct {
		name  string
		src   string
		site  string
		path  string
		kinds []string
	}{
		{"nested_map_key", `
type Meta = { counts: Map<$I, bool> };
type Envelope = { meta: Meta };
async fn probe(dst: Placement) -> far Channel<Envelope> {
    return channel_on::<Envelope>(dst, 1:uint);
}`, "channel_create", "meta.counts.key", []string{"int"}},
		{"tag_payload", `
tag Held(Channel<$I>);
tag Empty();
type Envelope = Held(Channel<$I>) | Empty();
async fn probe(dst: Placement) -> far Channel<Envelope> {
    return channel_on::<Envelope>(dst, 1:uint);
}`, "channel_create", "Held[0].payload[0]", []string{"int"}},
		{"map_key_and_value", `
async fn probe(dst: Placement) -> far Channel<Map<$I, $F>> {
    return channel_on::<Map<$I, $F>>(dst, 1:uint);
}`, "channel_create", "key", []string{"int", "float"}},
		{"far_await_channel_uint", `
async fn probe(task: far Task<Channel<$U>>) -> TaskResult<Channel<$U>> {
    return task.await();
}`, "far_await", "payload[0]", []string{"uint"}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			original := strings.NewReplacer("$I", "int", "$U", "uint", "$F", "float").Replace(row.src)
			repaired := strings.NewReplacer("$I", "int64", "$U", "uint64", "$F", "float64").Replace(row.src)
			message := requireCountedDiagnostic(t, original, diag.FutCrossingPayloadNotShippable)
			if !strings.Contains(message, "at `"+row.path+"`") {
				t.Fatalf("wrong first inaccessible path: %s", message)
			}
			for _, kind := range row.kinds {
				if !strings.Contains(message, "`"+kind+"` with `"+kind+"64`") {
					t.Errorf("missing required %s repair: %s", kind, message)
				}
			}
			requireCountedRepairEmits(t, repaired, row.site)
		})
	}
}

func TestCountedBlockDiagnosticValidDeclarationsRepair(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	for _, row := range []struct{ name, src string }{
		{"on_capture", countedOnCaptureSource},
		{"blocking_capture", countedBlockingCaptureSource},
	} {
		t.Run(row.name, func(t *testing.T) {
			src := "@copy @shard_movable\ntype Number = { value: $N };\n" +
				strings.ReplaceAll(row.src, "$N", "Number")
			message := requireCountedDiagnostic(t, strings.ReplaceAll(src, "$N", "int"), diag.SemaCrossNotShardMovable)
			for _, want := range []string{"`int` at `payload[0].value`", "replace `int` with `int64`"} {
				if !strings.Contains(message, want) {
					t.Fatalf("valid declaration lost proven width repair %q: %s", want, message)
				}
			}
			requireCountedRepairEmits(t, strings.ReplaceAll(src, "$N", "int64"), row.name)
		})
	}
}
