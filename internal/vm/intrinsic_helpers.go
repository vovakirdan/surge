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
	case LKArrayElem:
		obj := vm.Heap.Get(ptrVal.Loc.Handle)
		if obj.Kind != OKArray {
			return nil, vm.eb.typeMismatch("array bytes pointer", fmt.Sprintf("%v", obj.Kind))
		}
		start := int(ptrVal.Loc.Index)
		end := start + n
		if start < 0 || end < start || end > len(obj.Arr) {
			return nil, vm.eb.outOfBounds(end, len(obj.Arr))
		}
		out := make([]byte, n)
		for i := range n {
			b, vmErr := vm.valueToUint8(obj.Arr[start+i])
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
	case LKArrayElem:
		obj := vm.Heap.Get(ptrVal.Loc.Handle)
		if obj.Kind != OKArray {
			return nil, vm.eb.typeMismatch("array uint16 pointer", fmt.Sprintf("%v", obj.Kind))
		}
		start := int(ptrVal.Loc.Index)
		end := start + n
		if start < 0 || end < start || end > len(obj.Arr) {
			return nil, vm.eb.outOfBounds(end, len(obj.Arr))
		}
		out := make([]uint16, n)
		for i := range n {
			u, vmErr := vm.valueToUint16(obj.Arr[start+i])
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
