package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitFieldAccess(fa *mir.FieldAccess) (val, ty string, err error) {
	if fa == nil {
		return "", "", fmt.Errorf("nil field access")
	}
	objVal, objTy, err := fe.emitValueOperand(&fa.Object)
	if err != nil {
		return "", "", err
	}
	if objTy != "ptr" {
		return "", "", fmt.Errorf("field access expects ptr base, got %s", objTy)
	}
	objType := fa.Object.Type
	if objType == types.NoTypeID && fa.Object.Kind != mir.OperandConst {
		if baseType, baseErr := fe.placeBaseType(fa.Object.Place); baseErr == nil {
			objType = baseType
		}
	}
	if isRefType(fe.emitter.types, objType) && fa.Object.Kind != mir.OperandAddrOf && fa.Object.Kind != mir.OperandAddrOfMut {
		deref := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", deref, objVal)
		objVal = deref
	}
	structType := resolveValueType(fe.emitter.types, objType)
	fieldIdx, fieldType, err := fe.structFieldInfo(structType, mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: fa.FieldName, FieldIdx: fa.FieldIdx})
	if err != nil {
		return "", "", err
	}
	layoutInfo, err := fe.emitter.layoutOf(structType)
	if err != nil {
		return "", "", err
	}
	if fieldIdx < 0 || fieldIdx >= len(layoutInfo.FieldOffsets) {
		return "", "", fmt.Errorf("field index %d out of range", fieldIdx)
	}
	fieldLLVM, err := llvmValueType(fe.emitter.types, fieldType)
	if err != nil {
		return "", "", err
	}
	off := layoutInfo.FieldOffsets[fieldIdx]
	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, objVal, off)
	val = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", val, fieldLLVM, bytePtr)
	return val, fieldLLVM, nil
}

func (fe *funcEmitter) emitIndexAccess(idx *mir.IndexAccess) (val, ty string, errEmit error) {
	if idx == nil {
		return "", "", fmt.Errorf("nil index access")
	}
	objType := resolveValueType(fe.emitter.types, idx.Object.Type)
	if objType == types.NoTypeID && idx.Object.Kind != mir.OperandConst {
		var baseType types.TypeID
		if baseType, errEmit = fe.placeBaseType(idx.Object.Place); errEmit == nil {
			objType = baseType
		}
	}
	if elemType, length, ok := arrayFixedInfo(fe.emitter.types, objType); ok {
		var (
			objAddr  string
			idxVal   string
			idxTy    string
			elemPtr  string
			elemLLVM string
		)
		objAddr, errEmit = fe.emitHandleOperandPtr(&idx.Object)
		if errEmit != nil {
			return "", "", errEmit
		}
		idxVal, idxTy, errEmit = fe.emitValueOperand(&idx.Index)
		if errEmit != nil {
			return "", "", errEmit
		}
		elemPtr, elemLLVM, errEmit = fe.emitArrayFixedElemPtr(objAddr, idxVal, idxTy, idx.Index.Type, elemType, length)
		if errEmit != nil {
			return "", "", errEmit
		}
		val = fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", val, elemLLVM, elemPtr)
		return val, elemLLVM, nil
	}

	_, objTy, err := fe.emitValueOperand(&idx.Object)
	if err != nil {
		return "", "", err
	}
	if objTy != "ptr" {
		return "", "", fmt.Errorf("index access expects ptr base, got %s", objTy)
	}
	switch {
	case isStringLike(fe.emitter.types, objType):
		if isRangeType(fe.emitter.types, idx.Index.Type) {
			rangeVal, _, err := fe.emitOperand(&idx.Index)
			if err != nil {
				return "", "", err
			}
			handlePtr, err := fe.emitHandleOperandPtr(&idx.Object)
			if err != nil {
				return "", "", err
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_slice(ptr %s, ptr %s)\n", tmp, handlePtr, rangeVal)
			return tmp, "ptr", nil
		}
		idxVal, idxTy, err := fe.emitValueOperand(&idx.Index)
		if err != nil {
			return "", "", err
		}
		handlePtr, err := fe.emitHandleOperandPtr(&idx.Object)
		if err != nil {
			return "", "", err
		}
		lenVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_string_len(ptr %s)\n", lenVal, handlePtr)
		idx64, err := fe.emitIndexToI64(0, idxVal, idxTy, idx.Index.Type, lenVal)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i32 @rt_string_index(ptr %s, i64 %s)\n", tmp, handlePtr, idx64)
		return tmp, "i32", nil
	case isBytesViewType(fe.emitter.types, objType):
		idxVal, idxTy, err := fe.emitValueOperand(&idx.Index)
		if err != nil {
			return "", "", err
		}
		handlePtr, err := fe.emitHandleOperandPtr(&idx.Object)
		if err != nil {
			return "", "", err
		}
		return fe.emitBytesViewIndex(handlePtr, objType, idxVal, idxTy, idx.Index.Type)
	case isArrayLike(fe.emitter.types, objType):
		elemType, _, ok := arrayElemType(fe.emitter.types, objType)
		if !ok {
			return "", "", fmt.Errorf("unsupported index target")
		}
		idxVal, idxTy, err := fe.emitValueOperand(&idx.Index)
		if err != nil {
			return "", "", err
		}
		handlePtr, err := fe.emitHandleOperandPtr(&idx.Object)
		if err != nil {
			return "", "", err
		}
		elemPtr, elemLLVM, err := fe.emitArrayElemPtr(handlePtr, idxVal, idxTy, idx.Index.Type, elemType)
		if err != nil {
			return "", "", err
		}
		val = fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", val, elemLLVM, elemPtr)
		return val, elemLLVM, nil
	default:
		return "", "", fmt.Errorf("unsupported index target")
	}
}

func (fe *funcEmitter) emitHandleAddr(val string) string {
	ptr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", ptr)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", val, ptr)
	return ptr
}

