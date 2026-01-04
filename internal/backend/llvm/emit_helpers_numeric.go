package llvm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
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
