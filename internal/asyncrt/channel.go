package asyncrt

import "fortio.org/safecast"

// ChannelID identifies a channel instance.
type ChannelID uint64

type chanWaiter struct {
	taskID   TaskID
	value    any
	hasValue bool
}

// Channel represents a single-threaded FIFO channel.
type Channel struct {
	id     ChannelID
	cap    uint64
	closed bool

	buf  []any
	head int

	recvq      []TaskID
	sendq      []chanWaiter
	recvNotify []Waiter
	sendNotify []Waiter
}

// ChanNew allocates a new channel with the given capacity.
func (e *Executor) ChanNew(capacity uint64) ChannelID {
	if e == nil {
		return 0
	}
	if e.nextChanID == 0 {
		e.nextChanID = 1
	}
	id := e.nextChanID
	e.nextChanID++
	if e.channels == nil {
		e.channels = make(map[ChannelID]*Channel)
	}
	e.channels[id] = &Channel{
		id:  id,
		cap: capacity,
	}
	return id
}

// ChanIsClosed reports whether the channel is closed.
func (e *Executor) ChanIsClosed(id ChannelID) bool {
	if e == nil {
		return true
	}
	ch := e.channels[id]
	if ch == nil {
		return true
	}
	return ch.closed
}

// ChanClose marks the channel closed and wakes all waiters.
func (e *Executor) ChanClose(id ChannelID) {
	if e == nil {
		return
	}
	ch := e.channels[id]
	if ch == nil || ch.closed {
		return
	}
	ch.closed = true

	for _, taskID := range ch.recvq {
		task := e.tasks[taskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		task.ResumeKind = ResumeChanRecvClosed
		task.ResumeValue = nil
		e.Wake(taskID)
	}
	ch.recvq = nil

	for _, waiter := range ch.recvNotify {
		task := e.tasks[waiter.TaskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		e.Wake(waiter.TaskID)
	}
	ch.recvNotify = nil

	for _, waiter := range ch.sendq {
		task := e.tasks[waiter.taskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		task.ResumeKind = ResumeChanSendClosed
		if waiter.hasValue {
			task.ResumeValue = waiter.value
		} else {
			task.ResumeValue = nil
		}
		e.Wake(waiter.taskID)
	}
	ch.sendq = nil

	for _, waiter := range ch.sendNotify {
		task := e.tasks[waiter.TaskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		e.Wake(waiter.TaskID)
	}
	ch.sendNotify = nil
}

// ChanTrySend attempts to send without parking.
// Returns false if the channel is closed or full with no waiting receiver.
func (e *Executor) ChanTrySend(id ChannelID, value any) bool {
	if e == nil {
		return false
	}
	ch := e.channels[id]
	if ch == nil || ch.closed {
		return false
	}
	if recvID, ok := ch.popRecvWaiter(e); ok {
		task := e.tasks[recvID]
		if task != nil && task.Status != TaskDone {
			task.ResumeKind = ResumeChanRecvValue
			task.ResumeValue = value
			e.Wake(recvID)
			return true
		}
	}
	if ch.cap > 0 && ch.bufLenU64() < ch.cap {
		ch.bufPush(value)
		ch.notifyRecvWaiters(e)
		return true
	}
	return false
}

// ChanTryRecv attempts to receive without parking.
// Returns ok=false if the channel is empty (or closed).
func (e *Executor) ChanTryRecv(id ChannelID) (any, bool) {
	if e == nil {
		return nil, false
	}
	ch := e.channels[id]
	if ch == nil {
		return nil, false
	}
	if val, ok := ch.bufPop(); ok {
		ch.refillBufferFromSender(e)
		ch.notifySendWaiters(e)
		return val, true
	}
	if waiter, ok := ch.popSendWaiter(e); ok {
		task := e.tasks[waiter.taskID]
		if task != nil && task.Status != TaskDone {
			task.ResumeKind = ResumeChanSendAck
			task.ResumeValue = nil
			e.Wake(waiter.taskID)
		}
		ch.notifySendWaiters(e)
		return waiter.value, true
	}
	return nil, false
}

// ChanSendOrPark performs a send, or enqueues the sender if it would block.
// Returns true if the send completed.
func (e *Executor) ChanSendOrPark(id ChannelID, value any) bool {
	if e == nil {
		return false
	}
	ch := e.channels[id]
	if ch == nil || ch.closed {
		return false
	}
	if recvID, ok := ch.popRecvWaiter(e); ok {
		task := e.tasks[recvID]
		if task != nil && task.Status != TaskDone {
			task.ResumeKind = ResumeChanRecvValue
			task.ResumeValue = value
			e.Wake(recvID)
			return true
		}
	}
	if ch.cap > 0 && ch.bufLenU64() < ch.cap {
		ch.bufPush(value)
		ch.notifyRecvWaiters(e)
		return true
	}
	current := e.Current()
	if current == 0 {
		return false
	}
	if task := e.tasks[current]; task != nil && task.Cancelled {
		return false
	}
	ch.sendq = append(ch.sendq, chanWaiter{taskID: current, value: value, hasValue: true})
	ch.notifyRecvWaiters(e)
	return false
}

// ChanRecvOrPark performs a receive, or enqueues the receiver if it would block.
// Returns ok=true with a value on success.
func (e *Executor) ChanRecvOrPark(id ChannelID) (any, bool) {
	if e == nil {
		return nil, false
	}
	ch := e.channels[id]
	if ch == nil {
		return nil, false
	}
	if val, ok := ch.bufPop(); ok {
		ch.refillBufferFromSender(e)
		ch.notifySendWaiters(e)
		return val, true
	}
	if waiter, ok := ch.popSendWaiter(e); ok {
		task := e.tasks[waiter.taskID]
		if task != nil && task.Status != TaskDone {
			task.ResumeKind = ResumeChanSendAck
			task.ResumeValue = nil
			e.Wake(waiter.taskID)
		}
		ch.notifySendWaiters(e)
		return waiter.value, true
	}
	if ch.closed {
		return nil, false
	}
	current := e.Current()
	if current == 0 {
		return nil, false
	}
	if task := e.tasks[current]; task != nil && task.Cancelled {
		return nil, false
	}
	ch.recvq = append(ch.recvq, current)
	ch.notifySendWaiters(e)
	return nil, false
}

// ChanCanRecv reports whether a receive would complete immediately.
func (e *Executor) ChanCanRecv(id ChannelID) bool {
	if e == nil {
		return false
	}
	ch := e.channels[id]
	if ch == nil {
		return false
	}
	if ch.bufLen() > 0 {
		return true
	}
	if ch.hasSendWaiter(e) {
		return true
	}
	return ch.closed
}

// ChanCanSend reports whether a send would complete immediately.
func (e *Executor) ChanCanSend(id ChannelID) bool {
	if e == nil {
		return false
	}
	ch := e.channels[id]
	if ch == nil || ch.closed {
		return false
	}
	if ch.hasRecvWaiter(e) {
		return true
	}
	return ch.cap > 0 && ch.bufLenU64() < ch.cap
}

func (ch *Channel) bufLen() int {
	if ch == nil {
		return 0
	}
	return len(ch.buf) - ch.head
}

func (ch *Channel) bufLenU64() uint64 {
	if ch == nil {
		return 0
	}
	n := ch.bufLen()
	if n <= 0 {
		return 0
	}
	u, err := safecast.Conv[uint64](n)
	if err != nil {
		return 0
	}
	return u
}

func (ch *Channel) bufPush(value any) {
	if ch == nil {
		return
	}
	ch.buf = append(ch.buf, value)
}

func (ch *Channel) bufPop() (any, bool) {
	if ch == nil || ch.bufLen() == 0 {
		return nil, false
	}
	val := ch.buf[ch.head]
	ch.buf[ch.head] = nil
	ch.head++
	if ch.head >= len(ch.buf) {
		ch.buf = nil
		ch.head = 0
	} else if ch.head > 128 && ch.head*2 >= len(ch.buf) {
		remaining := append([]any(nil), ch.buf[ch.head:]...)
		ch.buf = remaining
		ch.head = 0
	}
	return val, true
}

func (ch *Channel) popRecvWaiter(e *Executor) (TaskID, bool) {
	for len(ch.recvq) > 0 {
		taskID := ch.recvq[0]
		ch.recvq = ch.recvq[1:]
		if e == nil {
			return taskID, true
		}
		task := e.tasks[taskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		return taskID, true
	}
	return 0, false
}

func (ch *Channel) popSendWaiter(e *Executor) (chanWaiter, bool) {
	for len(ch.sendq) > 0 {
		waiter := ch.sendq[0]
		ch.sendq = ch.sendq[1:]
		if !waiter.hasValue {
			continue
		}
		if e == nil {
			return waiter, true
		}
		task := e.tasks[waiter.taskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		return waiter, true
	}
	return chanWaiter{}, false
}

func (ch *Channel) hasRecvWaiter(e *Executor) bool {
	if ch == nil {
		return false
	}
	if e == nil {
		return len(ch.recvq) > 0
	}
	n := 0
	for _, taskID := range ch.recvq {
		task := e.tasks[taskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		ch.recvq[n] = taskID
		n++
	}
	ch.recvq = ch.recvq[:n]
	return n > 0
}

func (ch *Channel) hasSendWaiter(e *Executor) bool {
	if ch == nil {
		return false
	}
	if e == nil {
		return len(ch.sendq) > 0
	}
	n := 0
	for _, waiter := range ch.sendq {
		if !waiter.hasValue {
			continue
		}
		task := e.tasks[waiter.taskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		ch.sendq[n] = waiter
		n++
	}
	ch.sendq = ch.sendq[:n]
	return n > 0
}

func (ch *Channel) notifyRecvWaiters(e *Executor) {
	if ch == nil || e == nil || len(ch.recvNotify) == 0 {
		return
	}
	waiters := ch.recvNotify
	ch.recvNotify = nil
	for _, waiter := range waiters {
		task := e.tasks[waiter.TaskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		e.Wake(waiter.TaskID)
	}
}

func (ch *Channel) notifySendWaiters(e *Executor) {
	if ch == nil || e == nil || len(ch.sendNotify) == 0 {
		return
	}
	waiters := ch.sendNotify
	ch.sendNotify = nil
	for _, waiter := range waiters {
		task := e.tasks[waiter.TaskID]
		if task == nil || task.Status == TaskDone {
			continue
		}
		e.Wake(waiter.TaskID)
	}
}

func (ch *Channel) removeSelectWaiters(selectID SelectID) {
	if ch == nil || selectID == 0 {
		return
	}
	if len(ch.recvNotify) > 0 {
		n := 0
		for _, waiter := range ch.recvNotify {
			if waiter.SelectID == selectID {
				continue
			}
			ch.recvNotify[n] = waiter
			n++
		}
		ch.recvNotify = ch.recvNotify[:n]
	}
	if len(ch.sendNotify) > 0 {
		n := 0
		for _, waiter := range ch.sendNotify {
			if waiter.SelectID == selectID {
				continue
			}
			ch.sendNotify[n] = waiter
			n++
		}
		ch.sendNotify = ch.sendNotify[:n]
	}
}

func (ch *Channel) refillBufferFromSender(e *Executor) {
	if ch == nil || ch.cap == 0 {
		return
	}
	if ch.bufLenU64() >= ch.cap {
		return
	}
	waiter, ok := ch.popSendWaiter(e)
	if !ok {
		return
	}
	ch.bufPush(waiter.value)
	task := e.tasks[waiter.taskID]
	if task == nil || task.Status == TaskDone {
		return
	}
	task.ResumeKind = ResumeChanSendAck
	task.ResumeValue = nil
	e.Wake(waiter.taskID)
}