func (fe *funcEmitter) emitArrayLen(handlePtr string) string {
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handle, handlePtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, handle, arrayLenOffset)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", lenVal, lenPtr)
	return lenVal
}

func (fe *funcEmitter) emitArrayElemPtr(handlePtr, idxVal, idxTy string, idxType, elemType types.TypeID) (ptr, ty string, err error) {
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handle, handlePtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, handle, arrayLenOffset)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", lenVal, lenPtr)

	adjIdx, err := fe.emitBoundsCheckedIndex(1, idxVal, idxTy, idxType, lenVal, true, lenVal)
	if err != nil {
		return "", "", err
	}

	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, handle, arrayDataOffset)
	dataPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)

	elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
	if err != nil {
		return "", "", err
	}
	elemSize, elemAlign, err := llvmTypeSizeAlign(elemLLVM)
	if err != nil {
		return "", "", err
	}
	if elemAlign <= 0 {
		elemAlign = 1
	}
	stride := roundUpInt(elemSize, elemAlign)
	off := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", off, adjIdx, stride)
	elemPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", elemPtr, dataPtr, off)
	return elemPtr, elemLLVM, nil
}

func (fe *funcEmitter) emitArrayFixedElemPtr(handlePtr, idxVal, idxTy string, idxType, elemType types.TypeID, length uint32) (ptr, ty string, err error) {
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handle, handlePtr)
	lenVal := fmt.Sprintf("%d", length)

	adjIdx, err := fe.emitBoundsCheckedIndex(1, idxVal, idxTy, idxType, lenVal, true, lenVal)
	if err != nil {
		return "", "", err
	}

	elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
	if err != nil {
		return "", "", err
	}
	elemSize, elemAlign, err := llvmTypeSizeAlign(elemLLVM)
	if err != nil {
		return "", "", err
	}
	if elemAlign <= 0 {
		elemAlign = 1
	}
	stride := roundUpInt(elemSize, elemAlign)
	off := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", off, adjIdx, stride)
	elemPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", elemPtr, handle, off)
	return elemPtr, elemLLVM, nil
}

func (fe *funcEmitter) emitBytesViewLen(handlePtr string, viewType types.TypeID) (string, error) {
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handle, handlePtr)
	ptrOff, lenOff, lenLLVM, err := fe.bytesViewOffsets(viewType)
	if err != nil {
		return "", err
	}
	_ = ptrOff
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, handle, lenOff)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", lenVal, lenLLVM, lenPtr)
	if lenLLVM == "ptr" {
		conv, convErr := fe.emitCheckedBigUintToU64(lenVal, "bytes view length out of range")
		if convErr != nil {
			return "", convErr
		}
		lenVal = conv
	} else if lenLLVM != "i64" {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = zext %s %s to i64\n", tmp, lenLLVM, lenVal)
		lenVal = tmp
	}
	return lenVal, nil
}

