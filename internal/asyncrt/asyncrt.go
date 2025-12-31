package asyncrt

import "math/rand"

// Executor runs async tasks on a single thread with a deterministic FIFO scheduler by default.
// Fuzz scheduling is supported for reproducible interleavings.
type Executor struct {
	cfg         Config
	nextID      TaskID
	nextScopeID ScopeID
	ready       []TaskID
	readySet    map[TaskID]struct{}
	tasks       map[TaskID]*Task
	scopes      map[ScopeID]*Scope
	waiters     map[WakerKey][]TaskID
	parked      map[TaskID]WakerKey
	current     TaskID
	rng         *rand.Rand
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

// TaskResultKind describes how a task completed.
type TaskResultKind uint8

const (
	TaskResultSuccess TaskResultKind = iota
	TaskResultCancelled
)

// Task stores executor-visible task state.
type Task struct {
	ID               TaskID
	PollFuncID       int64
	State            any
	ResultKind       TaskResultKind
	ResultValue      any
	Status           TaskStatus
	Kind             TaskKind
	Cancelled        bool
	ScopeID          ScopeID
	ParentScopeID    ScopeID
	Children         []TaskID
	checkpointPolled bool
}

// Config configures executor scheduling behavior.
type Config struct {
	Deterministic bool
	Fuzz          bool
	Seed          uint64
}

// NewExecutor constructs an executor with the provided configuration.
func NewExecutor(cfg Config) *Executor {
	exec := &Executor{
		cfg:         cfg,
		nextID:      1,
		nextScopeID: 1,
		readySet:    make(map[TaskID]struct{}),
		tasks:       make(map[TaskID]*Task),
		scopes:      make(map[ScopeID]*Scope),
	}
	if cfg.Fuzz {
		seed := cfg.Seed
		if seed == 0 {
			seed = 1
		}
		exec.rng = rand.New(rand.NewSource(int64(seed))) //nolint:gosec // deterministic scheduler seed
	}
	return exec
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
	if e.current != 0 {
		if parent := e.tasks[e.current]; parent != nil {
			parent.Children = append(parent.Children, id)
		}
	}
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

// NextReady returns the next ready task according to scheduler policy.
func (e *Executor) NextReady() (TaskID, bool) {
	if e == nil || len(e.ready) == 0 {
		return 0, false
	}
	for len(e.ready) > 0 {
		idx := 0
		if e.cfg.Fuzz {
			if e.rng == nil {
				seed := e.cfg.Seed
				if seed == 0 {
					seed = 1
				}
				e.rng = rand.New(rand.NewSource(int64(seed))) //nolint:gosec // deterministic scheduler seed
			}
			idx = e.rng.Intn(len(e.ready))
		}
		id := e.ready[idx]
		copy(e.ready[idx:], e.ready[idx+1:])
		e.ready = e.ready[:len(e.ready)-1]
		delete(e.readySet, id)
		task := e.tasks[id]
		if task == nil || task.Status == TaskDone {
			continue
		}
		return id, true
	}
	return 0, false
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
	if key, ok := e.parked[id]; ok {
		e.removeWaiter(key, id)
		delete(e.parked, id)
	}
	e.enqueue(id)
}

// Yield requeues a task after it voluntarily yielded.
func (e *Executor) Yield(id TaskID) {
	if e == nil {
		return
	}
	task := e.tasks[id]
	if task == nil || task.Status == TaskDone {
		return
	}
	e.enqueue(id)
}

// ParkCurrent moves the current task into a wait queue for the key.
func (e *Executor) ParkCurrent(key WakerKey) {
	if e == nil || !key.IsValid() {
		return
	}
	if e.current == 0 {
		return
	}
	e.parkTask(e.current, key)
}

// WakeKeyOne wakes the oldest task waiting on a key.
func (e *Executor) WakeKeyOne(key WakerKey) {
	if e == nil || !key.IsValid() {
		return
	}
	waiters := e.waiters[key]
	if len(waiters) == 0 {
		return
	}
	id := waiters[0]
	waiters = waiters[1:]
	if len(waiters) == 0 {
		delete(e.waiters, key)
	} else {
		e.waiters[key] = waiters
	}
	delete(e.parked, id)
	e.Wake(id)
}

// WakeKeyAll wakes all tasks waiting on a key.
func (e *Executor) WakeKeyAll(key WakerKey) {
	if e == nil || !key.IsValid() {
		return
	}
	waiters := e.waiters[key]
	if len(waiters) == 0 {
		return
	}
	delete(e.waiters, key)
	for _, id := range waiters {
		delete(e.parked, id)
		e.Wake(id)
	}
}

// MarkDone marks a task as completed and wakes join waiters.
func (e *Executor) MarkDone(id TaskID, kind TaskResultKind, result any) {
	if e == nil {
		return
	}
	task := e.tasks[id]
	if task == nil {
		return
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
	e.WakeKeyAll(JoinKey(id))
}

// Cancel marks a task (and its descendants) as cancelled.
func (e *Executor) Cancel(id TaskID) {
	if e == nil {
		return
	}
	e.cancelRecursive(id)
}

func (e *Executor) cancelRecursive(id TaskID) {
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
	for _, child := range task.Children {
		e.cancelRecursive(child)
	}
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

func (e *Executor) parkTask(id TaskID, key WakerKey) {
	if e == nil || !key.IsValid() {
		return
	}
	task := e.tasks[id]
	if task == nil || task.Status == TaskDone {
		return
	}
	if e.waiters == nil {
		e.waiters = make(map[WakerKey][]TaskID)
	}
	if e.parked == nil {
		e.parked = make(map[TaskID]WakerKey)
	}
	if prev, ok := e.parked[id]; ok {
		if prev == key {
			task.Status = TaskWaiting
			return
		}
		e.removeWaiter(prev, id)
	}
	e.parked[id] = key
	e.waiters[key] = append(e.waiters[key], id)
	task.Status = TaskWaiting
}

func (e *Executor) removeWaiter(key WakerKey, id TaskID) {
	if e == nil {
		return
	}
	waiters := e.waiters[key]
	for i, waiter := range waiters {
		if waiter == id {
			copy(waiters[i:], waiters[i+1:])
			waiters = waiters[:len(waiters)-1]
			break
		}
	}
	if len(waiters) == 0 {
		delete(e.waiters, key)
		return
	}
	e.waiters[key] = waiters
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
		if e.scopes != nil {
			clear(e.scopes)
		}
		if e.waiters != nil {
			clear(e.waiters)
		}
		if e.parked != nil {
			clear(e.parked)
		}
		e.nextScopeID = 1
		e.current = 0
		return nil
	}
	tasks := make([]*Task, 0, len(e.tasks))
	for _, task := range e.tasks {
		tasks = append(tasks, task)
	}
	e.tasks = make(map[TaskID]*Task)
	if e.scopes != nil {
		clear(e.scopes)
	}
	e.ready = nil
	if e.readySet != nil {
		clear(e.readySet)
	}
	if e.waiters != nil {
		clear(e.waiters)
	}
	if e.parked != nil {
		clear(e.parked)
	}
	e.nextScopeID = 1
	e.current = 0
	return tasks
}
