package asyncrt

// ChannelSendRoute identifies the concrete slot owner selected before payload
// initialization. The scheduler reserves control; the payload lane initializes
// exact storage; Commit publishes only the resulting claim.
type ChannelSendRoute uint8

const (
	ChannelSendRendezvous ChannelSendRoute = iota + 1
	ChannelSendRing
	ChannelSendPark
)

type ChannelSendReservation[P Payload] struct {
	exec     *Executor[P]
	channel  *Channel[P]
	id       ChannelID
	route    ChannelSendRoute
	receiver TaskID
	sender   TaskID
	valid    bool
}

func (r ChannelSendReservation[P]) Route() ChannelSendRoute { return r.route }
func (r ChannelSendReservation[P]) Receiver() TaskID        { return r.receiver }
func (r ChannelSendReservation[P]) ChannelID() ChannelID    { return r.id }

func (e *Executor[P]) ChanReserveTrySend(id ChannelID) (ChannelSendReservation[P], bool) {
	return e.reserveChannelSend(id, false)
}

func (e *Executor[P]) ChanReserveSendOrPark(id ChannelID) (ChannelSendReservation[P], bool) {
	return e.reserveChannelSend(id, true)
}

func (e *Executor[P]) reserveChannelSend(
	id ChannelID,
	allowPark bool,
) (ChannelSendReservation[P], bool) {
	if e == nil {
		return ChannelSendReservation[P]{}, false
	}
	ch := e.channels[id]
	if ch == nil || ch.closed {
		return ChannelSendReservation[P]{}, false
	}
	reservation := ChannelSendReservation[P]{exec: e, channel: ch, id: id, valid: true}
	if recvID, ok := ch.popRecvWaiter(e); ok {
		reservation.route = ChannelSendRendezvous
		reservation.receiver = recvID
		return reservation, true
	}
	if ch.cap > 0 && ch.bufLenU64() < ch.cap {
		reservation.route = ChannelSendRing
		return reservation, true
	}
	current := e.Current()
	if !allowPark || current == 0 {
		return ChannelSendReservation[P]{}, false
	}
	if task := e.tasks[current]; task == nil || task.Cancelled {
		return ChannelSendReservation[P]{}, false
	}
	reservation.route = ChannelSendPark
	reservation.sender = current
	return reservation, true
}

// Commit publishes the initialized claim into the reserved control route. It
// returns whether the send completed now; a parked sender is committed but not
// complete and therefore returns false.
func (r *ChannelSendReservation[P]) Commit(value P) (bool, bool) {
	if r == nil || !r.valid || r.exec == nil || r.channel == nil ||
		r.exec.channels[r.id] != r.channel || r.channel.closed {
		return false, false
	}
	r.valid = false
	switch r.route {
	case ChannelSendRendezvous:
		task := r.exec.tasks[r.receiver]
		if task == nil || task.Status == TaskDone {
			return false, false
		}
		task.ResumeKind = ResumeChanRecvValue
		task.ResumeValue = value
		r.exec.Wake(r.receiver)
		return true, true
	case ChannelSendRing:
		if r.channel.bufLenU64() >= r.channel.cap {
			return false, false
		}
		r.channel.bufPush(value)
		r.channel.notifyRecvWaiters(r.exec)
		return true, true
	case ChannelSendPark:
		task := r.exec.tasks[r.sender]
		if task == nil || task.Status == TaskDone || task.Cancelled {
			return false, false
		}
		r.channel.sendq = append(r.channel.sendq, chanWaiter[P]{
			taskID: r.sender, value: value, hasValue: true,
		})
		r.channel.notifyRecvWaiters(r.exec)
		return false, true
	default:
		return false, false
	}
}

func (r *ChannelSendReservation[P]) Abort() {
	if r == nil || !r.valid {
		return
	}
	r.valid = false
	if r.route != ChannelSendRendezvous || r.channel == nil || r.receiver == 0 {
		return
	}
	r.channel.recvq = append(r.channel.recvq, 0)
	copy(r.channel.recvq[1:], r.channel.recvq)
	r.channel.recvq[0] = r.receiver
}

// ChanPreparePayloadCapacity performs the control-token allocations at channel
// creation rather than charging the first scalar or inline value sent.
func (e *Executor[P]) ChanPreparePayloadCapacity(id ChannelID) {
	if e == nil {
		return
	}
	ch := e.channels[id]
	if ch == nil {
		return
	}
	if ch.cap > 0 && ch.cap <= uint64(maxIntValue()) {
		ch.buf = make([]P, 0, int(ch.cap))
	}
	ch.sendq = make([]chanWaiter[P], 0, 4)
	ch.recvq = make([]TaskID, 0, 4)
}

func maxIntValue() int { return int(^uint(0) >> 1) }
