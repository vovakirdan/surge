package vm

import (
	"testing"

	"surge/internal/asyncrt"
)

func TestReservedTrySendErrorReleasesItsSourceValue(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	exec := asyncrt.NewExecutor[asyncPayload](asyncrt.Config{Deterministic: true})
	id := exec.ChanNew(1)
	handle := f.vm.Heap.AllocString(f.text, "wrong payload type")
	reservation, ready := exec.ChanReserveTrySend(id)
	if !ready {
		t.Fatal("empty buffered channel did not reserve a send")
	}
	if _, vmErr := f.vm.commitReservedTrySend(
		exec,
		reservation, MakeHandleString(handle, f.text),
	); vmErr == nil {
		t.Fatal("wrong payload type was accepted")
	}
	if !f.freed(t, handle) {
		t.Fatal("failed try_send kept ownership of its evaluated source")
	}
}

func TestRefillErrorDropsAlreadyPoppedRingPayload(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	exec := asyncrt.NewExecutor[asyncPayload](asyncrt.Config{Deterministic: true})
	id := exec.ChanNew(1)
	buffered, bufferedHandle := f.send(t, "buffered")
	if !exec.ChanTrySend(id, buffered) {
		t.Fatal("failed to fill channel ring")
	}

	sender := exec.Spawn(1, nil)
	exec.SetCurrent(sender)
	if vmErr := f.vm.registerAsyncTaskOwner(sender, f.text); vmErr != nil {
		t.Fatalf("register foreign owner: %v", vmErr)
	}
	foreignHandle := f.vm.Heap.AllocString(f.text, "foreign parked payload")
	foreign, vmErr := f.vm.stageAsyncTaskResult(sender, MakeHandleString(foreignHandle, f.text))
	if vmErr != nil {
		t.Fatalf("stage foreign payload: %v", vmErr)
	}
	if exec.ChanSendOrPark(id, foreign) {
		t.Fatal("full ring did not park sender")
	}

	if _, _, vmErr := f.vm.tryReceiveAsyncChannel(exec, id); vmErr == nil {
		t.Fatal("foreign refill type was accepted")
	}
	if !f.freed(t, bufferedHandle) {
		t.Fatal("refill error lost the already-popped ring obligation")
	}
	if f.freed(t, foreignHandle) {
		t.Fatal("failed refill destroyed the requeued sender payload")
	}
	for _, payload := range exec.ChanDestroy(id) {
		f.vm.dropAsyncPayload(payload)
	}
	if !f.freed(t, foreignHandle) {
		t.Fatal("parked payload was not recoverable after failed refill")
	}
}
