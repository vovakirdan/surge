package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// emitStructLit builds a struct in storage of its own and returns that
// storage's address.
//
// The storage is a frame slot, not an allocation. A literal is a value the
// program is in the middle of producing; where it finally lives is the
// assignment's business, and the byte move that puts it there is the
// assignment's too. So nothing here allocates and nothing here has to be freed
// — which is the whole of what a construction used to owe the allocator.
func (fe *funcEmitter) emitStructLit(lit *mir.StructLit) (val, ty string, err error) {
	if lit == nil {
		return "", "", fmt.Errorf("nil struct literal")
	}
	layoutInfo, err := fe.emitter.layoutOf(lit.TypeID)
	if err != nil {
		return "", "", err
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}
	mem, err := fe.emitValueStorage(lit.TypeID)
	if err != nil {
		return "", "", err
	}
	fieldOffsets := layoutInfo.FieldOffsets()
	for i := range lit.Fields {
		field := &lit.Fields[i]
		fieldIdx, fieldType, err := fe.structFieldInfo(lit.TypeID, mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: field.Name, FieldIdx: -1})
		if err != nil {
			return "", "", err
		}
		if fieldIdx < 0 || fieldIdx >= len(fieldOffsets) {
			return "", "", fmt.Errorf("field index %d out of range", fieldIdx)
		}
		op := field.Value
		if op.Type == types.NoTypeID {
			op.Type = fieldType
		}
		if op.Kind == mir.OperandConst && (op.Const.Type == types.NoTypeID || isNothingType(fe.emitter.types, op.Const.Type)) {
			op.Const.Type = fieldType
		}
		val, valTy, err := fe.emitValueOperand(&op)
		if err != nil {
			return "", "", err
		}
		fieldLLVM, err := fe.emitter.llvmValueType(fieldType)
		if err != nil {
			return "", "", err
		}
		if valTy != fieldLLVM {
			valType := operandValueType(fe.emitter.types, &op)
			if valType == types.NoTypeID && field.Value.Kind != mir.OperandConst {
				if baseType, err := fe.placeBaseType(field.Value.Place); err == nil {
					valType = baseType
				}
			}
			casted, castTy, err := fe.coerceNumericValue(val, valTy, valType, fieldType)
			if err != nil {
				return "", "", err
			}
			val = casted
			valTy = castTy
		}
		if valTy != fieldLLVM {
			valTy = fieldLLVM
		}
		off := fieldOffsets[fieldIdx]
		bytePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
		fe.emitValueStore(valTy, val, bytePtr, memberAccessAlign(align, off))
	}
	return mem, handleType, nil
}

func (fe *funcEmitter) emitTupleLit(lit *mir.TupleLit, dstType types.TypeID) (val, ty string, err error) {
	if lit == nil {
		return "", "", fmt.Errorf("nil tuple literal")
	}
	if len(lit.Elems) == 0 {
		llvmTy, typeErr := fe.emitter.llvmValueType(dstType)
		if typeErr != nil {
			return "", "", typeErr
		}
		return "0", llvmTy, nil
	}
	if fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	info, ok := fe.emitter.types.TupleInfo(resolveAliasAndOwn(fe.emitter.types, dstType))
	if !ok || info == nil {
		return "", "", fmt.Errorf("missing tuple info")
	}
	if len(info.Elems) != len(lit.Elems) {
		return "", "", fmt.Errorf("tuple literal length mismatch")
	}
	layoutInfo, err := fe.emitter.layoutOf(dstType)
	if err != nil {
		return "", "", err
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}
	mem, err := fe.emitStorageAlloca(dstType)
	if err != nil {
		return "", "", err
	}
	fieldOffsets := layoutInfo.FieldOffsets()
	for i := range lit.Elems {
		if i >= len(fieldOffsets) {
			return "", "", fmt.Errorf("tuple field %d out of range", i)
		}
		op := lit.Elems[i]
		elemType := info.Elems[i]
		if op.Type == types.NoTypeID {
			op.Type = elemType
		}
		if op.Kind == mir.OperandConst && (op.Const.Type == types.NoTypeID || isNothingType(fe.emitter.types, op.Const.Type)) {
			op.Const.Type = elemType
		}
		val, valTy, err := fe.emitValueOperand(&op)
		if err != nil {
			return "", "", err
		}
		elemLLVM, err := fe.emitter.llvmValueType(elemType)
		if err != nil {
			return "", "", err
		}
		if valTy != elemLLVM {
			valType := operandValueType(fe.emitter.types, &op)
			if valType == types.NoTypeID && lit.Elems[i].Kind != mir.OperandConst {
				if baseType, err := fe.placeBaseType(lit.Elems[i].Place); err == nil {
					valType = baseType
				}
			}
			casted, castTy, err := fe.coerceNumericValue(val, valTy, valType, elemType)
			if err != nil {
				return "", "", err
			}
			val = casted
			valTy = castTy
		}
		if valTy != elemLLVM {
			valTy = elemLLVM
		}
		off := fieldOffsets[i]
		bytePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
		fe.emitValueStore(valTy, val, bytePtr, memberAccessAlign(align, off))
	}
	return mem, handleType, nil
}

