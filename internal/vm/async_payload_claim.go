package vm

import (
	"fmt"
	"math"
)

func (vm *VM) inspectAsyncPayload(
	payload asyncPayload,
) (*asyncOwnerRegion, *asyncPayloadSlot, StorageRef, *VMError) {
	owner := vm.lookupAsyncOwner(payload)
	registeredGeneration, registered := vm.asyncOwners.generations[ownerIDOf(owner)]
	if owner == nil || owner.retired || owner.id.kind != payload.ownerKind ||
		owner.id.id != payload.ownerID || owner.id.arm != payload.region ||
		!registered || registeredGeneration != owner.generation ||
		owner.generation != payload.ownerGeneration {
		return nil, nil, StorageRef{}, vm.eb.makeError(PanicUseAfterMove, "async payload owner capability mismatch")
	}
	index := int(payload.index)
	if index < 0 || index >= len(owner.slots) {
		return nil, nil, StorageRef{}, vm.eb.makeError(PanicUseAfterMove, "async payload slot is out of range")
	}
	slot := &owner.slots[index]
	if slot.generation != payload.slotGeneration {
		return nil, nil, StorageRef{}, vm.eb.makeError(PanicUseAfterMove, "async payload generation is stale")
	}
	if !asyncSlotRoleAllowed(owner.id.kind, slot.role) {
		return nil, nil, StorageRef{}, vm.eb.makeError(PanicUseAfterMove, "async payload slot role mismatches its owner")
	}
	if slot.parkSeq != payload.parkSeq {
		return nil, nil, StorageRef{}, vm.eb.makeError(PanicUseAfterMove, "async payload park sequence is stale")
	}
	ref, err := vm.asyncSlotRef(owner, index)
	if err != nil {
		return nil, nil, StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return owner, slot, ref, nil
}

func ownerIDOf(owner *asyncOwnerRegion) asyncOwnerID {
	if owner == nil {
		return asyncOwnerID{}
	}
	return owner.id
}

func (vm *VM) claimAsyncPayload(
	payload asyncPayload,
) (*asyncOwnerRegion, *asyncPayloadSlot, StorageRef, *VMError) {
	owner, slot, ref, vmErr := vm.inspectAsyncPayload(payload)
	if vmErr != nil {
		return nil, nil, StorageRef{}, vmErr
	}
	if slot.state != asyncPayloadInitialized {
		return nil, nil, StorageRef{}, vm.eb.makeError(PanicUseAfterMove,
			fmt.Sprintf("async payload slot is %d, not initialized", slot.state))
	}
	slot.state = asyncPayloadClaimed
	return owner, slot, ref, nil
}

func (vm *VM) abortAsyncReservation(owner *asyncOwnerRegion, slot *asyncPayloadSlot) {
	if owner == nil || slot == nil {
		return
	}
	vm.finishAsyncClaim(owner, slot, asyncPayloadDropped)
}

func (vm *VM) finishAsyncClaim(
	owner *asyncOwnerRegion,
	slot *asyncPayloadSlot,
	terminal asyncPayloadState,
) {
	if owner == nil || slot == nil {
		return
	}
	slot.state = terminal
	if slot.generation == math.MaxUint32 {
		slot.role = 0
		slot.parkSeq = 0
		slot.state = asyncPayloadExhausted
		if owner.retiring && !owner.destroying {
			vm.retireAsyncOwnerIfQuiescent(owner)
		}
		return
	}
	slot.generation++
	slot.role = 0
	slot.parkSeq = 0
	slot.state = asyncPayloadEmpty
	if owner.retiring && !owner.destroying {
		vm.retireAsyncOwnerIfQuiescent(owner)
	}
}
