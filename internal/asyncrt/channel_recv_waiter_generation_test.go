package asyncrt

import "testing"

func runNextReadyTask(t *testing.T, exec *Executor[int], want TaskID) {
	t.Helper()
	got, ok := exec.NextReady()
	if !ok || got != want {
		t.Fatalf("next ready task = (%d, %t), want %d", got, ok, want)
	}
	exec.SetCurrent(want)
}

func TestChannelReservationSkipsReceiverReparkedOnAnotherChannel(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	first := exec.ChanNew(0)
	second := exec.ChanNew(0)

	runNextReadyTask(t, exec, receiver)
	if _, ok := exec.ChanRecvOrPark(first); ok {
		t.Fatal("empty first channel unexpectedly received")
	}
	exec.ParkCurrent(ChannelRecvKey(first))
	exec.SetCurrent(0)
	exec.Wake(receiver)

	runNextReadyTask(t, exec, receiver)
	if _, ok := exec.ChanRecvOrPark(second); ok {
		t.Fatal("empty second channel unexpectedly received")
	}
	exec.ParkCurrent(ChannelRecvKey(second))
	exec.SetCurrent(0)

	if reservation, ready := exec.ChanReserveTrySend(first); ready {
		reservation.Abort()
		t.Fatal("stale first-channel registration was accepted after receiver reparked")
	}
	if got := exec.parked[receiver]; got != ChannelRecvKey(second) {
		t.Fatalf("receiver park key = %#v, want second channel", got)
	}
}

func TestChannelReservationUsesNewestSameChannelParkSequence(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(0)

	runNextReadyTask(t, exec, receiver)
	if _, ok := exec.ChanRecvOrParkTransfer(channel, 1, nil); ok {
		t.Fatal("empty channel unexpectedly received")
	}
	exec.ParkCurrent(ChannelRecvKey(channel))
	exec.SetCurrent(0)
	exec.Wake(receiver)

	runNextReadyTask(t, exec, receiver)
	if _, ok := exec.ChanRecvOrParkTransfer(channel, 2, nil); ok {
		t.Fatal("empty channel unexpectedly received after repark")
	}
	exec.ParkCurrent(ChannelRecvKey(channel))
	exec.SetCurrent(0)

	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready {
		t.Fatal("newest same-channel registration was not available")
	}
	if got := reservation.ReceiverParkSequence(); got != 2 {
		t.Fatalf("reserved park sequence = %d, want 2", got)
	}
	completed, committed := reservation.Commit(42)
	if !completed || !committed {
		t.Fatalf("rendezvous result = (%t, %t), want complete and committed", completed, committed)
	}
	task := exec.Task(receiver)
	if task.ResumeKind != ResumeChanRecvValue || task.ResumeValue != 42 {
		t.Fatalf("resume = (%d, %d), want channel value 42", task.ResumeKind, task.ResumeValue)
	}
}

func TestChannelReservationCommitRejectsReceiverReparkedAfterClaim(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	first := exec.ChanNew(0)
	second := exec.ChanNew(0)

	runNextReadyTask(t, exec, receiver)
	exec.ChanRecvOrParkTransfer(first, 1, nil)
	exec.ParkCurrent(ChannelRecvKey(first))
	exec.SetCurrent(0)
	reservation, ready := exec.ChanReserveTrySend(first)
	if !ready {
		t.Fatal("live first-channel registration was not reserved")
	}

	exec.Wake(receiver)
	runNextReadyTask(t, exec, receiver)
	exec.ChanRecvOrParkTransfer(second, 2, nil)
	exec.ParkCurrent(ChannelRecvKey(second))
	exec.SetCurrent(0)
	if completed, committed := reservation.Commit(42); completed || committed {
		t.Fatalf("old reservation committed after repark: (%t, %t)", completed, committed)
	}
	task := exec.Task(receiver)
	if task.ResumeKind != ResumeNone {
		t.Fatalf("old reservation wrote resume kind %d", task.ResumeKind)
	}
	if got := exec.parked[receiver]; got != ChannelRecvKey(second) {
		t.Fatalf("receiver park key = %#v, want second channel", got)
	}

	exec.ChanClose(first)
	if task.Status != TaskWaiting {
		t.Fatalf("closing stale first channel changed receiver status to %d", task.Status)
	}
	exec.ChanClose(second)
	if task.ResumeKind != ResumeChanRecvClosed {
		t.Fatalf("closing current channel left resume kind %d", task.ResumeKind)
	}
	if _, parked := exec.parked[receiver]; parked {
		t.Fatal("closing current channel left receiver parked")
	}
}

func TestChannelReservationAbortRestoresExactReceiverSequence(t *testing.T) {
	exec := NewExecutor[int](Config{Deterministic: true})
	receiver := exec.Spawn(1, nil)
	channel := exec.ChanNew(0)

	runNextReadyTask(t, exec, receiver)
	exec.ChanRecvOrParkTransfer(channel, 7, nil)
	exec.ParkCurrent(ChannelRecvKey(channel))
	exec.SetCurrent(0)
	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready {
		t.Fatal("live registration was not reserved")
	}
	reservation.Abort()

	restored, ready := exec.ChanReserveTrySend(channel)
	if !ready || restored.ReceiverParkSequence() != 7 {
		t.Fatalf("restored reservation = (ready=%t, seq=%d), want seq 7", ready, restored.ReceiverParkSequence())
	}
	restored.Abort()
}
