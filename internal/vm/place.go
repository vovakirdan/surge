package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
)

func (vm *VM) EvalPlace(frame *Frame, p mir.Place) (Location, *VMError) {
	if vm == nil || frame == nil {
		return Location{}, &VMError{Code: PanicInvalidLocation, Message: "invalid location: nil frame"}
	}
	if len(vm.Stack) == 0 {
		return Location{}, &VMError{Code: PanicInvalidLocation, Message: "invalid location: empty stack"}
	}
	if !p.IsValid() {
		return Location{}, vm.eb.invalidLocation("invalid place")
	}

	frameIdx, err := safecast.Conv[int32](len(vm.Stack) - 1)
	if err != nil {
		return Location{}, vm.eb.invalidLocation("invalid place: stack too deep")
	}
	loc := Location{
		Kind:       LKLocal,
		Frame:      frameIdx,
		Local:      int32(p.Local),
		ByteOffset: 0,
		IsMut:      true,
	}

	for _, proj := range p.Proj {
		switch proj.Kind {
		case mir.PlaceProjDeref:
			v, vmErr := vm.loadLocationRaw(loc)
			if vmErr != nil {
				return Location{}, vmErr
			}
			switch v.Kind {
			case VKRef, VKRefMut:
				loc = v.Loc
			default:
				return Location{}, vm.eb.derefOnNonRef(v.Kind.String())
			}

		case mir.PlaceProjField:
			v, vmErr := vm.loadLocationRaw(loc)
			if vmErr != nil {
				return Location{}, vmErr
			}
			if v.Kind != VKHandleStruct {
				return Location{}, vm.eb.invalidLocation(fmt.Sprintf("field projection on non-struct value (got %s)", v.Kind))
			}

			obj, vmErr := vm.heapAliveForRef(v.H)
			if vmErr != nil {
				return Location{}, vmErr
			}
			if obj.Kind != OKStruct {
				return Location{}, vm.eb.invalidLocation(fmt.Sprintf("field projection on %v", obj.Kind))
			}
			layout, vmErr := vm.layouts.Struct(obj.TypeID)
			if vmErr != nil {
				return Location{}, vmErr
			}

			fieldIdx := proj.FieldIdx
			if fieldIdx < 0 {
				idx, ok := layout.IndexByName[proj.FieldName]
				if !ok {
					return Location{}, vm.eb.invalidLocation(fmt.Sprintf("unknown field %q on type#%d", proj.FieldName, layout.TypeID))
				}
				fieldIdx = idx
			}
			if fieldIdx < 0 || fieldIdx >= len(obj.Fields) {
				return Location{}, vm.eb.fieldIndexOutOfRange(fieldIdx, len(obj.Fields))
			}

			fieldIdx32, err := safecast.Conv[int32](fieldIdx)
			if err != nil {
				return Location{}, vm.eb.invalidLocation("field projection: index overflow")
			}

			var byteOffset int32
			if vm.Layout != nil {
				off := vm.Layout.FieldOffset(obj.TypeID, fieldIdx)
				bo, err := safecast.Conv[int32](off)
				if err != nil {
					return Location{}, vm.eb.invalidLocation("field projection: byte offset overflow")
				}
				byteOffset = bo
			}
			loc = Location{
				Kind:       LKStructField,
				Handle:     v.H,
				Index:      fieldIdx32,
				ByteOffset: byteOffset,
				IsMut:      loc.IsMut,
			}

		case mir.PlaceProjIndex:
			v, vmErr := vm.loadLocationRaw(loc)
			if vmErr != nil {
				return Location{}, vmErr
			}
			if v.Kind != VKHandleArray {
				return Location{}, vm.eb.invalidLocation(fmt.Sprintf("index projection on non-array value (got %s)", v.Kind))
			}

			obj, vmErr := vm.heapAliveForRef(v.H)
			if vmErr != nil {
				return Location{}, vmErr
			}
			if obj.Kind != OKArray {
				return Location{}, vm.eb.invalidLocation(fmt.Sprintf("index projection on %v", obj.Kind))
			}

			idxLocal := proj.IndexLocal
			idxVal, vmErr := vm.readLocal(frame, idxLocal)
			if vmErr != nil {
				return Location{}, vmErr
			}
			maxIndex := int(^uint(0) >> 1)
			maxInt := int64(maxIndex)
			maxUint := uint64(^uint(0) >> 1)
			var idx int
			switch idxVal.Kind {
			case VKInt:
				if idxVal.Int < 0 || idxVal.Int > maxInt {
					return Location{}, vm.eb.arrayIndexOutOfRange(maxIndex, len(obj.Arr))
				}
				ni, err := safecast.Conv[int](idxVal.Int)
				if err != nil {
					return Location{}, vm.eb.arrayIndexOutOfRange(maxIndex, len(obj.Arr))
				}
				idx = ni
			case VKBigInt:
				i, vmErr := vm.mustBigInt(idxVal)
				if vmErr != nil {
					return Location{}, vmErr
				}
				n, ok := i.Int64()
				if !ok || n < 0 || n > maxInt {
					return Location{}, vm.eb.arrayIndexOutOfRange(maxIndex, len(obj.Arr))
				}
				ni, err := safecast.Conv[int](n)
				if err != nil {
					return Location{}, vm.eb.arrayIndexOutOfRange(maxIndex, len(obj.Arr))
				}
				idx = ni
			case VKBigUint:
				u, vmErr := vm.mustBigUint(idxVal)
				if vmErr != nil {
					return Location{}, vmErr
				}
				n, ok := u.Uint64()
				if !ok || n > maxUint {
					return Location{}, vm.eb.arrayIndexOutOfRange(maxIndex, len(obj.Arr))
				}
				ni, err := safecast.Conv[int](n)
				if err != nil {
					return Location{}, vm.eb.arrayIndexOutOfRange(maxIndex, len(obj.Arr))
				}
				idx = ni
			default:
				return Location{}, vm.eb.typeMismatch("int", idxVal.Kind.String())
			}
			if idx < 0 || idx >= len(obj.Arr) {
				return Location{}, vm.eb.arrayIndexOutOfRange(idx, len(obj.Arr))
			}

			idx32, err := safecast.Conv[int32](idx)
			if err != nil {
				return Location{}, vm.eb.invalidLocation("index projection: index overflow")
			}

			var byteOffset int32
			if vm.Layout != nil && vm.Types != nil {
				elemType := types.NoTypeID
				arrType := vm.valueType(v.TypeID)
				if t, ok := vm.Types.ArrayInfo(arrType); ok {
					elemType = t
				} else if t, _, ok := vm.Types.ArrayFixedInfo(arrType); ok {
					elemType = t
				} else if tt, ok := vm.Types.Lookup(arrType); ok && tt.Kind == types.KindArray {
					elemType = tt.Elem
				}
				if elemType != types.NoTypeID {
					el := vm.Layout.LayoutOf(elemType)
					stride := roundUp(el.Size, maxIntValue(1, el.Align))
					off := stride * idx
					bo, err := safecast.Conv[int32](off)
					if err != nil {
						return Location{}, vm.eb.invalidLocation("index projection: byte offset overflow")
					}
					byteOffset = bo
				}
			}
			loc = Location{
				Kind:       LKArrayElem,
				Handle:     v.H,
				Index:      idx32,
				ByteOffset: byteOffset,
				IsMut:      loc.IsMut,
			}

		default:
			return Location{}, vm.eb.invalidLocation(fmt.Sprintf("invalid place projection kind %d", proj.Kind))
		}
	}

	return loc, nil
}