func (fe *funcEmitter) emitArrayLit(lit *mir.ArrayLit, dstType types.TypeID) (val, ty string, err error) {
	if lit == nil {
		return "", "", fmt.Errorf("nil array literal")
	}
	elemType, dynamic, ok := arrayElemType(fe.emitter.types, dstType)
	if !ok {
		return "", "", fmt.Errorf("unsupported array literal type")
	}

	elemLLVM, err := fe.emitter.llvmValueType(elemType)
	if err != nil {
		return "", "", err
	}
	// HANDLE-BACKED: only the dynamic arm below uses these, and there the
	// element buffer is one this emission is about to ask rt_alloc for at
	// exactly this alignment.
	stride, elemAlign, err := fe.emitter.handleArrayElemStrideAlign(elemType)
	if err != nil {
		return "", "", err
	}
	length := len(lit.Elems)

	if dynamic {
		// The element buffer holds elements at the ELEMENT TYPE's own layout
		// now, so a composite element occupies its own bytes rather than one
		// pointer-sized slot pointing at a box somewhere else. The buffer is
		// still allocated and still owned by the array header, which is what
		// keeps this a container question rather than a value one.
		dataSize := stride * uint64(length)

		// Both allocations name the ARRAY, not the element: a refused header is
		// as much a failure to build this array as a refused element buffer, and
		// the reader is holding one literal either way.
		dataPtr := fe.emitCheckedAlloc(allocSiteArrayElements, dstType, fmt.Sprintf("%d", dataSize), elemAlign)
		headPtr := fe.emitCheckedAlloc(allocSiteArrayHeader, dstType, fmt.Sprintf("%d", arrayHeaderSize), arrayHeaderAlign)

		lenPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, headPtr, arrayLenOffset)
		fmt.Fprintf(&fe.emitter.buf, "  store i64 %d, ptr %s, align %d\n", length, lenPtr, alignWord)
		capPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", capPtr, headPtr, arrayCapOffset)
		fmt.Fprintf(&fe.emitter.buf, "  store i64 %d, ptr %s, align %d\n", length, capPtr, alignWord)
		dataPtrPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, headPtr, arrayDataOffset)
		fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s, align %d\n", dataPtr, dataPtrPtr, alignPtr)

		for i := range lit.Elems {
			val, valTy, emitErr := fe.emitValueOperand(&lit.Elems[i])
			if emitErr != nil {
				return "", "", emitErr
			}
			if valTy != elemLLVM {
				valType := operandValueType(fe.emitter.types, &lit.Elems[i])
				if valType == types.NoTypeID && lit.Elems[i].Kind != mir.OperandConst {
					if baseType, baseErr := fe.placeBaseType(lit.Elems[i].Place); baseErr == nil {
						valType = baseType
					}
				}
				casted, castTy, emitErr := fe.coerceNumericValue(val, valTy, valType, elemType)
				if emitErr != nil {
					return "", "", emitErr
				}
				val = casted
				valTy = castTy
			}
			if valTy != elemLLVM {
				valTy = elemLLVM
			}
			offset := uint64(i) * stride
			elemPtr := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", elemPtr, dataPtr, offset)
			fe.emitValueStore(valTy, val, elemPtr, elemAlign)
		}
		return headPtr, handleType, nil
	}

	elemType, fixedLen, ok := arrayFixedInfo(fe.emitter.types, dstType)
	if !ok {
		return "", "", fmt.Errorf("unsupported array literal type")
	}
	if length != int(fixedLen) {
		return "", "", fmt.Errorf("array literal length mismatch")
	}
	// INLINE-FIXED-ARRAY-REACHABLE. The literal's elements are written into
	// inline storage, so their alignment is bounded by what that storage is
	// aligned to — and here that base is a slot this emission just reserved,
	// so its alignment is a fact rather than an inference. A literal is never
	// built directly into a `@packed` member today; it is built here and moved
	// in. Threading the base anyway is what keeps that true by construction
	// instead of by memory.
	mem, memAlign, err := fe.emitStorageAllocaAligned(dstType)
	if err != nil {
		return "", "", err
	}
	if fixedLen == 0 {
		return mem, handleType, nil
	}

	elemLLVM, err = fe.emitter.llvmValueType(elemType)
	if err != nil {
		return "", "", err
	}
	stride, elemAlign, err = fe.emitter.inlineArrayElemStrideAlign(memAlign, elemType)
	if err != nil {
		return "", "", err
	}
	for i := range lit.Elems {
		val, valTy, emitErr := fe.emitValueOperand(&lit.Elems[i])
		if emitErr != nil {
			return "", "", emitErr
		}
		if valTy != elemLLVM {
			valType := operandValueType(fe.emitter.types, &lit.Elems[i])
			if valType == types.NoTypeID && lit.Elems[i].Kind != mir.OperandConst {
				if baseType, baseErr := fe.placeBaseType(lit.Elems[i].Place); baseErr == nil {
					valType = baseType
				}
			}
			casted, castTy, emitErr := fe.coerceNumericValue(val, valTy, valType, elemType)
			if emitErr != nil {
				return "", "", emitErr
			}
			val = casted
			valTy = castTy
		}
		if valTy != elemLLVM {
			valTy = elemLLVM
		}
		offset := uint64(i) * stride
		elemPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", elemPtr, mem, offset)
		fe.emitValueStore(valTy, val, elemPtr, elemAlign)
	}
	return mem, handleType, nil
}
