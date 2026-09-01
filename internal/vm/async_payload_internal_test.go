package vm

import (
	"testing"

	"surge/internal/mir"
)

type asyncPayloadFixture struct {
	*storageFixture
	sender   *Frame
	receiver *Frame
	owner    *asyncOwnerRegion
}

func newAsyncPayloadFixture(t *testing.T) *asyncPayloadFixture {
	t.Helper()
	f := newStorageFixture(t)
	fn := &mir.Func{
		Locals: []mir.Local{{Name: "payload", Type: f.node}},
		Blocks: []mir.Block{{}}, Entry: 0,
	}
	sender := f.vm.activate(fn)
	receiver := f.vm.activate(fn)
	f.vm.Stack = []*Frame{sender}
	if vmErr := f.vm.registerAsyncChannelOwner(1, f.node, 2); vmErr != nil {
		t.Fatalf("registering a channel owner must succeed: %v", vmErr)
	}
	owner, vmErr := f.vm.asyncChannelOwner(1)
	if vmErr != nil {
		t.Fatalf("looking up a channel owner must succeed: %v", vmErr)
	}
	return &asyncPayloadFixture{storageFixture: f, sender: sender, receiver: receiver, owner: owner}
}

func (f *asyncPayloadFixture) send(t *testing.T, label string) (asyncPayload, Handle) {
	t.Helper()
	built, vmErr := f.vm.buildComposite(f.sender, f.node)
	if vmErr != nil {
		t.Fatalf("building a payload must succeed: %v", vmErr)
	}
	handle := f.writeNode(t, built, 1, 2, 3, label)
	payload, vmErr := f.vm.stageAsyncPayloadInto(f.owner, asyncSlotRing, MakeComposite(built))
	if vmErr != nil {
		t.Fatalf("staging a payload must succeed: %v", vmErr)
	}
	return payload, handle
}

func (f *asyncPayloadFixture) refCount(t *testing.T, handle Handle) uint32 {
	t.Helper()
	obj, ok := f.vm.Heap.lookup(handle)
	if !ok || obj == nil {
		t.Fatalf("handle %d names no object", handle)
	}
	return obj.RefCount
}

func (f *asyncPayloadFixture) freed(t *testing.T, handle Handle) bool {
	t.Helper()
	obj, ok := f.vm.Heap.lookup(handle)
	if !ok || obj == nil {
		t.Fatalf("handle %d names no object", handle)
	}
	return obj.Freed
}

func TestAsyncPayloadMovesIntoCallerStorageThenSourceExpires(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	claim, handle := f.send(t, "payload")
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("staged payload has %d owners, want 1", got)
	}

	f.vm.Stack = []*Frame{f.receiver}
	destination, vmErr := f.vm.buildComposite(f.receiver, f.node)
	if vmErr != nil {
		t.Fatalf("building caller destination must succeed: %v", vmErr)
	}
	if vmErr := f.vm.moveAsyncPayloadIntoStorage(claim, destination); vmErr != nil {
		t.Fatalf("moving into caller storage must succeed: %v", vmErr)
	}
	if destination.Arena != f.receiver.scratch.arena || destination.Arena == &f.owner.arena {
		t.Fatal("the payload did not land in caller-owned storage")
	}
	if vmErr := f.vm.moveAsyncPayloadIntoStorage(claim, destination); vmErr == nil {
		t.Fatal("a terminal claim was accepted twice")
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("direct move left %d owners, want 1", got)
	}
	f.vm.dropValue(MakeComposite(destination))
	if !f.freed(t, handle) {
		t.Fatal("caller-owned destination did not release its payload")
	}
}

func TestAsyncPayloadSurvivesItsProducerActivation(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	claim, handle := f.send(t, "outlives")
	if vmErr := f.vm.retireActivation(f.sender); vmErr != nil {
		t.Fatalf("retiring the sender must succeed: %v", vmErr)
	}
	f.vm.Stack = []*Frame{f.receiver}
	destination, vmErr := f.vm.buildComposite(f.receiver, f.node)
	if vmErr != nil {
		t.Fatal(vmErr)
	}
	if vmErr := f.vm.moveAsyncPayloadIntoStorage(claim, destination); vmErr != nil {
		t.Fatalf("payload did not outlive producer: %v", vmErr)
	}
	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.readCell(t, destination, members[2]); got.Int != 3 {
		t.Fatalf("surviving payload reads %d, want 3", got.Int)
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("surviving payload has %d owners, want 1", got)
	}
	f.vm.dropValue(MakeComposite(destination))
}

func TestAsyncPayloadCancelAndShutdownDropExactlyOnce(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	first, firstHandle := f.send(t, "cancelled")
	second, secondHandle := f.send(t, "shutdown")
	f.vm.dropAsyncPayload(first)
	f.vm.dropAsyncPayload(first)
	f.vm.destroyAsyncChannelOwner(1)
	if !f.freed(t, firstHandle) || !f.freed(t, secondHandle) {
		t.Fatal("cancel/shutdown did not release both exact owner slots")
	}
	if vmErr := f.vm.moveAsyncPayloadIntoStorage(second, StorageRef{}); vmErr == nil {
		t.Fatal("a claim survived destruction of its concrete owner")
	}
}
