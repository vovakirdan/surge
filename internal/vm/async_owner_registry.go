package vm

import (
	"surge/internal/asyncrt"
	"surge/internal/types"
)

type asyncSelectOwnerKey struct {
	task   asyncrt.TaskID
	region uint32
}

type asyncResumeOwnerKey struct {
	task   asyncrt.TaskID
	region uint32
}

type asyncResumeTypeKey struct {
	task   asyncrt.TaskID
	typeID types.TypeID
}

type asyncOwnerRegistry struct {
	channels         map[asyncrt.ChannelID]*asyncOwnerRegion
	tasks            map[asyncrt.TaskID]*asyncOwnerRegion
	selects          map[asyncSelectOwnerKey]*asyncOwnerRegion
	resumes          map[asyncResumeOwnerKey]*asyncOwnerRegion
	resumeTypes      map[asyncResumeTypeKey]uint32
	nextResumeRegion map[asyncrt.TaskID]uint32
	parkSeq          map[asyncrt.TaskID]uint64
	generations      map[asyncOwnerID]uint32
	nextGeneration   uint32
}

func (vm *VM) assignAsyncOwnerGeneration(owner *asyncOwnerRegion) *VMError {
	if owner == nil {
		return vm.eb.invalidLocation("async owner generation has no owner")
	}
	if vm.asyncOwners.generations == nil {
		vm.asyncOwners.generations = make(map[asyncOwnerID]uint32)
	}
	generation := vm.asyncOwners.nextGeneration + 1
	if generation == 0 {
		return vm.eb.invalidLocation("async owner generation exhausted")
	}
	vm.asyncOwners.nextGeneration = generation
	vm.asyncOwners.generations[owner.id] = generation
	owner.generation = generation
	return nil
}

func (vm *VM) runtimeHandlePayloadType(typeID types.TypeID, what string) (types.TypeID, *VMError) {
	if vm == nil || vm.Types == nil {
		return types.NoTypeID, vm.eb.makeError(PanicTypeMismatch, what+" has no type registry")
	}
	payloads, ok := vm.Types.RuntimeHandlePayloads(typeID)
	if !ok || len(payloads) != 1 || payloads[0] == types.NoTypeID {
		return types.NoTypeID, vm.eb.makeError(PanicTypeMismatch, what+" has no exact payload type")
	}
	return payloads[0], nil
}

func (vm *VM) registerAsyncChannelOwner(
	id asyncrt.ChannelID,
	typeID types.TypeID,
	capacity uint64,
) *VMError {
	if existing := vm.asyncOwners.channels[id]; existing != nil && !existing.retired {
		return vm.eb.invalidLocation("channel already has an exact payload owner")
	}
	initial := uint64(4)
	if capacity+2 > initial {
		initial = capacity + 2
	}
	if initial > uint64(maxIntValue()) {
		return vm.eb.invalidLocation("channel payload capacity overflows")
	}
	owner, vmErr := vm.newAsyncOwnerRegion(asyncOwnerID{
		kind: asyncOwnerChannel, id: uint64(id),
	}, typeID, int(initial))
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.assignAsyncOwnerGeneration(owner); vmErr != nil {
		return vmErr
	}
	if vm.asyncOwners.channels == nil {
		vm.asyncOwners.channels = make(map[asyncrt.ChannelID]*asyncOwnerRegion)
	}
	vm.asyncOwners.channels[id] = owner
	return nil
}

func (vm *VM) asyncChannelOwner(id asyncrt.ChannelID) (*asyncOwnerRegion, *VMError) {
	owner := vm.asyncOwners.channels[id]
	if owner == nil {
		return nil, vm.eb.invalidLocation("channel has no exact payload owner")
	}
	return owner, nil
}

func (vm *VM) registerAsyncTaskOwner(id asyncrt.TaskID, typeID types.TypeID) *VMError {
	if existing := vm.asyncOwners.tasks[id]; existing != nil && !existing.retired {
		return vm.eb.invalidLocation("task already has an exact result owner")
	}
	owner, vmErr := vm.newAsyncOwnerRegion(asyncOwnerID{
		kind: asyncOwnerTaskResult, id: uint64(id),
	}, typeID, 1)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.assignAsyncOwnerGeneration(owner); vmErr != nil {
		return vmErr
	}
	if vm.asyncOwners.tasks == nil {
		vm.asyncOwners.tasks = make(map[asyncrt.TaskID]*asyncOwnerRegion)
	}
	vm.asyncOwners.tasks[id] = owner
	return nil
}

func (vm *VM) asyncTaskOwner(id asyncrt.TaskID) (*asyncOwnerRegion, *VMError) {
	owner := vm.asyncOwners.tasks[id]
	if owner == nil {
		return nil, vm.eb.invalidLocation("task has no exact result owner")
	}
	return owner, nil
}

