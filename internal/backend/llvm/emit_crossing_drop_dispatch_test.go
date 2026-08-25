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
	// (state_drop_id == body_id, then the body's result TYPE id, then poll ==
	// body_id). The result slot is a type now, so this row pins the state id
	// and leaves the type to the sibling test that is about it.
	wantIDs := regexp.MustCompile(fmt.Sprintf(", i64 %d, i64 \\d+, i64 %d, ptr %%", bodyID, bodyID))
	if !wantIDs.MatchString(ir) {
		t.Fatalf("state-shipping publish must pass drop id == body id (%d):\n%s", bodyID, ir)
	}
	if !strings.Contains(ir, "call i32 @rt_remote_spawn_publish_placement(i64 0, i64 0, i64 0,") {
		t.Fatalf("retry publish must keep (id=0, state=null):\n%s", ir)
	}

	dispatch := findLLVMFuncBody(t, ir, "__surge_drop_call")
	armLabel := fmt.Sprintf("i64 %d, label %%drop.%d", bodyID, bodyID)
	if !strings.Contains(dispatch, armLabel) {
		t.Fatalf("__surge_drop_call switch missing the registered arm %q:\n%s", armLabel, dispatch)
	}
	// A shipped state lives in an allocation the runtime owns, so abandoning it
	// has two halves: release what the state's members own, and give the
	// allocation back. The arm reaches the release entry point, which does both
	// in that order — the recursive glue first, then the free.
	//
	// The arm used to call the recursive glue directly, because the glue also
	// freed the state: a composite WAS its allocation, so dropping it and
	// reclaiming it were one act. They are two now, and calling only the glue
	// here would leak the state of every abandoned spawn.
	armBody := regexp.MustCompile(fmt.Sprintf(
		`drop\.%d:\n  call void @(release\.type\d+)\(ptr %%state\)\n  ret void`, bodyID))
	arm := armBody.FindStringSubmatch(dispatch)
	if arm == nil {
		t.Fatalf("dispatch arm must release the runtime-owned state:\n%s", dispatch)
	}
	release := findLLVMFuncBody(t, ir, arm[1])
	if !regexp.MustCompile(`call void @drop\.type\d+\(ptr %p\)`).MatchString(release) {
		t.Fatalf("the release entry point must call the state's recursive glue:\n%s", release)
	}
	if !strings.Contains(release, "call void @rt_free(ptr %p,") {
		t.Fatalf("the release entry point must free the runtime-owned allocation:\n%s", release)
	}
	if !strings.Contains(dispatch, "drop_default:") {
		t.Fatalf("dispatch must keep the default panic arm for unregistered ids:\n%s", dispatch)
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

	// (i64 placement, i64 state_drop, i64 result_type, i64 poll, ptr state, ...)
	re := regexp.MustCompile(`call i32 @rt_immediate_on_execute\(i64 %[^,]+, i64 (\d+), i64 \d+, i64 (\d+), ptr %`)
	m := re.FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("missing state-shipping immediate-on execute call:\n%s", ir)
	}
	if m[1] == "0" || m[1] != m[2] {
		t.Fatalf("immediate-on drop id must equal the body id (got drop=%s body=%s)", m[1], m[2])
	}
	if !strings.Contains(ir, "call i32 @rt_immediate_on_execute(i64 0, i64 0, i64 0,") {
		t.Fatalf("immediate-on retry must keep (id=0, state=null):\n%s", ir)
	}
	dispatch := findLLVMFuncBody(t, ir, "__surge_drop_call")
	if !strings.Contains(dispatch, fmt.Sprintf("label %%drop.%s", m[1])) {
		t.Fatalf("dispatch missing the immediate-on arm %s:\n%s", m[1], dispatch)
	}
}
