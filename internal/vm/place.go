package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
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

	loc := Location{
		Kind:  LKLocal,
		Frame: len(vm.Stack) - 1,
		Local: int(p.Local),
		IsMut: true,
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

			loc = Location{
				Kind:   LKStructField,
				Handle: v.H,
				Index:  fieldIdx,
				IsMut:  loc.IsMut,
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
			if idxVal.Kind != VKInt {
				return Location{}, vm.eb.typeMismatch("int", idxVal.Kind.String())
			}
			maxInt := int64(^uint(0) >> 1)
			if idxVal.Int < 0 || idxVal.Int > maxInt {
				return Location{}, vm.eb.arrayIndexOutOfRange(int(idxVal.Int), len(obj.Arr))
			}
			idx := int(idxVal.Int)
			if idx < 0 || idx >= len(obj.Arr) {
				return Location{}, vm.eb.arrayIndexOutOfRange(idx, len(obj.Arr))
			}

			loc = Location{
				Kind:   LKArrayElem,
				Handle: v.H,
				Index:  idx,
				IsMut:  loc.IsMut,
			}

		default:
			return Location{}, vm.eb.invalidLocation(fmt.Sprintf("invalid place projection kind %d", proj.Kind))
		}
	}

	return loc, nil
}

func (vm *VM) loadLocationRaw(loc Location) (Value, *VMError) {
	switch loc.Kind {
	case LKLocal:
		if loc.Frame < 0 || loc.Frame >= len(vm.Stack) {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("invalid local frame %d", loc.Frame))
		}
		frame := &vm.Stack[loc.Frame]
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
		if loc.Index < 0 || loc.Index >= len(obj.Fields) {
			return Value{}, vm.eb.fieldIndexOutOfRange(loc.Index, len(obj.Fields))
		}
		return obj.Fields[loc.Index], nil

	case LKArrayElem:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if obj.Kind != OKArray {
			return Value{}, vm.eb.invalidLocation(fmt.Sprintf("expected array handle, got %v", obj.Kind))
		}
		if loc.Index < 0 || loc.Index >= len(obj.Arr) {
			return Value{}, vm.eb.arrayIndexOutOfRange(loc.Index, len(obj.Arr))
		}
		return obj.Arr[loc.Index], nil

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
		if loc.Frame < 0 || loc.Frame >= len(vm.Stack) {
			return vm.eb.invalidLocation(fmt.Sprintf("invalid local frame %d", loc.Frame))
		}
		frame := &vm.Stack[loc.Frame]
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
		if loc.Index < 0 || loc.Index >= len(obj.Fields) {
			return vm.eb.fieldIndexOutOfRange(loc.Index, len(obj.Fields))
		}
		vm.dropValue(obj.Fields[loc.Index])
		obj.Fields[loc.Index] = val
		return nil

	case LKArrayElem:
		obj, vmErr := vm.heapAliveForRef(loc.Handle)
		if vmErr != nil {
			return vmErr
		}
		if obj.Kind != OKArray {
			return vm.eb.invalidLocation(fmt.Sprintf("expected array handle, got %v", obj.Kind))
		}
		if loc.Index < 0 || loc.Index >= len(obj.Arr) {
			return vm.eb.arrayIndexOutOfRange(loc.Index, len(obj.Arr))
		}
		vm.dropValue(obj.Arr[loc.Index])
		obj.Arr[loc.Index] = val
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
	if !obj.Alive {
		return nil, vm.eb.referenceToFreedObject(fmt.Sprintf("reference to freed object: handle %d (alloc=%d)", h, obj.AllocID))
	}
	return obj, nil
}
