package llvm

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"surge/internal/sema"
)

// A crossing that ships an owned state registers its body FuncID as the
// drop-fn id: the publish call carries (drop_id == body_id) on the
// state-shipping first attempt, the retry keeps (0, null), and the
// __surge_drop_call switch routes the id to the release entry point of the
// state's type while unregistered ids keep the panic arm.
func TestEmitSpawnOnRegistersStateDropFn(t *testing.T) {
	sourceCode := `
@shard_movable
type Job = { id: int, note: string }

fn describe(j: own Job) -> int { return j.id; }

async fn run(dst: Placement) -> far Task<int> {
    let j: own Job = own Job{ id: 4, note: "built" };
    return spawn on dst { ret describe(own j); };
}
`

	mirMod, result := lowerCrossingMIRFromSource(t, sourceCode, sema.CrossingLoweringSpawnOn)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	bodyID := findSpawnOnPollFunc(t, mirMod).ID
	firstAttempt := fmt.Sprintf("call i32 @rt_remote_spawn_publish_placement(i64 %%") // placement reg
	if !strings.Contains(ir, firstAttempt) {
		t.Fatalf("missing state-shipping publish call:\n%s", ir)
	}
	// What the crossing carries for its shipped STATE is that state's TYPE id,
	// the same way it carries the body's result type: the runtime turns the
	// number back into a descriptor and destroys an abandoned state through it
	// -- the members by drop_in_place, then the storage at the width the
	// descriptor states. It used to carry the body's FuncID instead, which
	// named an arm in a dispatch table that no longer exists.
	//
	// (i64 placement, i64 state_type, i64 result_type, i64 poll == body id, ptr state, ...)
	stateSlot := regexp.MustCompile(fmt.Sprintf(
		`@rt_remote_spawn_publish_placement\(i64 %%[^,]+, i64 (\d+), i64 \d+, i64 %d, ptr %%`, bodyID))
	m := stateSlot.FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("state-shipping publish must pass a state type id and poll id %d:\n%s", bodyID, ir)
	}
	if m[1] == "0" {
		t.Fatalf("a shipped state must name a nonzero type id:\n%s", ir)
	}
	if !strings.Contains(ir, "call i32 @rt_remote_spawn_publish_placement(i64 0, i64 0, i64 0,") {
		t.Fatalf("retry publish must keep (id=0, state=null):\n%s", ir)
	}

	assertStateTypeIsReclaimable(t, ir, m[1])
}

// assertStateTypeIsReclaimable pins the whole chain a shipped state's type id
// stands for: the module defines that type's descriptor, its own lookup answers
// for the id, and the descriptor binds the drop the runtime will run.
//
// Without all three the runtime would resolve the id to the opaque word and
// free an abandoned state at eight bytes whatever its real width -- silently,
// on a path that only runs when something else has already gone wrong.
func assertStateTypeIsReclaimable(t *testing.T, ir, typeID string) {
	t.Helper()
	descriptor := fmt.Sprintf("@__surge_value_ops_type%s = constant", typeID)
	if !strings.Contains(ir, descriptor) {
		t.Fatalf("the module must define the descriptor %s the crossing names:\n%s", descriptor, ir)
	}
	lookup := findLLVMFuncBody(t, ir, "__surge_value_ops_for")
	if !strings.Contains(lookup, fmt.Sprintf("i64 %s, label %%value_ops.%s", typeID, typeID)) {
		t.Fatalf("the descriptor lookup must answer for type#%s:\n%s", typeID, lookup)
	}
	if !strings.Contains(ir, fmt.Sprintf("ptr @drop.type%s", typeID)) {
		t.Fatalf("the descriptor for type#%s must bind a drop body:\n%s", typeID, ir)
	}
	// The dispatch table this replaced is gone: nothing routes a numeric id to
	// a generated release any more.
	if strings.Contains(ir, "define void @__surge_drop_call(") {
		t.Fatalf("the numeric state-drop dispatch must not be emitted at all:\n%s", ir)
	}
	if strings.Contains(ir, "define void @__surge_drop_abandoned_state_call(") {
		t.Fatalf("the numeric abandoned-state dispatch must not be emitted at all:\n%s", ir)
	}
}

