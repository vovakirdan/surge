package llvm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
)

type numericKind uint8

const (
	numericNone numericKind = iota
	numericInt
	numericUint
	numericFloat
)

func (fe *funcEmitter) emitPanicNumeric(msg string) error {
	ptr, dataLen, err := fe.emitBytesConst(msg)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_panic_numeric(ptr %s, i64 %d)\n", ptr, dataLen)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}

func (fe *funcEmitter) emitCheckedBigIntToI64(val, msg string) (string, error) {
	outPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", outPtr)
	okVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_bigint_to_i64(ptr %s, ptr %s)\n", okVal, val, outPtr)
	okBB := fe.nextInlineBlock()
	badBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, okBB, badBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", badBB)
	if err := fe.emitPanicNumeric(msg); err != nil {
		return "", err
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
	outVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", outVal, outPtr)
	return outVal, nil
}

func (fe *funcEmitter) emitCheckedBigUintToU64(val, msg string) (string, error) {
	outPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", outPtr)
	okVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_biguint_to_u64(ptr %s, ptr %s)\n", okVal, val, outPtr)
	okBB := fe.nextInlineBlock()
	badBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, okBB, badBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", badBB)
	if err := fe.emitPanicNumeric(msg); err != nil {
		return "", err
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
	outVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", outVal, outPtr)
	return outVal, nil
}

func (fe *funcEmitter) emitCheckedBigFloatToF64(val string) (string, error) {
	outPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca double\n", outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store double 0.0, ptr %s\n", outPtr)
	okVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_bigfloat_to_f64(ptr %s, ptr %s)\n", okVal, val, outPtr)
	okBB := fe.nextInlineBlock()
	badBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, okBB, badBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", badBB)
	if err := fe.emitPanicNumeric("float overflow"); err != nil {
		return "", err
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
	outVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load double, ptr %s\n", outVal, outPtr)
	return outVal, nil
}

func llvmTypeSizeAlign(ty string) (size, align int, err error) {
	switch ty {
	case "i1", "i8":
		return 1, 1, nil
	case "i16", "half":
		return 2, 2, nil
	case "i32", "float":
		return 4, 4, nil
	case "i64", "double", "ptr":
		return 8, 8, nil
	default:
		return 0, 0, fmt.Errorf("unsupported llvm type size for %s", ty)
	}
}

func roundUpInt(n, align int) int {
	if align <= 1 {
		return n
	}
	r := n % align
	if r == 0 {
		return n
	}
	return n + (align - r)
}

func safeLocalID(i int) (mir.LocalID, error) {
	localID, err := safecast.Conv[mir.LocalID](i)
	if err != nil {
		return mir.NoLocalID, fmt.Errorf("local id overflow: %w", err)
	}
	return localID, nil
}

func safeGlobalID(i int) (mir.GlobalID, error) {
	globalID, err := safecast.Conv[mir.GlobalID](i)
	if err != nil {
		return mir.NoGlobalID, fmt.Errorf("global id overflow: %w", err)
	}
	return globalID, nil
}

func numericKindOf(typesIn *types.Interner, id types.TypeID) numericKind {
	if typesIn == nil || id == types.NoTypeID {
		return numericNone
	}
	id = resolveValueType(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return numericNone
	}
	switch tt.Kind {
	case types.KindInt:
		return numericInt
	case types.KindUint:
		return numericUint
	case types.KindFloat:
		return numericFloat
	default:
		return numericNone
	}
}

func isNumericType(typesIn *types.Interner, id types.TypeID) bool {
	return numericKindOf(typesIn, id) != numericNone
}

func llvmNumericTypeID(typesIn *types.Interner, llvmTy string, kind numericKind) types.TypeID {
	if typesIn == nil {
		return types.NoTypeID
	}
	b := typesIn.Builtins()
	switch kind {
	case numericInt:
		switch llvmTy {
		case "i8":
			return b.Int8
		case "i16":
			return b.Int16
		case "i32":
			return b.Int32
		case "i64":
			return b.Int64
		default:
			return types.NoTypeID
		}
	case numericUint:
		switch llvmTy {
		case "i8":
			return b.Uint8
		case "i16":
			return b.Uint16
		case "i32":
			return b.Uint32
		case "i64":
			return b.Uint64
		default:
			return types.NoTypeID
		}
	case numericFloat:
		switch llvmTy {
		case "half":
			return b.Float16
		case "float":
			return b.Float32
		case "double":
			return b.Float64
		default:
			return types.NoTypeID
		}
	default:
		return types.NoTypeID
	}
}

func (fe *funcEmitter) coerceNumericValue(val, valTy string, srcType, dstType types.TypeID) (outVal, outTy string, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return val, valTy, nil
	}
	if srcType == types.NoTypeID || dstType == types.NoTypeID {
		return val, valTy, nil
	}
	srcType = resolveValueType(fe.emitter.types, srcType)
	dstType = resolveValueType(fe.emitter.types, dstType)
	if srcType == dstType {
		return val, valTy, nil
	}
	if !isNumericType(fe.emitter.types, srcType) || !isNumericType(fe.emitter.types, dstType) {
		return val, valTy, nil
	}
	casted, castTy, err := fe.emitNumericCast(val, valTy, srcType, dstType)
	if err != nil {
		return "", "", err
	}
	return casted, castTy, nil
}

type numericPair struct {
	leftVal  string
	leftTy   string
	leftType types.TypeID

	rightVal  string
	rightTy   string
	rightType types.TypeID
}

func newNumericPair(leftVal, leftTy string, leftType types.TypeID, rightVal, rightTy string, rightType types.TypeID) numericPair {
	return numericPair{
		leftVal:   leftVal,
		leftTy:    leftTy,
		leftType:  leftType,
		rightVal:  rightVal,
		rightTy:   rightTy,
		rightType: rightType,
	}
}

func (fe *funcEmitter) coerceNumericPair(leftVal, leftTy string, leftType types.TypeID, rightVal, rightTy string, rightType types.TypeID) (numericPair, error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return newNumericPair(leftVal, leftTy, leftType, rightVal, rightTy, rightType), nil
	}
	leftType = resolveValueType(fe.emitter.types, leftType)
	rightType = resolveValueType(fe.emitter.types, rightType)
	leftKind := numericKindOf(fe.emitter.types, leftType)
	rightKind := numericKindOf(fe.emitter.types, rightType)
	if leftType == types.NoTypeID || rightType == types.NoTypeID {
		if leftType == types.NoTypeID && rightKind != numericNone {
			if inferred := llvmNumericTypeID(fe.emitter.types, leftTy, rightKind); inferred != types.NoTypeID {
				leftType = inferred
			}
		}
		if rightType == types.NoTypeID && leftKind != numericNone {
			if inferred := llvmNumericTypeID(fe.emitter.types, rightTy, leftKind); inferred != types.NoTypeID {
				rightType = inferred
			}
		}
		if leftType == types.NoTypeID && rightKind != numericNone && leftTy == "ptr" {
			leftType = rightType
		}
		if rightType == types.NoTypeID && leftKind != numericNone && rightTy == "ptr" {
			rightType = leftType
		}
		if leftType == types.NoTypeID || rightType == types.NoTypeID {
			return newNumericPair(leftVal, leftTy, leftType, rightVal, rightTy, rightType), nil
		}
		leftKind = numericKindOf(fe.emitter.types, leftType)
		rightKind = numericKindOf(fe.emitter.types, rightType)
	}
	if leftKind == numericNone || rightKind == numericNone || leftKind != rightKind {
		return newNumericPair(leftVal, leftTy, leftType, rightVal, rightTy, rightType), nil
	}
	if leftType == rightType {
		return newNumericPair(leftVal, leftTy, leftType, rightVal, rightTy, rightType), nil
	}

	leftBig := isBigIntType(fe.emitter.types, leftType) || isBigUintType(fe.emitter.types, leftType) || isBigFloatType(fe.emitter.types, leftType)
	rightBig := isBigIntType(fe.emitter.types, rightType) || isBigUintType(fe.emitter.types, rightType) || isBigFloatType(fe.emitter.types, rightType)

	if leftBig && !rightBig {
		casted, castTy, err := fe.emitNumericCast(rightVal, rightTy, rightType, leftType)
		if err != nil {
			return newNumericPair("", "", leftType, "", "", rightType), err
		}
		return newNumericPair(leftVal, leftTy, leftType, casted, castTy, leftType), nil
	}
	if rightBig && !leftBig {
		casted, castTy, err := fe.emitNumericCast(leftVal, leftTy, leftType, rightType)
		if err != nil {
			return newNumericPair("", "", leftType, "", "", rightType), err
		}
		return newNumericPair(casted, castTy, rightType, rightVal, rightTy, rightType), nil
	}

	if !leftBig && !rightBig {
		switch leftKind {
		case numericInt, numericUint:
			leftMeta, leftOK := intInfo(fe.emitter.types, leftType)
			rightMeta, rightOK := intInfo(fe.emitter.types, rightType)
			if !leftOK || !rightOK || leftMeta.bits == rightMeta.bits {
				return newNumericPair(leftVal, leftTy, leftType, rightVal, rightTy, rightType), nil
			}
			if leftMeta.bits > rightMeta.bits {
				casted, castTy, err := fe.emitNumericCast(rightVal, rightTy, rightType, leftType)
				if err != nil {
					return newNumericPair("", "", leftType, "", "", rightType), err
				}
				return newNumericPair(leftVal, leftTy, leftType, casted, castTy, leftType), nil
			}
			casted, castTy, err := fe.emitNumericCast(leftVal, leftTy, leftType, rightType)
			if err != nil {
				return newNumericPair("", "", leftType, "", "", rightType), err
			}
			return newNumericPair(casted, castTy, rightType, rightVal, rightTy, rightType), nil
		case numericFloat:
			leftMeta, leftOK := floatInfo(fe.emitter.types, leftType)
			rightMeta, rightOK := floatInfo(fe.emitter.types, rightType)
			if !leftOK || !rightOK || leftMeta.bits == rightMeta.bits {
				return newNumericPair(leftVal, leftTy, leftType, rightVal, rightTy, rightType), nil
			}
			if leftMeta.bits > rightMeta.bits {
				casted, castTy, err := fe.emitNumericCast(rightVal, rightTy, rightType, leftType)
				if err != nil {
					return newNumericPair("", "", leftType, "", "", rightType), err
				}
				return newNumericPair(leftVal, leftTy, leftType, casted, castTy, leftType), nil
			}
			casted, castTy, err := fe.emitNumericCast(leftVal, leftTy, leftType, rightType)
			if err != nil {
				return newNumericPair("", "", leftType, "", "", rightType), err
			}
			return newNumericPair(casted, castTy, rightType, rightVal, rightTy, rightType), nil
		}
	}

	return newNumericPair(leftVal, leftTy, leftType, rightVal, rightTy, rightType), nil
}

func operandValueType(typesIn *types.Interner, op *mir.Operand) types.TypeID {
	if op == nil {
		return types.NoTypeID
	}
	if op.Kind == mir.OperandAddrOf || op.Kind == mir.OperandAddrOfMut {
		if next, ok := derefType(typesIn, op.Type); ok {
			return next
		}
	}
	return op.Type
}

func derefType(typesIn *types.Interner, id types.TypeID) (types.TypeID, bool) {
	if typesIn == nil || id == types.NoTypeID {
		return types.NoTypeID, false
	}
	for i := 0; i < 32 && id != types.NoTypeID; i++ {
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return types.NoTypeID, false
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID {
				return types.NoTypeID, false
			}
			id = target
		case types.KindOwn, types.KindReference, types.KindPointer:
			return tt.Elem, true
		default:
			return types.NoTypeID, false
		}
	}
	return types.NoTypeID, false
}
