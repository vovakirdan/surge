package vm

import "surge/internal/asyncrt"

// A task's ENTITLEMENT COHORT: everything that may still consume its one
// canonical result.
//
// The storage model gives a task a single exact-sized result slot and gives
// each handle a separate entitlement to consume it, and it says the canonical
// value "may be dropped only when there are no live/claimed entitlements,
// clone readers, or move waiter". The VM implemented the CLONE half of that
// sentence and never the drop half: every delivery cloned the task's value
// (`taskResultValue`) and nothing ever released what the task itself held. One
// heap object per completed task with a heap-bearing result therefore survived
// until the shutdown drain swept it — a bare `spawn`+`await` retained one, and
// a scalar channel round trip through two tasks retained two.
//
// This census is the missing half, in the smallest form that answers the
// question the drop rule asks: how many entitlements can still consume this
// result. Only the LIVE count is tracked today. The claimed, clone-reader and
// move-waiter states of the same model belong to this record and go here when
// the cohort learns to tell them apart; the fields are deliberately named for
// the model rather than for the count so that adding them does not rename what
// is already here.
type taskCohort struct {
	// live counts entitlements that exist and have not been given up. On the
	// VM one entitlement is one OKResource object: `taskValue` allocates a
	// fresh object per spawn/checkpoint/sleep, and `Task<T>.clone()` allocates
	// another rather than aliasing the first, so counting objects counts
	// entitlements exactly.
	live int32
}

// taskHandleCreated records one new entitlement.
//
// Every caller is a site that hands a task word to something that can later
// consume the result: the four `taskValue` sites, and the runtime-created
// `timeout` task whose single entitlement belongs to the awaiting operation
// rather than to a name in source.
func (vm *VM) taskHandleCreated(id asyncrt.TaskID) {
	if vm == nil || id == 0 {
		return
	}
	if vm.taskCohorts == nil {
		vm.taskCohorts = make(map[asyncrt.TaskID]*taskCohort, 16)
	}
	cohort := vm.taskCohorts[id]
	if cohort == nil {
		cohort = &taskCohort{}
		vm.taskCohorts[id] = cohort
	}
	cohort.live++
}

// taskHandleReleased gives up one entitlement, and releases the canonical
// result if that was the last one and the result is already there.
//
// A word that names no cohort is not this census's to answer for: a resource
// object can carry a channel or a file, and a task that never had an
// entitlement recorded keeps whatever ownership it had before.
func (vm *VM) taskHandleReleased(id asyncrt.TaskID) {
	if vm == nil || id == 0 {
		return
	}
	cohort := vm.taskCohorts[id]
	if cohort == nil || cohort.live == 0 {
		return
	}
	cohort.live--
	if cohort.live == 0 {
		vm.releaseUnclaimableResult(id)
	}
}

// taskCohortEmpty reports whether a task has a cohort and nothing in it can
// still consume the result.
//
// A task whose cohort was already settled has no record at all, so this is
// false for it: the question is asked by the shutdown check, and there the
// answer must distinguish "nobody can claim this and it is still here" from
// "this was never governed by a cohort".
func (vm *VM) taskCohortEmpty(id asyncrt.TaskID) bool {
	if vm == nil || id == 0 {
		return false
	}
	cohort := vm.taskCohorts[id]
	return cohort != nil && cohort.live == 0
}

// taskCompleted is the same rule seen from the other side: the cohort emptied
// before the task published its result, so completion is the moment the value
// becomes unclaimable. A scope-cancelled child whose parent frame is already
// gone arrives here rather than through a handle release.
func (vm *VM) taskCompleted(id asyncrt.TaskID) {
	if vm == nil || !vm.taskCohortEmpty(id) {
		return
	}
	vm.releaseUnclaimableResult(id)
}

// releaseUnclaimableResult drops a completed task's canonical result exactly
// once and forgets the cohort.
//
// The slot is emptied BEFORE the release runs: dropping a composite walks its
// members and can free further objects, and none of that work may find a
// second route back to a value this call already owns.
func (vm *VM) releaseUnclaimableResult(id asyncrt.TaskID) {
	if vm == nil || vm.Async == nil {
		return
	}
	task := vm.Async.Task(id)
	if task == nil {
		delete(vm.taskCohorts, id)
		return
	}
	if task.Status != asyncrt.TaskDone {
		// The result is not there yet. The cohort record stays so that
		// completion can ask this same question again.
		return
	}
	result := task.ResultValue
	task.ResultValue = Value{}
	delete(vm.taskCohorts, id)
	vm.dropValue(result)
}

// valueHoldsStorage reports whether a value owns something a release would
// have to reclaim. It is the shutdown check's predicate, not a general one:
// what it must not do is count a plain integer or a `nothing` as a leak.
func valueHoldsStorage(v Value) bool {
	if v.Kind == VKComposite {
		return true
	}
	return v.IsHeap() && v.H != 0
}
