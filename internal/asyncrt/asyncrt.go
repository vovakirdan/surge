// Package asyncrt provides an async runtime executor for deterministic task scheduling.
package asyncrt

import "math/rand"

// Executor runs async tasks on a single thread with a deterministic FIFO scheduler by default.
// Fuzz scheduling is supported for reproducible interleavings.
type Executor[P Payload] struct {
	cfg          Config
	nextID       TaskID
	nextScopeID  ScopeID
	nextChanID   ChannelID
	nextTimerID  TimerID
	nextSelectID SelectID
	nowMs        uint64
	clock        Clock
	ready        []TaskID
	readySet     map[TaskID]struct{}
	tasks        map[TaskID]*Task[P]
	scopes       map[ScopeID]*Scope
	channels     map[ChannelID]*Channel[P]
	timers       timerHeap
	timerByID    map[TimerID]*Timer
	waiters      map[WakerKey][]Waiter
	selectSubs   map[SelectID]*selectSub
	parked       map[TaskID]WakerKey
	current      TaskID
	rng          *rand.Rand
}

// TaskID identifies a task.
type TaskID uint64

// TaskStatus describes task scheduling state.
type TaskStatus uint8

const (
	// TaskReady indicates the task is ready to run.
	TaskReady TaskStatus = iota
	// TaskRunning indicates the task is currently running.
	TaskRunning
	// TaskWaiting indicates the task is waiting.
	TaskWaiting
	// TaskDone indicates the task is done.
	TaskDone
)

// TaskKind identifies special runtime tasks.
type TaskKind uint8

const (
	// TaskKindUser indicates a regular user task.
	TaskKindUser TaskKind = iota
	// TaskKindCheckpoint indicates a checkpoint task.
	TaskKindCheckpoint
	// TaskKindSleep indicates a sleep task.
	TaskKindSleep
	// TaskKindTimeout indicates a timeout task.
	TaskKindTimeout
)

// TaskResultKind describes how a task completed.
type TaskResultKind uint8

const (
	// TaskResultSuccess indicates successful completion.
	TaskResultSuccess TaskResultKind = iota
	// TaskResultCancelled indicates the task was cancelled.
	TaskResultCancelled
)

// ResumeKind indicates a resume payload for parked tasks.
type ResumeKind uint8

const (
	// ResumeNone indicates no resume payload.
	ResumeNone ResumeKind = iota
	// ResumeChanRecvValue indicates a channel receive value resume.
	ResumeChanRecvValue
	// ResumeChanRecvClosed indicates a channel receive closed resume.
	ResumeChanRecvClosed
	// ResumeChanSendAck indicates a channel send acknowledgment resume.
	ResumeChanSendAck
	// ResumeChanSendClosed indicates a channel send closed resume.
	ResumeChanSendClosed
)

// Task stores executor-visible task state.
type Task[P Payload] struct {
	ID               TaskID
	PollFuncID       int64
	State            TaskState
	ResultKind       TaskResultKind
	ResultValue      P
	ResumeKind       ResumeKind
	ResumeValue      P
	Status           TaskStatus
	Kind             TaskKind
	Cancelled        bool
	ScopeID          ScopeID
	ParentScopeID    ScopeID
	ScopeRegistered  bool
	Children         []TaskID
	TimeoutTaskID    TaskID
	SelectID         SelectID
	channelRecvSeq   uint64
	checkpointPolled bool
}

// DrainedTasks contains executor-owned payloads that must be released by the caller.
type DrainedTasks[P Payload] struct {
	Tasks           []*Task[P]
	ChannelPayloads []P
}

// Config configures executor scheduling behavior.
type Config struct {
	Deterministic bool
	Fuzz          bool
	Seed          uint64
	TimerMode     TimerMode
	Clock         Clock
}

