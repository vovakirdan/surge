package vm

import (
	"fmt"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

func (vm *VM) evalIntrinsicTo(src Value, dstType types.TypeID) (Value, *VMError) {
	if dstType == types.NoTypeID {
		return src, nil
	}
	if vm.Types == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "__to requires type information")
	}

	dstValTy := vm.valueType(dstType)
	dstTT, ok := vm.Types.Lookup(dstValTy)
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("__to: unknown destination type type#%d", dstValTy))
	}

	// Legacy: allow custom exit code structs with `code: int`.
	if dstValTy == vm.Types.Builtins().Int && src.Kind == VKHandleStruct {
		obj := vm.Heap.Get(src.H)
		layout, vmErr := vm.layouts.Struct(obj.TypeID)
		if vmErr != nil {
			return Value{}, vmErr
		}
		idx, ok := layout.IndexByName["code"]
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("type#%d has no field \"code\" for __to(int)", obj.TypeID))
		}
		if idx < 0 || idx >= len(obj.Fields) {
			return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("field index %d out of bounds for type#%d", idx, obj.TypeID))
		}
		field := obj.Fields[idx]
		switch field.Kind {
		case VKBigInt:
			// Convert by retaining the field, then letting the caller drop the struct.
			out, vmErr := vm.cloneForShare(field)
			if vmErr != nil {
				return Value{}, vmErr
			}
			out.TypeID = dstType
			return out, nil
		case VKInt:
			return vm.makeBigInt(dstType, bignum.IntFromInt64(field.Int)), nil
		default:
			return Value{}, vm.eb.typeMismatch("int", field.Kind.String())
		}
	}

	switch dstTT.Kind {
	case types.KindString:
		strTy := dstType
		switch src.Kind {
		case VKHandleString:
			src.TypeID = strTy
			return src, nil
		case VKBigInt:
			i, vmErr := vm.mustBigInt(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			h := vm.Heap.AllocString(strTy, bignum.FormatInt(i))
			return MakeHandleString(h, strTy), nil
		case VKBigUint:
			u, vmErr := vm.mustBigUint(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			h := vm.Heap.AllocString(strTy, bignum.FormatUint(u))
			return MakeHandleString(h, strTy), nil
		case VKBigFloat:
			f, vmErr := vm.mustBigFloat(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			s, err := bignum.FormatFloat(f)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			h := vm.Heap.AllocString(strTy, s)
			return MakeHandleString(h, strTy), nil
		case VKBool:
			s := "false"
			if src.Bool {
				s = "true"
			}
			h := vm.Heap.AllocString(strTy, s)
			return MakeHandleString(h, strTy), nil
		case VKInt:
			h := vm.Heap.AllocString(strTy, bignum.FormatInt(bignum.IntFromInt64(src.Int)))
			return MakeHandleString(h, strTy), nil
		default:
			return Value{}, vm.eb.unimplemented("__to to string")
		}

	case types.KindInt:
		if dstTT.Width != types.WidthAny {
			return Value{}, vm.eb.unimplemented("__to to fixed-width int")
		}
		switch src.Kind {
		case VKBigInt:
			src.TypeID = dstType
			return src, nil
		case VKBigUint:
			u, vmErr := vm.mustBigUint(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return vm.makeBigInt(dstType, bignum.BigInt{Limbs: u.Limbs}), nil
		case VKBigFloat:
			f, vmErr := vm.mustBigFloat(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			i, err := bignum.FloatToIntTrunc(f)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(dstType, i), nil
		case VKHandleString:
			s := vm.Heap.Get(src.H).Str
			i, err := bignum.ParseInt(s)
			if err != nil {
				return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as int: %v", s, err))
			}
			return vm.makeBigInt(dstType, i), nil
		case VKInt:
			return vm.makeBigInt(dstType, bignum.IntFromInt64(src.Int)), nil
		case VKBool:
			n := int64(0)
			if src.Bool {
				n = 1
			}
			return vm.makeBigInt(dstType, bignum.IntFromInt64(n)), nil
		default:
			return Value{}, vm.eb.unimplemented("__to to int")
		}

	case types.KindUint:
		if dstTT.Width != types.WidthAny {
			return Value{}, vm.eb.unimplemented("__to to fixed-width uint")
		}
		switch src.Kind {
		case VKBigUint:
			src.TypeID = dstType
			return src, nil
		case VKBigInt:
			i, vmErr := vm.mustBigInt(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			if i.Neg && !i.IsZero() {
				return Value{}, vm.eb.invalidNumericConversion("cannot convert negative int to uint")
			}
			return vm.makeBigUint(dstType, i.Abs()), nil
		case VKBigFloat:
			f, vmErr := vm.mustBigFloat(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			u, err := bignum.FloatToUintTrunc(f)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(dstType, u), nil
		case VKHandleString:
			s := vm.Heap.Get(src.H).Str
			u, err := bignum.ParseUint(s)
			if err != nil {
				return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as uint: %v", s, err))
			}
			return vm.makeBigUint(dstType, u), nil
		case VKInt:
			if src.Int < 0 {
				return Value{}, vm.eb.invalidNumericConversion("cannot convert negative int to uint")
			}
			return vm.makeBigUint(dstType, bignum.UintFromUint64(uint64(src.Int))), nil
		case VKBool:
			n := uint64(0)
			if src.Bool {
				n = 1
			}
			return vm.makeBigUint(dstType, bignum.UintFromUint64(n)), nil
		default:
			return Value{}, vm.eb.unimplemented("__to to uint")
		}

	case types.KindFloat:
		if dstTT.Width != types.WidthAny {
			return Value{}, vm.eb.unimplemented("__to to fixed-width float")
		}
		switch src.Kind {
		case VKBigFloat:
			src.TypeID = dstType
			return src, nil
		case VKBigInt:
			i, vmErr := vm.mustBigInt(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			f, err := bignum.FloatFromInt(i)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(dstType, f), nil
		case VKBigUint:
			u, vmErr := vm.mustBigUint(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			f, err := bignum.FloatFromUint(u)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(dstType, f), nil
		case VKHandleString:
			s := vm.Heap.Get(src.H).Str
			f, err := bignum.ParseFloat(s)
			if err != nil {
				return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as float: %v", s, err))
			}
			return vm.makeBigFloat(dstType, f), nil
		case VKInt:
			f, err := bignum.FloatFromInt(bignum.IntFromInt64(src.Int))
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(dstType, f), nil
		case VKBool:
			n := int64(0)
			if src.Bool {
				n = 1
			}
			f, err := bignum.FloatFromInt(bignum.IntFromInt64(n))
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(dstType, f), nil
		default:
			return Value{}, vm.eb.unimplemented("__to to float")
		}

	default:
		return Value{}, vm.eb.unimplemented("__to conversion")
	}
}
