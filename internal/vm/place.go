package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
)

// EvalPlace evaluates a place expression and returns its location.
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
		loc = Location{
			Kind:       LKLocal,
			FrameRef:   frame,
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
			case VKPtr:
				loc = v.Loc
				loc.IsMut = true
			default:
				return Location{}, vm.eb.derefOnNonRef(v.Kind.String())
			}

		case mir.PlaceProjField:
			v, vmErr := vm.loadLocationRaw(loc)
			if vmErr != nil {
				return Location{}, vmErr
			}
			for i := 0; i < 8 && (v.Kind == VKRef || v.Kind == VKRefMut); i++ {
				v, vmErr = vm.loadLocationRaw(v.Loc)
				if vmErr != nil {
					return Location{}, vmErr
				}
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

			if vm.Layouts == nil {
				return Location{}, vm.eb.invalidLocation("field projection: missing finalized layout registry")
			}
			typeForOffset := v.TypeID
			if typeForOffset == types.NoTypeID {
				typeForOffset = obj.TypeID
			}
			off, err := vm.Layouts.FieldOffset(typeForOffset, fieldIdx)
			if err != nil {
				return Location{}, vm.eb.invalidLocation(fmt.Sprintf("field projection: %v", err))
			}
			byteOffset, err := safecast.Conv[int32](off)
			if err != nil {
				return Location{}, vm.eb.invalidLocation("field projection: byte offset overflow")
			}
			loc = Location{
				Kind:       LKStructField,
				Handle:     v.H,
				Index:      fieldIdx32,
				ByteOffset: byteOffset,
				IsMut:      loc.IsMut,
			}

		case mir.PlaceProjIndex:
			next, vmErr := vm.projectIndexLocation(frame, loc, proj)
			if vmErr != nil {
				return Location{}, vmErr
			}
			loc = next

		default:
			return Location{}, vm.eb.invalidLocation(fmt.Sprintf("invalid place projection kind %d", proj.Kind))
		}
	}

	return loc, nil
}

func (vm *VM) loadLocationRaw(loc Location) (Value, *VMError) {
	switch loc.Kind {
	case LKLocal:
		frame := loc.FrameRef
		if frame == nil {
			return Value{}, vm.eb.invalidLocation("invalid local frame <nil>")
		}
		localID, err := safecast.Conv[mir.LocalID](loc.Local)
		if err != nil {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("invalid local id %d", loc.Local))
		}
		slot := &frame.Locals[localID]
		if slot.IsInit && !slot.IsDropped && slot.IsMoved && slot.PinCount != 0 {
			return slot.V, nil
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

	case LKTagField:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if obj.Kind != OKTag {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("expected tag handle, got %v", obj.Kind))
		}
		fieldIdx := int(loc.Index)
		if loc.Index < 0 || fieldIdx < 0 || fieldIdx >= len(obj.Tag.Fields) {
			return Value{}, vm.eb.tagPayloadIndexOutOfRange(fieldIdx, len(obj.Tag.Fields))
		}
		return obj.Tag.Fields[fieldIdx], nil

	case LKArrayElem:
		return vm.loadArrayElem(loc)

	case LKMapElem:
		return vm.loadMapElem(loc)

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
		frame := loc.FrameRef
		if frame == nil {
			return vm.eb.invalidLocation("invalid local frame <nil>")
		}
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

	case LKTagField:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return vmErr
		}
		if obj.Kind != OKTag {
			return vm.eb.invalidLocation(fmt.Sprintf("expected tag handle, got %v", obj.Kind))
		}
		fieldIdx := int(loc.Index)
		if loc.Index < 0 || fieldIdx < 0 || fieldIdx >= len(obj.Tag.Fields) {
			return vm.eb.tagPayloadIndexOutOfRange(fieldIdx, len(obj.Tag.Fields))
		}
		vm.dropValue(obj.Tag.Fields[fieldIdx])
		obj.Tag.Fields[fieldIdx] = val
		return nil

	case LKArrayElem:
		return vm.storeArrayElem(loc, val)

	case LKMapElem:
		return vm.storeMapElem(loc, val)

	case LKRawBytes, LKStringBytes:
		b, vmErr := vm.valueToUint8(val)
		if vmErr != nil {
			return vmErr
		}
		ptr := MakePtr(loc, types.NoTypeID)
		return vm.writeBytesToPointer(ptr, []byte{b})

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
