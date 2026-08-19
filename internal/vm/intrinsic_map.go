package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

func (vm *VM) handleMapNew(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_map_new requires 0 arguments")
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	h := vm.Heap.AllocMap(dstType)
	val := MakeHandleMap(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.Heap.Release(h)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	}
	return nil
}

func (vm *VM) handleMapLen(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_map_len requires 1 argument")
	}
	mapVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(mapVal)
	obj, _, vmErr := vm.mapObjectFromValue(mapVal)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	u64, err := safecast.Conv[uint64](obj.MapLen)
	if err != nil {
		return vm.eb.invalidNumericConversion("map length out of range")
	}
	val := vm.makeBigUint(dstType, bignum.UintFromUint64(u64))
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	}
	return nil
}

func (vm *VM) handleMapContains(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_map_contains requires 2 arguments")
	}
	mapVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(mapVal)
	keyArg, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(keyArg)
	obj, _, vmErr := vm.mapObjectFromValue(mapVal)
	if vmErr != nil {
		return vmErr
	}
	keyType, _ := vm.mapValueTypes(obj.TypeID)
	key, _, vmErr := vm.mapKeyFromValue(keyArg, keyType)
	if vmErr != nil {
		return vmErr
	}
	_, ok := obj.MapIndex[key]
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	val := MakeBool(ok, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	}
	return nil
}

func (vm *VM) handleMapGetRef(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_map_get_ref requires 2 arguments")
	}
	mapVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(mapVal)
	keyArg, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(keyArg)
	obj, handle, vmErr := vm.mapObjectFromValue(mapVal)
	if vmErr != nil {
		return vmErr
	}
	keyType, valueType := vm.mapValueTypes(obj.TypeID)
	key, _, vmErr := vm.mapKeyFromValue(keyArg, keyType)
	if vmErr != nil {
		return vmErr
	}
	idx, ok := obj.MapIndex[key]
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	if !ok {
		res, makeErr := vm.makeOptionNothing(dstType)
		if makeErr != nil {
			return makeErr
		}
		if writeErr := vm.writeLocal(frame, dstLocal, res); writeErr != nil {
			vm.dropValue(res)
			return writeErr
		}
		if writes != nil {
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   res,
			})
		}
		return nil
	}
	idx32, err := safecast.Conv[int32](idx)
	if err != nil {
		return vm.eb.invalidLocation("map index overflow")
	}
	refType := types.NoTypeID
	if vm.Types != nil && valueType != types.NoTypeID {
		refType = vm.Types.Intern(types.MakeReference(valueType, false))
	}
	ref := MakeRef(Location{Kind: LKMapElem, Handle: handle, Index: idx32}, refType)
	res, vmErr := vm.makeOptionSome(dstType, ref)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		vm.dropValue(res)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   res,
		})
	}
	return nil
}

func (vm *VM) handleMapGetMut(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_map_get_mut requires 2 arguments")
	}
	mapVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(mapVal)
	keyArg, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(keyArg)
	obj, handle, vmErr := vm.mapObjectFromValue(mapVal)
	if vmErr != nil {
		return vmErr
	}
	keyType, valueType := vm.mapValueTypes(obj.TypeID)
	key, _, vmErr := vm.mapKeyFromValue(keyArg, keyType)
	if vmErr != nil {
		return vmErr
	}
	idx, ok := obj.MapIndex[key]
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	if !ok {
		res, makeErr := vm.makeOptionNothing(dstType)
		if makeErr != nil {
			return makeErr
		}
		if writeErr := vm.writeLocal(frame, dstLocal, res); writeErr != nil {
			vm.dropValue(res)
			return writeErr
		}
		if writes != nil {
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   res,
			})
		}
		return nil
	}
	idx32, err := safecast.Conv[int32](idx)
	if err != nil {
		return vm.eb.invalidLocation("map index overflow")
	}
	refType := types.NoTypeID
	if vm.Types != nil && valueType != types.NoTypeID {
		refType = vm.Types.Intern(types.MakeReference(valueType, true))
	}
	ref := MakeRefMut(Location{Kind: LKMapElem, Handle: handle, Index: idx32}, refType)
	res, vmErr := vm.makeOptionSome(dstType, ref)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		vm.dropValue(res)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   res,
		})
	}
	return nil
}

