package vm

// asyncPayload is a control-only capability. The bytes stay in the concrete
// channel, task, select, or resume owner named by ownerID.
type asyncPayload struct {
	ownerKind       asyncOwnerKind
	ownerID         uint64
	ownerGeneration uint32
	region          uint32
	index           uint32
	slotGeneration  uint32
	parkSeq         uint64
}

func (vm *VM) bindAsyncPayloadParkSequence(
	payload asyncPayload,
	sequence uint64,
) asyncPayload {
	owner := vm.lookupAsyncOwner(payload)
	if owner == nil || owner.id.arm != payload.region ||
		owner.generation != payload.ownerGeneration || int(payload.index) >= len(owner.slots) {
		vm.panic(PanicUseAfterMove, "async resume capability is stale")
		return asyncPayload{}
	}
	payload.parkSeq = sequence
	owner.slots[payload.index].parkSeq = sequence
	return payload
}

type asyncOwnerKind uint8

const (
	asyncOwnerChannel asyncOwnerKind = iota + 1
	asyncOwnerTaskResult
	asyncOwnerSelect
	asyncOwnerResume
)

type asyncOwnerID struct {
	kind asyncOwnerKind
	id   uint64
	arm  uint32
}

type asyncSlotRole uint8

const (
	asyncSlotRing asyncSlotRole = iota + 1
	asyncSlotPark
	asyncSlotTaskResult
	asyncSlotSelect
	asyncSlotResume
)

type asyncPayloadState uint8

const (
	asyncPayloadEmpty asyncPayloadState = iota
	asyncPayloadReserved
	asyncPayloadInitialized
	asyncPayloadClaimed
	asyncPayloadMoved
	asyncPayloadDropped
	asyncPayloadExhausted
)

type asyncPayloadSlot struct {
	state      asyncPayloadState
	generation uint32
	role       asyncSlotRole
	parkSeq    uint64
}

func asyncSlotRoleAllowed(kind asyncOwnerKind, role asyncSlotRole) bool {
	switch kind {
	case asyncOwnerChannel:
		return role == asyncSlotRing || role == asyncSlotPark
	case asyncOwnerTaskResult:
		return role == asyncSlotTaskResult
	case asyncOwnerSelect:
		return role == asyncSlotSelect
	case asyncOwnerResume:
		return role == asyncSlotResume
	default:
		return false
	}
}

// stageAsyncPayloadInto moves one value into storage owned by owner. No Value
// or StorageRef is retained in the claim or slot header.
func (vm *VM) stageAsyncPayloadInto(
	owner *asyncOwnerRegion,
	role asyncSlotRole,
	value Value,
) (asyncPayload, *VMError) {
	payload, slot, dst, vmErr := vm.reserveAsyncPayload(owner, role)
	if vmErr != nil {
		return asyncPayload{}, vmErr
	}
	if vmErr := vm.initializeAsyncSlot(owner, dst, value); vmErr != nil {
		vm.abortAsyncReservation(owner, slot)
		return asyncPayload{}, vmErr
	}
	if slot.state != asyncPayloadReserved {
		vm.abortAsyncReservation(owner, slot)
		return asyncPayload{}, vm.eb.invalidLocation("async payload reservation lost its owner state")
	}
	slot.state = asyncPayloadInitialized
	return payload, nil
}

func (vm *VM) initializeAsyncSlot(owner *asyncOwnerRegion, dst StorageRef, value Value) *VMError {
	if owner == nil || owner.retiring || owner.retired {
		return vm.eb.invalidLocation("async payload has no concrete owner")
	}
	coerced, vmErr := vm.coerceToSlotType(vm.currentFrame(), value, owner.typeID)
	if vmErr != nil {
		return vmErr
	}
	value = coerced
	if vm.valueType(value.TypeID) != vm.valueType(owner.typeID) {
		return vm.eb.makeError(PanicTypeMismatch, "async payload type does not match its owner")
	}
	if owner.cell.Kind == cellComposite {
		src, ok := value.Storage()
		if !ok {
			return vm.eb.typeMismatch("composite", value.Kind.String())
		}
		if err := vm.storageMoveInit(dst, src); err != nil {
			return vm.eb.makeError(PanicUnimplemented, err.Error())
		}
		return vm.releaseTemporary(vm.currentFrame(), src)
	}
	if value.Kind == VKComposite {
		return vm.eb.typeMismatch("inline scalar", value.Kind.String())
	}
	if err := vm.storageWriteCell(dst, owner.cell, value); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return nil
}

func (vm *VM) dropAsyncPayload(payload asyncPayload) {
	owner, slot, src, vmErr := vm.claimAsyncPayload(payload)
	if vmErr != nil {
		return
	}
	var dropErr error
	if owner.cell.Kind == cellComposite {
		dropErr = vm.storageDrop(src)
	} else {
		value, err := vm.storageReadCell(src, owner.cell)
		if err == nil {
			dropErr = vm.storageZero(src)
			vm.dropValue(value)
		} else {
			dropErr = err
		}
	}
	if dropErr != nil {
		slot.state = asyncPayloadInitialized
		return
	}
	vm.finishAsyncClaim(owner, slot, asyncPayloadDropped)
}

func (vm *VM) asyncPayloadHoldsStorage(payload asyncPayload) bool {
	owner, slot, src, vmErr := vm.inspectAsyncPayload(payload)
	if vmErr != nil || slot.state != asyncPayloadInitialized {
		return false
	}
	if owner.cell.Kind == cellComposite {
		return true
	}
	value, err := vm.storageReadCell(src, owner.cell)
	return err == nil && valueHoldsStorage(value)
}
