package vm

import (
	"math"
	"testing"
)

func TestAsyncClaimMovesCompositeIntoCallerStorageThenExpires(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	if vmErr := f.vm.registerAsyncChannelOwner(41, f.node, 0); vmErr != nil {
		t.Fatalf("create channel owner: %v", vmErr)
	}
	owner, _ := f.vm.asyncChannelOwner(41)

	source, vmErr := f.vm.buildComposite(f.sender, f.node)
	if vmErr != nil {
		t.Fatalf("build source: %v", vmErr)
	}
	handle := f.writeNode(t, source, 1, 2, 3, "payload")
	claim, vmErr := f.vm.stageAsyncPayloadInto(owner, asyncSlotRing, MakeComposite(source))
	if vmErr != nil {
		t.Fatalf("stage channel payload: %v", vmErr)
	}

	f.vm.Stack = []*Frame{f.receiver}
	destination, vmErr := f.vm.buildComposite(f.receiver, f.node)
	if vmErr != nil {
		t.Fatalf("build caller destination: %v", vmErr)
	}
	if moveErr := f.vm.moveAsyncPayloadIntoStorage(claim, destination); moveErr != nil {
		t.Fatalf("move into caller destination: %v", moveErr)
	}
	if destination.Arena != f.receiver.scratch.arena {
		t.Fatal("the payload did not land in caller-owned storage")
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("direct move left %d owners, want 1", got)
	}
	if moveErr := f.vm.moveAsyncPayloadIntoStorage(claim, destination); moveErr == nil {
		t.Fatal("a terminal claim was accepted twice")
	}

	f.vm.Stack = []*Frame{f.sender}
	next, vmErr := f.vm.buildComposite(f.sender, f.node)
	if vmErr != nil {
		t.Fatalf("build next source: %v", vmErr)
	}
	nextHandle := f.writeNode(t, next, 4, 5, 6, "next")
	reused, vmErr := f.vm.stageAsyncPayloadInto(owner, asyncSlotRing, MakeComposite(next))
	if vmErr != nil {
		t.Fatalf("reuse channel slot: %v", vmErr)
	}
	if claim.index != reused.index || claim.slotGeneration == reused.slotGeneration {
		t.Fatalf("slot reuse = (%d,%d) then (%d,%d)",
			claim.index, claim.slotGeneration, reused.index, reused.slotGeneration)
	}
	f.vm.dropAsyncPayload(reused)
	f.vm.dropValue(MakeComposite(destination))
	if !f.freed(t, handle) || !f.freed(t, nextHandle) {
		t.Fatal("caller/source terminal drops did not release both payloads")
	}
}

func TestAsyncClaimRejectsARealOwnerSubstitution(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	if vmErr := f.vm.registerAsyncChannelOwner(7, f.node, 0); vmErr != nil {
		t.Fatalf("create channel owner: %v", vmErr)
	}
	channel, _ := f.vm.asyncChannelOwner(7)
	if vmErr := f.vm.registerAsyncTaskOwner(7, f.node); vmErr != nil {
		t.Fatalf("create task owner: %v", vmErr)
	}

	source, vmErr := f.vm.buildComposite(f.sender, f.node)
	if vmErr != nil {
		t.Fatalf("build source: %v", vmErr)
	}
	handle := f.writeNode(t, source, 1, 2, 3, "bound")
	claim, vmErr := f.vm.stageAsyncPayloadInto(channel, asyncSlotPark, MakeComposite(source))
	if vmErr != nil {
		t.Fatalf("stage channel payload: %v", vmErr)
	}
	forged := claim
	forged.ownerKind = asyncOwnerTaskResult

	f.vm.Stack = []*Frame{f.receiver}
	destination, vmErr := f.vm.buildComposite(f.receiver, f.node)
	if vmErr != nil {
		t.Fatalf("build destination: %v", vmErr)
	}
	if vmErr := f.vm.moveAsyncPayloadIntoStorage(forged, destination); vmErr == nil {
		t.Fatal("a channel claim forged as a task claim was accepted")
	}
	wrongRegion := claim
	wrongRegion.region++
	if vmErr := f.vm.moveAsyncPayloadIntoStorage(wrongRegion, destination); vmErr == nil {
		t.Fatal("a claim with a mutated owner-local region was accepted")
	}
	if got := f.refCount(t, handle); got != 1 {
		t.Fatalf("failed owner validation changed refcount to %d", got)
	}
	f.vm.dropAsyncPayload(claim)
	if !f.freed(t, handle) {
		t.Fatal("the validated source owner could no longer drop its payload")
	}
}

