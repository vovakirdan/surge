package vm

import (
	"fmt"

	"fortio.org/safecast"
)

// arrayView names a run of elements without saying where they live. The two
// bases are a heap array object, whose elements are whole Values in a Go slice,
// and an arena extent, whose elements are layout-encoded bytes reached through
// StorageRef. Every reader goes through the accessors below rather than reaching
// for heapObj, because only one of the two is ever set.
type arrayView struct {
	baseHandle Handle
	heapObj    *Object
	baseRef    StorageRef
	start      int
	length     int
}

// inArena reports whether this view's elements live in an arena rather than in
// a heap array object.
func (v arrayView) inArena() bool { return v.baseRef.Arena != nil }

func (vm *VM) arrayViewFromHandle(handle Handle) (arrayView, *VMError) {
	obj := vm.Heap.Get(handle)
	if obj == nil {
		return arrayView{}, vm.eb.makeError(PanicOutOfBounds, "invalid array handle")
	}
	switch obj.Kind {
	case OKArray:
		return arrayView{
			baseHandle: handle,
			heapObj:    obj,
			start:      0,
			length:     obj.ArrLen,
		}, nil
	case OKArraySlice:
		return vm.arrayViewFromSlice(obj)
	default:
		return arrayView{}, vm.eb.typeMismatch("array", fmt.Sprintf("%v", obj.Kind))
	}
}

