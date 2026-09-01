package asyncrt

import "testing"

func parkExactReceiver(
	t *testing.T,
	exec *Executor[int],
	task TaskID,
	channel ChannelID,
	sequence uint64,
) {
	t.Helper()
	ready, ok := exec.NextReady()
	if !ok || ready != task {
		t.Fatalf("next ready task = (%d, %t), want receiver %d", ready, ok, task)
	}
	exec.SetCurrent(task)
	if _, received := exec.ChanRecvOrParkTransfer(channel, sequence, nil); received {
		t.Fatal("empty channel unexpectedly produced a value")
	}
	exec.ParkCurrent(ChannelRecvKey(channel))
	exec.SetCurrent(0)
}

func requireOnlyReady(t *testing.T, exec *Executor[int], want TaskID) {
	t.Helper()
	got, ok := exec.NextReady()
	if !ok || got != want {
		t.Fatalf("next ready task = (%d, %t), want %d", got, ok, want)
	}
	if extra, ok := exec.NextReady(); ok {
		t.Fatalf("unexpected second ready task %d", extra)
	}
}

func TestChannelRendezvousReserveCloseCommitIsCloseWon(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(0)
	parkExactReceiver(t, exec, receiver, channel, 1)

	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready || reservation.Route() != ChannelSendRendezvous {
		t.Fatalf("reservation = (ready=%t, route=%d), want rendezvous", ready, reservation.Route())
	}
	exec.ChanClose(channel)

	task := exec.Task(receiver)
	if task.ResumeKind != ResumeChanRecvClosed || task.ResumeValue != 0 {
		t.Fatalf("close resume = (%d, %d), want closed/nothing", task.ResumeKind, task.ResumeValue)
	}
	if _, parked := exec.parked[receiver]; parked {
		t.Fatal("close-won receiver remained parked")
	}
	if completed, committed := reservation.Commit(42); completed || committed {
		t.Fatalf("close-won commit = (%t, %t), want rejected", completed, committed)
	}
	if task.ResumeKind != ResumeChanRecvClosed || task.ResumeValue != 0 {
		t.Fatalf("late commit changed close terminal state to (%d, %d)", task.ResumeKind, task.ResumeValue)
	}
	requireOnlyReady(t, exec, receiver)
}

func TestChannelRendezvousReserveCommitCloseKeepsPublishedValue(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(0)
	parkExactReceiver(t, exec, receiver, channel, 1)

	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready {
		t.Fatal("live receiver was not reserved")
	}
	if completed, committed := reservation.Commit(42); !completed || !committed {
		t.Fatalf("commit = (%t, %t), want published rendezvous", completed, committed)
	}
	exec.ChanClose(channel)

	task := exec.Task(receiver)
	if task.ResumeKind != ResumeChanRecvValue || task.ResumeValue != 42 {
		t.Fatalf("post-close resume = (%d, %d), want published value 42", task.ResumeKind, task.ResumeValue)
	}
	requireOnlyReady(t, exec, receiver)
}

func TestChannelRendezvousReserveCloseAbortRetiresCloseWonClaim(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(0)
	parkExactReceiver(t, exec, receiver, channel, 1)

	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready {
		t.Fatal("live receiver was not reserved")
	}
	exec.ChanClose(channel)
	reservation.Abort()
	reservation.Abort()

	if got := len(reservation.channel.recvq); got != 0 {
		t.Fatalf("close-won abort restored %d receive registrations", got)
	}
	task := exec.Task(receiver)
	if task.ResumeKind != ResumeChanRecvClosed || task.ResumeValue != 0 {
		t.Fatalf("abort changed close terminal state to (%d, %d)", task.ResumeKind, task.ResumeValue)
	}
	requireOnlyReady(t, exec, receiver)
}

