package asyncrt

// SelectID identifies an active select registration.
type SelectID uint64

type selectSub struct {
	taskID     TaskID
	keys       []WakerKey
	recvNotify []ChannelID
	sendNotify []ChannelID
	timers     map[int]TimerID
}

// SelectNew allocates a new select registration for the task.
func (e *Executor) SelectNew(taskID TaskID) SelectID {
	if e == nil {
		return 0
	}
	if e.nextSelectID == 0 {
		e.nextSelectID = 1
	}
	id := e.nextSelectID
	e.nextSelectID++
	if e.selectSubs == nil {
		e.selectSubs = make(map[SelectID]*selectSub)
	}
	e.selectSubs[id] = &selectSub{
		taskID: taskID,
		timers: make(map[int]TimerID),
	}
	return id
}

// SelectTimer returns the timer ID for an arm, if any.
func (e *Executor) SelectTimer(id SelectID, arm int) TimerID {
	if e == nil {
		return 0
	}
	sub := e.selectSubs[id]
	if sub == nil || sub.timers == nil {
		return 0
	}
	return sub.timers[arm]
}

// SelectSetTimer records a timer ID for an arm.
func (e *Executor) SelectSetTimer(id SelectID, arm int, timerID TimerID) {
	if e == nil || id == 0 {
		return
	}
	sub := e.selectSubs[id]
	if sub == nil {
		return
	}
	if sub.timers == nil {
		sub.timers = make(map[int]TimerID)
	}
	sub.timers[arm] = timerID
}

// SelectSubscribeKey registers a select waiter on a key.
func (e *Executor) SelectSubscribeKey(id SelectID, key WakerKey) {
	if e == nil || id == 0 || !key.IsValid() {
		return
	}
	sub := e.selectSubs[id]
	if sub == nil {
		return
	}
	if e.waiters == nil {
		e.waiters = make(map[WakerKey][]Waiter)
	}
	e.waiters[key] = append(e.waiters[key], Waiter{TaskID: sub.taskID, SelectID: id})
	sub.keys = append(sub.keys, key)
}

// SelectSubscribeRecv registers a select waiter for channel recv readiness.
func (e *Executor) SelectSubscribeRecv(id SelectID, channelID ChannelID) {
	if e == nil || id == 0 {
		return
	}
	sub := e.selectSubs[id]
	if sub == nil {
		return
	}
	ch := e.channels[channelID]
	if ch == nil {
		return
	}
	ch.recvNotify = append(ch.recvNotify, Waiter{TaskID: sub.taskID, SelectID: id})
	sub.recvNotify = append(sub.recvNotify, channelID)
}

// SelectSubscribeSend registers a select waiter for channel send readiness.
func (e *Executor) SelectSubscribeSend(id SelectID, channelID ChannelID) {
	if e == nil || id == 0 {
		return
	}
	sub := e.selectSubs[id]
	if sub == nil {
		return
	}
	ch := e.channels[channelID]
	if ch == nil {
		return
	}
	ch.sendNotify = append(ch.sendNotify, Waiter{TaskID: sub.taskID, SelectID: id})
	sub.sendNotify = append(sub.sendNotify, channelID)
}

// SelectClearWaiters removes select waiters for a select ID but keeps timers.
func (e *Executor) SelectClearWaiters(id SelectID) {
	if e == nil || id == 0 {
		return
	}
	sub := e.selectSubs[id]
	if sub == nil {
		return
	}
	for _, key := range sub.keys {
		e.removeSelectWaiters(key, id)
	}
	for _, chID := range sub.recvNotify {
		if ch := e.channels[chID]; ch != nil {
			ch.removeSelectWaiters(id)
		}
	}
	for _, chID := range sub.sendNotify {
		if ch := e.channels[chID]; ch != nil {
			ch.removeSelectWaiters(id)
		}
	}
	sub.keys = nil
	sub.recvNotify = nil
	sub.sendNotify = nil
}

// SelectClear removes all waiters and timers for a select ID.
func (e *Executor) SelectClear(id SelectID) {
	if e == nil || id == 0 {
		return
	}
	sub := e.selectSubs[id]
	if sub == nil {
		return
	}
	e.SelectClearWaiters(id)
	for _, timerID := range sub.timers {
		e.TimerCancel(timerID)
	}
	delete(e.selectSubs, id)
}

func (e *Executor) removeSelectWaiters(key WakerKey, selectID SelectID) {
	if e == nil || !key.IsValid() {
		return
	}
	waiters := e.waiters[key]
	if len(waiters) == 0 {
		return
	}
	n := 0
	for _, waiter := range waiters {
		if waiter.SelectID == selectID {
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