// arrayViewFromComposite views a fixed array in ordinary storage as an array.
// Its elements are the composite's members, so their count is the array's
// length and the layout registry already knows their stride.
func (vm *VM) arrayViewFromComposite(owner StorageRef) (arrayView, *VMError) {
	members, err := vm.compositeMembers(owner.TypeID)
	if err != nil {
		return arrayView{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return arrayView{baseRef: owner, start: 0, length: len(members)}, nil
}

func (vm *VM) arrayViewFromSlice(obj *Object) (arrayView, *VMError) {
	if obj == nil || obj.Kind != OKArraySlice {
		return arrayView{}, vm.eb.typeMismatch("array slice", "nil")
	}
	if obj.ArrSliceBase == 0 && obj.ArrSliceStorage.Arena != nil {
		base, vmErr := vm.arrayViewFromComposite(obj.ArrSliceStorage)
		if vmErr != nil {
			return arrayView{}, vmErr
		}
		start, length := obj.ArrSliceStart, obj.ArrSliceLen
		if start < 0 || length < 0 || start+length > base.length {
			return arrayView{}, vm.eb.makeError(PanicOutOfBounds, "array slice out of bounds")
		}
		base.start, base.length = start, length
		return base, nil
	}
	baseHandle := obj.ArrSliceBase
	if baseHandle == 0 {
		return arrayView{}, vm.eb.makeError(PanicOutOfBounds, "invalid array slice base handle")
	}
	baseObj := vm.Heap.Get(baseHandle)
	if baseObj == nil {
		return arrayView{}, vm.eb.makeError(PanicOutOfBounds, "invalid array slice base")
	}
	start := obj.ArrSliceStart
	length := obj.ArrSliceLen
	if start < 0 || length < 0 {
		return arrayView{}, vm.eb.makeError(PanicOutOfBounds, "invalid array slice bounds")
	}
	switch baseObj.Kind {
	case OKArray:
		if start+length > baseObj.ArrLen {
			return arrayView{}, vm.eb.makeError(PanicOutOfBounds, "array slice out of bounds")
		}
		return arrayView{
			baseHandle: baseHandle,
			heapObj:    baseObj,
			start:      start,
			length:     length,
		}, nil
	case OKArraySlice:
		baseView, vmErr := vm.arrayViewFromHandle(baseHandle)
		if vmErr != nil {
			return arrayView{}, vmErr
		}
		if start+length > baseView.length {
			return arrayView{}, vm.eb.makeError(PanicOutOfBounds, "array slice out of bounds")
		}
		return arrayView{
			baseHandle: baseView.baseHandle,
			heapObj:    baseView.heapObj,
			baseRef:    baseView.baseRef,
			start:      baseView.start + start,
			length:     length,
		}, nil
	default:
		return arrayView{}, vm.eb.typeMismatch("array", fmt.Sprintf("%v", baseObj.Kind))
	}
}

// The five accessors below are the only places that know which of the two bases
// a view has. Everything else asks them.

// viewElemRef names element i's extent.
//
// Both bases answer now. A fixed array in ordinary storage is a composite whose
// members ARE its elements, so it is projected through its member list; a heap
// array's elements are a run, addressed by arithmetic off the descriptor. What
// the two share is the thing that matters to every caller above: an element is
// an extent, and there is exactly one way to read or write one.
func (vm *VM) viewElemRef(view arrayView, i int) (StorageRef, *VMError) {
	if view.inArena() {
		return vm.memberStorage(view.baseRef, view.start+i)
	}
	obj := view.heapObj
	if obj == nil {
		return StorageRef{}, vm.eb.makeError(PanicOutOfBounds, "array view has no base")
	}
	ref, err := vm.runElemRef(obj.ArrElems, obj.ArrElemType, view.start+i)
	if err != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return ref, nil
}

// viewElemLocation names element i as a place to read or write through.
//
// The heap case names the BASE handle and an absolute index rather than the
// slice header and a view-relative one. Both denote the same element, because
// every reader re-derives a view from whatever handle it is given, but naming
// the base survives being captured into task state, where a slice header may
// not.
func (vm *VM) viewElemLocation(view arrayView, i int, isMut bool) (Location, *VMError) {
	if view.inArena() {
		ref, vmErr := vm.viewElemRef(view, i)
		if vmErr != nil {
			return Location{}, vmErr
		}
		return Location{Kind: LKStorage, Storage: ref, IsMut: isMut}, nil
	}
	index, err := safecast.Conv[int32](view.start + i)
	if err != nil {
		return Location{}, vm.eb.invalidLocation("array index overflow")
	}
	return Location{
		Kind:   LKArrayElem,
		Handle: view.baseHandle,
		Index:  index,
		IsMut:  isMut,
	}, nil
}

// viewElemValue reads element i as an owned value the caller must release.
func (vm *VM) viewElemValue(frame *Frame, view arrayView, i int) (Value, *VMError) {
	ref, vmErr := vm.viewElemRef(view, i)
	if vmErr != nil {
		return Value{}, vmErr
	}
	return vm.readStorageValue(frame, ref)
}

// viewElemPeek reads element i without taking a count on it.
func (vm *VM) viewElemPeek(view arrayView, i int) (Value, *VMError) {
	ref, vmErr := vm.viewElemRef(view, i)
	if vmErr != nil {
		return Value{}, vmErr
	}
	return vm.peekStorage(ref)
}

// viewElemStore writes element i, releasing whatever was there.
//
// This is a REPLACE at both bases now: the slot is initialised, so what it held
// is the store's to release. A slot past the length is not reachable here — an
// index is bounds-checked against the view before it gets this far — which is
// what keeps the dead tail out of a path that would drop it.
func (vm *VM) viewElemStore(frame *Frame, view arrayView, i int, val Value) *VMError {
	ref, vmErr := vm.viewElemRef(view, i)
	if vmErr != nil {
		return vmErr
	}
	return vm.storeStorage(frame, ref, val)
}

// viewByteAt reads element i of a byte array. Its callers all take a `uint8[]`
// argument, which since fixed arrays became composites can be a slice over an
// arena rather than over a heap array.
func (vm *VM) viewByteAt(view arrayView, i int) (byte, *VMError) {
	elem, vmErr := vm.viewElemPeek(view, i)
	if vmErr != nil {
		return 0, vmErr
	}
	return vm.valueToUint8(elem)
}

func (vm *VM) arrayIndexFromValue(idx Value, length int) (int, *VMError) {
	maxIndex := int(^uint(0) >> 1)
	maxInt := int64(maxIndex)
	maxUint := uint64(^uint(0) >> 1)
	length64 := int64(length)
	var index64 int64

	switch idx.Kind {
	case VKInt:
		index64 = idx.Int
	case VKBigInt:
		i, vmErr := vm.mustBigInt(idx)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok {
			return 0, vm.eb.arrayIndexOutOfRange(maxIndex, length)
		}
		index64 = n
	case VKBigUint:
		u, vmErr := vm.mustBigUint(idx)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > maxUint {
			return 0, vm.eb.arrayIndexOutOfRange(maxIndex, length)
		}
		index64 = int64(n)
	default:
		return 0, vm.eb.typeMismatch("int", idx.Kind.String())
	}

	if index64 < -maxInt || index64 > maxInt {
		return 0, vm.eb.arrayIndexOutOfRange(maxIndex, length)
	}
	if index64 < 0 {
		index64 += length64
	}
	if index64 < 0 || index64 >= length64 {
		return 0, vm.eb.arrayIndexOutOfRange(int(index64), length)
	}
	ni, err := safecast.Conv[int](index64)
	if err != nil {
		return 0, vm.eb.arrayIndexOutOfRange(maxIndex, length)
	}
	return ni, nil
}

func (vm *VM) arrayOwnedFromValue(val Value) (*Object, *VMError) {
	if val.Kind == VKRef || val.Kind == VKRefMut {
		loaded, vmErr := vm.loadLocationRaw(val.Loc)
		if vmErr != nil {
			return nil, vmErr
		}
		val = loaded
	}
	if val.Kind != VKHandleArray {
		return nil, vm.eb.typeMismatch("array", val.Kind.String())
	}
	obj, vmErr := vm.heapAliveForRef(val.H)
	if vmErr != nil {
		return nil, vmErr
	}
	switch obj.Kind {
	case OKArray:
		return obj, nil
	case OKArraySlice:
		return nil, vm.eb.makeError(PanicTypeMismatch, "array view is not resizable")
	default:
		return nil, vm.eb.typeMismatch("array", fmt.Sprintf("%v", obj.Kind))
	}
}
