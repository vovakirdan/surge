package vm

import (
	"fmt"
	"math"

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
			value, vmErr := vm.toInt64ForCast(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			minVal, maxVal, ok := intRangeForWidth(dstTT.Width)
			if !ok {
				return Value{}, vm.eb.unimplemented("__to to fixed-width int")
			}
			if value < minVal || value > maxVal {
				return Value{}, vm.eb.invalidNumericConversion("integer overflow")
			}
			return MakeInt(value, dstType), nil
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
			s := vm.stringBytes(vm.Heap.Get(src.H))
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
			value, vmErr := vm.toUint64ForCast(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			maxVal, ok := uintMaxForWidth(dstTT.Width)
			if !ok {
				return Value{}, vm.eb.unimplemented("__to to fixed-width uint")
			}
			if value > maxVal || value > math.MaxInt64 {
				return Value{}, vm.eb.invalidNumericConversion("unsigned overflow")
			}
			return MakeInt(int64(value), dstType), nil
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
			s := vm.stringBytes(vm.Heap.Get(src.H))
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
			s := vm.stringBytes(vm.Heap.Get(src.H))
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

func intRangeForWidth(width types.Width) (int64, int64, bool) {
	switch width {
	case types.Width8:
		return math.MinInt8, math.MaxInt8, true
	case types.Width16:
		return math.MinInt16, math.MaxInt16, true
	case types.Width32:
		return math.MinInt32, math.MaxInt32, true
	case types.Width64:
		return math.MinInt64, math.MaxInt64, true
	default:
		return 0, 0, false
	}
}

func uintMaxForWidth(width types.Width) (uint64, bool) {
	switch width {
	case types.Width8:
		return math.MaxUint8, true
	case types.Width16:
		return math.MaxUint16, true
	case types.Width32:
		return math.MaxUint32, true
	case types.Width64:
		return math.MaxUint64, true
	default:
		return 0, false
	}
}

func (vm *VM) toInt64ForCast(src Value) (int64, *VMError) {
	switch src.Kind {
	case VKInt:
		return src.Int, nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(src)
		if vmErr != nil {
			return 0, vmErr
		}
		val, ok := i.Int64()
		if !ok {
			return 0, vm.eb.invalidNumericConversion("integer overflow")
		}
		return val, nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(src)
		if vmErr != nil {
			return 0, vmErr
		}
		val, ok := u.Uint64()
		if !ok || val > math.MaxInt64 {
			return 0, vm.eb.invalidNumericConversion("integer overflow")
		}
		return int64(val), nil
	case VKBigFloat:
		f, vmErr := vm.mustBigFloat(src)
		if vmErr != nil {
			return 0, vmErr
		}
		i, err := bignum.FloatToIntTrunc(f)
		if err != nil {
			return 0, vm.bignumErr(err)
		}
		val, ok := i.Int64()
		if !ok {
			return 0, vm.eb.invalidNumericConversion("integer overflow")
		}
		return val, nil
	case VKHandleString:
		s := vm.stringBytes(vm.Heap.Get(src.H))
		i, err := bignum.ParseInt(s)
		if err != nil {
			return 0, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as int: %v", s, err))
		}
		val, ok := i.Int64()
		if !ok {
			return 0, vm.eb.invalidNumericConversion("integer overflow")
		}
		return val, nil
	case VKBool:
		if src.Bool {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, vm.eb.unimplemented("__to to fixed-width int")
	}
}

func (vm *VM) toUint64ForCast(src Value) (uint64, *VMError) {
	switch src.Kind {
	case VKInt:
		if src.Int < 0 {
			return 0, vm.eb.invalidNumericConversion("cannot convert negative int to uint")
		}
		return uint64(src.Int), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(src)
		if vmErr != nil {
			return 0, vmErr
		}
		if i.Neg && !i.IsZero() {
			return 0, vm.eb.invalidNumericConversion("cannot convert negative int to uint")
		}
		u := i.Abs()
		val, ok := u.Uint64()
		if !ok {
			return 0, vm.eb.invalidNumericConversion("unsigned overflow")
		}
		return val, nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(src)
		if vmErr != nil {
			return 0, vmErr
		}
		val, ok := u.Uint64()
		if !ok {
			return 0, vm.eb.invalidNumericConversion("unsigned overflow")
		}
		return val, nil
	case VKBigFloat:
		f, vmErr := vm.mustBigFloat(src)
		if vmErr != nil {
			return 0, vmErr
		}
		i, err := bignum.FloatToIntTrunc(f)
		if err != nil {
			return 0, vm.bignumErr(err)
		}
		if i.Neg && !i.IsZero() {
			return 0, vm.eb.invalidNumericConversion("cannot convert negative float to uint")
		}
		u := i.Abs()
		val, ok := u.Uint64()
		if !ok {
			return 0, vm.eb.invalidNumericConversion("unsigned overflow")
		}
		return val, nil
	case VKHandleString:
		s := vm.stringBytes(vm.Heap.Get(src.H))
		u, err := bignum.ParseUint(s)
		if err != nil {
			return 0, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as uint: %v", s, err))
		}
		val, ok := u.Uint64()
		if !ok {
			return 0, vm.eb.invalidNumericConversion("unsigned overflow")
		}
		return val, nil
	case VKBool:
		if src.Bool {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, vm.eb.unimplemented("__to to fixed-width uint")
	}
}
