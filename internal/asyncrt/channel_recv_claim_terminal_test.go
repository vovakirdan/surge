package asyncrt

import "testing"

func parkSendSubscriber(t *testing.T, exec *Executor[int], task TaskID, channel ChannelID) {
	t.Helper()
	ready, ok := exec.NextReady()
	if !ok || ready != task {
		t.Fatalf("next ready task = (%d, %t), want subscriber %d", ready, ok, task)
	}
	exec.SetCurrent(task)
	selection := exec.SelectNew(task)
	exec.SelectSubscribeSend(selection, channel)
	exec.ParkCurrent(SelectKey(selection))
	exec.SetCurrent(0)
}

func TestChannelRendezvousCancelRetiresOnlyControlClaim(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(1)
	parkExactReceiver(t, exec, receiver, channel, 1)
	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready {
		t.Fatal("receiver was not reserved")
	}
	subscriber := exec.Spawn(2, nil)
	parkSendSubscriber(t, exec, subscriber, channel)

	exec.Cancel(receiver)
	reservation.Abort()
	if got := len(reservation.channel.recvq); got != 0 {
		t.Fatalf("cancelled reservation restored %d registrations", got)
	}
	if completed, committed := reservation.Commit(42); completed || committed {
		t.Fatalf("cancelled reservation committed: (%t, %t)", completed, committed)
	}
	first, ok := exec.NextReady()
	if !ok || first != receiver {
		t.Fatalf("first cancel wake = (%d, %t), want receiver %d", first, ok, receiver)
	}
	second, ok := exec.NextReady()
	if !ok || second != subscriber {
		t.Fatalf("claim-release wake = (%d, %t), want subscriber %d", second, ok, subscriber)
	}
	if extra, ok := exec.NextReady(); ok {
		t.Fatalf("unexpected duplicate wake of task %d", extra)
	}
}

func TestChannelRendezvousExternalWakeRetiresExactClaim(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(1)
	parkExactReceiver(t, exec, receiver, channel, 1)
	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready {
		t.Fatal("receiver was not reserved")
	}
	subscriber := exec.Spawn(2, nil)
	parkSendSubscriber(t, exec, subscriber, channel)

	exec.Wake(receiver)
	reservation.Abort()
	if got := len(reservation.channel.recvq); got != 0 {
		t.Fatalf("externally woken reservation restored %d registrations", got)
	}
	if exec.ChanTrySend(channel, 7) != true {
		t.Fatal("released claim left the ring unavailable")
	}
	first, ok := exec.NextReady()
	if !ok || first != receiver {
		t.Fatalf("external wake = (%d, %t), want receiver %d", first, ok, receiver)
	}
	second, ok := exec.NextReady()
	if !ok || second != subscriber {
		t.Fatalf("claim-release wake = (%d, %t), want subscriber %d", second, ok, subscriber)
	}
	if extra, ok := exec.NextReady(); ok {
		t.Fatalf("unexpected duplicate wake of task %d", extra)
	}
}

func TestChannelRendezvousDrainRetiresDetachedClaim(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(0)
	parkExactReceiver(t, exec, receiver, channel, 1)
	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready {
		t.Fatal("receiver was not reserved")
	}

	drained := exec.DrainTasks()
	if len(drained.Tasks) != 1 || len(drained.ChannelPayloads) != 0 {
		t.Fatalf("drain = (tasks=%d, payloads=%d), want one task and no control payload", len(drained.Tasks), len(drained.ChannelPayloads))
	}
	reservation.Abort()
	if got := len(reservation.channel.recvq); got != 0 {
		t.Fatalf("drained reservation restored %d detached registrations", got)
	}
	if completed, committed := reservation.Commit(42); completed || committed {
		t.Fatalf("drained reservation committed: (%t, %t)", completed, committed)
	}
}

func TestChannelRendezvousCloseWakesRefusedSendRetryExactlyOnce(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(0)
	parkExactReceiver(t, exec, receiver, channel, 1)
	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready {
		t.Fatal("receiver was not reserved")
	}

	sender := exec.Spawn(2, nil)
	readySender, ok := exec.NextReady()
	if !ok || readySender != sender {
		t.Fatalf("next sender = (%d, %t), want %d", readySender, ok, sender)
	}
	exec.SetCurrent(sender)
	if _, ready := exec.ChanReserveSendOrPark(channel); ready {
		t.Fatal("sender reserved ahead of active rendezvous claim")
	}
	if got := exec.parked[sender]; got != ChannelSendKey(channel) {
		t.Fatalf("refused sender park key = %#v, want channel retry key", got)
	}
	exec.SetCurrent(0)

	exec.ChanClose(channel)
	if completed, committed := reservation.Commit(42); completed || committed {
		t.Fatalf("close-won reservation committed: (%t, %t)", completed, committed)
	}
	first, ok := exec.NextReady()
	if !ok || first != receiver {
		t.Fatalf("close receiver wake = (%d, %t), want %d", first, ok, receiver)
	}
	second, ok := exec.NextReady()
	if !ok || second != sender {
		t.Fatalf("closed retry wake = (%d, %t), want %d", second, ok, sender)
	}
	if task := exec.Task(sender); task.ResumeKind != ResumeNone || task.ResumeValue != 0 {
		t.Fatalf("refused sender acquired payload state (%d, %d)", task.ResumeKind, task.ResumeValue)
	}
	if extra, ok := exec.NextReady(); ok {
		t.Fatalf("unexpected duplicate close wake of task %d", extra)
	}
}

