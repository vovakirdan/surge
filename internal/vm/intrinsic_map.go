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
	u64, err := safecast.Conv[uint64](len(obj.MapEntries))
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
		if retagged, ok := vm.retagUnionValue(valArg, valueType); ok {
			valArg = retagged
		}
	}

	if idx, ok := obj.MapIndex[key]; ok {
		entry := &obj.MapEntries[idx]
		oldVal := entry.Value
		entry.Value = valArg
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

	obj.MapEntries = append(obj.MapEntries, mapEntry{Key: keyVal, Value: valArg})
	obj.MapIndex[key] = len(obj.MapEntries) - 1

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

	entry := obj.MapEntries[idx]
	lastIdx := len(obj.MapEntries) - 1
	if idx != lastIdx {
		swap := obj.MapEntries[lastIdx]
		obj.MapEntries[idx] = swap
		swapKey, _, swapErr := vm.mapKeyFromValue(swap.Key, keyType)
		if swapErr != nil {
			return swapErr
		}
		obj.MapIndex[swapKey] = idx
	}
	obj.MapEntries[lastIdx] = mapEntry{}
	obj.MapEntries = obj.MapEntries[:lastIdx]
	delete(obj.MapIndex, key)

	vm.dropValue(entry.Key)
	if !call.HasDst {
		vm.dropValue(entry.Value)
		return nil
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	res, vmErr := vm.makeOptionSome(dstType, entry.Value)
	if vmErr != nil {
		vm.dropValue(entry.Value)
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
	elems := make([]Value, len(obj.MapEntries))
	for i, entry := range obj.MapEntries {
		cloned, cloneErr := vm.cloneForShare(entry.Key)
		if cloneErr != nil {
			for j := 0; j < i; j++ {
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

func (vm *VM) mapKeyFromValue(val Value, keyType types.TypeID) (mapKey, Value, *VMError) {
	keyVal := val
	if keyVal.Kind == VKRef || keyVal.Kind == VKRefMut {
		loaded, vmErr := vm.loadLocationRaw(keyVal.Loc)
		if vmErr != nil {
			return mapKey{}, Value{}, vmErr
		}
		keyVal = loaded
	}

	if keyType != types.NoTypeID && vm.Types != nil {
		keyType = vm.valueType(keyType)
		if tt, ok := vm.Types.Lookup(keyType); ok {
			switch tt.Kind {
			case types.KindString:
				if keyVal.Kind != VKHandleString {
					return mapKey{}, Value{}, vm.eb.typeMismatch("string", keyVal.Kind.String())
				}
				obj := vm.Heap.Get(keyVal.H)
				return mapKey{kind: mapKeyString, str: vm.stringBytes(obj)}, keyVal, nil
			case types.KindInt:
				return vm.mapKeyFromIntValue(keyVal)
			case types.KindUint:
				return vm.mapKeyFromUintValue(keyVal)
			default:
				return mapKey{}, Value{}, vm.eb.typeMismatch("hashable key", keyVal.Kind.String())
			}
		}
	}

	switch keyVal.Kind {
	case VKHandleString:
		obj := vm.Heap.Get(keyVal.H)
		return mapKey{kind: mapKeyString, str: vm.stringBytes(obj)}, keyVal, nil
	case VKInt, VKBigInt:
		return vm.mapKeyFromIntValue(keyVal)
	case VKBigUint:
		return vm.mapKeyFromUintValue(keyVal)
	default:
		return mapKey{}, Value{}, vm.eb.typeMismatch("hashable key", keyVal.Kind.String())
	}
}

func (vm *VM) mapKeyFromIntValue(val Value) (mapKey, Value, *VMError) {
	switch val.Kind {
	case VKInt:
		return mapKey{kind: mapKeyInt, i64: val.Int}, val, nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return mapKey{}, Value{}, vmErr
		}
		if n, ok := i.Int64(); ok {
			return mapKey{kind: mapKeyInt, i64: n}, val, nil
		}
		return mapKey{kind: mapKeyBigInt, str: bignum.FormatInt(i)}, val, nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return mapKey{}, Value{}, vmErr
		}
		if n, ok := u.Uint64(); ok {
			if n <= uint64(^uint64(0)>>1) {
				return mapKey{kind: mapKeyInt, i64: int64(n)}, val, nil
			}
		}
		return mapKey{}, Value{}, vm.eb.invalidNumericConversion("int key out of range")
	default:
		return mapKey{}, Value{}, vm.eb.typeMismatch("int", val.Kind.String())
	}
}

func (vm *VM) mapKeyFromUintValue(val Value) (mapKey, Value, *VMError) {
	switch val.Kind {
	case VKInt:
		if val.Int < 0 {
			return mapKey{}, Value{}, vm.eb.invalidNumericConversion("uint key out of range")
		}
		return mapKey{kind: mapKeyUint, u64: uint64(val.Int)}, val, nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return mapKey{}, Value{}, vmErr
		}
		if n, ok := u.Uint64(); ok {
			return mapKey{kind: mapKeyUint, u64: n}, val, nil
		}
		return mapKey{kind: mapKeyBigUint, str: bignum.FormatUint(u)}, val, nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return mapKey{}, Value{}, vmErr
		}
		if i.Neg {
			return mapKey{}, Value{}, vm.eb.invalidNumericConversion("uint key out of range")
		}
		u := i.Abs()
		if n, ok := u.Uint64(); ok {
			return mapKey{kind: mapKeyUint, u64: n}, val, nil
		}
		return mapKey{kind: mapKeyBigUint, str: bignum.FormatUint(u)}, val, nil
	default:
		return mapKey{}, Value{}, vm.eb.typeMismatch("uint", val.Kind.String())
	}
}
