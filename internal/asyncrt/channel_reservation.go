package asyncrt

// ChannelSendRoute identifies the concrete slot owner selected before payload
// initialization. The scheduler reserves control; the payload lane initializes
// exact storage; Commit publishes only the resulting claim.
type ChannelSendRoute uint8

const (
	// ChannelSendRendezvous reserves one exact waiting receiver.
	ChannelSendRendezvous ChannelSendRoute = iota + 1
	// ChannelSendRing reserves one buffered channel slot.
	ChannelSendRing
	// ChannelSendPark reserves ownership in the parked sender queue.
	ChannelSendPark
)

// ChannelSendReservation is a one-shot control claim. It never owns a payload;
// Commit transfers an already initialized payload into the selected route.
type ChannelSendReservation[P Payload] struct {
	exec     *Executor[P]
	channel  *Channel[P]
	id       ChannelID
	route    ChannelSendRoute
	receiver TaskID
	recvSeq  uint64
	sender   TaskID
	valid    bool
}

// Route reports the reserved publication route.
func (r ChannelSendReservation[P]) Route() ChannelSendRoute { return r.route }

// Receiver reports the task selected by a rendezvous reservation.
func (r ChannelSendReservation[P]) Receiver() TaskID { return r.receiver }

// ReceiverParkSequence reports the selected receiver generation.
func (r ChannelSendReservation[P]) ReceiverParkSequence() uint64 {
	return r.recvSeq
}

// ChannelID reports the channel that owns the reservation.
func (r ChannelSendReservation[P]) ChannelID() ChannelID { return r.id }

// ChanReserveTrySend reserves an immediately available send route.
func (e *Executor[P]) ChanReserveTrySend(id ChannelID) (ChannelSendReservation[P], bool) {
	return e.reserveChannelSend(id, false)
}

// ChanReserveSendOrPark reserves a send route or registers the current task to retry.
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
	if ch.hasRecvClaim(e) {
		if allowPark {
			e.parkChannelSendRetry(id)
		}
		return ChannelSendReservation[P]{}, false
	}
	if waiter, ok := ch.claimRecvWaiter(e); ok {
		reservation.route = ChannelSendRendezvous
		reservation.receiver = waiter.taskID
		reservation.recvSeq = waiter.parkSeq
		return reservation, true
	}
	if ch.cap > 0 && ch.bufLenU64() < ch.cap {
		reservation.route = ChannelSendRing
		return reservation, true
	}
	return e.reserveParkedChannelSend(reservation, allowPark)
}

func (e *Executor[P]) parkChannelSendRetry(id ChannelID) bool {
	if e == nil {
		return false
	}
	current := e.Current()
	task := e.tasks[current]
	if current == 0 || task == nil || task.Status == TaskDone || task.Cancelled {
		return false
	}
	e.parkTask(current, ChannelSendKey(id))
	return true
}

func (e *Executor[P]) reserveParkedChannelSend(
	reservation ChannelSendReservation[P],
	allowPark bool,
) (ChannelSendReservation[P], bool) {
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
func (r *ChannelSendReservation[P]) Commit(value P) (completed, committed bool) {
	if r == nil || !r.valid {
		return false, false
	}
	r.valid = false
	if r.exec == nil || r.channel == nil || r.exec.channels[r.id] != r.channel {
		return false, false
	}
	switch r.route {
	case ChannelSendRendezvous:
		published := r.channel.commitRecvClaim(
			r.exec, chanRecvWaiter{taskID: r.receiver, parkSeq: r.recvSeq}, value,
		)
		return published, published
	case ChannelSendRing:
		if r.channel.closed || r.channel.hasRecvClaim(r.exec) ||
			r.channel.bufLenU64() >= r.channel.cap {
			return false, false
		}
		r.channel.bufPush(value)
		r.channel.notifyRecvWaiters(r.exec)
		return true, true
	case ChannelSendPark:
		if r.channel.closed {
			return false, false
		}
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

// Abort retires this reservation without publishing its caller-owned payload.
func (r *ChannelSendReservation[P]) Abort() {
	if r == nil || !r.valid {
		return
	}
	r.valid = false
	if r.route != ChannelSendRendezvous || r.channel == nil || r.receiver == 0 {
		return
	}
	r.channel.abortRecvClaim(
		r.exec, chanRecvWaiter{taskID: r.receiver, parkSeq: r.recvSeq},
	)
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
	if ch.cap > 0 && ch.cap <= uint64(maxIntValue()) { //nolint:gosec // the positive int bound widens without loss
		ch.buf = make([]P, 0, int(ch.cap)) //nolint:gosec // the preceding bound proves this conversion
	}
	ch.sendq = make([]chanWaiter[P], 0, 4)
	ch.recvq = make([]chanRecvWaiter, 0, 4)
}

func maxIntValue() int { return int(^uint(0) >> 1) }
