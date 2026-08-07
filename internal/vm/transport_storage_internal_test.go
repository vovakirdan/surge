package vm

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// The round trip a transported value makes, and the two ways it can end early.
//
// Every one of these asserts on a REFCOUNT rather than on a value read back,
// because what a transport gets wrong is not the bytes — it is how many owners
// they have. A payload with two owners is freed twice and a payload with none
// is leaked, and both of those are invisible to a test that only checks the
// value arrived.
//
// Exactly-once is asserted from both sides, and it is worth saying how, because
// "exactly once" is easy to claim and easy to test at only one bound. Heap.Release
// panics on an object that is already freed, so a release that ran twice cannot
// pass these tests silently — surviving the call is the upper bound. And the
// object's Freed flag is the lower bound. Together they are the whole claim.

// transportFixture is a Node — a struct with a string member — plus a frame to
// receive into. The string is the whole point: it has a refcount, so every
// handover it makes is countable.
type transportFixture struct {
	*storageFixture
	sender   *Frame
	receiver *Frame
}

func newTransportFixture(t *testing.T) *transportFixture {
	t.Helper()
	f := newStorageFixture(t)
	fn := &mir.Func{
		Locals: []mir.Local{{Name: "payload", Type: f.node}},
		Blocks: []mir.Block{{}},
		Entry:  0,
	}
	sender := f.vm.activate(fn)
	receiver := f.vm.activate(fn)
	f.vm.Stack = []*Frame{sender}
	return &transportFixture{storageFixture: f, sender: sender, receiver: receiver}
}

// send builds a payload in the SENDER's scratch, the way a producer does, and
// hands it to the transport. The returned handle is the string it carries.
func (f *transportFixture) send(t *testing.T, label string) (Value, Handle) {
	t.Helper()
	built, vmErr := f.vm.buildComposite(f.sender, f.node)
	if vmErr != nil {
		t.Fatalf("building a payload must succeed: %v", vmErr)
	}
	handle := f.writeNode(t, built, 1, 2, 3, label)
	inFlight, vmErr := f.vm.transportCopyIn(MakeComposite(built))
	if vmErr != nil {
		t.Fatalf("copy-in must succeed: %v", vmErr)
	}
	return inFlight, handle
}

func (f *transportFixture) refCount(t *testing.T, handle Handle) uint32 {
	t.Helper()
	obj, ok := f.vm.Heap.lookup(handle)
	if !ok || obj == nil {
		t.Fatalf("handle %d names no object", handle)
	}
	return obj.RefCount
}

func (f *transportFixture) freed(t *testing.T, handle Handle) bool {
	t.Helper()
	obj, ok := f.vm.Heap.lookup(handle)
	if !ok || obj == nil {
		t.Fatalf("handle %d names no object", handle)
	}
	return obj.Freed
}

// A payload keeps ONE owner across the whole round trip, and the owner moves.
// If copy-in duplicated without giving up the source the count would climb; if
// it gave up the source without duplicating, the count would fall to zero and
// the receive would read freed storage.
func TestTransportRoundTripKeepsOneOwner(t *testing.T) {
	f := newTransportFixture(t)

	inFlight, handle := f.send(t, "payload")
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("after copy-in the payload has %d owners, want 1", got)
	}

	received, vmErr := f.vm.transportCopyOut(f.receiver, inFlight)
	if vmErr != nil {
		t.Fatalf("copy-out must succeed: %v", vmErr)
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("after copy-out the payload has %d owners, want 1", got)
	}

	// And the owner is the RECEIVER's storage, not the transport's and not the
	// sender's — which is what makes the value safe to use after both of those
	// have gone.
	ref, isComposite := received.Storage()
	if !isComposite {
		t.Fatalf("the received payload is carried as %s, want composite", received.Kind)
	}
	if ref.Arena != f.receiver.scratch.arena {
		t.Fatal("the received payload does not live in the receiving activation's storage")
	}

	f.vm.dropValue(received)
	if !f.freed(t, handle) {
		t.Fatal("dropping the received payload must release what it held")
	}
}

