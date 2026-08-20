package llvm

import (
	"fmt"
	"strconv"
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
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s, align %d\n", deref, objVal, alignPtr)
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
	fieldOffsets := layoutInfo.FieldOffsets()
	if fieldIdx < 0 || fieldIdx >= len(fieldOffsets) {
		return "", "", fmt.Errorf("field index %d out of range", fieldIdx)
	}
	fieldLLVM, err := fe.emitter.llvmValueType(fieldType)
	if err != nil {
		return "", "", err
	}
	baseAlign, err := fe.emitter.storageAlignOf(structType, objTy)
	if err != nil {
		return "", "", err
	}
	off := fieldOffsets[fieldIdx]
	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, objVal, off)
	return fe.emitStorageMemberLoad(fieldLLVM, bytePtr, memberAccessAlign(baseAlign, off))
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
			objAddr   string
			objAlign  uint64
			idxVal    string
			idxTy     string
			elemPtr   string
			elemLLVM  string
			elemAlign uint64
		)
		// INLINE-FIXED-ARRAY-REACHABLE. The array is a member of whatever the
		// operand names, so a `@packed` container puts it at an offset its
		// element type does not divide, and the walk's answer is the only one
		// that describes the address.
		objAddr, objAlign, errEmit = fe.emitHandleOperandStorage(&idx.Object)
		if errEmit != nil {
			return "", "", errEmit
		}
		if isRangeType(fe.emitter.types, idx.Index.Type) {
			rangeVal, _, err := fe.emitOperand(&idx.Index)
			if err != nil {
				return "", "", err
			}
			tmp, err := fe.emitArrayFixedSlice(objAddr, rangeVal, elemType, length)
			if err != nil {
				return "", "", err
			}
			return tmp, "ptr", nil
		}
		idxVal, idxTy, errEmit = fe.emitValueOperand(&idx.Index)
		if errEmit != nil {
			return "", "", errEmit
		}
		elemPtr, elemLLVM, elemAlign, errEmit = fe.emitArrayFixedElemPtr(objAddr, objAlign, idxVal, idxTy, idx.Index.Type, elemType, length)
		if errEmit != nil {
			return "", "", errEmit
		}
		return fe.emitStorageMemberLoad(elemLLVM, elemPtr, elemAlign)
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
			fe.emitRangeObjectFree(rangeVal)
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
		if isRangeType(fe.emitter.types, idx.Index.Type) {
			rangeVal, _, err := fe.emitOperand(&idx.Index)
			if err != nil {
				return "", "", err
			}
			handlePtr, err := fe.emitHandleOperandPtr(&idx.Object)
			if err != nil {
				return "", "", err
			}
			tmp, err := fe.emitArraySlice(handlePtr, rangeVal, elemType)
			if err != nil {
				return "", "", err
			}
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
		// HANDLE-BACKED. The elements are in the runtime's buffer, reached
		// through the array header, so nothing the container guarantees
		// reaches them and emitArrayElemPtr answers from the element type.
		elemPtr, elemLLVM, elemAlign, err := fe.emitArrayElemPtr(handlePtr, idxVal, idxTy, idx.Index.Type, elemType)
		if err != nil {
			return "", "", err
		}
		return fe.emitStorageMemberLoad(elemLLVM, elemPtr, elemAlign)
	default:
		return "", "", fmt.Errorf("unsupported index target")
	}
}

