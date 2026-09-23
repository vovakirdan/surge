package asyncrt

// Cold creation (RV2-DEBT-370).
//
// A call of an `async fn` and an `async { }` block create a task that does not
// run until `spawn` starts it or something awaits it
// (docs/RUNTIME_MODEL_EXPLAINED.ru.md 6.3). Create records the task exactly as
// a spawned one is recorded -- a child of its creator, a member of the scope
// that created it, decided once and never re-derived (docs/RUNTIME_V2.md
// section 9) -- and leaves it out of the ready queue. The first enqueue, which
// every Wake makes, publishes it: `spawn`, an await, a timeout or select arm, a
// cancel, and the join of its scope. DiscardCold ends a task whose last handle
// goes first. The native runtime keeps the same three states in the task's
// publication word (runtime/native/rt_task_cold.c).

// Create registers a user task that nothing has made runnable yet.
func (e *Executor[P]) Create(pollFuncID int64, state TaskState) TaskID {
	if e == nil {
		return 0
	}
	if e.nextID == 0 {
		e.nextID = 1
	}
	id := e.nextID
	e.nextID++

	task := &Task[P]{
		ID:         id,
		PollFuncID: pollFuncID,
		State:      state,
		Status:     TaskReady,
		Kind:       TaskKindUser,
		Cold:       true,
	}
	if e.tasks == nil {
		e.tasks = make(map[TaskID]*Task[P])
	}
	e.tasks[id] = task
	if e.current != 0 {
		if parent := e.tasks[e.current]; parent != nil {
			parent.Children = append(parent.Children, id)
			e.registerCreatedScopeMember(parent, task)
		}
	}
	return id
}

// DiscardCold ends a task whose last handle went while it was still cold, and
// returns it so the caller can release the start state it holds: nothing will
// run it, so nothing else would. The task is retired from its scope with no
// result kind -- a task that never ran committed nothing -- and answers done to
// anything that still names it. It answers nil,
// and changes nothing, for a task that is not cold.
func (e *Executor[P]) DiscardCold(id TaskID) *Task[P] {
	if e == nil {
		return nil
	}
	task := e.tasks[id]
	if task == nil || !task.Cold || task.Status == TaskDone {
		return nil
	}
	// The native runtime commits NONE, a kind the VM does not have. ResultKind is
	// left at its zero value, TaskResultSuccess with no ResultValue, and must
	// never be read for a discarded task: every reader of a result goes through
	// a handle (pollTask, taskResultFromTask), and DiscardCold is reached only
	// when the last one is gone. The scope retires the member here, not through
	// MarkDone, so no fail-fast is raised (RUNTIME_V2 section 9, "Only a member
	// raises fail-fast").
	task.Cold = false
	task.Status = TaskDone
	e.unregisterScopeChild(task)
	return task
}

// publishColdChildren is the join's half: a member created cold whose handle
// outlived the body -- returned, or stored where the body could not drop it --
// is joined the way an await joins it, so the scope never waits on a task that
// nothing will start.
func (e *Executor[P]) publishColdChildren(scope *Scope) {
	if e == nil || scope == nil {
		return
	}
	for _, child := range scope.Children {
		if task := e.tasks[child]; task != nil && task.Cold {
			e.Wake(child)
		}
	}
}
