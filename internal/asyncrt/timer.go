package asyncrt

import "container/heap"

// TimerID identifies a scheduled timer.
type TimerID uint64

// Timer represents a single scheduled wakeup.
type Timer struct {
	id         TimerID
	deadlineMs uint64
	key        WakerKey
	taskID     TaskID
	cancelled  bool
}

type timerHeap []*Timer

func (h timerHeap) Len() int { return len(h) }

func (h timerHeap) Less(i, j int) bool {
	if h[i].deadlineMs == h[j].deadlineMs {
		return h[i].id < h[j].id
	}
	return h[i].deadlineMs < h[j].deadlineMs
}

func (h timerHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *timerHeap) Push(x any) {
	timer, ok := x.(*Timer)
	if !ok || timer == nil {
		return
	}
	*h = append(*h, timer)
}

func (h *timerHeap) Pop() any {
	old := *h
	n := len(old)
	if n == 0 {
		return (*Timer)(nil)
	}
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// TimerScheduleAfter schedules a timer for the current executor time + delayMs.
func (e *Executor) TimerScheduleAfter(taskID TaskID, delayMs uint64) TimerID {
	if e == nil {
		return 0
	}
	if e.nextTimerID == 0 {
		e.nextTimerID = 1
	}
	id := e.nextTimerID
	e.nextTimerID++
	timer := &Timer{
		id:         id,
		deadlineMs: e.nowMs + delayMs,
		key:        TimerKey(id),
		taskID:     taskID,
	}
	if e.timerByID == nil {
		e.timerByID = make(map[TimerID]*Timer)
	}
	e.timerByID[id] = timer
	heap.Push(&e.timers, timer)
	return id
}

// TimerCancel marks a timer as cancelled and removes it from lookup maps.
func (e *Executor) TimerCancel(id TimerID) {
	if e == nil || id == 0 {
		return
	}
	timer := e.timerByID[id]
	if timer == nil {
		return
	}
	timer.cancelled = true
	delete(e.timerByID, id)
}

// TimerActive reports whether a timer is still pending.
func (e *Executor) TimerActive(id TimerID) bool {
	if e == nil || id == 0 {
		return false
	}
	timer := e.timerByID[id]
	return timer != nil && !timer.cancelled
}

func (e *Executor) advanceTimeToNextTimer() bool {
	if e == nil {
		return false
	}
	for {
		if len(e.timers) == 0 {
			return false
		}
		timer, ok := heap.Pop(&e.timers).(*Timer)
		if !ok || timer == nil {
			continue
		}
		if timer.cancelled {
			continue
		}
		e.nowMs = timer.deadlineMs
		e.fireTimer(timer)
		for len(e.timers) > 0 {
			next := e.timers[0]
			if next == nil {
				heap.Pop(&e.timers)
				continue
			}
			if next.cancelled {
				heap.Pop(&e.timers)
				continue
			}
			if next.deadlineMs > e.nowMs {
				break
			}
			heap.Pop(&e.timers)
			e.fireTimer(next)
		}
		return true
	}
}

func (e *Executor) fireTimer(timer *Timer) {
	if e == nil || timer == nil {
		return
	}
	timer.cancelled = true
	delete(e.timerByID, timer.id)
	if timer.taskID != 0 {
		e.Wake(timer.taskID)
		return
	}
	e.WakeKeyAll(timer.key)
}
