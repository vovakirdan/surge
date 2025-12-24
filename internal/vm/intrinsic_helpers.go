package vm

import (
	"fmt"
)

// readBytesFromPointer reads n bytes from a pointer value.
func (vm *VM) readBytesFromPointer(ptrVal Value, n int) ([]byte, *VMError) {
	if n < 0 {
		return nil, vm.eb.invalidNumericConversion("byte length out of range")
	}
	switch ptrVal.Loc.Kind {
	case LKStringBytes:
		obj := vm.Heap.Get(ptrVal.Loc.Handle)
		if obj.Kind != OKString {
			return nil, vm.eb.typeMismatch("string bytes pointer", fmt.Sprintf("%v", obj.Kind))
		}
		s := vm.stringBytes(obj)
		off := int(ptrVal.Loc.ByteOffset)
		end := off + n
		if off < 0 || end < off || end > len(s) {
			return nil, vm.eb.outOfBounds(end, len(s))
		}
		return []byte(s[off:end]), nil
	case LKRawBytes:
		if ptrVal.Loc.Handle == 0 {
			if n == 0 {
				return []byte{}, nil
			}
			return nil, vm.eb.makeError(PanicInvalidHandle, "invalid raw handle 0")
		}
		alloc, vmErr := vm.rawGet(ptrVal.Loc.Handle)
		if vmErr != nil {
			return nil, vmErr
		}
		off := int(ptrVal.Loc.ByteOffset)
		end := off + n
		if off < 0 || end < off || end > len(alloc.data) {
			return nil, vm.eb.outOfBounds(end, len(alloc.data))
		}
		out := make([]byte, n)
		copy(out, alloc.data[off:end])
		return out, nil
	case LKArrayElem:
		view, vmErr := vm.arrayViewFromHandle(ptrVal.Loc.Handle)
		if vmErr != nil {
			return nil, vmErr
		}
		start := int(ptrVal.Loc.Index)
		end := start + n
		if start < 0 || end < start || end > view.length {
			return nil, vm.eb.outOfBounds(end, view.length)
		}
		baseStart := view.start + start
		out := make([]byte, n)
		for i := range n {
			b, vmErr := vm.valueToUint8(view.baseObj.Arr[baseStart+i])
			if vmErr != nil {
				return nil, vmErr
			}
			out[i] = b
		}
		return out, nil
	default:
		return nil, vm.eb.invalidLocation("unsupported pointer kind")
	}
}

// readUint16sFromPointer reads n uint16 values from a pointer.
func (vm *VM) readUint16sFromPointer(ptrVal Value, n int) ([]uint16, *VMError) {
	if n < 0 {
		return nil, vm.eb.invalidNumericConversion("uint16 length out of range")
	}
	switch ptrVal.Loc.Kind {
	case LKRawBytes:
		if ptrVal.Loc.Handle == 0 {
			if n == 0 {
				return []uint16{}, nil
			}
			return nil, vm.eb.makeError(PanicInvalidHandle, "invalid raw handle 0")
		}
		alloc, vmErr := vm.rawGet(ptrVal.Loc.Handle)
		if vmErr != nil {
			return nil, vmErr
		}
		off := int(ptrVal.Loc.ByteOffset)
		byteLen := n * 2
		end := off + byteLen
		if off < 0 || end < off || end > len(alloc.data) {
			return nil, vm.eb.outOfBounds(end, len(alloc.data))
		}
		out := make([]uint16, n)
		for i := range n {
			b0 := uint16(alloc.data[off+i*2])
			b1 := uint16(alloc.data[off+i*2+1])
			out[i] = b0 | (b1 << 8)
		}
		return out, nil
	case LKArrayElem:
		view, vmErr := vm.arrayViewFromHandle(ptrVal.Loc.Handle)
		if vmErr != nil {
			return nil, vmErr
		}
		start := int(ptrVal.Loc.Index)
		end := start + n
		if start < 0 || end < start || end > view.length {
			return nil, vm.eb.outOfBounds(end, view.length)
		}
		baseStart := view.start + start
		out := make([]uint16, n)
		for i := range n {
			u, vmErr := vm.valueToUint16(view.baseObj.Arr[baseStart+i])
			if vmErr != nil {
				return nil, vmErr
			}
			out[i] = u
		}
		return out, nil
	default:
		return nil, vm.eb.invalidLocation("unsupported pointer kind")
	}
}

// valueToUint8 converts a value to uint8.
func (vm *VM) valueToUint8(v Value) (byte, *VMError) {
	switch v.Kind {
	case VKInt:
		if v.Int < 0 || v.Int > 255 {
			return 0, vm.eb.invalidNumericConversion("byte out of range")
		}
		return byte(v.Int), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > 255 {
			return 0, vm.eb.invalidNumericConversion("byte out of range")
		}
		return byte(n), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 || n > 255 {
			return 0, vm.eb.invalidNumericConversion("byte out of range")
		}
		return byte(n), nil
	default:
		return 0, vm.eb.typeMismatch("uint8", v.Kind.String())
	}
}

// valueToUint16 converts a value to uint16.
func (vm *VM) valueToUint16(v Value) (uint16, *VMError) {
	switch v.Kind {
	case VKInt:
		if v.Int < 0 || v.Int > 65535 {
			return 0, vm.eb.invalidNumericConversion("uint16 out of range")
		}
		return uint16(v.Int), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > 65535 {
			return 0, vm.eb.invalidNumericConversion("uint16 out of range")
		}
		return uint16(n), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 || n > 65535 {
			return 0, vm.eb.invalidNumericConversion("uint16 out of range")
		}
		return uint16(n), nil
	default:
		return 0, vm.eb.typeMismatch("uint16", v.Kind.String())
	}
}