func TestAsyncOwnerTeardownInvalidatesReservedSlot(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	_, slot, destination, vmErr := f.vm.reserveAsyncPayload(f.owner, asyncSlotRing)
	if vmErr != nil {
		t.Fatalf("reserve owner slot: %v", vmErr)
	}
	f.vm.destroyAsyncChannelOwner(1)
	if !f.owner.retired || slot.state != asyncPayloadEmpty {
		t.Fatalf("reserved teardown left retired=%v state=%v", f.owner.retired, slot.state)
	}
	if vmErr := f.vm.initializeAsyncSlot(f.owner, destination, MakeComposite(destination)); vmErr == nil {
		t.Fatal("a retired reservation accepted initialization")
	}
}

func TestAsyncOwnerTeardownWaitsForActiveClaim(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	claim, handle := f.send(t, "claimed")
	owner, slot, source, vmErr := f.vm.claimAsyncPayload(claim)
	if vmErr != nil {
		t.Fatalf("claim payload: %v", vmErr)
	}
	f.vm.destroyAsyncChannelOwner(1)
	if owner.retired {
		t.Fatal("teardown retired bytes still held by an active claim")
	}
	if err := f.vm.storageDrop(source); err != nil {
		t.Fatalf("active claimant drop: %v", err)
	}
	f.vm.finishAsyncClaim(owner, slot, asyncPayloadDropped)
	if !owner.retired {
		t.Fatal("terminal claim did not complete deferred owner teardown")
	}
	if !f.freed(t, handle) {
		t.Fatal("deferred teardown lost the active claim's lifecycle obligation")
	}
}

func TestAsyncSlotGenerationExhaustionPoisonsOnlyThatQuiescentSlot(t *testing.T) {
	f := newAsyncPayloadFixture(t)
	const taskID = 99
	if vmErr := f.vm.registerAsyncTaskOwner(taskID, f.node); vmErr != nil {
		t.Fatalf("create task owner: %v", vmErr)
	}
	owner, vmErr := f.vm.asyncTaskOwner(taskID)
	if vmErr != nil {
		t.Fatalf("lookup task owner: %v", vmErr)
	}
	owner.slots[0].generation = math.MaxUint32

	f.vm.Stack = []*Frame{f.sender}
	source, vmErr := f.vm.buildComposite(f.sender, f.node)
	if vmErr != nil {
		t.Fatalf("build source: %v", vmErr)
	}
	firstHandle := f.writeNode(t, source, 1, 2, 3, "last generation")
	claim, vmErr := f.vm.stageAsyncTaskResult(taskID, MakeComposite(source))
	if vmErr != nil {
		t.Fatalf("stage last-generation result: %v", vmErr)
	}

	f.vm.Stack = []*Frame{f.receiver}
	destination, vmErr := f.vm.buildComposite(f.receiver, f.node)
	if vmErr != nil {
		t.Fatalf("build destination: %v", vmErr)
	}
	if moveErr := f.vm.moveAsyncPayloadIntoStorage(claim, destination); moveErr != nil {
		t.Fatalf("move last-generation result: %v", moveErr)
	}
	if owner.slots[0].state != asyncPayloadExhausted {
		t.Fatalf("last-generation slot state = %v, want exhausted", owner.slots[0].state)
	}

	f.vm.Stack = []*Frame{f.sender}
	nextSource, vmErr := f.vm.buildComposite(f.sender, f.node)
	if vmErr != nil {
		t.Fatalf("build next source: %v", vmErr)
	}
	nextHandle := f.writeNode(t, nextSource, 4, 5, 6, "replacement slot")
	next, vmErr := f.vm.stageAsyncTaskResult(taskID, MakeComposite(nextSource))
	if vmErr != nil {
		t.Fatalf("stage after slot exhaustion: %v", vmErr)
	}
	if next.index == claim.index || len(owner.slots) != 2 {
		t.Fatalf("post-exhaustion claim index=%d slots=%d, want new slot", next.index, len(owner.slots))
	}
	f.vm.dropAsyncPayload(next)
	f.vm.destroyAsyncTaskOwners(taskID)
	f.vm.dropValue(MakeComposite(destination))
	if !owner.retired || !f.freed(t, firstHandle) || !f.freed(t, nextHandle) {
		t.Fatal("exhausted-slot teardown lost liveness or a payload obligation")
	}
}