func (vm *VM) asyncSelectOwner(
	task asyncrt.TaskID,
	arm uint32,
	typeID types.TypeID,
) (*asyncOwnerRegion, *VMError) {
	key := asyncSelectOwnerKey{task: task, region: arm}
	if owner := vm.asyncOwners.selects[key]; owner != nil {
		if owner.typeID == typeID {
			return owner, nil
		}
		if !asyncOwnerIsEmpty(owner) {
			return nil, vm.eb.invalidLocation("select arm changed type while its slot is live")
		}
		vm.destroyAsyncOwner(owner)
		delete(vm.asyncOwners.generations, owner.id)
		delete(vm.asyncOwners.selects, key)
	}
	owner, vmErr := vm.newAsyncOwnerRegion(asyncOwnerID{
		kind: asyncOwnerSelect, id: uint64(task), arm: arm,
	}, typeID, 1)
	if vmErr != nil {
		return nil, vmErr
	}
	if vmErr := vm.assignAsyncOwnerGeneration(owner); vmErr != nil {
		return nil, vmErr
	}
	if vm.asyncOwners.selects == nil {
		vm.asyncOwners.selects = make(map[asyncSelectOwnerKey]*asyncOwnerRegion)
	}
	vm.asyncOwners.selects[key] = owner
	return owner, nil
}

func (vm *VM) asyncResumeOwner(
	task asyncrt.TaskID,
	typeID types.TypeID,
) (*asyncOwnerRegion, *VMError) {
	typeKey := asyncResumeTypeKey{task: task, typeID: typeID}
	region := vm.asyncOwners.resumeTypes[typeKey]
	if region == 0 {
		if vm.asyncOwners.nextResumeRegion == nil {
			vm.asyncOwners.nextResumeRegion = make(map[asyncrt.TaskID]uint32)
		}
		region = vm.asyncOwners.nextResumeRegion[task] + 1
		if region == 0 {
			return nil, vm.eb.invalidLocation("resume owner region overflows")
		}
		vm.asyncOwners.nextResumeRegion[task] = region
		if vm.asyncOwners.resumeTypes == nil {
			vm.asyncOwners.resumeTypes = make(map[asyncResumeTypeKey]uint32)
		}
		vm.asyncOwners.resumeTypes[typeKey] = region
	}
	key := asyncResumeOwnerKey{task: task, region: region}
	if owner := vm.asyncOwners.resumes[key]; owner != nil {
		return owner, nil
	}
	owner, vmErr := vm.newAsyncOwnerRegion(asyncOwnerID{
		kind: asyncOwnerResume, id: uint64(task), arm: region,
	}, typeID, 1)
	if vmErr != nil {
		return nil, vmErr
	}
	if vmErr := vm.assignAsyncOwnerGeneration(owner); vmErr != nil {
		return nil, vmErr
	}
	if vm.asyncOwners.resumes == nil {
		vm.asyncOwners.resumes = make(map[asyncResumeOwnerKey]*asyncOwnerRegion)
	}
	vm.asyncOwners.resumes[key] = owner
	return owner, nil
}

func (vm *VM) nextAsyncParkSequence(task asyncrt.TaskID) (uint64, *VMError) {
	if vm.asyncOwners.parkSeq == nil {
		vm.asyncOwners.parkSeq = make(map[asyncrt.TaskID]uint64)
	}
	if vm.asyncOwners.parkSeq[task] == ^uint64(0) {
		return 0, vm.eb.invalidLocation("async park sequence exhausted")
	}
	vm.asyncOwners.parkSeq[task]++
	return vm.asyncOwners.parkSeq[task], nil
}

func (vm *VM) currentAsyncParkSequence(task asyncrt.TaskID) uint64 {
	return vm.asyncOwners.parkSeq[task]
}

func (vm *VM) lookupAsyncOwner(payload asyncPayload) *asyncOwnerRegion {
	switch payload.ownerKind {
	case asyncOwnerChannel:
		return vm.asyncOwners.channels[asyncrt.ChannelID(payload.ownerID)]
	case asyncOwnerTaskResult:
		return vm.asyncOwners.tasks[asyncrt.TaskID(payload.ownerID)]
	case asyncOwnerSelect:
		return vm.asyncOwners.selects[asyncSelectOwnerKey{
			task: asyncrt.TaskID(payload.ownerID), region: payload.region,
		}]
	case asyncOwnerResume:
		return vm.asyncOwners.resumes[asyncResumeOwnerKey{
			task: asyncrt.TaskID(payload.ownerID), region: payload.region,
		}]
	default:
		return nil
	}
}