func (fe *funcEmitter) emitBytesViewIndex(handlePtr string, viewType types.TypeID, idxVal, idxTy string, idxType types.TypeID) (val, ty string, err error) {
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handle, handlePtr)
	ptrOff, lenOff, lenLLVM, err := fe.bytesViewOffsets(viewType)
	if err != nil {
		return "", "", err
	}
	ptrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", ptrPtr, handle, ptrOff)
	ptrVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", ptrVal, ptrPtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, handle, lenOff)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", lenVal, lenLLVM, lenPtr)
	if lenLLVM == "ptr" {
		conv, convErr := fe.emitCheckedBigUintToU64(lenVal, "bytes view length out of range")
		if convErr != nil {
			return "", "", convErr
		}
		lenVal = conv
	} else if lenLLVM != "i64" {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = zext %s %s to i64\n", tmp, lenLLVM, lenVal)
		lenVal = tmp
	}

	adjIdx, err := fe.emitBoundsCheckedIndex(0, idxVal, idxTy, idxType, lenVal, false, "0")
	if err != nil {
		return "", "", err
	}

	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", bytePtr, ptrVal, adjIdx)
	val = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", val, bytePtr)
	return val, "i8", nil
}

func (fe *funcEmitter) emitBoundsCheckedIndex(kind int, idxVal, idxTy string, idxType types.TypeID, lenVal string, allowNegative bool, overflowLen string) (string, error) {
	idx64, err := fe.emitIndexToI64(kind, idxVal, idxTy, idxType, overflowLen)
	if err != nil {
		return "", err
	}
	adj := idx64
	if allowNegative {
		neg := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", neg, idx64)
		add := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = add i64 %s, %s\n", add, idx64, lenVal)
		adj = fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, i64 %s, i64 %s\n", adj, neg, add, idx64)
	}
	tooLow := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", tooLow, adj)
	tooHigh := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sge i64 %s, %s\n", tooHigh, adj, lenVal)
	oob := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", oob, tooLow, tooHigh)

	fail := fe.nextInlineBlock()
	cont := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", oob, fail, cont)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_panic_bounds(i64 %d, i64 %s, i64 %s)\n", kind, adj, lenVal)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	return adj, nil
}

func (fe *funcEmitter) emitIndexToI64(kind int, idxVal, idxTy string, idxType types.TypeID, overflowLen string) (string, error) {
	maxIndex := int64(^uint64(0) >> 1)
	if isBigIntType(fe.emitter.types, idxType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", outPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_bigint_to_i64(ptr %s, ptr %s)\n", okVal, idxVal, outPtr)
		okBB := fe.nextInlineBlock()
		badBB := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, okBB, badBB)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", badBB)
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_panic_bounds(i64 %d, i64 %d, i64 %s)\n", kind, maxIndex, overflowLen)
		fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
		outVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", outVal, outPtr)
		return outVal, nil
	}
	if isBigUintType(fe.emitter.types, idxType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", outPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_biguint_to_u64(ptr %s, ptr %s)\n", okVal, idxVal, outPtr)
		okBB := fe.nextInlineBlock()
		badBB := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, okBB, badBB)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", badBB)
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_panic_bounds(i64 %d, i64 %d, i64 %s)\n", kind, maxIndex, overflowLen)
		fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
		outVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", outVal, outPtr)
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, outVal, maxIndex)
		limitBB := fe.nextInlineBlock()
		contBB := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", tooHigh, limitBB, contBB)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", limitBB)
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_panic_bounds(i64 %d, i64 %d, i64 %s)\n", kind, maxIndex, overflowLen)
		fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
		return outVal, nil
	}
	return fe.coerceIntToI64(idxVal, idxTy, idxType)
}

func (fe *funcEmitter) coerceIntToI64(val, ty string, typeID types.TypeID) (string, error) {
	if ty == "i64" {
		return val, nil
	}
	info, ok := intInfo(fe.emitter.types, typeID)
	if !ok {
		if typeID == types.NoTypeID && strings.HasPrefix(ty, "i") {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = sext %s %s to i64\n", tmp, ty, val)
			return tmp, nil
		}
		return "", fmt.Errorf("expected integer type")
	}
	op := "zext"
	if info.signed {
		op = "sext"
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s to i64\n", tmp, op, ty, val)
	return tmp, nil
}