func TestChannelRendezvousReleaseCreditRetiresLateParkSubscription(t *testing.T) {
	for _, test := range []struct {
		name    string
		lateKey func(ChannelID) WakerKey
	}{
		{name: "same-key", lateKey: ChannelSendKey},
		{name: "unrelated-key", lateKey: func(ChannelID) WakerKey { return TimerKey(99) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			exec := NewExecutor[int](Config{Deterministic: true})
			firstReceiver := exec.Spawn(1, nil)
			secondReceiver := exec.Spawn(2, nil)
			channel := exec.ChanNew(0)
			parkExactReceiver(t, exec, firstReceiver, channel, 1)
			parkExactReceiver(t, exec, secondReceiver, channel, 1)
			reservation, ready := exec.ChanReserveTrySend(channel)
			if !ready || reservation.Receiver() != firstReceiver {
				t.Fatalf("first reservation = (ready=%t, receiver=%d), want %d", ready, reservation.Receiver(), firstReceiver)
			}
			sender := exec.Spawn(3, nil)
			readySender, ok := exec.NextReady()
			if !ok || readySender != sender {
				t.Fatalf("next sender = (%d, %t), want %d", readySender, ok, sender)
			}
			exec.SetCurrent(sender)
			if _, senderReady := exec.ChanReserveSendOrPark(channel); senderReady {
				t.Fatal("sender reserved ahead of active claim")
			}

			// Model release between reserve/refusal and the poller's final
			// ParkCurrent. The durable ready credit must refuse this late park,
			// regardless of the key the completed poll reports.
			if completed, committed := reservation.Commit(7); !completed || !committed {
				t.Fatalf("claim commit = (%t, %t), want published", completed, committed)
			}
			lateKey := test.lateKey(channel)
			exec.ParkCurrent(lateKey)
			exec.SetCurrent(0)
			if _, parked := exec.parked[sender]; parked {
				t.Fatal("late park left a subscription beside durable ready credit")
			}
			if got := len(exec.waiters[lateKey]); got != 0 {
				t.Fatalf("late park left %d waiter rows beside durable ready credit", got)
			}
			if status := exec.Task(sender).Status; status != TaskReady {
				t.Fatalf("late park changed credited sender status to %d", status)
			}

			first, ok := exec.NextReady()
			if !ok || first != firstReceiver {
				t.Fatalf("publication wake = (%d, %t), want receiver %d", first, ok, firstReceiver)
			}
			second, ok := exec.NextReady()
			if !ok || second != sender {
				t.Fatalf("durable retry credit = (%d, %t), want sender %d", second, ok, sender)
			}
			if _, parked := exec.parked[sender]; parked {
				t.Fatal("consumed ready credit retained a late park subscription")
			}

			// Consume the credit by retrying through the next FIFO receiver. Its
			// successful release must not observe a stale subscription for the
			// sender that is currently committing it.
			exec.SetCurrent(sender)
			retry, retryReady := exec.ChanReserveSendOrPark(channel)
			if !retryReady || retry.Receiver() != secondReceiver {
				t.Fatalf("retry = (ready=%t, receiver=%d), want %d", retryReady, retry.Receiver(), secondReceiver)
			}
			if completed, committed := retry.Commit(9); !completed || !committed {
				t.Fatalf("retry commit = (%t, %t), want published", completed, committed)
			}
			exec.SetCurrent(0)

			// The unrelated-key row additionally proves that a later event on
			// the attempted key cannot resurrect the consumed credit.
			exec.WakeKeyAll(lateKey)
			wokenReceiver, ok := exec.NextReady()
			if !ok || wokenReceiver != secondReceiver {
				t.Fatalf("retry receiver wake = (%d, %t), want %d", wokenReceiver, ok, secondReceiver)
			}
			if extra, extraReady := exec.NextReady(); extraReady {
				t.Fatalf("later wake re-enqueued credited task %d", extra)
			}
		})
	}
}
