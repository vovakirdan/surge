package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

const (
	durationOpaqueField = "__opaque"
	nanosPerMicro       = int64(1_000)
	nanosPerMilli       = int64(1_000_000)
	nanosPerSecond      = int64(1_000_000_000)
)

func (fe *funcEmitter) emitRtMonotonicNow(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return fmt.Errorf("monotonic_now requires 0 arguments")
	}
	if !call.HasDst {
		return nil
	}
	_, _, _, ok, err := fe.durationLayoutForPlace(call.Dst)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("monotonic_now requires Duration destination")
	}
	elapsedNs := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_monotonic_now()\n", elapsedNs)
	return fe.emitDurationValue(call.Dst, elapsedNs)
}

func (fe *funcEmitter) emitDurationMethodIntrinsic(name string, call *mir.CallInstr) (bool, error) {
	switch name {
	case "sub":
		return fe.emitDurationSub(call)
	case "as_seconds":
		return fe.emitDurationUnit(call, name, nanosPerSecond)
	case "as_millis":
		return fe.emitDurationUnit(call, name, nanosPerMilli)
	case "as_micros":
		return fe.emitDurationUnit(call, name, nanosPerMicro)
	case "as_nanos":
		return fe.emitDurationUnit(call, name, 1)
	default:
		return false, nil
	}
}

func (fe *funcEmitter) emitDurationSub(call *mir.CallInstr) (bool, error) {
	if call == nil || len(call.Args) != 2 {
		return true, fmt.Errorf("duration.sub requires 2 arguments")
	}
	left, ok, err := fe.emitDurationNanosOperand(&call.Args[0])
	if err != nil || !ok {
		return ok, err
	}
	right, ok, err := fe.emitDurationNanosOperand(&call.Args[1])
	if err != nil {
		return true, err
	}
	if !ok {
		return true, fmt.Errorf("duration.sub requires Duration argument")
	}
	if !call.HasDst {
		return true, nil
	}
	diff := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = sub i64 %s, %s\n", diff, left, right)
	return true, fe.emitDurationValue(call.Dst, diff)
}

func (fe *funcEmitter) emitDurationUnit(call *mir.CallInstr, name string, divisor int64) (bool, error) {
	if call == nil || len(call.Args) != 1 {
		return true, fmt.Errorf("%s requires 1 argument", name)
	}
	nanos, ok, err := fe.emitDurationNanosOperand(&call.Args[0])
	if err != nil || !ok {
		return ok, err
	}
	if !call.HasDst {
		return true, nil
	}
	out := nanos
	if divisor != 1 {
		out = fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = sdiv i64 %s, %d\n", out, nanos, divisor)
	}
	return true, fe.emitI64Result(call.Dst, out)
}

func (fe *funcEmitter) emitDurationNanosOperand(op *mir.Operand) (nanos string, ok bool, err error) {
	typeID := operandValueType(fe.emitter.types, op)
	if typeID == types.NoTypeID && op != nil && (op.Kind == mir.OperandCopy || op.Kind == mir.OperandMove) {
		var placeErr error
		typeID, placeErr = fe.placeBaseType(op.Place)
		if placeErr != nil {
			return "", false, placeErr
		}
	}
	_, _, opaqueOffset, ok, err := fe.durationLayoutForType(typeID)
	if err != nil || !ok {
		return "", ok, err
	}
	value, ty, err := fe.emitValueOperand(op)
	if err != nil {
		return "", true, err
	}
	if ty != "ptr" {
		return "", true, fmt.Errorf("duration value must be ptr, got %s", ty)
	}
	if isRefType(fe.emitter.types, op.Type) {
		loaded := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", loaded, value)
		value = loaded
	}
	opaquePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", opaquePtr, value, opaqueOffset)
	nanos = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", nanos, opaquePtr)
	return nanos, true, nil
}

func (fe *funcEmitter) emitDurationValue(dst mir.Place, nanos string) error {
	size, align, opaqueOffset, ok, err := fe.durationLayoutForPlace(dst)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("duration destination must contain int64 __opaque field")
	}
	mem := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", mem, size, align)
	opaquePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", opaquePtr, mem, opaqueOffset)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", nanos, opaquePtr)
	ptr, dstTy, err := fe.emitPlacePtr(dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, mem, ptr)
	return nil
}

func (fe *funcEmitter) emitI64Result(dst mir.Place, value string) error {
	ptr, dstTy, err := fe.emitPlacePtr(dst)
	if err != nil {
		return err
	}
	out, outTy := value, "i64"
	dstType, err := fe.placeBaseType(dst)
	if err != nil {
		return err
	}
	if dstTy != outTy {
		out, outTy, err = fe.emitNumericCast(value, "i64", fe.emitter.types.Builtins().Int64, dstType)
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", outTy, out, ptr)
	return nil
}

func (fe *funcEmitter) durationLayoutForPlace(place mir.Place) (size, align, opaqueOffset int, ok bool, err error) {
	typeID, err := fe.placeBaseType(place)
	if err != nil {
		return 0, 0, 0, false, err
	}
	return fe.durationLayoutForType(typeID)
}

func (fe *funcEmitter) durationLayoutForType(typeID types.TypeID) (size, align, opaqueOffset int, ok bool, err error) {
	typeID = resolveAliasAndOwn(fe.emitter.types, typeID)
	if _, ok := fe.emitter.types.StructInfo(typeID); !ok {
		return 0, 0, 0, false, nil
	}
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return 0, 0, 0, false, err
	}
	fieldIdx, fieldType, err := fe.structFieldInfo(typeID, mir.PlaceProj{
		Kind:      mir.PlaceProjField,
		FieldName: durationOpaqueField,
		FieldIdx:  -1,
	})
	if err != nil {
		return 0, 0, 0, false, nil
	}
	if fieldIdx < 0 || fieldIdx >= len(layoutInfo.FieldOffsets) {
		return 0, 0, 0, false, fmt.Errorf("duration field index %d out of range", fieldIdx)
	}
	fieldLLVM, err := llvmValueType(fe.emitter.types, fieldType)
	if err != nil {
		return 0, 0, 0, false, err
	}
	if fieldLLVM != "i64" {
		return 0, 0, 0, false, nil
	}
	size = layoutInfo.Size
	align = layoutInfo.Align
	if size <= 0 {
		size = 1
	}
	if align <= 0 {
		align = 1
	}
	return size, align, layoutInfo.FieldOffsets[fieldIdx], true, nil
}