// NewExecutor constructs an executor with the provided configuration.
func NewExecutor[P Payload](cfg Config) *Executor[P] {
	exec := &Executor[P]{
		cfg:         cfg,
		nextID:      1,
		nextScopeID: 1,
		nextChanID:  1,
		nextTimerID: 1,
		readySet:    make(map[TaskID]struct{}),
		tasks:       make(map[TaskID]*Task[P]),
		scopes:      make(map[ScopeID]*Scope),
	}
	if cfg.Clock != nil {
		exec.clock = cfg.Clock
	} else {
		exec.clock = &VirtualClock[P]{ex: exec}
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

// Spawn registers a task and enqueues it for execution.
func (e *Executor[P]) Spawn(pollFuncID int64, state TaskState) TaskID {
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
	}
	if e.tasks == nil {
		e.tasks = make(map[TaskID]*Task[P])
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

func (e *Executor[P]) spawnBuiltin(kind TaskKind, state TaskState, attach bool) TaskID {
	if e == nil {
		return 0
	}
	if e.nextID == 0 {
		e.nextID = 1
	}
	id := e.nextID
	e.nextID++

	task := &Task[P]{
		ID:     id,
		State:  state,
		Status: TaskReady,
		Kind:   kind,
	}
	if e.tasks == nil {
		e.tasks = make(map[TaskID]*Task[P])
	}
	e.tasks[id] = task
	if attach && e.current != 0 {
		if parent := e.tasks[e.current]; parent != nil {
			parent.Children = append(parent.Children, id)
		}
	}
	e.enqueue(id)
	return id
}

// SpawnCheckpoint registers a checkpoint task and enqueues it.
func (e *Executor[P]) SpawnCheckpoint() TaskID {
	if e == nil {
		return 0
	}
	if e.nextID == 0 {
		e.nextID = 1
	}
	id := e.nextID
	e.nextID++

	task := &Task[P]{
		ID:     id,
		Status: TaskReady,
		Kind:   TaskKindCheckpoint,
	}
	if e.tasks == nil {
		e.tasks = make(map[TaskID]*Task[P])
	}
	e.tasks[id] = task
	e.enqueue(id)
	return id
}

// CheckpointPolled reports whether a checkpoint task has yielded once.
func (t *Task[P]) CheckpointPolled() bool {
	if t == nil {
		return false
	}
	return t.checkpointPolled
}

// MarkCheckpointPolled marks a checkpoint task as having yielded once.
func (t *Task[P]) MarkCheckpointPolled() {
	if t == nil {
		return
	}
	t.checkpointPolled = true
}

// NextReady returns the next ready task according to scheduler policy.
func (e *Executor[P]) NextReady() (TaskID, bool) {
	if e == nil {
		return 0, false
	}
	for len(e.ready) == 0 {
		if e.hasNetWaiters() {
			timeoutMs := int64(0)
			deadline, hasTimer := e.nextTimerDeadline()
			if hasTimer {
				if e.cfg.TimerMode == TimerModeReal {
					nowMs := e.nowMs
					if e.clock != nil {
						nowMs = e.clock.NowMs()
						e.nowMs = nowMs
					}
					if deadline > nowMs {
						const maxInt64 = int64(^uint64(0) >> 1)
						delta := deadline - nowMs
						if delta > uint64(maxInt64) {
							timeoutMs = maxInt64
						} else {
							timeoutMs = int64(delta) //nolint:gosec // delta clamped to maxInt64.
						}
					} else {
						timeoutMs = 0
					}
				} else {
					timeoutMs = 0
				}
			} else {
				timeoutMs = -1
			}
			if e.netPoll(timeoutMs) {
				continue
			}
		}
		if !e.advanceTimeToNextTimer() {
			return 0, false
		}
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

func (e *Executor[P]) hasNetWaiters() bool {
	if e == nil || len(e.waiters) == 0 {
		return false
	}
	for key := range e.waiters {
		switch key.Kind {
		case WakerNetAccept, WakerNetRead, WakerNetWrite:
			return true
		default:
		}
	}
	return false
}

// Yield requeues a task after it voluntarily yielded.
func (e *Executor[P]) Yield(id TaskID) {
	if e == nil {
		return
	}
	task := e.tasks[id]
	if task == nil || task.Status == TaskDone {
		return
	}
	// Wake is the single existing-task path into readySet: it retires any
	// parked subscription (and exact receive claim) before publishing credit.
	e.Wake(id)
}

// ParkCurrent moves the current task into a wait queue for the key.
func (e *Executor[P]) ParkCurrent(key WakerKey) {
	if e == nil || !key.IsValid() {
		return
	}
	if e.current == 0 {
		return
	}
	e.parkTask(e.current, key)
}

// WakeKeyOne wakes the oldest task waiting on a key.
func (e *Executor[P]) WakeKeyOne(key WakerKey) {
	if e == nil || !key.IsValid() {
		return
	}
	waiters := e.waiters[key]
	if len(waiters) == 0 {
		return
	}
	waiter := waiters[0]
	waiters = waiters[1:]
	if len(waiters) == 0 {
		delete(e.waiters, key)
	} else {
		e.waiters[key] = waiters
	}
	e.Wake(waiter.TaskID)
}

// WakeKeyAll wakes all tasks waiting on a key.
func (e *Executor[P]) WakeKeyAll(key WakerKey) {
	if e == nil || !key.IsValid() {
		return
	}
	waiters := e.waiters[key]
	if len(waiters) == 0 {
		return
	}
	delete(e.waiters, key)
	for _, waiter := range waiters {
		e.Wake(waiter.TaskID)
	}
}

// MarkDone and Cancel -- the terminal transitions, and the one decision they
// share -- live in task_complete.go.

func (e *Executor[P]) enqueue(id TaskID) {
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

func (e *Executor[P]) parkTask(id TaskID, key WakerKey) {
	if e == nil || !key.IsValid() {
		return
	}
	task := e.tasks[id]
	if task == nil || task.Status == TaskDone {
		return
	}
	// readySet is the owner-lane no-sleep handshake. Spawned tasks have no park
	// state, while Wake (and Yield through Wake) retires it before enqueue, so
	// observing a credit here also proves that no parked row needs cleanup.
	if _, ready := e.readySet[id]; ready {
		task.Status = TaskReady
		return
	}
	if e.waiters == nil {
		e.waiters = make(map[WakerKey][]Waiter)
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
	e.waiters[key] = append(e.waiters[key], Waiter{TaskID: id})
	task.Status = TaskWaiting
}

func (e *Executor[P]) removeWaiter(key WakerKey, id TaskID) {
	if e == nil {
		return
	}
	waiters := e.waiters[key]
	if len(waiters) == 0 {
		return
	}
	n := 0
	for _, waiter := range waiters {
		if waiter.TaskID == id {
			continue
		}
		waiters[n] = waiter
		n++
	}
	waiters = waiters[:n]
	if len(waiters) == 0 {
		delete(e.waiters, key)
		return
	}
	e.waiters[key] = waiters
}

// DrainTasks returns all tasks plus pending channel payloads and resets executor queues.
func (e *Executor[P]) DrainTasks() DrainedTasks[P] {
	if e == nil {
		return DrainedTasks[P]{}
	}
	channelPayloads := e.drainChannelPayloads()
	if len(e.tasks) == 0 {
		e.ready = nil
		if e.readySet != nil {
			clear(e.readySet)
		}
		if e.scopes != nil {
			clear(e.scopes)
		}
		if e.channels != nil {
			clear(e.channels)
		}
		if e.timerByID != nil {
			clear(e.timerByID)
		}
		if e.selectSubs != nil {
			clear(e.selectSubs)
		}
		e.timers = nil
		if e.waiters != nil {
			clear(e.waiters)
		}
		if e.parked != nil {
			clear(e.parked)
		}
		e.nextScopeID = 1
		e.nextChanID = 1
		e.nextTimerID = 1
		e.nextSelectID = 1
		e.nowMs = 0
		e.current = 0
		return DrainedTasks[P]{ChannelPayloads: channelPayloads}
	}
	tasks := make([]*Task[P], 0, len(e.tasks))
	for _, task := range e.tasks {
		tasks = append(tasks, task)
	}
	e.tasks = make(map[TaskID]*Task[P])
	if e.scopes != nil {
		clear(e.scopes)
	}
	if e.channels != nil {
		clear(e.channels)
	}
	if e.timerByID != nil {
		clear(e.timerByID)
	}
	if e.selectSubs != nil {
		clear(e.selectSubs)
	}
	e.timers = nil
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
	e.nextChanID = 1
	e.nextTimerID = 1
	e.nowMs = 0
	e.current = 0
	return DrainedTasks[P]{
		Tasks:           tasks,
		ChannelPayloads: channelPayloads,
	}
}

func (e *Executor[P]) drainChannelPayloads() []P {
	if e == nil || len(e.channels) == 0 {
		return nil
	}
	payloads := make([]P, 0)
	for _, ch := range e.channels {
		if ch == nil {
			continue
		}
		payloads = append(payloads, ch.buf...)
		clear(ch.buf)
		ch.buf = nil
		ch.head = 0

		for i := range ch.sendq {
			waiter := &ch.sendq[i]
			if waiter.hasValue {
				payloads = append(payloads, waiter.value)
			}
			waiter.value = zeroPayload[P]()
			waiter.hasValue = false
		}
		ch.sendq = nil
		ch.recvq = nil
		ch.recvClaim = recvClaim{}
		ch.recvNotify = nil
		ch.sendNotify = nil
	}
	if len(payloads) == 0 {
		return nil
	}
	return payloads
}
