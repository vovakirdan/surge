package vm

import (
	"fmt"

	"fortio.org/safecast"
)

type arrayView struct {
	baseHandle Handle
	baseObj    *Object
	start      int
	length     int
}

func (vm *VM) arrayViewFromHandle(handle Handle) (arrayView, *VMError) {
	obj := vm.Heap.Get(handle)
	if obj == nil {
		return arrayView{}, vm.eb.makeError(PanicOutOfBounds, "invalid array handle")
	}
	switch obj.Kind {
	case OKArray:
		return arrayView{
			baseHandle: handle,
			baseObj:    obj,
			start:      0,
			length:     len(obj.Arr),
		}, nil
	case OKArraySlice:
		return vm.arrayViewFromSlice(obj)
	default:
		return arrayView{}, vm.eb.typeMismatch("array", fmt.Sprintf("%v", obj.Kind))
	}
}

func (vm *VM) arrayViewFromSlice(obj *Object) (arrayView, *VMError) {
	if obj == nil || obj.Kind != OKArraySlice {
		return arrayView{}, vm.eb.typeMismatch("array slice", "nil")
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
		if start+length > len(baseObj.Arr) {
			return arrayView{}, vm.eb.makeError(PanicOutOfBounds, "array slice out of bounds")
		}
		return arrayView{
			baseHandle: baseHandle,
			baseObj:    baseObj,
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
			baseObj:    baseView.baseObj,
			start:      baseView.start + start,
			length:     length,
		}, nil
	default:
		return arrayView{}, vm.eb.typeMismatch("array", fmt.Sprintf("%v", baseObj.Kind))
	}
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