// The defect this exists to prevent: a payload outliving the activation that
// sent it. The sender's storage is retired between the send and the receive,
// exactly as it is when a task suspends, and the value must still be readable.
func TestTransportedValueSurvivesTheActivationThatSentIt(t *testing.T) {
	f := newTransportFixture(t)

	inFlight, handle := f.send(t, "outlives")

	// The sending activation leaves. Retiring bumps the generation, which is
	// what makes a reference into its arena refuse to resolve rather than read
	// whatever occupies those bytes next.
	if vmErr := f.vm.retireActivation(f.sender); vmErr != nil {
		t.Fatalf("retiring the sender must succeed: %v", vmErr)
	}
	f.vm.Stack = []*Frame{f.receiver}

	received, vmErr := f.vm.transportCopyOut(f.receiver, inFlight)
	if vmErr != nil {
		t.Fatalf("a payload must outlive its sender, got: %v", vmErr)
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("the surviving payload has %d owners, want 1", got)
	}
	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	ref, _ := received.Storage()
	if got := f.readCell(t, ref, members[2]); got.Int != 3 {
		t.Fatalf("the surviving payload reads back %d, want 3", got.Int)
	}

	f.vm.dropValue(received)
}

// Cancel-before-receive: the payload reached a task that will never run the
// receive. It is released where the cancellation is noticed, exactly once.
func TestTransportCancelBeforeReceiveReleasesExactlyOnce(t *testing.T) {
	f := newTransportFixture(t)

	inFlight, handle := f.send(t, "cancelled")
	if f.freed(t, handle) {
		t.Fatal("the payload was released before anything cancelled")
	}

	f.vm.transportRelease(inFlight)

	if !f.freed(t, handle) {
		t.Fatal("a cancelled receive must release the payload it will never read")
	}
	if got := f.refCount(t, handle); got != 0 {
		t.Fatalf("the released payload still has %d owners", got)
	}
}

// Drop-without-receive: the payload is still in the runtime's hold when the
// program ends, and the shutdown drain is what releases it — through the same
// helper and with the same exactly-once obligation.
func TestTransportDropWithoutReceiveReleasesExactlyOnce(t *testing.T) {
	f := newTransportFixture(t)

	first, firstHandle := f.send(t, "queued-a")
	second, secondHandle := f.send(t, "queued-b")

	// The sender is gone before the drain runs, which is the real ordering: at
	// shutdown every activation has left and only the runtime's hold remains.
	// Retiring rewinds the sender's scratch, and a rewind DROPS what is still
	// live there — so this is also where a payload that never left the sender's
	// arena is released by the wrong owner, at the wrong time, and then either
	// released again by the drain or silently not at all.
	if vmErr := f.vm.retireActivation(f.sender); vmErr != nil {
		t.Fatalf("retiring the sender must succeed: %v", vmErr)
	}
	f.vm.Stack = nil

	// ZERO releases so far. The sender gave the payloads up at copy-in, so its
	// retirement has nothing of theirs to reclaim. This is the half of
	// exactly-once that says "not yet".
	for _, handle := range []Handle{firstHandle, secondHandle} {
		if f.freed(t, handle) {
			t.Fatalf("handle %d was released by the sender leaving, not by the drain", handle)
		}
	}

	// One pass, each payload exactly once. A second pass over the same values
	// would panic in Heap.Release rather than pass quietly, which is the other
	// half.
	for _, payload := range []Value{first, second} {
		f.vm.transportRelease(payload)
	}

	for _, handle := range []Handle{firstHandle, secondHandle} {
		if !f.freed(t, handle) {
			t.Fatalf("handle %d survived the drain that was supposed to release it", handle)
		}
	}
}

// A non-composite payload crosses the boundary UNCHANGED, in both directions.
// It already outlives its producer — a handle governs its own lifetime — so
// copying it would be a second owner nobody asked for.
func TestTransportLeavesAHandlePayloadAlone(t *testing.T) {
	f := newTransportFixture(t)

	handle := f.vm.Heap.AllocString(types.NoTypeID, "not a composite")
	payload := MakeHandleString(handle, types.NoTypeID)

	inFlight, vmErr := f.vm.transportCopyIn(payload)
	if vmErr != nil {
		t.Fatalf("copy-in must succeed: %v", vmErr)
	}
	if inFlight != payload {
		t.Fatalf("copy-in changed a handle payload: %v", inFlight)
	}
	received, vmErr := f.vm.transportCopyOut(f.receiver, inFlight)
	if vmErr != nil {
		t.Fatalf("copy-out must succeed: %v", vmErr)
	}
	if received != payload {
		t.Fatalf("copy-out changed a handle payload: %v", received)
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("a handle payload gained owners in transit: %d", got)
	}

	f.vm.dropValue(received)
	if !f.freed(t, handle) {
		t.Fatal("the handle payload must still be releasable after the round trip")
	}
}
