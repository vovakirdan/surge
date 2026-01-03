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

	var loc Location
	if p.Kind == mir.PlaceGlobal {
		loc = Location{
			Kind:       LKGlobal,
			Global:     int32(p.Global),
			ByteOffset: 0,
			IsMut:      true,
		}
	} else {
		frameIdx, err := safecast.Conv[int32](len(vm.Stack) - 1)
		if err != nil {
			return Location{}, vm.eb.invalidLocation("invalid place: stack too deep")
		}
		loc = Location{
			Kind:       LKLocal,
			Frame:      frameIdx,
			Local:      int32(p.Local),
			ByteOffset: 0,
			IsMut:      true,
		}
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

			fieldIdx := proj.FieldIdx
			if fieldIdx < 0 {
				layout, vmErr := vm.layouts.Struct(obj.TypeID)
				if vmErr != nil {
					return Location{}, vmErr
				}
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
				typeForOffset := v.TypeID
				if typeForOffset == types.NoTypeID {
					typeForOffset = obj.TypeID
				}
				off, err := vm.Layout.FieldOffset(typeForOffset, fieldIdx)
				if err != nil {
					return Location{}, vm.eb.invalidLocation(fmt.Sprintf("field projection: %v", err))
				}
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

			idxLocal := proj.IndexLocal
			idxVal, vmErr := vm.readLocal(frame, idxLocal)
			if vmErr != nil {
				return Location{}, vmErr
			}
			if obj.Kind != OKArray && obj.Kind != OKArraySlice {
				return Location{}, vm.eb.invalidLocation(fmt.Sprintf("index projection on %v", obj.Kind))
			}
			view, vmErr := vm.arrayViewFromHandle(v.H)
			if vmErr != nil {
				return Location{}, vmErr
			}
			idx, vmErr := vm.arrayIndexFromValue(idxVal, view.length)
			if vmErr != nil {
				return Location{}, vmErr
			}

			baseIdx := view.start + idx
			idx32, err := safecast.Conv[int32](baseIdx)
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
					el, err := vm.Layout.LayoutOf(elemType)
					if err != nil {
						return Location{}, vm.eb.invalidLocation(fmt.Sprintf("index projection: %v", err))
					}
					stride := roundUp(el.Size, maxIntValue(1, el.Align))
					off := stride * baseIdx
					bo, err := safecast.Conv[int32](off)
					if err != nil {
						return Location{}, vm.eb.invalidLocation("index projection: byte offset overflow")
					}
					byteOffset = bo
				}
			}
			loc = Location{
				Kind:       LKArrayElem,
				Handle:     view.baseHandle,
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

	case LKGlobal:
		globalID, err := safecast.Conv[mir.GlobalID](loc.Global)
		if err != nil {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("invalid global id %d", loc.Global))
		}
		return vm.readGlobal(globalID)

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
		if obj.Kind != OKArray && obj.Kind != OKArraySlice {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("expected array handle, got %v", obj.Kind))
		}
		view, vmErr := vm.arrayViewFromHandle(loc.Handle)
		if vmErr != nil {
			return Value{}, vmErr
		}
		idx := int(loc.Index)
		if loc.Index < 0 || idx < 0 || idx >= view.length {
			return Value{}, vm.eb.arrayIndexOutOfRange(idx, view.length)
		}
		val := view.baseObj.Arr[view.start+idx]
		if vm.Types != nil && view.baseObj != nil {
			if elemType, ok := vm.Types.ArrayInfo(view.baseObj.TypeID); ok {
				if retagged, ok := vm.retagUnionValue(val, elemType); ok {
					val = retagged
				}
			}
		}
		return val, nil

	case LKMapElem:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if obj.Kind != OKMap {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("expected map handle, got %v", obj.Kind))
		}
		idx := int(loc.Index)
		if loc.Index < 0 || idx < 0 || idx >= len(obj.MapEntries) {
			return Value{}, vm.eb.outOfBounds(idx, len(obj.MapEntries))
		}
		val := obj.MapEntries[idx].Value
		if vm.Types != nil {
			if _, valueType, ok := vm.Types.MapInfo(obj.TypeID); ok {
				if retagged, ok := vm.retagUnionValue(val, valueType); ok {
					val = retagged
				}
			}
		}
		return val, nil

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

	case LKGlobal:
		globalID, err := safecast.Conv[mir.GlobalID](loc.Global)
		if err != nil {
			return vm.eb.invalidLocation(fmt.Sprintf("invalid global id %d", loc.Global))
		}
		return vm.writeGlobal(globalID, val)

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
		if obj.Kind != OKArray && obj.Kind != OKArraySlice {
			return vm.eb.invalidLocation(fmt.Sprintf("expected array handle, got %v", obj.Kind))
		}
		view, vmErr := vm.arrayViewFromHandle(loc.Handle)
		if vmErr != nil {
			return vmErr
		}
		idx := int(loc.Index)
		if loc.Index < 0 || idx < 0 || idx >= view.length {
			return vm.eb.arrayIndexOutOfRange(idx, view.length)
		}
		if vm.Types != nil && view.baseObj != nil {
			if elemType, ok := vm.Types.ArrayInfo(view.baseObj.TypeID); ok {
				if retagged, ok := vm.retagUnionValue(val, elemType); ok {
					val = retagged
				}
			}
		}
		baseIdx := view.start + idx
		vm.dropValue(view.baseObj.Arr[baseIdx])
		view.baseObj.Arr[baseIdx] = val
		return nil

	case LKMapElem:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return vmErr
		}
		if obj.Kind != OKMap {
			return vm.eb.invalidLocation(fmt.Sprintf("expected map handle, got %v", obj.Kind))
		}
		idx := int(loc.Index)
		if loc.Index < 0 || idx < 0 || idx >= len(obj.MapEntries) {
			return vm.eb.outOfBounds(idx, len(obj.MapEntries))
		}
		if vm.Types != nil {
			if _, valueType, ok := vm.Types.MapInfo(obj.TypeID); ok {
				if retagged, ok := vm.retagUnionValue(val, valueType); ok {
					val = retagged
				}
			}
		}
		vm.dropValue(obj.MapEntries[idx].Value)
		obj.MapEntries[idx].Value = val
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
