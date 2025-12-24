package vm

import "surge/internal/types"

type ptrRange struct {
	kind   LocKind
	handle Handle
	start  int
	end    int
}

func (vm *VM) pointerRange(ptrVal Value, n int) (ptrRange, bool, *VMError) {
	if n < 0 {
		return ptrRange{}, false, vm.eb.invalidNumericConversion("byte length out of range")
	}
	switch ptrVal.Loc.Kind {
	case LKRawBytes:
		if ptrVal.Loc.Handle == 0 {
			if n == 0 {
				return ptrRange{}, false, nil
			}
			return ptrRange{}, false, vm.eb.makeError(PanicInvalidHandle, "invalid raw handle 0")
		}
		alloc, vmErr := vm.rawGet(ptrVal.Loc.Handle)
		if vmErr != nil {
			return ptrRange{}, false, vmErr
		}
		off := int(ptrVal.Loc.ByteOffset)
		end := off + n
		if off < 0 || end < off || end > len(alloc.data) {
			return ptrRange{}, false, vm.eb.outOfBounds(end, len(alloc.data))
		}
		return ptrRange{kind: LKRawBytes, handle: ptrVal.Loc.Handle, start: off, end: end}, true, nil
	case LKArrayElem:
		view, vmErr := vm.arrayViewFromHandle(ptrVal.Loc.Handle)
		if vmErr != nil {
			return ptrRange{}, false, vmErr
		}
		start := view.start + int(ptrVal.Loc.Index)
		end := start + n
		limit := view.start + view.length
		if start < view.start || end < start || end > limit {
			return ptrRange{}, false, vm.eb.outOfBounds(end, view.length)
		}
		return ptrRange{kind: LKArrayElem, handle: view.baseHandle, start: start, end: end}, true, nil
	case LKStringBytes:
		return ptrRange{}, false, nil
	default:
		return ptrRange{}, false, vm.eb.invalidLocation("unsupported pointer kind")
	}
}

func (vm *VM) writeBytesToPointer(ptrVal Value, data []byte) *VMError {
	if ptrVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*byte", ptrVal.Kind.String())
	}
	switch ptrVal.Loc.Kind {
	case LKRawBytes:
		if ptrVal.Loc.Handle == 0 {
			if len(data) == 0 {
				return nil
			}
			return vm.eb.makeError(PanicInvalidHandle, "invalid raw handle 0")
		}
		alloc, vmErr := vm.rawGet(ptrVal.Loc.Handle)
		if vmErr != nil {
			return vmErr
		}
		off := int(ptrVal.Loc.ByteOffset)
		end := off + len(data)
		if off < 0 || end < off || end > len(alloc.data) {
			return vm.eb.outOfBounds(end, len(alloc.data))
		}
		copy(alloc.data[off:end], data)
		return nil
	case LKArrayElem:
		view, vmErr := vm.arrayViewFromHandle(ptrVal.Loc.Handle)
		if vmErr != nil {
			return vmErr
		}
		start := view.start + int(ptrVal.Loc.Index)
		end := start + len(data)
		limit := view.start + view.length
		if start < view.start || end < start || end > limit {
			return vm.eb.outOfBounds(end, view.length)
		}
		elemType := types.NoTypeID
		if vm.Types != nil && ptrVal.TypeID != types.NoTypeID {
			if t, ok := vm.Types.Lookup(ptrVal.TypeID); ok && t.Kind == types.KindPointer {
				elemType = t.Elem
			}
			if elemType == types.NoTypeID {
				elemType = vm.Types.Builtins().Uint8
			}
		}
		for i, b := range data {
			idx := start + i
			vm.dropValue(view.baseObj.Arr[idx])
			view.baseObj.Arr[idx] = MakeInt(int64(b), elemType)
		}
		return nil
	case LKStringBytes:
		return vm.eb.makeError(PanicInvalidLocation, "cannot write to string bytes")
	default:
		return vm.eb.invalidLocation("unsupported pointer kind")
	}
}

func rangesOverlap(aStart, aEnd, bStart, bEnd int) bool {
	return aStart < bEnd && bStart < aEnd
}
