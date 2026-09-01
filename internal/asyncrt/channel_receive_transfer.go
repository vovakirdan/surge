package asyncrt

// ChannelRefillTransfer moves a parked sender claim into the exact ring slot
// that becomes free while a buffered value is received.
type ChannelRefillTransfer[P Payload] func(P) (P, bool)

// ChanTryRecvTransfer receives without parking and applies transfer when a
// buffered receive exposes a queued sender payload for the ring.
func (e *Executor[P]) ChanTryRecvTransfer(
	id ChannelID,
	transfer ChannelRefillTransfer[P],
) (P, bool) {
	return e.chanRecvTransfer(id, false, 0, transfer)
}

// ChanRecvOrParkTransfer receives immediately or registers the current exact
// park generation; transfer moves a queued sender payload into the freed ring.
func (e *Executor[P]) ChanRecvOrParkTransfer(
	id ChannelID,
	parkSeq uint64,
	transfer ChannelRefillTransfer[P],
) (P, bool) {
	return e.chanRecvTransfer(id, true, parkSeq, transfer)
}

func (e *Executor[P]) chanRecvTransfer(
	id ChannelID,
	allowPark bool,
	parkSeq uint64,
	transfer ChannelRefillTransfer[P],
) (P, bool) {
	if e == nil {
		return zeroPayload[P](), false
	}
	ch := e.channels[id]
	if ch == nil {
		return zeroPayload[P](), false
	}
	if ch.hasRecvClaim(e) {
		if !allowPark || ch.closed {
			return zeroPayload[P](), false
		}
		current := e.Current()
		if current == 0 || parkSeq == 0 || !ch.registerRecvWaiter(e, current, parkSeq) {
			return zeroPayload[P](), false
		}
		ch.notifySendWaiters(e)
		return zeroPayload[P](), false
	}
	if value, ok := ch.bufPop(); ok {
		ch.refillBufferFromSenderTransfer(e, transfer)
		ch.notifySendWaiters(e)
		return value, true
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
	if ch.closed || !allowPark {
		return zeroPayload[P](), false
	}
	current := e.Current()
	if current == 0 {
		return zeroPayload[P](), false
	}
	if parkSeq == 0 || !ch.registerRecvWaiter(e, current, parkSeq) {
		return zeroPayload[P](), false
	}
	ch.notifySendWaiters(e)
	return zeroPayload[P](), false
}

func (ch *Channel[P]) refillBufferFromSenderTransfer(
	e *Executor[P],
	transfer ChannelRefillTransfer[P],
) {
	if ch == nil || ch.cap == 0 || ch.bufLenU64() >= ch.cap {
		return
	}
	if ch.hasRecvClaim(e) {
		return
	}
	waiter, ok := ch.popSendWaiter(e)
	if !ok {
		return
	}
	value := waiter.value
	if transfer != nil {
		var transferred bool
		value, transferred = transfer(value)
		if !transferred {
			ch.sendq = append(ch.sendq, chanWaiter[P]{})
			copy(ch.sendq[1:], ch.sendq)
			ch.sendq[0] = waiter
			return
		}
	}
	ch.bufPush(value)
	task := e.tasks[waiter.taskID]
	if task == nil || task.Status == TaskDone {
		return
	}
	task.ResumeKind = ResumeChanSendAck
	task.ResumeValue = zeroPayload[P]()
	e.Wake(waiter.taskID)
}