func TestChannelRendezvousClaimCannotBeOvertaken(t *testing.T) {
	t.Run("queued receiver and parked sender", func(t *testing.T) {
		exec := NewExecutor[int](Config{Deterministic: true})
		first := exec.Spawn(1, nil)
		channel := exec.ChanNew(0)
		parkExactReceiver(t, exec, first, channel, 1)
		claim, ready := exec.ChanReserveTrySend(channel)
		if !ready {
			t.Fatal("first receiver was not reserved")
		}

		second := exec.Spawn(2, nil)
		parkExactReceiver(t, exec, second, channel, 1)
		if exec.ChanCanSend(channel) {
			t.Fatal("later receiver made send immediately ready ahead of active claim")
		}
		if _, secondReady := exec.ChanReserveTrySend(channel); secondReady {
			t.Fatal("a second immediate rendezvous overtook the active claim")
		}

		sender := exec.Spawn(3, nil)
		readySender, ok := exec.NextReady()
		if !ok || readySender != sender {
			t.Fatalf("next sender = (%d, %t), want %d", readySender, ok, sender)
		}
		exec.SetCurrent(sender)
		if refused, senderReady := exec.ChanReserveSendOrPark(channel); senderReady || refused.Route() != 0 {
			t.Fatalf("later send reservation = (ready=%t, route=%d), want refused", senderReady, refused.Route())
		}
		if got := exec.parked[sender]; got != ChannelSendKey(channel) {
			t.Fatalf("refused sender park key = %#v, want channel retry key", got)
		}
		exec.SetCurrent(0)
		if value, received := exec.ChanTryRecv(channel); received {
			t.Fatalf("refused later send %d overtook active rendezvous", value)
		}

		if completed, committed := claim.Commit(11); !completed || !committed {
			t.Fatalf("first claim commit = (%t, %t), want published", completed, committed)
		}
		readyFirst, ok := exec.NextReady()
		if !ok || readyFirst != first {
			t.Fatalf("first publication wake = (%d, %t), want receiver %d", readyFirst, ok, first)
		}
		readySender, ok = exec.NextReady()
		if !ok || readySender != sender {
			t.Fatalf("claim-release wake = (%d, %t), want sender %d", readySender, ok, sender)
		}
		exec.SetCurrent(sender)
		retry, ready := exec.ChanReserveSendOrPark(channel)
		if !ready || retry.Route() != ChannelSendRendezvous || retry.Receiver() != second {
			t.Fatalf("retry = (ready=%t, route=%d, receiver=%d), want second receiver", ready, retry.Route(), retry.Receiver())
		}
		if completed, committed := retry.Commit(22); !completed || !committed {
			t.Fatalf("retry commit = (%t, %t), want published", completed, committed)
		}
		readySecond, ok := exec.NextReady()
		if !ok || readySecond != second {
			t.Fatalf("retry wake = (%d, %t), want receiver %d", readySecond, ok, second)
		}
	})

	t.Run("ring", func(t *testing.T) {
		exec := NewExecutor[int](Config{Deterministic: true})
		receiver := exec.Spawn(1, nil)
		channel := exec.ChanNew(1)
		parkExactReceiver(t, exec, receiver, channel, 1)
		claim, ready := exec.ChanReserveTrySend(channel)
		if !ready {
			t.Fatal("receiver was not reserved")
		}
		if exec.ChanCanSend(channel) || exec.ChanTrySend(channel, 99) {
			t.Fatal("empty ring accepted a later send ahead of active rendezvous")
		}
		sender := exec.Spawn(2, nil)
		readySender, ok := exec.NextReady()
		if !ok || readySender != sender {
			t.Fatalf("ring sender = (%d, %t), want %d", readySender, ok, sender)
		}
		exec.SetCurrent(sender)
		if _, senderReady := exec.ChanReserveSendOrPark(channel); senderReady {
			t.Fatal("ring sender reserved ahead of active claim")
		}
		if got := exec.parked[sender]; got != ChannelSendKey(channel) {
			t.Fatalf("ring sender park key = %#v, want channel retry key", got)
		}
		exec.SetCurrent(0)
		if completed, committed := claim.Commit(11); !completed || !committed {
			t.Fatalf("ring claim commit = (%t, %t), want published", completed, committed)
		}
		readyReceiver, ok := exec.NextReady()
		if !ok || readyReceiver != receiver {
			t.Fatalf("ring receiver wake = (%d, %t), want %d", readyReceiver, ok, receiver)
		}
		readySender, ok = exec.NextReady()
		if !ok || readySender != sender {
			t.Fatalf("ring release wake = (%d, %t), want %d", readySender, ok, sender)
		}
		exec.SetCurrent(sender)
		retry, ready := exec.ChanReserveSendOrPark(channel)
		if !ready || retry.Route() != ChannelSendRing {
			t.Fatalf("ring retry = (ready=%t, route=%d), want ring", ready, retry.Route())
		}
		if completed, committed := retry.Commit(99); !completed || !committed {
			t.Fatalf("ring retry commit = (%t, %t), want published", completed, committed)
		}
		if value, ok := exec.ChanTryRecv(channel); !ok || value != 99 {
			t.Fatalf("ring receive = (%d, %t), want 99", value, ok)
		}
	})
}

