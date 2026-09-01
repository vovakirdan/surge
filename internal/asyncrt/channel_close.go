package asyncrt

// ChanClose marks the channel closed and wakes all waiters. A rendezvous
// reservation is not publication: an exact active receive claim is therefore
// settled closed before the remaining FIFO registrations are drained.
func (e *Executor[P]) ChanClose(id ChannelID) {
	if e == nil {
		return
	}
	ch := e.channels[id]
	if ch == nil || ch.closed {
		return
	}
	ch.closed = true
	ch.closeRecvClaim(e)

	for _, waiter := range ch.recvq {
		ch.settleRecvClosed(e, waiter)
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
			task.ResumeValue = zeroPayload[P]()
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
	e.WakeKeyAll(ChannelSendKey(id))
}
