package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitStructLit(lit *mir.StructLit) (val, ty string, err error) {
	if lit == nil {
		return "", "", fmt.Errorf("nil struct literal")
	}
	layoutInfo, err := fe.emitter.layoutOf(lit.TypeID)
	if err != nil {
		return "", "", err
	}
	size := layoutInfo.Size
	align := layoutInfo.Align
	if align <= 0 {
		align = 1
	}
	mem := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", mem, size, align)
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
		fieldLLVM, err := llvmValueType(fe.emitter.types, fieldType)
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
		if err := fe.emitStore(valTy, val, bytePtr); err != nil {
			return "", "", err
		}
	}
	return mem, "ptr", nil
}

func (fe *funcEmitter) emitTupleLit(lit *mir.TupleLit, dstType types.TypeID) (val, ty string, err error) {
	if lit == nil {
		return "", "", fmt.Errorf("nil tuple literal")
	}
	if len(lit.Elems) == 0 {
		llvmTy, typeErr := llvmValueType(fe.emitter.types, dstType)
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
	size := layoutInfo.Size
	align := layoutInfo.Align
	if align <= 0 {
		align = 1
	}
	mem := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", mem, size, align)
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
		elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
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
		if err := fe.emitStore(valTy, val, bytePtr); err != nil {
			return "", "", err
		}
	}
	return mem, "ptr", nil
}

func (fe *funcEmitter) emitArrayLit(lit *mir.ArrayLit, dstType types.TypeID) (val, ty string, err error) {
	if lit == nil {
		return "", "", fmt.Errorf("nil array literal")
	}
	elemType, dynamic, ok := arrayElemType(fe.emitter.types, dstType)
	if !ok {
		return "", "", fmt.Errorf("unsupported array literal type")
	}

	elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
	if err != nil {
		return "", "", err
	}
	elemLayout, err := fe.emitter.layoutOf(elemType)
	if err != nil {
		return "", "", err
	}
	stride := elemLayout.Stride
	length := len(lit.Elems)

	if dynamic {
		// Dynamic arrays still store the emitted value representation.  Until
		// Wave C inlines aggregates, a composite element is a pointer even when
		// its canonical language layout is wider; using that language stride
		// leaves holes that every array reader correctly interprets as elements.
		emittedStride, emittedAlign, sizeErr := llvmTypeStrideAlign(elemLLVM)
		if sizeErr != nil {
			return "", "", sizeErr
		}
		dataSize := emittedStride * uint64(length)

		dataPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", dataPtr, dataSize, emittedAlign)
		headPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", headPtr, arrayHeaderSize, arrayHeaderAlign)

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
			offset := uint64(i) * emittedStride
			elemPtr := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", elemPtr, dataPtr, offset)
			if storeErr := fe.emitStore(valTy, val, elemPtr); storeErr != nil {
				return "", "", storeErr
			}
		}
		return headPtr, "ptr", nil
	}

	elemType, fixedLen, ok := arrayFixedInfo(fe.emitter.types, dstType)
	if !ok {
		return "", "", fmt.Errorf("unsupported array literal type")
	}
	if length != int(fixedLen) {
		return "", "", fmt.Errorf("array literal length mismatch")
	}
	layoutInfo, err := fe.emitter.layoutOf(dstType)
	if err != nil {
		return "", "", err
	}
	size := layoutInfo.Size
	align := layoutInfo.Align
	if size <= 0 {
		size = 1
	}
	if align <= 0 {
		align = 1
	}
	mem := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", mem, size, align)
	if fixedLen == 0 {
		return mem, "ptr", nil
	}

	elemLLVM, err = llvmValueType(fe.emitter.types, elemType)
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
		if err := fe.emitStore(valTy, val, elemPtr); err != nil {
			return "", "", err
		}
	}
	return mem, "ptr", nil
}
