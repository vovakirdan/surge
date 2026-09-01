package asyncrt

// Current returns the ID of the task being polled.
func (e *Executor[P]) Current() TaskID {
	if e == nil {
		return 0
	}
	return e.current
}

// SetCurrent sets the currently running task ID.
func (e *Executor[P]) SetCurrent(id TaskID) {
	if e == nil {
		return
	}
	e.current = id
}

// Task returns a task by ID.
func (e *Executor[P]) Task(id TaskID) *Task[P] {
	if e == nil {
		return nil
	}
	return e.tasks[id]
}