// RV2-DEBT-053a: a spawn_on whose body returns a heap-carried RESULT
// registers the result payload type as a result-drop-fn id: the publish call
// carries a nonzero result drop id in its dedicated slot, the
// __surge_drop_result_call switch routes that id to a per-type drop wrapper,
// and the wrapper frees the value through its leaf/glue (here rt_string_free).
// (The buildpipeline non-copy reply gate blocks this from compiled source
// today; the MIR-level lowering used here bypasses that gate so the owner-side
// codegen can be proven ahead of the gate opening.)
const publishResultTypePattern = `@rt_remote_spawn_publish_placement\(i64 %[^,]+, i64 \d+, i64 (\d+), i64 \d+, ptr`

func TestEmitSpawnOnRegistersResultDropFn(t *testing.T) {
	sourceCode := `
async fn run(dst: Placement) -> far Task<string> {
    return spawn on dst { ret "shipped-result"; };
}
`

	mirMod, result := lowerCrossingMIRFromSource(t, sourceCode, sema.CrossingLoweringSpawnOn)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %s", err)
	}

	// What the crossing carries for the body's RESULT is its TYPE id, not a
	// drop-glue id. The body's own result slot is bound with the descriptor
	// that id resolves to, and the slot's dispose is what reclaims a result
	// nobody consumed. A crossing cannot carry the descriptor itself -- a
	// pointer does not survive one -- so the number it carries is a type.
	//
	// (i64 placement, i64 state_drop, i64 result_type, i64 poll, ptr state, ...)
	re := regexp.MustCompile(publishResultTypePattern)
	m := re.FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("missing state-shipping publish call with a result-type slot:\n%s", ir)
	}
	if m[1] == "0" {
		t.Fatalf("result type id must be nonzero for a heap string result:\n%s", ir)
	}

	descriptor := fmt.Sprintf("@__surge_value_ops_type%s = constant", m[1])
	if !strings.Contains(ir, descriptor) {
		t.Fatalf("the module must define the descriptor %s the crossing names:\n%s", descriptor, ir)
	}
	// The owner side resolves that id through the module's own lookup, and the
	// descriptor it answers with is what carries the drop.
	lookup := findLLVMFuncBody(t, ir, "__surge_value_ops_for")
	if !strings.Contains(lookup, fmt.Sprintf("i64 %s, label %%value_ops.%s", m[1], m[1])) {
		t.Fatalf("the descriptor lookup must answer for type#%s:\n%s", m[1], lookup)
	}
	if !strings.Contains(ir, fmt.Sprintf("ptr @drop.type%s", m[1])) {
		t.Fatalf("the descriptor for type#%s must bind a drop body:\n%s", m[1], ir)
	}
}

func TestEmitImmediateOnRegistersStateDropFn(t *testing.T) {
	sourceCode := `
@shard_movable
type Job = { id: int, note: string }

fn describe(j: own Job) -> int { return j.id; }

async fn run(dst: Placement) -> int {
    let j: own Job = own Job{ id: 9, note: "built" };
    let got: TaskResult<int> = on dst { ret describe(own j); };
    return compare got { Success(x) => x; Cancelled() => 0; };
}
`

	mirMod, result := lowerCrossingMIRFromSource(t, sourceCode, sema.CrossingLoweringOnPlacement)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	// (i64 placement, i64 state_type, i64 result_type, i64 poll, ptr state, ...)
	re := regexp.MustCompile(`call i32 @rt_immediate_on_execute\(i64 %[^,]+, i64 (\d+), i64 \d+, i64 (\d+), ptr %`)
	m := re.FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("missing state-shipping immediate-on execute call:\n%s", ir)
	}
	if m[1] == "0" {
		t.Fatalf("a shipped state must name a nonzero type id (got %s)", m[1])
	}
	if m[2] == "0" {
		t.Fatalf("the execute must still name its body's poll id (got %s)", m[2])
	}
	if !strings.Contains(ir, "call i32 @rt_immediate_on_execute(i64 0, i64 0, i64 0,") {
		t.Fatalf("immediate-on retry must keep (id=0, state=null):\n%s", ir)
	}
	assertStateTypeIsReclaimable(t, ir, m[1])
}