func roundUp(n, align int) int {
	if align <= 1 {
		return n
	}
	r := n % align
	if r == 0 {
		return n
	}
	return n + (align - r)
}

func maxIntValue(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (vm *VM) loadLocationRaw(loc Location) (Value, *VMError) {
	switch loc.Kind {
	case LKLocal:
		frameIdx := int(loc.Frame)
		if loc.Frame < 0 || frameIdx < 0 || frameIdx >= len(vm.Stack) {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("invalid local frame %d", loc.Frame))
		}
		frame := &vm.Stack[frameIdx]
		localID, err := safecast.Conv[mir.LocalID](loc.Local)
		if err != nil {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("invalid local id %d", loc.Local))
		}
		return vm.readLocal(frame, localID)

	case LKStructField:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if obj.Kind != OKStruct {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("expected struct handle, got %v", obj.Kind))
		}
		fieldIdx := int(loc.Index)
		if loc.Index < 0 || fieldIdx < 0 || fieldIdx >= len(obj.Fields) {
			return Value{}, vm.eb.fieldIndexOutOfRange(fieldIdx, len(obj.Fields))
		}
		return obj.Fields[fieldIdx], nil

	case LKArrayElem:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if obj.Kind != OKArray {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("expected array handle, got %v", obj.Kind))
		}
		idx := int(loc.Index)
		if loc.Index < 0 || idx < 0 || idx >= len(obj.Arr) {
			return Value{}, vm.eb.arrayIndexOutOfRange(idx, len(obj.Arr))
		}
		return obj.Arr[idx], nil

	default:
		return Value{}, vm.eb.invalidLocation("unknown location kind")
	}
}

