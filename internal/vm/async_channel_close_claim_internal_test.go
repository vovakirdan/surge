package vm

import (
	"testing"

	"surge/internal/asyncrt"
)

type vmRendezvousReservation struct {
	receiver    asyncrt.TaskID
	channel     asyncrt.ChannelID
	sequence    uint64
	reservation asyncrt.ChannelSendReservation[asyncPayload]
}

func reserveVMRendezvous(
	t *testing.T,
	f *asyncPayloadFixture,
	exec *asyncrt.Executor[asyncPayload],
) vmRendezvousReservation {
	t.Helper()
	receiver := exec.Spawn(1, nil)
	ready, ok := exec.NextReady()
	if !ok || ready != receiver {
		t.Fatalf("next ready task = (%d, %t), want receiver %d", ready, ok, receiver)
	}
	channel := exec.ChanNew(0)
	if channel != 1 {
		t.Fatalf("fixture channel id = %d, want registered owner 1", channel)
	}
	sequence, vmErr := f.vm.nextAsyncParkSequence(receiver)
	if vmErr != nil {
		t.Fatalf("next park sequence: %v", vmErr)
	}
	exec.SetCurrent(receiver)
	if _, received, vmErr := f.vm.receiveOrParkAsyncChannel(exec, channel, sequence); vmErr != nil || received {
		t.Fatalf("empty receive = (received=%t, err=%v)", received, vmErr)
	}
	exec.ParkCurrent(asyncrt.ChannelRecvKey(channel))
	exec.SetCurrent(0)
	reservation, available := exec.ChanReserveTrySend(channel)
	if !available || reservation.Route() != asyncrt.ChannelSendRendezvous {
		t.Fatalf("reservation = (available=%t, route=%d), want rendezvous", available, reservation.Route())
	}
	return vmRendezvousReservation{
		receiver: receiver, channel: channel, sequence: sequence, reservation: reservation,
	}
}

func buildOwnedNode(t *testing.T, f *asyncPayloadFixture, label string) (StorageRef, Handle) {
	t.Helper()
	f.vm.Stack = []*Frame{f.sender}
	source, vmErr := f.vm.buildComposite(f.sender, f.node)
	if vmErr != nil {
		t.Fatalf("build source: %v", vmErr)
	}
	handle := f.writeNode(t, source, 1, 2, 3, label)
	return source, handle
}

func requireFreedOnce(t *testing.T, f *asyncPayloadFixture, handle Handle, before uint64) {
	t.Helper()
	if !f.freed(t, handle) {
		t.Fatalf("handle %d was not freed", handle)
	}
	if got := f.vm.heapCounters.freeCount; got != before+1 {
		t.Fatalf("free count = %d, want %d", got, before+1)
	}
}

func TestReservedRendezvousCloseDropsCompositeExactlyOnce(t *testing.T) {
	for _, teardownFirst := range []bool{false, true} {
		name := "payload-first"
		if teardownFirst {
			name = "receiver-first"
		}
		t.Run(name, func(t *testing.T) {
			f := newAsyncPayloadFixture(t)
			exec := asyncrt.NewExecutor[asyncPayload](asyncrt.Config{Deterministic: true})
			rendezvous := reserveVMRendezvous(t, f, exec)
			source, handle := buildOwnedNode(t, f, "close direct")
			before := f.vm.heapCounters.freeCount
			payload, vmErr := f.vm.stageReservedChannelSend(
				rendezvous.reservation, MakeComposite(source),
			)
			if vmErr != nil {
				t.Fatalf("stage rendezvous payload: %v", vmErr)
			}

			exec.ChanClose(rendezvous.channel)
			if completed, committed := rendezvous.reservation.Commit(payload); completed || committed {
				t.Fatalf("close-won commit = (%t, %t), want rejected", completed, committed)
			}
			task := exec.Task(rendezvous.receiver)
			if task.ResumeKind != asyncrt.ResumeChanRecvClosed || task.ResumeValue != (asyncPayload{}) {
				t.Fatalf("receiver resume = (%d, %#v), want closed/nothing", task.ResumeKind, task.ResumeValue)
			}
			if teardownFirst {
				f.vm.destroyAsyncTaskOwners(rendezvous.receiver)
			}
			f.vm.dropAsyncPayload(payload)
			f.vm.dropAsyncPayload(payload)
			if !teardownFirst {
				f.vm.destroyAsyncTaskOwners(rendezvous.receiver)
			}
			requireFreedOnce(t, f, handle, before)
		})
	}
}