func (vm *VM) handleMapInsert(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_map_insert requires 3 arguments")
	}
	mapVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(mapVal)
	keyArg, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	valArg, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		vm.dropValue(keyArg)
		return vmErr
	}

	obj, _, vmErr := vm.mapObjectFromValue(mapVal)
	if vmErr != nil {
		vm.dropValue(keyArg)
		vm.dropValue(valArg)
		return vmErr
	}
	keyType, valueType := vm.mapValueTypes(obj.TypeID)
	key, keyVal, vmErr := vm.mapKeyFromValue(keyArg, keyType)
	if vmErr != nil {
		vm.dropValue(keyArg)
		vm.dropValue(valArg)
		return vmErr
	}
	if vm.Types != nil && valueType != types.NoTypeID {
		retagged, converted, retagErr := vm.retagUnionValue(valArg, valueType)
		if retagErr != nil {
			vm.dropValue(keyVal)
			vm.dropValue(valArg)
			return retagErr
		}
		if converted {
			valArg = retagged
		}
	}
	// The map keeps what it is given for as long as it lives, which is longer
	// than the activation that built it. The run's own writers move a composite
	// into the map's storage, so nothing is adopted ahead of them here: adopting
	// first would copy the value into the arena and then move it again.

	if idx, ok := obj.MapIndex[key]; ok {
		// Replacing: take the old value OUT so it can be handed back, then
		// initialise the slot that take left empty.
		oldVal, takeErr := vm.takeRunSlot(frame, obj.MapVals, obj.MapValType, idx)
		if takeErr != nil {
			vm.dropValue(keyVal)
			vm.dropValue(valArg)
			return takeErr
		}
		if initErr := vm.initRunSlot(frame, obj.MapVals, obj.MapValType, idx, valArg); initErr != nil {
			vm.dropValue(keyVal)
			vm.dropValue(oldVal)
			return initErr
		}
		vm.dropValue(keyVal)
		if !call.HasDst {
			vm.dropValue(oldVal)
			return nil
		}
		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		res, makeErr := vm.makeOptionSome(dstType, oldVal)
		if makeErr != nil {
			vm.dropValue(oldVal)
			return makeErr
		}
		if writeErr := vm.writeLocal(frame, dstLocal, res); writeErr != nil {
			vm.dropValue(res)
			return writeErr
		}
		if writes != nil {
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   res,
			})
		}
		return nil
	}

	if obj.MapLen == obj.MapCap {
		if growErr := vm.growMapRuns(obj, obj.MapLen+1); growErr != nil {
			vm.dropValue(keyVal)
			vm.dropValue(valArg)
			return growErr
		}
	}
	at := obj.MapLen
	if initErr := vm.initMapEntry(frame, obj, at, keyVal, valArg); initErr != nil {
		vm.dropValue(keyVal)
		vm.dropValue(valArg)
		return initErr
	}
	if lenErr := vm.setMapLen(obj, at+1); lenErr != nil {
		return lenErr
	}
	obj.MapIndex[key] = at

	if !call.HasDst {
		return nil
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	res, vmErr := vm.makeOptionNothing(dstType)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		vm.dropValue(res)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   res,
		})
	}
	return nil
}

