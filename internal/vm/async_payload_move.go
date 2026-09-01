package vm

// moveAsyncPayloadIntoStorage initializes caller-owned exact storage and only
// then makes the source slot terminal and reusable.
func (vm *VM) moveAsyncPayloadIntoStorage(payload asyncPayload, dst StorageRef) *VMError {
	owner, slot, src, vmErr := vm.claimAsyncPayload(payload)
	if vmErr != nil {
		return vmErr
	}
	if vm.valueType(dst.TypeID) != vm.valueType(owner.typeID) {
		slot.state = asyncPayloadInitialized
		return vm.eb.makeError(PanicTypeMismatch, "async payload destination type mismatch")
	}
	if vmErr := vm.moveClaimedAsyncStorage(owner, src, dst); vmErr != nil {
		slot.state = asyncPayloadInitialized
		return vmErr
	}
	vm.finishAsyncClaim(owner, slot, asyncPayloadMoved)
	return nil
}

func (vm *VM) moveClaimedAsyncStorage(owner *asyncOwnerRegion, src, dst StorageRef) *VMError {
	if owner.cell.Kind == cellComposite {
		if err := vm.storageMoveInit(dst, src); err != nil {
			return vm.eb.makeError(PanicUnimplemented, err.Error())
		}
		return nil
	}
	if err := vm.storageMoveCellInit(dst, src, owner.cell); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return nil
}

func (vm *VM) cloneAsyncPayloadIntoStorage(payload asyncPayload, dst StorageRef) *VMError {
	owner, slot, src, vmErr := vm.inspectAsyncPayload(payload)
	if vmErr != nil {
		return vmErr
	}
	if slot.state != asyncPayloadInitialized {
		return vm.eb.makeError(PanicUseAfterMove, "async payload is not initialized")
	}
	if vm.valueType(dst.TypeID) != vm.valueType(owner.typeID) {
		return vm.eb.makeError(PanicTypeMismatch, "async payload destination type mismatch")
	}
	if owner.cell.Kind == cellComposite {
		if err := vm.storageCopy(dst, src); err != nil {
			if dropErr := vm.storageDrop(dst); dropErr != nil {
				return vm.eb.makeError(PanicUnimplemented, dropErr.Error())
			}
			return vm.eb.makeError(PanicUnimplemented, err.Error())
		}
		return nil
	}
	value, err := vm.storageReadCell(src, owner.cell)
	if err != nil {
		return vm.eb.makeError(PanicRCUseAfterFree, err.Error())
	}
	cloned, cloneErr := vm.cloneForShare(value)
	if cloneErr != nil {
		return cloneErr
	}
	if err := vm.storageWriteCell(dst, owner.cell, cloned); err != nil {
		vm.dropValue(cloned)
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return nil
}

func (vm *VM) moveAsyncPayloadIntoOwner(
	payload asyncPayload,
	destination *asyncOwnerRegion,
	role asyncSlotRole,
) (asyncPayload, *VMError) {
	next, slot, dst, vmErr := vm.reserveAsyncPayload(destination, role)
	if vmErr != nil {
		return asyncPayload{}, vmErr
	}
	if vmErr := vm.moveAsyncPayloadIntoStorage(payload, dst); vmErr != nil {
		vm.abortAsyncReservation(destination, slot)
		return asyncPayload{}, vmErr
	}
	slot.state = asyncPayloadInitialized
	return next, nil
}
