package buildpipeline

import (
	"fmt"
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
fn relay(task: Task<Channel<$N>>) -> Task<Channel<$N>> { return task; }
fn probe() -> Task<Task<Channel<$N>>> {
    let task: Task<Channel<$N>> = @local spawn async {
        ret Channel::<$N>::new(1:uint);
    };
    return blocking { ret relay(task); };
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
	const invalid = "@shard_movable\ntype Payload = { ch: Channel<$N> };"
	const invalidCopy = "@copy\n" + invalid
	const nested = "@copy @shard_movable\ntype Bad = { ch: Channel<$N> };\n"
	rows := []struct {
		name, declarations, payload string
		owned, blocking             bool
	}{
		{"declaration_on_owned", invalid, "Payload", true, false},
		{"declaration_on_copy", invalidCopy, "Payload", false, false},
		{"declaration_blocking_owned", invalid, "Payload", true, true},
		{"declaration_nested_on_copy", nested + "@copy\ntype Payload = { bad: Bad };", "Payload", false, false},
		{"declaration_nested_blocking_owned", nested + "type Payload = { bad: Bad };", "Payload", true, true},
		{"declaration_handle_payload_on", invalidCopy, "Channel<Payload>", false, false},
		{"declaration_tag_payload_on", nested + `
tag Held(Bad);
tag Empty();
@copy
type Payload = Held(Bad) | Empty();`, "Payload", false, false},
		{"declaration_nonculprit_sibling_on", `
@copy @shard_movable
type Bad = { ch: Channel<int64> };
@copy
type Payload = { first: Channel<$N>, bad: Bad };`, "Payload", false, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			src := countedDeclarationCaptureSource(row.declarations, row.payload, row.owned, row.blocking)
			requireCountedDeclarationRefusal(t, src)
		})
	}
}

func countedDeclarationCaptureSource(declarations, payload string, owned, blocking bool) string {
	argument := "value"
	if owned {
		payload = "own " + payload
		argument = "own value"
	}
	use := fmt.Sprintf("\nfn use(value: %s) -> bool { return true; }\n", payload)
	if blocking {
		return declarations + use + fmt.Sprintf(`
async fn probe(value: %s) -> bool {
    let job: Task<bool> = blocking { ret use(%s); };
    return compare job.await() { Success(v) => v; Cancelled() => false; };
}`, payload, argument)
	}
	return declarations + use + fmt.Sprintf(`
async fn probe(dst: Placement, value: %s) -> TaskResult<bool> {
    return on dst { ret use(%s); };
}`, payload, argument)
}

func requireCountedDeclarationRefusal(t *testing.T, src string) {
	t.Helper()
	res, err := countedDiagnosticCompile(t, strings.ReplaceAll(src, "$N", "int"))
	if err == nil || res.MIR != nil || res.Diagnose == nil || res.Diagnose.Bag == nil {
		t.Fatalf("invalid shard-movable declaration did not stop before MIR: %v", err)
	}
	seen := map[diag.Code]bool{}
	var countedMessage string
	for _, item := range res.Diagnose.Bag.Items() {
		if item.Severity != diag.SevError {
			continue
		}
		if item.Code != diag.SemaCrossNotShardMovable && item.Code != diag.SemaShardMovableField {
			t.Fatalf("unrelated declaration diagnostic: %s: %s", item.Code.ID(), item.Message)
		}
		seen[item.Code] = true
		if item.Code == diag.SemaCrossNotShardMovable {
			countedMessage += item.Message + "\n"
		}
	}
	if !seen[diag.SemaCrossNotShardMovable] || !seen[diag.SemaShardMovableField] {
		t.Fatalf("declaration control missed one of its independent refusals: %v", seen)
	}
	// Prove the width-only replacement reaches the same declaration error even
	// on a baseline that still emits the false advice asserted below.
	requireCountedDiagnostic(t, strings.ReplaceAll(src, "$N", "int64"), diag.SemaShardMovableField)
	if strings.Contains(countedMessage, "If fixed precision is sufficient") {
		t.Fatalf("width advice would leave the declared field non-shard-movable: %s", countedMessage)
	}
}
