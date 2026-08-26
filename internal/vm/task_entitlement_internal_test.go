package vm

import (
	"testing"

	"surge/internal/asyncrt"
	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

// The reserved final move, asked where it is a DECISION rather than an answer.
//
// It cannot be asked as an answer. Every asker receives an independent value
// either way — a duplication and a move are indistinguishable to the program
// that reads the result — and the canonical value's lifetime is already minimal
// after RV2-DEBT-167, because the cohort releases it the moment the last handle
// dies. What the move changes is the COST: for a closed cohort of `E`
// entitlements the model states exactly `E-1` duplications and one move, and
// on the VM one duplication of a heap-bearing result is one refcount operation
// against a per-iteration background of roughly two hundred. Pinning that
// absolute would be a test of the string machinery, not of this rule.
//
// So the rule is asked directly: does the LAST asker leave the slot empty.
func newTaskResultFixture(t *testing.T) (*VM, types.TypeID) {
	t.Helper()
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()
	str := interner.Builtins().String
	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{str})
	if err != nil {
		t.Fatalf("freezing the fixture layouts must succeed: %v", err)
	}
	machine := New(&mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}}, nil, nil, interner, nil)
	return machine, str
}

func TestTaskResultIsMovedByTheLastAskerAndDuplicatedForEveryEarlierOne(t *testing.T) {
	machine, str := newTaskResultFixture(t)

	handle := machine.Heap.AllocString(str, "the canonical result")
	task := &asyncrt.Task[Value]{ID: 7, ResultValue: MakeHandleString(handle, str)}

	// A cohort of two: the handle the task was spawned with, and one clone.
	machine.taskHandleCreated(task.ID)
	machine.taskHandleCreated(task.ID)

	first, vmErr := machine.takeCanonicalResult(task)
	if vmErr != nil {
		t.Fatalf("the first asker must be served: %v", vmErr)
	}
	if task.ResultValue.Kind == VKInvalid {
		t.Fatalf("the first of two askers emptied the slot; the second has nothing to take")
	}
	if got := machine.Heap.Get(handle).RefCount; got != 2 {
		t.Fatalf("a duplication for an earlier asker left refcount %d, want 2", got)
	}

	// That asker's handle dies. One entitlement is left, so the next asker is
	// the last one there can be.
	machine.taskHandleReleased(task.ID)
	if task.ResultValue.Kind == VKInvalid {
		t.Fatalf("the result was released while an entitlement could still claim it")
	}

	second, vmErr := machine.takeCanonicalResult(task)
	if vmErr != nil {
		t.Fatalf("the last asker must be served: %v", vmErr)
	}
	if task.ResultValue.Kind != VKInvalid {
		t.Fatalf("the last asker DUPLICATED instead of moving: the slot still holds %v", task.ResultValue.Kind)
	}
	if got := machine.Heap.Get(handle).RefCount; got != 2 {
		t.Fatalf("the last asker's take changed refcount to %d, want 2 — it moved, so it counted nothing", got)
	}

	// Both askers hold a value of their own and neither is the empty one.
	if first.Kind != VKHandleString || second.Kind != VKHandleString {
		t.Fatalf("askers received %v and %v, want two strings", first.Kind, second.Kind)
	}

	// And the cohort's own release finds nothing left to do, which is what
	// makes the move exactly once rather than once plus a stale second owner.
	machine.taskHandleReleased(task.ID)
	if got := machine.Heap.Get(handle).RefCount; got != 2 {
		t.Fatalf("emptying the cohort released a value the last asker had moved out; refcount %d, want 2", got)
	}
}
