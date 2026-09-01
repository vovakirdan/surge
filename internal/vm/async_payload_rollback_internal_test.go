package vm

import "testing"

func TestFailedAsyncStageKeepsSourceOwnedAndDestinationEmpty(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	source, vmErr := f.vm.buildComposite(f.sender, f.node)
	if vmErr != nil {
		t.Fatalf("building the source must succeed: %v", vmErr)
	}
	handle := f.writeNode(t, source, 1, 2, 3, "must roll back")

	// Retiring the scratch entry first injects the only local error that used
	// to follow a successful storageMoveInit. The stage must now reject before
	// moving, so the caller still owns source and the owner slot stays empty.
	if err := f.sender.scratch.release(source); err != nil {
		t.Fatalf("fault injection must retire the scratch entry: %v", err)
	}
	if _, vmErr := f.vm.stageAsyncPayloadInto(
		f.owner,
		asyncSlotRing,
		MakeComposite(source),
	); vmErr == nil {
		t.Fatal("staging must report the injected scratch handoff failure")
	}

	for i := range f.owner.slots {
		if state := f.owner.slots[i].state; state != asyncPayloadEmpty {
			t.Fatalf("owner slot %d state = %d, want EMPTY", i, state)
		}
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("failed stage left %d source owners, want 1", got)
	}

	f.vm.dropValue(MakeComposite(source))
	f.vm.destroyAsyncChannelOwner(1)
	if !f.freed(t, handle) {
		t.Fatal("failed stage did not leave ownership with the source")
	}
}
