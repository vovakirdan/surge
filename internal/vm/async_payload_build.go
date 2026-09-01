package vm

import (
	"fmt"

	"surge/internal/types"
)

func (vm *VM) makeOptionSomeFromAsync(
	typeID types.TypeID,
	payload asyncPayload,
) (Value, *VMError) {
	return vm.buildTagFromAsyncPayload(typeID, "Some", payload, false)
}

func (vm *VM) makeTaskResultSuccessFromAsync(
	typeID types.TypeID,
	payload asyncPayload,
	clone bool,
) (Value, *VMError) {
	return vm.buildTagFromAsyncPayload(typeID, "Success", payload, clone)
}

func (vm *VM) buildTagFromAsyncPayload(
	typeID types.TypeID,
	caseName string,
	payload asyncPayload,
	clone bool,
) (Value, *VMError) {
	layout, vmErr := vm.tagLayoutFor(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	tagCase, ok := layout.CaseByName(caseName)
	if !ok || len(tagCase.PayloadTypes) != 1 {
		return Value{}, vm.eb.makeError(PanicTypeMismatch,
			fmt.Sprintf("%s expects exactly one payload", caseName))
	}
	shape, err := vm.unionMembers(typeID)
	if err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	index, err := vm.storageCaseIndexOf(typeID, shape, tagCase.TagSym)
	if err != nil {
		return Value{}, vm.eb.unknownTagLayout(err.Error())
	}
	if len(shape.Cases[index].Payload) != 1 {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, caseName+" has no exact payload slot")
	}
	frame := vm.currentFrame()
	destination, vmErr := vm.buildComposite(frame, typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	rollback := func(failure *VMError) (Value, *VMError) {
		if dropErr := vm.storageDrop(destination); dropErr != nil {
			return Value{}, vm.eb.makeError(PanicUnimplemented, dropErr.Error())
		}
		if releaseErr := vm.releaseTemporary(frame, destination); releaseErr != nil {
			return Value{}, releaseErr
		}
		return Value{}, failure
	}
	if activateErr := vm.storageSetActiveCase(destination, shape, index); activateErr != nil {
		return rollback(vm.eb.makeError(PanicUnimplemented, activateErr.Error()))
	}
	payloadDestination, err := destination.memberRef(shape.Cases[index].Payload[0])
	if err != nil {
		return rollback(vm.eb.makeError(PanicUnimplemented, err.Error()))
	}
	if clone {
		vmErr = vm.cloneAsyncPayloadIntoStorage(payload, payloadDestination)
	} else {
		vmErr = vm.moveAsyncPayloadIntoStorage(payload, payloadDestination)
	}
	if vmErr != nil {
		return rollback(vmErr)
	}
	return MakeComposite(destination), nil
}
