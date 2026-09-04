package llvm

import (
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/types"
	"surge/internal/valueops"
)

// crossMoveBodiesFor emits the crossing bodies for a synthetic shard-movable
// (and not cross-clonable) entry and returns the text. The entry carries
// physical facts directly because the bodies read nothing else: a move's plan
// IS its layout. A bare-scalar entry has no members, so the clone demand it
// would raise stays empty and only the plan and move apply are written.
func crossMoveBodiesFor(t *testing.T, id types.TypeID, size, align uint64) string {
	t.Helper()
	emitter := &Emitter{}
	entry := &valueops.Entry{
		Type:  id,
		Facts: layout.PhysicalFacts{Size: size, Align: align},
	}
	emitter.emitCrossBodies(entry, valueops.FlagShardMovable)
	if err := emitter.emitCrossGlue(); err != nil {
		t.Fatalf("emit cross glue: %v", err)
	}
	return emitter.buf.String()
}

// The plan a move writes is exactly its own layout, and it charges no sidecars.
//
// This is the whole of why the move half can land before the clone half: a
// transfer allocates nothing, so `sidecar_bytes` and `sidecar_count` are zero,
// the apply's allowances start and end at zero, and no member walk is needed.
// If a later change makes a move charge a sidecar, this row goes red and the
// claim above stops being true silently.
func TestCrossMovePlanChargesNoSidecars(t *testing.T) {
	ir := crossMoveBodiesFor(t, 7, 24, 8)

	for _, want := range []string{
		"define internal zeroext i32 @plan_cross.type7(ptr %src, i8 zeroext %mode, ptr %out)",
		// ops, then the caller's own mode (one body serves both modes), then the
		// layout the plan describes.
		"store ptr @__surge_value_ops_type7",
		"store i8 %mode, ptr %p1",
		"store i64 24, ptr %p4", // payload_bytes
		"store i64 8, ptr %p5",  // payload_align
		"store i64 0, ptr %p6",  // sidecar_bytes
		"store i64 24, ptr %p7", // total_bytes: the payload alone
		"store i64 0, ptr %p8",  // sidecar_count
		"ret i32 0",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("plan body is missing %q:\n%s", want, ir)
		}
	}
}

// A mode the descriptor cannot serve does not return a status.
//
// The storage model is explicit that a call outside a descriptor's cross
// capability is a protocol violation by the CALLER, not a recoverable refusal,
// and that answering with a status would assert the call was legal and merely
// declined. A future edit that "helpfully" returns INVALID_STATE here would be
// making that assertion; this row is what stops it.
func TestCrossPlanRefusesAnUnservableModeWithoutReturning(t *testing.T) {
	ir := crossMoveBodiesFor(t, 3, 8, 8)

	if !strings.Contains(ir, "call void @llvm.trap()") {
		t.Fatalf("an unservable mode must not return; got:\n%s", ir)
	}
	if !strings.Contains(ir, "unreachable") {
		t.Fatalf("the trap arm must be terminated by unreachable:\n%s", ir)
	}
	// The only `ret` in the plan body is the OK one on the move arm.
	planBody := ir[:strings.Index(ir, "define internal zeroext i32 @cross_move.")]
	if got := strings.Count(planBody, "ret i32"); got != 1 {
		t.Fatalf("plan body should have exactly one status return, got %d:\n%s", got, planBody)
	}
}

// The apply re-derives the plan and refuses a disagreement before it copies.
//
// The frozen contract requires ops, mode, layout and the exact byte totals to
// agree, and requires both allocator allowances to be zero on success. Each
// check must sit BEFORE the memcpy: a body that copied first and checked after
// would have already written a destination its caller was told stayed empty.
func TestCrossMoveChecksThePlanBeforeItCopies(t *testing.T) {
	ir := crossMoveBodiesFor(t, 11, 16, 8)
	body := ir[strings.Index(ir, "define internal zeroext i32 @cross_move."):]

	copyAt := strings.Index(body, "call void @llvm.memcpy")
	if copyAt < 0 {
		t.Fatalf("the apply must transfer the bytes:\n%s", body)
	}
	for _, check := range []string{
		"icmp eq ptr",                            // ops
		"icmp eq i8",                             // mode
		"%struct.rt_cross_allocator, ptr %alloc", // both allowances
		"i32 0, i32 4",                           // payload_bytes
		"i32 0, i32 5",                           // payload_align
	} {
		at := strings.Index(body, check)
		if at < 0 {
			t.Fatalf("the apply is missing the %q check:\n%s", check, body)
		}
		if at > copyAt {
			t.Fatalf("the %q check sits AFTER the copy; a refused plan would already have written the destination:\n%s",
				check, body)
		}
	}
	if !strings.Contains(body, "mismatch:\n  ret i32 5") {
		t.Fatalf("a disagreement must answer PLAN_MISMATCH:\n%s", body)
	}
}

// Two entries with different facts get two different bodies.
//
// The cross bodies are keyed on the EXACT registry type id rather than the
// resolved one, for the same reason move_init is: two entries can carry
// different physical facts, and the plan a body writes is made of those facts,
// so sharing one body between them would hand a caller the other's layout.
func TestCrossBodiesAreKeyedOnTheExactType(t *testing.T) {
	narrow := crossMoveBodiesFor(t, 1, 8, 8)
	wide := crossMoveBodiesFor(t, 2, 64, 16)

	if !strings.Contains(narrow, "@plan_cross.type1") || !strings.Contains(wide, "@plan_cross.type2") {
		t.Fatalf("each entry must get its own body:\n%s\n%s", narrow, wide)
	}
	if !strings.Contains(narrow, "store i64 8, ptr %p4") {
		t.Fatalf("narrow entry lost its own payload size:\n%s", narrow)
	}
	if !strings.Contains(wide, "store i64 64, ptr %p4") {
		t.Fatalf("wide entry lost its own payload size:\n%s", wide)
	}
}
