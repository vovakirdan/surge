package asyncrt

// Executor runs async tasks on a single thread; deterministic by default.
// Future iterations will add scheduler fuzzing and structured cancel/join policy.
type Executor struct {
	cfg      Config
	nextID   TaskID
	ready    []TaskID
	readySet map[TaskID]struct{}
	tasks    map[TaskID]*Task
	current  TaskID
}

// TaskID identifies a spawned task.
type TaskID uint64

// TaskStatus describes task scheduling state.
type TaskStatus uint8

const (
	TaskReady TaskStatus = iota
	TaskRunning
	TaskWaiting
	TaskDone
)

// TaskKind identifies special runtime tasks.
type TaskKind uint8

const (
	TaskKindUser TaskKind = iota
	TaskKindCheckpoint
)

// Task stores executor-visible task state.
type Task struct {
	ID               TaskID
	PollFuncID       int64
	State            any
	Result           any
	Status           TaskStatus
	Kind             TaskKind
	JoinWaiters      []TaskID
	checkpointPolled bool
}

// Config configures executor behavior.
type Config struct {
	Deterministic bool
	Fuzz          bool
	Seed          uint64
}

// NewExecutor constructs an executor with the provided configuration.
func NewExecutor(cfg Config) *Executor {
	return &Executor{
		cfg:      cfg,
		nextID:   1,
		readySet: make(map[TaskID]struct{}),
		tasks:    make(map[TaskID]*Task),
	}
}

// Current returns the ID of the task being polled.
func (e *Executor) Current() TaskID {
	if e == nil {
		return 0
	}
	return e.current
}

// SetCurrent sets the currently running task ID.
func (e *Executor) SetCurrent(id TaskID) {
	if e == nil {
		return
	}
	e.current = id
}

// Task returns a task by ID.
func (e *Executor) Task(id TaskID) *Task {
	if e == nil {
		return nil
	}
	return e.tasks[id]
}

// Spawn registers a task and enqueues it for execution.
func (e *Executor) Spawn(pollFuncID int64, state any) TaskID {
	if e == nil {
		return 0
	}
	if e.nextID == 0 {
		e.nextID = 1
	}
	id := e.nextID
	e.nextID++

	task := &Task{
		ID:         id,
		PollFuncID: pollFuncID,
		State:      state,
		Status:     TaskReady,
		Kind:       TaskKindUser,
	}
	if e.tasks == nil {
		e.tasks = make(map[TaskID]*Task)
	}
	e.tasks[id] = task
	e.enqueue(id)
	return id
}

// SpawnCheckpoint registers a checkpoint task and enqueues it.
func (e *Executor) SpawnCheckpoint() TaskID {
	if e == nil {
		return 0
	}
	if e.nextID == 0 {
		e.nextID = 1
	}
	id := e.nextID
	e.nextID++

	task := &Task{
		ID:     id,
		Status: TaskReady,
		Kind:   TaskKindCheckpoint,
	}
	if e.tasks == nil {
		e.tasks = make(map[TaskID]*Task)
	}
	e.tasks[id] = task
	e.enqueue(id)
	return id
}

// CheckpointPolled reports whether a checkpoint task has yielded once.
func (t *Task) CheckpointPolled() bool {
	if t == nil {
		return false
	}
	return t.checkpointPolled
}

// MarkCheckpointPolled marks a checkpoint task as having yielded once.
func (t *Task) MarkCheckpointPolled() {
	if t == nil {
		return
	}
	t.checkpointPolled = true
}

// Dequeue pops the next ready task ID.
func (e *Executor) Dequeue() (TaskID, bool) {
	if e == nil || len(e.ready) == 0 {
		return 0, false
	}
	id := e.ready[0]
	e.ready = e.ready[1:]
	delete(e.readySet, id)
	return id, true
}

// Wake enqueues a task if it is not done.
func (e *Executor) Wake(id TaskID) {
	if e == nil {
		return
	}
	task := e.tasks[id]
	if task == nil || task.Status == TaskDone {
		return
	}
	e.enqueue(id)
}

// MarkDone marks a task as completed and wakes join waiters.
func (e *Executor) MarkDone(id TaskID, result any) {
	if e == nil {
		return
	}
	task := e.tasks[id]
	if task == nil {
		return
	}
	task.Result = result
	task.Status = TaskDone
	for _, waiter := range task.JoinWaiters {
		e.Wake(waiter)
	}
	task.JoinWaiters = nil
}

// Join waits for a task to complete and returns its result if ready.
func (e *Executor) Join(waiter, target TaskID) (ready bool, result any) {
	if e == nil {
		return false, nil
	}
	task := e.tasks[target]
	if task == nil {
		return false, nil
	}
	if task.Status == TaskDone {
		return true, task.Result
	}
	if waiter != 0 {
		task.JoinWaiters = append(task.JoinWaiters, waiter)
		if w := e.tasks[waiter]; w != nil && w.Status != TaskDone {
			w.Status = TaskWaiting
		}
	}
	return false, nil
}

func (e *Executor) enqueue(id TaskID) {
	if e == nil {
		return
	}
	if e.readySet == nil {
		e.readySet = make(map[TaskID]struct{})
	}
	if _, ok := e.readySet[id]; ok {
		return
	}
	e.ready = append(e.ready, id)
	e.readySet[id] = struct{}{}
	if task := e.tasks[id]; task != nil && task.Status != TaskDone {
		task.Status = TaskReady
	}
}

// DrainTasks returns all tasks and resets executor queues.
func (e *Executor) DrainTasks() []*Task {
	if e == nil {
		return nil
	}
	if len(e.tasks) == 0 {
		e.ready = nil
		if e.readySet != nil {
			clear(e.readySet)
		}
		e.current = 0
		return nil
	}
	tasks := make([]*Task, 0, len(e.tasks))
	for _, task := range e.tasks {
		tasks = append(tasks, task)
	}
	e.tasks = make(map[TaskID]*Task)
	e.ready = nil
	if e.readySet != nil {
		clear(e.readySet)
	}
	e.current = 0
	return tasks
}
