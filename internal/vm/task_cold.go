package vm

import "surge/internal/asyncrt"

// discardColdTask ends a task nothing started once the last entitlement to it
// is gone (RV2-DEBT-370).
//
// A call of an `async fn` creates its task cold (handleTaskCreate,
// asyncrt.Executor.Create): a member of its scope and a child of its creator,
// in no ready queue. When the cohort empties before a spawn, an await, a join
// or a cancel published it, nothing will ever run it, so the release that
// emptied the cohort ends it here: the executor retires its membership without
// a fail-fast, and its start state -- every value it captured and every
// storage pin it holds on its creator's frame -- is dropped without the body
// being entered. That is what keeps a dropped `worker(&l)` from reading `l`
// after the frame that owned it is gone.
//
// It answers false, and changes nothing, for a task that is not cold: that
// task runs, and its result is released by the ordinary rule.
func (vm *VM) discardColdTask(id asyncrt.TaskID) bool {
	if vm == nil || vm.Async == nil {
		return false
	}
	task := vm.Async.DiscardCold(id)
	if task == nil {
		return false
	}
	delete(vm.taskCohorts, id)
	vm.releaseTaskState(task)
	vm.destroyAsyncTaskOwners(id)
	return true
}