func (fe *funcEmitter) emitArraySlice(handlePtr, rangeVal string, elemType types.TypeID) (string, error) {
	stride, err := fe.emitter.arrayElemStride(elemType)
	if err != nil {
		return "", err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_array_slice(ptr %s, ptr %s, i64 %d)\n", tmp, handlePtr, rangeVal, stride)
	fe.emitRangeObjectFree(rangeVal)
	return tmp, nil
}

// elemsPtr addresses the fixed array's element buffer — the same address
// element indexing walks. A fixed array carries no header to indirect through,
// so the view the runtime builds points straight into that storage.
func (fe *funcEmitter) emitArrayFixedSlice(elemsPtr, rangeVal string, elemType types.TypeID, length uint32) (string, error) {
	stride, err := fe.emitter.arrayElemStride(elemType)
	if err != nil {
		return "", err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_array_slice_fixed(ptr %s, ptr %s, i64 %d, i64 %d)\n", tmp, elemsPtr, rangeVal, length, stride)
	fe.emitRangeObjectFree(rangeVal)
	return tmp, nil
}

func (fe *funcEmitter) emitHandleAddr(val string) string {
	ptr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr, align %d\n", ptr, alignPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s, align %d\n", val, ptr, alignPtr)
	return ptr
}

func (fe *funcEmitter) emitArrayLen(handlePtr string) string {
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s, align %d\n", handle, handlePtr, alignPtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, handle, arrayLenOffset)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s, align %d\n", lenVal, lenPtr, alignWord)
	return lenVal
}

// emitArrayElemPtr addresses one element of a DYNAMIC array and reports what
// that address is aligned to.
//
// The alignment comes back from here rather than being looked up again by the
// caller so that the emitter that MADE the address is the one that says what it
// is aligned to. `handlePtr` addresses the slot holding the array handle, and
// what that slot is aligned to is not what the element buffer is aligned to —
// so nothing about it is threaded in.
func (fe *funcEmitter) emitArrayElemPtr(handlePtr, idxVal, idxTy string, idxType, elemType types.TypeID) (ptr, ty string, align uint64, err error) {
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s, align %d\n", handle, handlePtr, alignPtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, handle, arrayLenOffset)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s, align %d\n", lenVal, lenPtr, alignWord)

	adjIdx, err := fe.emitBoundsCheckedIndex(1, idxVal, idxTy, idxType, lenVal, true, lenVal)
	if err != nil {
		return "", "", 0, err
	}

	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, handle, arrayDataOffset)
	dataPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s, align %d\n", dataPtr, dataPtrPtr, alignPtr)

	elemLLVM, err := fe.emitter.llvmValueType(elemType)
	if err != nil {
		return "", "", 0, err
	}
	stride, elemAlign, err := fe.emitter.handleArrayElemStrideAlign(elemType)
	if err != nil {
		return "", "", 0, err
	}
	off := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", off, adjIdx, stride)
	elemPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", elemPtr, dataPtr, off)
	return elemPtr, elemLLVM, elemAlign, nil
}

// emitArrayFixedElemPtr addresses one element of a FIXED array and reports what
// that address is aligned to.
//
// `arrayPtr` is the array's own storage. A fixed array lives inline, like every
// other value composite: there is no buffer somewhere else and so no handle to
// load first, and the element's address is the array's plus a stride offset.
//
// `baseAlign` is what `arrayPtr` is aligned to, and every caller must produce
// it. That is the point of the parameter: the element's address is the base's
// plus a multiple of the stride, so it can be no better aligned than the base
// — and inside a `@packed` container the base is deliberately at an offset the
// element type does not divide. A caller that could reach this without stating
// the base's alignment is a caller that would answer from the element type
// again, which is the whole of RV2-DEBT-226.
func (fe *funcEmitter) emitArrayFixedElemPtr(arrayPtr string, baseAlign uint64, idxVal, idxTy string, idxType, elemType types.TypeID, length uint32) (ptr, ty string, align uint64, err error) {
	lenVal := fmt.Sprintf("%d", length)

	adjIdx, err := fe.emitBoundsCheckedIndex(1, idxVal, idxTy, idxType, lenVal, true, lenVal)
	if err != nil {
		return "", "", 0, err
	}

	elemLLVM, err := fe.emitter.llvmValueType(elemType)
	if err != nil {
		return "", "", 0, err
	}
	stride, elemAlign, err := fe.emitter.inlineArrayElemStrideAlign(baseAlign, elemType)
	if err != nil {
		return "", "", 0, err
	}
	off := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", off, adjIdx, stride)
	elemPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", elemPtr, arrayPtr, off)
	return elemPtr, elemLLVM, elemAlign, nil
}