func TestChanCanSendFiltersReceiveGenerations(t *testing.T) {
	t.Run("different channel", func(t *testing.T) {
		exec := NewExecutor[int](Config{Deterministic: true})
		receiver := exec.Spawn(1, nil)
		first := exec.ChanNew(0)
		second := exec.ChanNew(0)
		parkExactReceiver(t, exec, receiver, first, 1)
		exec.Wake(receiver)
		parkExactReceiver(t, exec, receiver, second, 2)

		if exec.ChanCanSend(first) {
			t.Fatal("stale different-channel generation reported send-ready")
		}
		if got := len(exec.channels[first].recvq); got != 0 {
			t.Fatalf("stale different-channel queue retained %d entries", got)
		}
		if !exec.ChanCanSend(second) {
			t.Fatal("current different-channel generation was not send-ready")
		}
	})

	t.Run("same channel", func(t *testing.T) {
		exec := NewExecutor[int](Config{Deterministic: true})
		receiver := exec.Spawn(1, nil)
		channel := exec.ChanNew(0)
		parkExactReceiver(t, exec, receiver, channel, 1)
		exec.Wake(receiver)
		parkExactReceiver(t, exec, receiver, channel, 2)

		if !exec.ChanCanSend(channel) {
			t.Fatal("newest same-channel generation was not send-ready")
		}
		queue := exec.channels[channel].recvq
		if len(queue) != 1 || queue[0].parkSeq != 2 {
			t.Fatalf("filtered same-channel queue = %#v, want only sequence 2", queue)
		}
	})

	t.Run("claimed generation reparked on different channel", func(t *testing.T) {
		exec := NewExecutor[int](Config{Deterministic: true})
		receiver := exec.Spawn(1, nil)
		first := exec.ChanNew(1)
		second := exec.ChanNew(0)
		parkExactReceiver(t, exec, receiver, first, 1)
		reservation, ready := exec.ChanReserveTrySend(first)
		if !ready {
			t.Fatal("first generation was not reserved")
		}
		if !exec.channels[second].registerRecvWaiter(exec, receiver, 2) {
			t.Fatal("second-channel generation was not registered")
		}
		exec.parkTask(receiver, ChannelRecvKey(second))

		if !exec.ChanCanSend(first) {
			t.Fatal("stale different-channel claim kept an empty ring unavailable")
		}
		reservation.Abort()
		if got := len(exec.channels[first].recvq); got != 0 {
			t.Fatalf("stale different-channel abort restored %d registrations", got)
		}
		if !exec.ChanCanSend(second) {
			t.Fatal("current second-channel generation was not send-ready")
		}
	})

	t.Run("claimed generation reparked on same channel", func(t *testing.T) {
		exec := NewExecutor[int](Config{Deterministic: true})
		receiver := exec.Spawn(1, nil)
		channel := exec.ChanNew(0)
		parkExactReceiver(t, exec, receiver, channel, 1)
		reservation, ready := exec.ChanReserveTrySend(channel)
		if !ready {
			t.Fatal("first generation was not reserved")
		}
		if !exec.channels[channel].registerRecvWaiter(exec, receiver, 2) {
			t.Fatal("new same-channel generation was not registered")
		}
		exec.parkTask(receiver, ChannelRecvKey(channel))

		if !exec.ChanCanSend(channel) {
			t.Fatal("stale same-channel claim hid the current generation")
		}
		reservation.Abort()
		queue := exec.channels[channel].recvq
		if len(queue) != 1 || queue[0].parkSeq != 2 {
			t.Fatalf("same-channel claim filter left queue %#v, want sequence 2", queue)
		}
	})
}

func TestChannelReceiveGenerationOverflowFailsClosed(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(0)
	parkExactReceiver(t, exec, receiver, channel, ^uint64(0))

	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready || reservation.ReceiverParkSequence() != ^uint64(0) {
		t.Fatalf("max reservation = (ready=%t, seq=%d)", ready, reservation.ReceiverParkSequence())
	}
	reservation.Abort()
	exec.Wake(receiver)
	readyReceiver, ok := exec.NextReady()
	if !ok || readyReceiver != receiver {
		t.Fatalf("ready after max generation = (%d, %t)", readyReceiver, ok)
	}
	if exec.channels[channel].registerRecvWaiter(exec, receiver, 0) {
		t.Fatal("implicit receive generation wrapped after MaxUint64")
	}
	if got := exec.Task(receiver).channelRecvSeq; got != ^uint64(0) {
		t.Fatalf("receive generation wrapped to %d", got)
	}
}
