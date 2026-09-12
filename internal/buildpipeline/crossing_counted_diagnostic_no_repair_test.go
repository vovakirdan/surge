package buildpipeline

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

func countedOwnedCaptureSource(attribute string) string {
	return attribute + `
type Payload = { ch: Channel<$N> };
fn use(value: own Payload) -> bool { return true; }
async fn probe(dst: Placement, value: own Payload) -> TaskResult<bool> {
    return on dst { ret use(own value); };
}
`
}

func TestCountedBlockDiagnosticDoesNotOfferFalseRepair(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	rows := []struct {
		name       string
		src        string
		before     diag.Code
		after      diag.Code
		afterCause string
	}{
		{"on_send_only", countedOwnedCaptureSource("@send\n"),
			diag.SemaCrossNotShardMovable, diag.SemaShardMovableSendInsufficient, "`@send` is not sufficient"},
		{"on_owned_copy", countedOwnedCaptureSource("@copy\n"),
			diag.SemaCrossNotShardMovable, diag.SemaShardMovableCopyInsufficient, "`@copy` is not sufficient"},
		{"on_pinned", countedOwnedCaptureSource("@shard_pinned\n"),
			diag.SemaCrossNotShardMovable, diag.SemaCrossPinnedCapture, "shard-pinned"},
		{"on_nosend", countedOwnedCaptureSource("@nosend\n"),
			diag.SemaCrossNotShardMovable, diag.SemaCrossNosendCapture, "`@nosend`"},
		{"blocking_nested_nosend", `
@nosend
type Local = { ch: Channel<$N> };
type Payload = { local: Local };
fn use(value: own Payload) -> bool { return true; }
async fn probe(value: own Payload) -> bool {
    let job: Task<bool> = blocking { ret use(own value); };
    return compare job.await() { Success(v) => v; Cancelled() => false; };
}`, diag.SemaCrossNotShardMovable, diag.SemaNosendInSpawn, "@nosend field"},
		{"blocking_local_task", `
fn probe() -> Task<Task<Channel<$N>>> {
    let task: Task<Channel<$N>> = @local spawn async {
        ret Channel::<$N>::new(1:uint);
    };
    return blocking { ret task; };
}`, diag.SemaCrossNotShardMovable, diag.SemaNosendInSpawn, "local task handle"},
		{"on_array_view", `
fn use(values: own $N[]) -> bool { return true; }
async fn probe(dst: Placement) -> TaskResult<bool> {
    let base: $N[] = [1:$N, 2:$N, 3:$N];
    let view: $N[] = base[[0..2]];
    return on dst { ret use(own view); };
}`, diag.SemaCrossNotShardMovable, diag.SemaCrossNotShardMovable, "is a view of another array"},
		{"on_held_array_view", `
fn use(values: own $N[][]) -> bool { return true; }
async fn probe(dst: Placement) -> TaskResult<bool> {
    let base: $N[] = [1:$N, 2:$N, 3:$N];
    let view: $N[] = base[[0..2]];
    let holder: $N[][] = [view];
    return on dst { ret use(own holder); };
}`, diag.SemaCrossNotShardMovable, diag.SemaCrossNotShardMovable, "is a view of another array"},
		{"blocking_unchecked_array", `
type Payload = { ch: Channel<$N>, values: Map<int64, int64[]> };
fn use(value: own Payload) -> bool { return true; }
async fn probe(value: own Payload) -> bool {
    let job: Task<bool> = blocking { ret use(own value); };
    return compare job.await() { Success(v) => v; Cancelled() => false; };
}`, diag.SemaCrossNotShardMovable, diag.SemaCrossNotShardMovable, "dynamic array in storage"},
		{"reply_unchecked_array", `
@copy
type Payload = { ch: Channel<$N>, values: Channel<int64[]> };
async fn probe(dst: Placement) -> far Task<Payload> {
    return spawn on dst {
        ret Payload { ch: Channel::<$N>::new(1:uint), values: Channel::<int64[]>::new(1:uint) };
    };
}`, diag.FutCrossingPayloadNotShippable, diag.FutCrossingPayloadNotShippable, "dynamic array in storage"},
		{"channel_unchecked_array", `
type Payload = { ch: Channel<$N>, values: Map<int64, int64[]> };
async fn probe(dst: Placement) -> far Channel<Payload> {
    return channel_on::<Payload>(dst, 1:uint);
}`, diag.FutCrossingPayloadNotShippable, diag.FutCrossingPayloadNotShippable, "dynamic array in storage"},
		{"map_reply_remains_noncopy", `
async fn probe(dst: Placement) -> far Task<Map<$N, bool>> {
    return spawn on dst { ret Map::<$N, bool>.new(); };
}`, diag.FutCrossingPayloadNotShippable, diag.FutCrossingPayloadNotShippable, "not plain-copy"},
		{"on_borrow_precedes_counted", `
fn use(ch: &Channel<$N>) -> bool { return true; }
async fn probe(dst: Placement, ch: &Channel<$N>) -> TaskResult<bool> {
    return on dst { ret use(ch); };
}`, diag.SemaCrossBorrowCapture, diag.SemaCrossBorrowCapture, "borrowed values"},
		{"blocking_borrow_precedes_counted", `
fn use(ch: &Channel<$N>) -> bool { return true; }
async fn probe(ch: &Channel<$N>) -> bool {
    let job: Task<bool> = blocking { ret use(ch); };
    return compare job.await() { Success(v) => v; Cancelled() => false; };
}`, diag.SemaBlockingBorrowCapture, diag.SemaBlockingBorrowCapture, "cannot capture reference"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			before := requireCountedDiagnostic(t, strings.ReplaceAll(row.src, "$N", "int"), row.before)
			for _, forbidden := range []string{"If fixed precision is sufficient", "fixed-width type (`float64`)", "values themselves"} {
				if strings.Contains(before, forbidden) {
					t.Errorf("unproved width repair %q: %s", forbidden, before)
				}
			}
			// Taking the purported repair must still hit the independent refusal,
			// not merely fail for any reason such as a malformed fixture.
			after := requireCountedDiagnostic(t, strings.ReplaceAll(row.src, "$N", "int64"), row.after)
			if !strings.Contains(after, row.afterCause) {
				t.Fatalf("width-only change lost the expected independent cause %q: %s", row.afterCause, after)
			}
		})
	}
	requireCountedWidthAdviceDoesNotPromiseADeclarationRepair(t)
}