func (vm *VM) storeLocation(loc Location, val Value) *VMError {
	if !loc.IsMut {
		return vm.eb.storeThroughNonMutRef()
	}

	switch loc.Kind {
	case LKLocal:
		frameIdx := int(loc.Frame)
		if loc.Frame < 0 || frameIdx < 0 || frameIdx >= len(vm.Stack) {
			return vm.eb.invalidLocation(fmt.Sprintf("invalid local frame %d", loc.Frame))
		}
		frame := &vm.Stack[frameIdx]
		localID, err := safecast.Conv[mir.LocalID](loc.Local)
		if err != nil {
			return vm.eb.invalidLocation(fmt.Sprintf("invalid local id %d", loc.Local))
		}
		return vm.writeLocal(frame, localID, val)

	case LKStructField:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return vmErr
		}
		if obj.Kind != OKStruct {
			return vm.eb.invalidLocation(fmt.Sprintf("expected struct handle, got %v", obj.Kind))
		}
		fieldIdx := int(loc.Index)
		if loc.Index < 0 || fieldIdx < 0 || fieldIdx >= len(obj.Fields) {
			return vm.eb.fieldIndexOutOfRange(fieldIdx, len(obj.Fields))
		}
		vm.dropValue(obj.Fields[fieldIdx])
		obj.Fields[fieldIdx] = val
		return nil

	case LKArrayElem:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return vmErr
		}
		if obj.Kind != OKArray {
			return vm.eb.invalidLocation(fmt.Sprintf("expected array handle, got %v", obj.Kind))
		}
		idx := int(loc.Index)
		if loc.Index < 0 || idx < 0 || idx >= len(obj.Arr) {
			return vm.eb.arrayIndexOutOfRange(idx, len(obj.Arr))
		}
		vm.dropValue(obj.Arr[idx])
		obj.Arr[idx] = val
		return nil

	default:
		return vm.eb.invalidLocation("unknown location kind")
	}
}

func (vm *VM) heapAliveForRef(h Handle) (*Object, *VMError) {
	if vm == nil || vm.Heap == nil {
		return nil, &VMError{Code: PanicInvalidLocation, Message: "invalid location: no heap"}
	}
	if h == 0 {
		return nil, vm.eb.invalidLocation("invalid handle 0")
	}
	obj, ok := vm.Heap.lookup(h)
	if !ok || obj == nil {
		return nil, vm.eb.invalidLocation(fmt.Sprintf("invalid handle %d", h))
	}
	if obj.Freed || obj.RefCount == 0 {
		return nil, vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: handle %d (alloc=%d)", h, obj.AllocID))
	}
	return obj, nil
}