// viewPtr addresses the view's own fields, not a slot holding a pointer to
// them: a bytes view is a plain struct that lives wherever its owner put it.
func (fe *funcEmitter) emitBytesViewLen(viewPtr string, viewType types.TypeID) (string, error) {
	_, lenOff, lenLLVM, err := fe.bytesViewOffsets(viewType)
	if err != nil {
		return "", err
	}
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, viewPtr, lenOff)
	lenVal := fe.nextTemp()
	if err := fe.emitLoad(lenVal, lenLLVM, lenPtr); err != nil {
		return "", err
	}
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

// viewPtr addresses the view's own fields; see emitBytesViewLen.
func (fe *funcEmitter) emitBytesViewIndex(viewPtr string, viewType types.TypeID, idxVal, idxTy string, idxType types.TypeID) (val, ty string, err error) {
	ptrOff, lenOff, lenLLVM, err := fe.bytesViewOffsets(viewType)
	if err != nil {
		return "", "", err
	}
	ptrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", ptrPtr, viewPtr, ptrOff)
	ptrVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s, align %d\n", ptrVal, ptrPtr, alignPtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, viewPtr, lenOff)
	lenVal := fe.nextTemp()
	if loadErr := fe.emitLoad(lenVal, lenLLVM, lenPtr); loadErr != nil {
		return "", "", loadErr
	}
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
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s, align 1\n", val, bytePtr)
	return val, "i8", nil
}

// emitPanicBounds writes the out-of-range report together with the unreachable
// that ends the cold block it sits in. It is one helper rather than four copies
// of the same Fprintf because every one of them now has to carry the source
// location as well, and a copy that forgot it would be a panic that silently
// stopped saying where it happened.
func (fe *funcEmitter) emitPanicBounds(kind int, index, length string) {
	fmt.Fprintf(&fe.emitter.buf,
		"  call void @rt_panic_bounds(i64 %d, i64 %s, i64 %s, %s)\n",
		kind, index, length, fe.panicSpanArgs())
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
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
	fe.emitPanicBounds(kind, adj, lenVal)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	return adj, nil
}

func (fe *funcEmitter) emitIndexToI64(kind int, idxVal, idxTy string, idxType types.TypeID, overflowLen string) (string, error) {
	maxIndex := int64(^uint64(0) >> 1)
	if isBigIntType(fe.emitter.types, idxType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", outPtr, alignWord)
		fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s, align %d\n", outPtr, alignWord)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_bigint_to_i64(ptr %s, ptr %s)\n", okVal, idxVal, outPtr)
		okBB := fe.nextInlineBlock()
		badBB := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, okBB, badBB)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", badBB)
		fe.emitPanicBounds(kind, strconv.FormatInt(maxIndex, 10), overflowLen)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
		outVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s, align %d\n", outVal, outPtr, alignWord)
		return outVal, nil
	}
	if isBigUintType(fe.emitter.types, idxType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", outPtr, alignWord)
		fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s, align %d\n", outPtr, alignWord)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_biguint_to_u64(ptr %s, ptr %s)\n", okVal, idxVal, outPtr)
		okBB := fe.nextInlineBlock()
		badBB := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, okBB, badBB)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", badBB)
		fe.emitPanicBounds(kind, strconv.FormatInt(maxIndex, 10), overflowLen)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
		outVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s, align %d\n", outVal, outPtr, alignWord)
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, outVal, maxIndex)
		limitBB := fe.nextInlineBlock()
		contBB := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", tooHigh, limitBB, contBB)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", limitBB)
		fe.emitPanicBounds(kind, strconv.FormatInt(maxIndex, 10), overflowLen)
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