func (vm *VM) handleMapRemove(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_map_remove requires 2 arguments")
	}
	mapVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(mapVal)
	keyArg, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(keyArg)
	obj, _, vmErr := vm.mapObjectFromValue(mapVal)
	if vmErr != nil {
		return vmErr
	}
	keyType, _ := vm.mapValueTypes(obj.TypeID)
	key, _, vmErr := vm.mapKeyFromValue(keyArg, keyType)
	if vmErr != nil {
		return vmErr
	}
	idx, ok := obj.MapIndex[key]
	if !ok {
		if !call.HasDst {
			return nil
		}
		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		res, makeErr := vm.makeOptionNothing(dstType)
		if makeErr != nil {
			return makeErr
		}
		if writeErr := vm.writeLocal(frame, dstLocal, res); writeErr != nil {
			vm.dropValue(res)
			return writeErr
		}
		if writes != nil {
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   res,
			})
		}
		return nil
	}

	// Take the entry OUT first, which empties both of its slots, and only then
	// move the last entry down onto them. Copying onto slots that still held a
	// value would overwrite what nothing has released.
	removedKey, removedVal, takeErr := vm.takeMapEntry(frame, obj, idx)
	if takeErr != nil {
		return takeErr
	}
	lastIdx := obj.MapLen - 1
	if idx != lastIdx {
		swapKeyRef, refErr := vm.mapKeySlot(obj, lastIdx)
		if refErr != nil {
			vm.dropValue(removedKey)
			vm.dropValue(removedVal)
			return refErr
		}
		swapKeyVal, peekErr := vm.peekStorage(swapKeyRef)
		if peekErr != nil {
			vm.dropValue(removedKey)
			vm.dropValue(removedVal)
			return peekErr
		}
		swapKey, _, swapErr := vm.mapKeyFromValue(swapKeyVal, keyType)
		if swapErr != nil {
			vm.dropValue(removedKey)
			vm.dropValue(removedVal)
			return swapErr
		}
		if copyErr := vm.copyMapEntry(obj, idx, lastIdx); copyErr != nil {
			vm.dropValue(removedKey)
			vm.dropValue(removedVal)
			return copyErr
		}
		obj.MapIndex[swapKey] = idx
	}
	if lenErr := vm.setMapLen(obj, lastIdx); lenErr != nil {
		vm.dropValue(removedKey)
		vm.dropValue(removedVal)
		return lenErr
	}
	delete(obj.MapIndex, key)

	vm.dropValue(removedKey)
	if !call.HasDst {
		vm.dropValue(removedVal)
		return nil
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	res, vmErr := vm.makeOptionSome(dstType, removedVal)
	if vmErr != nil {
		vm.dropValue(removedVal)
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		vm.dropValue(res)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   res,
		})
	}
	return nil
}

func (vm *VM) handleMapKeys(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_map_keys requires 1 argument")
	}
	mapVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(mapVal)
	obj, _, vmErr := vm.mapObjectFromValue(mapVal)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	elems := make([]Value, obj.MapLen)
	for i := range obj.MapLen {
		keyRef, refErr := vm.mapKeySlot(obj, i)
		if refErr != nil {
			for j := range i {
				vm.dropValue(elems[j])
			}
			return refErr
		}
		cloned, cloneErr := vm.readStorageValue(frame, keyRef)
		if cloneErr != nil {
			for j := range i {
				vm.dropValue(elems[j])
			}
			return cloneErr
		}
		elems[i] = cloned
	}
	handle := vm.Heap.AllocArray(dstType, elems)
	val := MakeHandleArray(handle, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.dropValue(val)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	}
	return nil
}

func (vm *VM) mapObjectFromValue(val Value) (*Object, Handle, *VMError) {
	if val.Kind == VKRef || val.Kind == VKRefMut {
		loaded, vmErr := vm.loadLocationRaw(val.Loc)
		if vmErr != nil {
			return nil, 0, vmErr
		}
		val = loaded
	}
	if val.Kind != VKHandleMap {
		return nil, 0, vm.eb.typeMismatch("map", val.Kind.String())
	}
	obj, vmErr := vm.heapAliveForRef(val.H)
	if vmErr != nil {
		return nil, 0, vmErr
	}
	if obj.Kind != OKMap {
		return nil, 0, vm.eb.typeMismatch("map", fmt.Sprintf("%v", obj.Kind))
	}
	return obj, val.H, nil
}

func (vm *VM) mapValueTypes(mapType types.TypeID) (key, value types.TypeID) {
	if vm == nil || vm.Types == nil || mapType == types.NoTypeID {
		return types.NoTypeID, types.NoTypeID
	}
	key, value, _ = vm.Types.MapInfo(mapType)
	return key, value
}
