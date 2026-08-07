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
	_, ok, err := fe.durationLayoutForPlace(call.Dst)
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
	if typeID == types.NoTypeID && op != nil && (op.Kind == mir.OperandCopy || op.Kind == mir.OperandCopyValue || op.Kind == mir.OperandMove) {
		var placeErr error
		typeID, placeErr = fe.placeBaseType(op.Place)
		if placeErr != nil {
			return "", false, placeErr
		}
	}
	if op != nil && isRefType(fe.emitter.types, op.Type) {
		if elem, refOK := derefType(fe.emitter.types, op.Type); refOK {
			typeID = elem
		}
	}
	opaqueOffset, ok, err := fe.durationLayoutForType(typeID)
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
	opaqueOffset, ok, err := fe.durationLayoutForPlace(dst)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("duration destination must contain int64 __opaque field")
	}
	// A Duration is an ordinary one-field value, so it is written straight into
	// the destination's own storage. There is no intermediate to build it in
	// and no allocation to copy it out of.
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		return fmt.Errorf("duration destination must lower to inline storage, got %s", dstTy)
	}
	opaquePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", opaquePtr, ptr, opaqueOffset)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s, align %d\n",
		nanos, opaquePtr, memberAccessAlign(dstAlign, opaqueOffset))
	return nil
}

func (fe *funcEmitter) emitI64Result(dst mir.Place, value string) error {
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(dst)
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
	fe.emitValueStore(outTy, out, ptr, dstAlign)
	return nil
}

// durationLayoutForPlace answers only what its callers ask: where the opaque
// field sits, and whether the place is a duration at all. The whole layout is
// available from durationLayoutForType for anyone who needs the rest.
func (fe *funcEmitter) durationLayoutForPlace(place mir.Place) (opaqueOffset uint64, ok bool, err error) {
	typeID, err := fe.placeBaseType(place)
	if err != nil {
		return 0, false, err
	}
	return fe.durationLayoutForType(typeID)
}

// durationLayoutForType reports where a duration's opaque field sits, and
// whether the type is a duration at all. The size and alignment of the whole
// struct used to come back with it and no caller ever read them.
func (fe *funcEmitter) durationLayoutForType(typeID types.TypeID) (opaqueOffset uint64, ok bool, err error) {
	typeID = resolveAliasAndOwn(fe.emitter.types, typeID)
	if _, isStruct := fe.emitter.types.StructInfo(typeID); !isStruct {
		return 0, false, nil
	}
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return 0, false, err
	}
	fieldIdx, fieldType, err := fe.structFieldInfo(typeID, mir.PlaceProj{
		Kind:      mir.PlaceProjField,
		FieldName: durationOpaqueField,
		FieldIdx:  -1,
	})
	if err != nil {
		return 0, false, nil
	}
	fieldOffsets := layoutInfo.FieldOffsets()
	if fieldIdx < 0 || fieldIdx >= len(fieldOffsets) {
		return 0, false, fmt.Errorf("duration field index %d out of range", fieldIdx)
	}
	fieldLLVM, err := fe.emitter.llvmValueType(fieldType)
	if err != nil {
		return 0, false, err
	}
	if fieldLLVM != "i64" {
		return 0, false, nil
	}
	return fieldOffsets[fieldIdx], true, nil
}
