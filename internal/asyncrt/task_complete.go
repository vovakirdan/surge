package asyncrt

// Terminal task transitions: completion and cancellation.
//
// The two belong together and are split out of asyncrt.go for the same reason
// the native runtime keeps them in one module (runtime/native/rt_task_complete.c):
// they decide ONE thing between them -- which kind a task commits -- and a
// reader who has only half of that decision in front of them cannot check it.
// The native side needs a memory-ordering protocol to make the decision
// well-defined because a cancel and a completion can run on two threads
// (RV2-DEBT-263); this executor is single-threaded, so the decision is all
// there is here.

// commitKind decides the kind a completion actually commits (RV2-DEBT-263).
//
// A task's answer belongs to the moment it commits, not to whoever carried the
// kind in. A body can run all the way to a value and still have been cancelled
// after its last suspension point -- pollTask hands back a DONE target
// unconditionally and lets the SUSPENSION carry the cancellation, so a body
// whose awaits all resolve from already-DONE children has no suspension left to
// observe a cancel at. `cancel` through a live handle is task-global: before
// committed success it must be observed by every awaited entitlement
// (docs/runtime-v2-epics/23-storage-model-and-typed-carrier-abi.md).
func (e *Executor[P]) commitKind(task *Task[P], kind TaskResultKind) TaskResultKind {
	if kind == TaskResultSuccess && task != nil && task.Cancelled {
		return TaskResultCancelled
	}
	return kind
}

// CommitKindFor reports the kind MarkDone would commit for this task, so a
// caller holding an OWNED payload can ask before it hands it over.
//
// This executor is generic over the payload and cannot destroy one; a value a
// completion refuses has to go back to the lane that knows how (the VM's
// dropValue). Asking first is what keeps that lane from leaking it, and both
// answers come from commitKind so the two cannot drift apart.
func (e *Executor[P]) CommitKindFor(id TaskID, kind TaskResultKind) TaskResultKind {
	if e == nil {
		return kind
	}
	return e.commitKind(e.tasks[id], kind)
}

// MarkDone marks a task as completed and wakes join waiters.
func (e *Executor[P]) MarkDone(id TaskID, kind TaskResultKind, result P) {
	if e == nil {
		return
	}
	task := e.tasks[id]
	if task == nil {
		return
	}
	// RV2-DEBT-263: the kind is decided HERE, at the commit. A refused value is
	// not kept -- a Cancelled task has no result, and holding one would leave a
	// payload no reader may take and no owner will destroy. The caller asks
	// CommitKindFor first when it owns something that needs destroying.
	kind = e.commitKind(task, kind)
	if kind == TaskResultCancelled {
		var none P
		result = none
	}
	task.ResultKind = kind
	task.ResultValue = result
	task.Status = TaskDone
	if key, ok := e.parked[id]; ok {
		e.removeWaiter(key, id)
		delete(e.parked, id)
	}
	if kind == TaskResultCancelled && task.ParentScopeID != 0 {
		if scope := e.scopes[task.ParentScopeID]; scope != nil && scope.Failfast && !scope.FailfastTriggered {
			scope.FailfastTriggered = true
			e.CancelAllChildren(scope.ID)
			if owner := e.tasks[scope.Owner]; owner != nil && owner.Status != TaskDone {
				e.Wake(scope.Owner)
			}
		}
	}
	e.unregisterScopeChild(task)
	e.WakeKeyAll(JoinKey(id))
}

// Cancel marks a task (and its descendants) as cancelled.
func (e *Executor[P]) Cancel(id TaskID) {
	if e == nil {
		return
	}
	e.cancelRecursive(id)
}

func (e *Executor[P]) cancelRecursive(id TaskID) {
	if e == nil {
		return
	}
	task := e.tasks[id]
	if task == nil || task.Status == TaskDone {
		return
	}
	if !task.Cancelled {
		task.Cancelled = true
	}
	if task.Status == TaskWaiting {
		e.Wake(id)
	}
	for _, child := range task.Children {
		e.cancelRecursive(child)
	}
}
