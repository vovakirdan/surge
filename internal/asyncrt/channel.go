package asyncrt

import "fortio.org/safecast"

// ChannelID identifies a channel instance.
type ChannelID uint64

type chanWaiter[P Payload] struct {
	taskID   TaskID
	value    P
	hasValue bool
}

// Channel represents a single-threaded FIFO channel.
type Channel[P Payload] struct {
	id     ChannelID
	cap    uint64
	closed bool

	buf  []P
	head int

	recvq      []chanRecvWaiter
	recvClaim  recvClaim
	sendq      []chanWaiter[P]
	recvNotify []Waiter
	sendNotify []Waiter
}

// ChanNew allocates a new channel with the given capacity.
func (e *Executor[P]) ChanNew(capacity uint64) ChannelID {
	if e == nil {
		return 0
	}
	if e.nextChanID == 0 {
		e.nextChanID = 1
	}
	id := e.nextChanID
	e.nextChanID++
	if e.channels == nil {
		e.channels = make(map[ChannelID]*Channel[P])
	}
	e.channels[id] = &Channel[P]{
		id:  id,
		cap: capacity,
	}
	return id
}

// ChanDestroy forgets a channel nothing names any more and hands back every
// payload it still owned: the buffered values and the values parked senders
// had staged. The caller releases them -- they leave the executor's hold in
// the same step, so this is the one release they get.
//
// This is the VM lane's half of the channel lifetime RUNTIME_V2 section 7
// describes: destruction is what the LAST handle release performs, and only
// there do the remaining payloads get dropped. It is not `close`: a closed
// channel is still drained by its receivers, a destroyed one has none left.
// The caller guarantees no task is parked on the channel -- a parked task holds
// a handle -- so the waiter lists are expected empty and are dropped as such.
func (e *Executor[P]) ChanDestroy(id ChannelID) []P {
	if e == nil || len(e.channels) == 0 {
		return nil
	}
	ch := e.channels[id]
	if ch == nil {
		return nil
	}
	delete(e.channels, id)
	payloads := make([]P, 0, ch.bufLen()+len(ch.sendq))
	payloads = append(payloads, ch.buf[ch.head:]...)
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
	if len(payloads) == 0 {
		return nil
	}
	return payloads
}

// ChanIsClosed reports whether the channel is closed.
func (e *Executor[P]) ChanIsClosed(id ChannelID) bool {
	if e == nil {
		return true
	}
	ch := e.channels[id]
	if ch == nil {
		return true
	}
	return ch.closed
}

