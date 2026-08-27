//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// RV2-DEBT-263 keyed by the SHAPE of the code, for the two claims a behaviour
// row cannot make on its own.
//
// The first is the protocol. The fix's whole argument is that a cancel and a
// completion each move ONE word with ONE read-modify-write, so exactly one of
// them wins. The shape this replaced -- a store-then-load on one side paired
// with a load-then-store on the other -- looks like a memory-ordering fix, is
// spelled with the same words, and does not linearise anything: it leaves a
// window from the flag read to the DONE store with the whole of the completion
// inside it. A future edit that "simplifies" the CAS back into a load and a
// store would reopen exactly that, and no timing-based row could be relied on
// to notice. So the two transitions are pinned here by name.
func TestRuntimeV2LifecycleStaticCancelGateOneRMWPerSide(t *testing.T) {
	cancel := lifecycleFindFunctionBody(t, "cancel_task")
	if !strings.Contains(cancel, "task_cancel_gate_request(task)") {
		t.Fatalf("cancel_task must claim the gate with the compare-and-swap, not a store:\n%s", cancel)
	}
	if strings.Contains(cancel, "atomic_store_explicit(&task->cancelled") {
		t.Fatalf("cancel_task must not write the gate word directly; the CAS is the whole protocol:\n%s", cancel)
	}
	markDone := lifecycleFindFunctionBody(t, "mark_done")
	if !strings.Contains(markDone, "task_cancel_gate_seal(task)") {
		t.Fatalf("mark_done must seal the gate with the compare-and-swap:\n%s", markDone)
	}
	if strings.Contains(markDone, "atomic_load_explicit(&task->cancelled") {
		t.Fatalf("mark_done must decide by sealing, not by reading the gate word:\n%s", markDone)
	}
	// Both transitions are compare-and-swaps out of the SAME open state, which
	// is what makes "both sides believed they won" unrepresentable rather than
	// merely unlikely.
	source := lifecycleReadNativeFile(t, "rt_task_complete.c")
	for _, want := range []string{
		"uint8_t open = RT_TASK_CANCEL_OPEN;",
		"RT_TASK_CANCEL_REQUESTED",
		"RT_TASK_CANCEL_SEALED",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("rt_task_complete.c no longer spells the cancel gate's %q", want)
		}
	}
	if strings.Count(source, "atomic_compare_exchange_strong_explicit(&task->cancelled") != 2 {
		t.Fatalf("the cancel gate must have exactly two claimants, each with one CAS:\n%s", source)
	}
}

// The second is the reader RV2-DEBT-263 nearly left behind. A completion that
// refuses its value empties the slot before TASK_DONE is published
// (rt_task_result_refuse), so a task answering Cancelled should never reach the
// far reply path with a ready result cell -- but rt_remote_task_pin_result used
// to decide purely on "is there a value here", and the holder of the capability
// it mints moves the value out UNCONDITIONALLY (finish_retry) while the
// generated Cancelled arm never reads the storage it lands in
// (emit_crossing_far_task.go). Asking the kind FIRST is what makes that
// combination unreachable from either direction, and it is the rule the local
// path (rt_far_task_take_result) has always followed.
func TestRuntimeV2LifecycleStaticFarReplyNamesResultOnlyForSuccess(t *testing.T) {
	pin := lifecycleFindFunctionBody(t, "rt_remote_task_pin_result")
	if !strings.Contains(pin, "rt_remote_task_result_kind(task) != 1") {
		t.Fatalf("rt_remote_task_pin_result must ask the kind before it names a slot:\n%s", pin)
	}
	take := lifecycleFindFunctionBody(t, "rt_far_task_take_result")
	if !strings.Contains(take, "rt_remote_task_result_kind(producer)") {
		t.Fatalf("rt_far_task_take_result must keep asking the kind first:\n%s", take)
	}
	// And the invariant the guard is second in line behind: mark_done empties
	// the slot at the commit, so no reader is offered a value by a task that
	// answers Cancelled.
	markDone := lifecycleFindFunctionBody(t, "mark_done")
	if !strings.Contains(markDone, "rt_task_result_refuse(ex, task)") {
		t.Fatalf("mark_done must empty the slot of a refused result before publishing TASK_DONE:\n%s", markDone)
	}
}
