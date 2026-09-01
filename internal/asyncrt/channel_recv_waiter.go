package asyncrt

type chanRecvWaiter struct {
	taskID  TaskID
	parkSeq uint64
}

func (ch *Channel[P]) registerRecvWaiter(e *Executor[P], taskID TaskID, parkSeq uint64) bool {
	if ch == nil || e == nil {
		return false
	}
	task := e.tasks[taskID]
	if task == nil || task.Cancelled {
		return false
	}
	if parkSeq == 0 {
		if task.channelRecvSeq == ^uint64(0) {
			return false
		}
		parkSeq = task.channelRecvSeq + 1
	} else if parkSeq <= task.channelRecvSeq {
		return false
	}
	task.channelRecvSeq = parkSeq
	ch.recvq = append(ch.recvq, chanRecvWaiter{taskID: taskID, parkSeq: parkSeq})
	return true
}

func (ch *Channel[P]) popRecvWaiter(e *Executor[P]) (chanRecvWaiter, bool) {
	for len(ch.recvq) > 0 {
		waiter := ch.recvq[0]
		ch.recvq = ch.recvq[1:]
		if e == nil {
			return waiter, true
		}
		if !ch.recvWaiterLive(e, waiter) {
			continue
		}
		return waiter, true
	}
	return chanRecvWaiter{}, false
}

func (ch *Channel[P]) recvWaiterLive(e *Executor[P], waiter chanRecvWaiter) bool {
	if ch == nil || e == nil {
		return false
	}
	task := e.tasks[waiter.taskID]
	if task == nil || task.Status != TaskWaiting || task.channelRecvSeq != waiter.parkSeq {
		return false
	}
	key, parked := e.parked[waiter.taskID]
	return parked && key == ChannelRecvKey(ch.id)
}

func (ch *Channel[P]) hasRecvWaiter(e *Executor[P]) bool {
	if ch == nil {
		return false
	}
	if e == nil {
		return len(ch.recvq) > 0
	}
	n := 0
	for _, waiter := range ch.recvq {
		if !ch.recvWaiterLive(e, waiter) {
			continue
		}
		ch.recvq[n] = waiter
		n++
	}
	ch.recvq = ch.recvq[:n]
	return n > 0
}
