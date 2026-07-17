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
// __surge_drop_call switch routes the id to the state's recursive glue
// while unregistered ids keep the panic arm.
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
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	bodyID := findSpawnOnPollFunc(t, mirMod).ID
	firstAttempt := fmt.Sprintf("call i32 @rt_remote_spawn_publish_placement(i64 %%") // placement reg
	if !strings.Contains(ir, firstAttempt) {
		t.Fatalf("missing state-shipping publish call:\n%s", ir)
	}
	wantIDs := fmt.Sprintf(", i64 %d, i64 %d, ptr %%", bodyID, bodyID)
	if !strings.Contains(ir, wantIDs) {
		t.Fatalf("state-shipping publish must pass drop id == body id (%d):\n%s", bodyID, ir)
	}
	if !strings.Contains(ir, "call i32 @rt_remote_spawn_publish_placement(i64 0, i64 0,") {
		t.Fatalf("retry publish must keep (id=0, state=null):\n%s", ir)
	}

	dispatch := findLLVMFuncBody(t, ir, "__surge_drop_call")
	armLabel := fmt.Sprintf("i64 %d, label %%drop.%d", bodyID, bodyID)
	if !strings.Contains(dispatch, armLabel) {
		t.Fatalf("__surge_drop_call switch missing the registered arm %q:\n%s", armLabel, dispatch)
	}
	armBody := regexp.MustCompile(fmt.Sprintf(`drop\.%d:\n  call void @drop\.type\d+\(ptr %%state\)\n  ret void`, bodyID))
	if !armBody.MatchString(dispatch) {
		t.Fatalf("dispatch arm must call the state's recursive glue:\n%s", dispatch)
	}
	if !strings.Contains(dispatch, "drop_default:") {
		t.Fatalf("dispatch must keep the default panic arm for unregistered ids:\n%s", dispatch)
	}
}

// The placement `on` form ships its state under the same contract.
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
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	re := regexp.MustCompile(`call i32 @rt_immediate_on_execute\(i64 %[^,]+, i64 (\d+), i64 (\d+), ptr %`)
	m := re.FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("missing state-shipping immediate-on execute call:\n%s", ir)
	}
	if m[1] == "0" || m[1] != m[2] {
		t.Fatalf("immediate-on drop id must equal the body id (got drop=%s body=%s)", m[1], m[2])
	}
	if !strings.Contains(ir, "call i32 @rt_immediate_on_execute(i64 0, i64 0,") {
		t.Fatalf("immediate-on retry must keep (id=0, state=null):\n%s", ir)
	}
	dispatch := findLLVMFuncBody(t, ir, "__surge_drop_call")
	if !strings.Contains(dispatch, fmt.Sprintf("label %%drop.%s", m[1])) {
		t.Fatalf("dispatch missing the immediate-on arm %s:\n%s", m[1], dispatch)
	}
}
