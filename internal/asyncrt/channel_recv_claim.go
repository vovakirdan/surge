package asyncrt

// recvClaim is the channel-owned control state for the one rendezvous receiver
// removed from recvq but not published to yet. The payload remains entirely in
// the caller's owner lane until Commit succeeds.
type recvClaim struct {
	waiter chanRecvWaiter
}

func (claim recvClaim) active() bool { return claim.waiter.taskID != 0 }

func (ch *Channel[P]) hasRecvClaim(e *Executor[P]) bool {
	if ch == nil || !ch.recvClaim.active() {
		return false
	}
	if ch.recvWaiterLive(e, ch.recvClaim.waiter) {
		return true
	}
	ch.recvClaim = recvClaim{}
	ch.releaseRecvClaim(e)
	return false
}

func (ch *Channel[P]) claimRecvWaiter(e *Executor[P]) (chanRecvWaiter, bool) {
	if ch == nil || ch.hasRecvClaim(e) {
		return chanRecvWaiter{}, false
	}
	waiter, ok := ch.popRecvWaiter(e)
	if !ok {
		return chanRecvWaiter{}, false
	}
	ch.recvClaim = recvClaim{waiter: waiter}
	return waiter, true
}

func (ch *Channel[P]) takeRecvClaim(taskID TaskID, parkSeq uint64) (chanRecvWaiter, bool) {
	if ch == nil || !ch.recvClaim.active() {
		return chanRecvWaiter{}, false
	}
	waiter := ch.recvClaim.waiter
	if waiter.taskID != taskID || waiter.parkSeq != parkSeq {
		return chanRecvWaiter{}, false
	}
	ch.recvClaim = recvClaim{}
	return waiter, true
}

func (ch *Channel[P]) commitRecvClaim(e *Executor[P], waiter chanRecvWaiter, value P) bool {
	claimed, ok := ch.takeRecvClaim(waiter.taskID, waiter.parkSeq)
	if !ok {
		return false
	}
	defer ch.releaseRecvClaim(e)
	if ch.closed {
		ch.settleRecvClosed(e, claimed)
		return false
	}
	if !ch.recvWaiterLive(e, claimed) {
		return false
	}
	task := e.tasks[claimed.taskID]
	task.ResumeKind = ResumeChanRecvValue
	task.ResumeValue = value
	e.Wake(claimed.taskID)
	return true
}

func (ch *Channel[P]) abortRecvClaim(e *Executor[P], waiter chanRecvWaiter) {
	claimed, ok := ch.takeRecvClaim(waiter.taskID, waiter.parkSeq)
	if !ok {
		return
	}
	defer ch.releaseRecvClaim(e)
	if ch.closed {
		ch.settleRecvClosed(e, claimed)
		return
	}
	if !ch.recvWaiterLive(e, claimed) {
		return
	}
	ch.recvq = append(ch.recvq, chanRecvWaiter{})
	copy(ch.recvq[1:], ch.recvq)
	ch.recvq[0] = claimed
}

func (ch *Channel[P]) settleRecvClosed(e *Executor[P], waiter chanRecvWaiter) {
	if !ch.recvWaiterLive(e, waiter) {
		return
	}
	task := e.tasks[waiter.taskID]
	task.ResumeKind = ResumeChanRecvClosed
	task.ResumeValue = zeroPayload[P]()
	e.Wake(waiter.taskID)
}

func (ch *Channel[P]) closeRecvClaim(e *Executor[P]) {
	if ch == nil || !ch.recvClaim.active() {
		return
	}
	waiter := ch.recvClaim.waiter
	ch.recvClaim = recvClaim{}
	ch.settleRecvClosed(e, waiter)
}

func (ch *Channel[P]) releaseRecvClaim(e *Executor[P]) {
	if ch == nil || e == nil {
		return
	}
	ch.notifySendWaiters(e)
	e.WakeKeyAll(ChannelSendKey(ch.id))
}

func (e *Executor[P]) retireRecvClaimForWake(id TaskID, key WakerKey) *Channel[P] {
	if e == nil || key.Kind != WakerChannelRecv {
		return nil
	}
	ch := e.channels[ChannelID(key.A)]
	task := e.tasks[id]
	if ch == nil || task == nil {
		return nil
	}
	if _, ok := ch.takeRecvClaim(id, task.channelRecvSeq); ok {
		return ch
	}
	return nil
}
