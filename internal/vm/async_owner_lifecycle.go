package vm

import "surge/internal/asyncrt"

func (vm *VM) stageAsyncTaskResult(id asyncrt.TaskID, value Value) (asyncPayload, *VMError) {
	owner, vmErr := vm.asyncTaskOwner(id)
	if vmErr != nil {
		return asyncPayload{}, vmErr
	}
	return vm.stageAsyncPayloadInto(owner, asyncSlotTaskResult, value)
}

func (vm *VM) destroyAsyncChannelOwner(id asyncrt.ChannelID) {
	owner := vm.asyncOwners.channels[id]
	vm.destroyAsyncOwner(owner)
	if owner != nil {
		delete(vm.asyncOwners.generations, owner.id)
	}
	delete(vm.asyncOwners.channels, id)
}

func (vm *VM) destroyAsyncTaskOwners(id asyncrt.TaskID) {
	taskOwner := vm.asyncOwners.tasks[id]
	vm.destroyAsyncOwner(taskOwner)
	if taskOwner != nil {
		delete(vm.asyncOwners.generations, taskOwner.id)
	}
	delete(vm.asyncOwners.tasks, id)
	delete(vm.asyncOwners.parkSeq, id)
	delete(vm.asyncOwners.nextResumeRegion, id)
	for key, owner := range vm.asyncOwners.selects {
		if key.task == id {
			vm.destroyAsyncOwner(owner)
			delete(vm.asyncOwners.generations, owner.id)
			delete(vm.asyncOwners.selects, key)
		}
	}
	for key, owner := range vm.asyncOwners.resumes {
		if key.task == id {
			vm.destroyAsyncOwner(owner)
			delete(vm.asyncOwners.generations, owner.id)
			delete(vm.asyncOwners.resumes, key)
		}
	}
	for key := range vm.asyncOwners.resumeTypes {
		if key.task == id {
			delete(vm.asyncOwners.resumeTypes, key)
		}
	}
}

func (vm *VM) destroyAsyncOwner(owner *asyncOwnerRegion) {
	if owner == nil || owner.retired || owner.destroying {
		return
	}
	owner.retiring = true
	owner.destroying = true
	for i := range owner.slots {
		slot := &owner.slots[i]
		switch slot.state {
		case asyncPayloadReserved:
			vm.finishAsyncClaim(owner, slot, asyncPayloadDropped)
		case asyncPayloadInitialized:
			vm.dropAsyncPayload(asyncPayload{
				ownerKind: owner.id.kind, ownerID: owner.id.id,
				ownerGeneration: owner.generation, region: owner.id.arm,
				index: uint32(i), slotGeneration: slot.generation,
				parkSeq: slot.parkSeq,
			})
		case asyncPayloadClaimed:
			// The single-threaded VM cannot start another claim here. A claim
			// already executing keeps the arena alive until its terminal commit.
		case asyncPayloadExhausted:
			// No bytes remain live; this slot is terminally unavailable so its
			// generation can never wrap into an ABA-equivalent capability.
		default:
			continue
		}
	}
	owner.destroying = false
	vm.retireAsyncOwnerIfQuiescent(owner)
}

func (vm *VM) retireAsyncOwnerIfQuiescent(owner *asyncOwnerRegion) {
	if owner == nil || owner.retired || owner.destroying || !owner.retiring {
		return
	}
	for i := range owner.slots {
		switch owner.slots[i].state {
		case asyncPayloadClaimed:
			return
		case asyncPayloadEmpty, asyncPayloadExhausted:
			continue
		default:
			vm.panic(PanicUnimplemented, "async owner teardown left a live slot")
		}
	}
	owner.arena.retire()
	owner.retired = true
}

func asyncOwnerIsEmpty(owner *asyncOwnerRegion) bool {
	if owner == nil {
		return true
	}
	for i := range owner.slots {
		if owner.slots[i].state != asyncPayloadEmpty && owner.slots[i].state != asyncPayloadExhausted {
			return false
		}
	}
	return true
}
