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
// result. LIVE entitlements and CLAIMED ones are tracked. The clone-reader and
// move-waiter states of the same model belong to this record and go here when
// a taker learns to tell a duplication from the last consumer's move; the
// fields are deliberately named for the model rather than for the count so
// that adding them does not rename what is already here.
type taskCohort struct {
	// live counts entitlements that exist and have not been given up. On the
	// VM one entitlement is one OKResource object: `taskValue` allocates a
	// fresh object per spawn/checkpoint/sleep, and `Task<T>.clone()` allocates
	// another rather than aliasing the first, so counting objects counts
	// entitlements exactly.
	live int32
	// claimed counts askers that hold no handle and have nevertheless
	// committed to consuming the result. `timeout(t, ms)` is the one the model
	// already describes and the VM already had: it builds a runtime task no
	// name in source ever holds, and it is the only thing that will ever come
	// for that task's result. A claim is how such an asker is counted without
	// inventing a handle for it — which was tried and detonated, because a
	// handle also has to be freed by something, and nothing owns this one.
	claimed int32
}

// canStillClaim reports whether anything may still consume the result.
//
// The model states the drop rule as a conjunction — no live entitlements, no
// claimed ones, no clone readers, no move waiter — and this is the half of it
// the VM can answer today. Every release path asks THIS rather than reading a
// count, so a state added later is honoured everywhere at once.
func (c *taskCohort) canStillClaim() bool {
	return c != nil && (c.live > 0 || c.claimed > 0)
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
	if !cohort.canStillClaim() {
		vm.releaseUnclaimableResult(id)
	}
}

// taskClaimTaken records an asker that holds no handle.
//
// It is taken when the operation commits — at the moment the runtime task is
// spawned, not when its result is wanted — because between those two moments
// the task completes, and completion is one of the two places that asks
// whether anything can still claim the value.
func (vm *VM) taskClaimTaken(id asyncrt.TaskID) {
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
	cohort.claimed++
}

// taskClaimRetired gives one claim up, once its holder has taken what it came
// for. It is the counterpart of taskHandleReleased and asks the same question.
func (vm *VM) taskClaimRetired(id asyncrt.TaskID) {
	if vm == nil || id == 0 {
		return
	}
	cohort := vm.taskCohorts[id]
	if cohort == nil || cohort.claimed == 0 {
		return
	}
	cohort.claimed--
	if !cohort.canStillClaim() {
		vm.releaseUnclaimableResult(id)
	}
}

// taskHasOtherAskers reports whether anything BESIDES the asker being served
// right now can still consume the result.
//
// This is the question the reserved final move turns on. The asker calling it
// is still counted — its handle is alive in a local, or its claim is
// outstanding — so "somebody else" means a count above one. Below that, no
// further entitlement can appear: a clone may only be taken from a live handle,
// and the one live handle there is is the one being consumed.
//
// The model reserves the final move by parking the last waiter until the clone
// readers retire. There are no clone readers to wait for here and there never
// will be: a duplication on the VM is `cloneValueComposite`, which runs to
// completion on the one thread that asked for it and cannot park. So the
// reservation and the wait collapse into this single question, asked at the
// moment of the take.
func (vm *VM) taskHasOtherAskers(id asyncrt.TaskID) bool {
	if vm == nil || id == 0 {
		return false
	}
	cohort := vm.taskCohorts[id]
	if cohort == nil {
		// No cohort means nothing is counted against this result, so the asker
		// in hand is the only one there can be.
		return false
	}
	return cohort.live+cohort.claimed > 1
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
	return cohort != nil && !cohort.canStillClaim()
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
	task.ResultValue = asyncPayload{}
	delete(vm.taskCohorts, id)
	vm.dropAsyncPayload(result)
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