func requireCountedWidthAdviceDoesNotPromiseADeclarationRepair(t *testing.T) {
	t.Helper()
	src := countedOwnedCaptureSource("@shard_movable\n")
	res, err := countedDiagnosticCompile(t, strings.ReplaceAll(src, "$N", "int"))
	if err == nil || res.MIR != nil || res.Diagnose == nil || res.Diagnose.Bag == nil {
		t.Fatalf("invalid shard-movable declaration did not stop before MIR: %v", err)
	}
	seen := map[diag.Code]bool{}
	for _, item := range res.Diagnose.Bag.Items() {
		if item.Severity != diag.SevError {
			continue
		}
		if item.Code != diag.SemaCrossNotShardMovable && item.Code != diag.SemaShardMovableField {
			t.Fatalf("unrelated declaration diagnostic: %s: %s", item.Code.ID(), item.Message)
		}
		seen[item.Code] = true
		if strings.Contains(item.Message, "If fixed precision is sufficient") {
			t.Fatalf("width advice would leave the declared field non-shard-movable: %s", item.Message)
		}
	}
	if !seen[diag.SemaCrossNotShardMovable] || !seen[diag.SemaShardMovableField] {
		t.Fatalf("declaration control missed one of its independent refusals: %v", seen)
	}
	requireCountedDiagnostic(t, strings.ReplaceAll(src, "$N", "int64"), diag.SemaShardMovableField)
}