// ChanTrySend attempts to send without parking.
// Returns false if the channel is closed or full with no waiting receiver.
func (e *Executor[P]) ChanTrySend(id ChannelID, value P) bool {
	if e == nil {
		return false
	}
	ch := e.channels[id]
	if ch == nil || ch.closed {
		return false
	}
	if ch.hasRecvClaim(e) {
		return false
	}
	if waiter, ok := ch.popRecvWaiter(e); ok {
		recvID := waiter.taskID
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
func (e *Executor[P]) ChanTryRecv(id ChannelID) (P, bool) {
	if e == nil {
		return zeroPayload[P](), false
	}
	ch := e.channels[id]
	if ch == nil {
		return zeroPayload[P](), false
	}
	if ch.hasRecvClaim(e) {
		return zeroPayload[P](), false
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
			task.ResumeValue = zeroPayload[P]()
			e.Wake(waiter.taskID)
		}
		ch.notifySendWaiters(e)
		return waiter.value, true
	}
	return zeroPayload[P](), false
}

// ChanSendOrPark performs a send, or enqueues the sender if it would block.
// Returns true if the send completed.
func (e *Executor[P]) ChanSendOrPark(id ChannelID, value P) bool {
	if e == nil {
		return false
	}
	ch := e.channels[id]
	if ch == nil || ch.closed {
		return false
	}
	blockedByClaim := ch.hasRecvClaim(e)
	if blockedByClaim {
		e.parkChannelSendRetry(id)
		return false
	}
	if waiter, ok := ch.popRecvWaiter(e); ok {
		recvID := waiter.taskID
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
	ch.sendq = append(ch.sendq, chanWaiter[P]{taskID: current, value: value, hasValue: true})
	ch.notifyRecvWaiters(e)
	return false
}

// ChanRecvOrPark performs a receive, or enqueues the receiver if it would block.
// Returns ok=true with a value on success.
func (e *Executor[P]) ChanRecvOrPark(id ChannelID) (P, bool) {
	if e == nil {
		return zeroPayload[P](), false
	}
	ch := e.channels[id]
	if ch == nil {
		return zeroPayload[P](), false
	}
	if ch.hasRecvClaim(e) {
		if ch.closed {
			return zeroPayload[P](), false
		}
		current := e.Current()
		if current == 0 || !ch.registerRecvWaiter(e, current, 0) {
			return zeroPayload[P](), false
		}
		ch.notifySendWaiters(e)
		return zeroPayload[P](), false
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
			task.ResumeValue = zeroPayload[P]()
			e.Wake(waiter.taskID)
		}
		ch.notifySendWaiters(e)
		return waiter.value, true
	}
	if ch.closed {
		return zeroPayload[P](), false
	}
	current := e.Current()
	if current == 0 {
		return zeroPayload[P](), false
	}
	if !ch.registerRecvWaiter(e, current, 0) {
		return zeroPayload[P](), false
	}
	ch.notifySendWaiters(e)
	return zeroPayload[P](), false
}

// ChanCanRecv reports whether a receive would complete immediately.
func (e *Executor[P]) ChanCanRecv(id ChannelID) bool {
	if e == nil {
		return false
	}
	ch := e.channels[id]
	if ch == nil {
		return false
	}
	if ch.hasRecvClaim(e) {
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
func (e *Executor[P]) ChanCanSend(id ChannelID) bool {
	if e == nil {
		return false
	}
	ch := e.channels[id]
	if ch == nil || ch.closed {
		return false
	}
	if ch.hasRecvClaim(e) {
		return false
	}
	if ch.hasRecvWaiter(e) {
		return true
	}
	return ch.cap > 0 && ch.bufLenU64() < ch.cap
}

func (ch *Channel[P]) bufLen() int {
	if ch == nil {
		return 0
	}
	return len(ch.buf) - ch.head
}

func (ch *Channel[P]) bufLenU64() uint64 {
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

func (ch *Channel[P]) bufPush(value P) {
	if ch == nil {
		return
	}
	ch.buf = append(ch.buf, value)
}

func (ch *Channel[P]) bufPop() (P, bool) {
	if ch == nil || ch.bufLen() == 0 {
		return zeroPayload[P](), false
	}
	val := ch.buf[ch.head]
	// The vacated slot is cleared so a drained buffer cannot keep the payload
	// alive past the receive that took it.
	ch.buf[ch.head] = zeroPayload[P]()
	ch.head++
	if ch.head >= len(ch.buf) {
		ch.buf = nil
		ch.head = 0
	} else if ch.head > 128 && ch.head*2 >= len(ch.buf) {
		remaining := append([]P(nil), ch.buf[ch.head:]...)
		ch.buf = remaining
		ch.head = 0
	}
	return val, true
}

func (ch *Channel[P]) popSendWaiter(e *Executor[P]) (chanWaiter[P], bool) {
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
	return chanWaiter[P]{}, false
}

func (ch *Channel[P]) hasSendWaiter(e *Executor[P]) bool {
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

func (ch *Channel[P]) notifyRecvWaiters(e *Executor[P]) {
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

func (ch *Channel[P]) notifySendWaiters(e *Executor[P]) {
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

func (ch *Channel[P]) removeSelectWaiters(selectID SelectID) {
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

func (ch *Channel[P]) refillBufferFromSender(e *Executor[P]) {
	if ch == nil || ch.cap == 0 {
		return
	}
	if ch.hasRecvClaim(e) {
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
	task.ResumeValue = zeroPayload[P]()
	e.Wake(waiter.taskID)
}

// zeroPayload is the value a payload slot holds when it holds NOTHING.
//
// Under `any` that was `nil`, which is also a perfectly good payload — a
// receiver could not tell "the channel gave me nothing" from "the channel gave
// me nil". The type's own zero cannot be confused with a value the language
// produced, because the language's value type has no inhabitant that reads as
// absent; absence is carried by the accompanying bool, and this is only the
// storage that bool makes meaningless.
func zeroPayload[P Payload]() P {
	var zero P
	return zero
}
