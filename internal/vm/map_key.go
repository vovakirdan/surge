package vm

import (
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// Turning a value into the key a map is indexed by.
//
// A key is not the value it came from: it is a comparable encoding of it, and
// the two integer widths need their own encodings so that a signed and an
// unsigned key of the same bits do not collide.
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