func TestReservedTrySendCloseReturnsFalseAndDropsComposite(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	exec := asyncrt.NewExecutor[asyncPayload](asyncrt.Config{Deterministic: true})
	rendezvous := reserveVMRendezvous(t, f, exec)
	source, handle := buildOwnedNode(t, f, "closed try send")
	before := f.vm.heapCounters.freeCount

	exec.ChanClose(rendezvous.channel)
	sent, vmErr := f.vm.commitReservedTrySend(
		exec, rendezvous.reservation, MakeComposite(source),
	)
	if vmErr != nil || sent {
		t.Fatalf("close-won try_send = (sent=%t, err=%v), want false without error", sent, vmErr)
	}
	task := exec.Task(rendezvous.receiver)
	if task.ResumeKind != asyncrt.ResumeChanRecvClosed || task.ResumeValue != (asyncPayload{}) {
		t.Fatalf("try_send receiver resume = (%d, %#v), want closed/nothing", task.ResumeKind, task.ResumeValue)
	}
	f.vm.destroyAsyncTaskOwners(rendezvous.receiver)
	requireFreedOnce(t, f, handle, before)
}

func TestRoutedSelectRendezvousCloseDropsCompositeExactlyOnce(t *testing.T) {
	for _, teardownFirst := range []bool{false, true} {
		name := "payload-first"
		if teardownFirst {
			name = "receiver-first"
		}
		t.Run(name, func(t *testing.T) {
			f := newAsyncPayloadFixture(t)
			exec := asyncrt.NewExecutor[asyncPayload](asyncrt.Config{Deterministic: true})
			rendezvous := reserveVMRendezvous(t, f, exec)
			source, handle := buildOwnedNode(t, f, "close routed select")
			before := f.vm.heapCounters.freeCount
			selectOwner, vmErr := f.vm.asyncSelectOwner(rendezvous.receiver, 7, f.node)
			if vmErr != nil {
				t.Fatalf("select owner: %v", vmErr)
			}
			staged, vmErr := f.vm.stageAsyncPayloadInto(
				selectOwner, asyncSlotSelect, MakeComposite(source),
			)
			if vmErr != nil {
				t.Fatalf("stage selected payload: %v", vmErr)
			}
			routed, vmErr := f.vm.routeAsyncPayloadToChannel(rendezvous.reservation, staged)
			if vmErr != nil {
				t.Fatalf("route selected payload: %v", vmErr)
			}

			exec.ChanClose(rendezvous.channel)
			if completed, committed := rendezvous.reservation.Commit(routed); completed || committed {
				t.Fatalf("close-won routed commit = (%t, %t), want rejected", completed, committed)
			}
			task := exec.Task(rendezvous.receiver)
			if task.ResumeKind != asyncrt.ResumeChanRecvClosed || task.ResumeValue != (asyncPayload{}) {
				t.Fatalf("routed receiver resume = (%d, %#v), want closed/nothing", task.ResumeKind, task.ResumeValue)
			}
			if teardownFirst {
				f.vm.destroyAsyncTaskOwners(rendezvous.receiver)
			}
			f.vm.dropAsyncPayload(staged)
			f.vm.dropAsyncPayload(routed)
			f.vm.dropAsyncPayload(routed)
			if !teardownFirst {
				f.vm.destroyAsyncTaskOwners(rendezvous.receiver)
			}
			requireFreedOnce(t, f, handle, before)
		})
	}
}

func TestReservedRendezvousSequenceChangeLeavesSourceUnchanged(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	exec := asyncrt.NewExecutor[asyncPayload](asyncrt.Config{Deterministic: true})
	rendezvous := reserveVMRendezvous(t, f, exec)
	source, handle := buildOwnedNode(t, f, "direct source unchanged")
	before := f.vm.heapCounters.freeCount
	if _, vmErr := f.vm.nextAsyncParkSequence(rendezvous.receiver); vmErr != nil {
		t.Fatalf("advance receiver sequence: %v", vmErr)
	}
	if _, vmErr := f.vm.stageReservedChannelSend(
		rendezvous.reservation, MakeComposite(source),
	); vmErr == nil {
		t.Fatal("stage accepted a changed receiver sequence")
	}
	if err := f.sender.scratch.canRelease(source); err != nil {
		t.Fatalf("failed stage changed source ownership: %v", err)
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("failed stage left %d source owners, want 1", got)
	}
	rendezvous.reservation.Abort()
	f.vm.dropValue(MakeComposite(source))
	requireFreedOnce(t, f, handle, before)
}

func TestRoutedRendezvousSequenceChangeLeavesStagedSourceUnchanged(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	exec := asyncrt.NewExecutor[asyncPayload](asyncrt.Config{Deterministic: true})
	rendezvous := reserveVMRendezvous(t, f, exec)
	source, handle := buildOwnedNode(t, f, "routed source unchanged")
	before := f.vm.heapCounters.freeCount
	selectOwner, vmErr := f.vm.asyncSelectOwner(rendezvous.receiver, 9, f.node)
	if vmErr != nil {
		t.Fatalf("select owner: %v", vmErr)
	}
	staged, vmErr := f.vm.stageAsyncPayloadInto(selectOwner, asyncSlotSelect, MakeComposite(source))
	if vmErr != nil {
		t.Fatalf("stage selected source: %v", vmErr)
	}
	if _, vmErr := f.vm.nextAsyncParkSequence(rendezvous.receiver); vmErr != nil {
		t.Fatalf("advance receiver sequence: %v", vmErr)
	}
	if _, vmErr := f.vm.routeAsyncPayloadToChannel(rendezvous.reservation, staged); vmErr == nil {
		t.Fatal("route accepted a changed receiver sequence")
	}
	if !f.vm.asyncPayloadHoldsStorage(staged) {
		t.Fatal("failed route consumed its staged source")
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("failed route left %d source owners, want 1", got)
	}
	rendezvous.reservation.Abort()
	f.vm.dropAsyncPayload(staged)
	f.vm.destroyAsyncTaskOwners(rendezvous.receiver)
	requireFreedOnce(t, f, handle, before)
}

func TestReservedRendezvousTerminalOwnerEventsLeavePayloadToVM(t *testing.T) {
	events := []struct {
		name string
		run  func(*asyncrt.Executor[asyncPayload], vmRendezvousReservation)
	}{
		{name: "cancel", run: func(exec *asyncrt.Executor[asyncPayload], r vmRendezvousReservation) {
			exec.Cancel(r.receiver)
		}},
		{name: "external-wake", run: func(exec *asyncrt.Executor[asyncPayload], r vmRendezvousReservation) {
			exec.Wake(r.receiver)
		}},
		{name: "drain", run: func(exec *asyncrt.Executor[asyncPayload], _ vmRendezvousReservation) {
			exec.DrainTasks()
		}},
	}
	for _, event := range events {
		t.Run(event.name, func(t *testing.T) {
			f := newAsyncPayloadFixture(t)
			exec := asyncrt.NewExecutor[asyncPayload](asyncrt.Config{Deterministic: true})
			rendezvous := reserveVMRendezvous(t, f, exec)
			source, handle := buildOwnedNode(t, f, event.name)
			before := f.vm.heapCounters.freeCount
			payload, vmErr := f.vm.stageReservedChannelSend(
				rendezvous.reservation, MakeComposite(source),
			)
			if vmErr != nil {
				t.Fatalf("stage payload: %v", vmErr)
			}
			event.run(exec, rendezvous)
			if completed, committed := rendezvous.reservation.Commit(payload); completed || committed {
				t.Fatalf("terminal-owner commit = (%t, %t), want rejected", completed, committed)
			}
			if f.freed(t, handle) {
				t.Fatal("control event released VM-owned payload")
			}
			f.vm.dropAsyncPayload(payload)
			f.vm.destroyAsyncTaskOwners(rendezvous.receiver)
			requireFreedOnce(t, f, handle, before)
		})
	}
}
