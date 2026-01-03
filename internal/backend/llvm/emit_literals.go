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
	for i := range lit.Fields {
		field := &lit.Fields[i]
		fieldIdx, fieldType, err := fe.structFieldInfo(lit.TypeID, mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: field.Name, FieldIdx: -1})
		if err != nil {
			return "", "", err
		}
		if fieldIdx < 0 || fieldIdx >= len(layoutInfo.FieldOffsets) {
			return "", "", fmt.Errorf("field index %d out of range", fieldIdx)
		}
		val, valTy, err := fe.emitValueOperand(&field.Value)
		if err != nil {
			return "", "", err
		}
		fieldLLVM, err := llvmValueType(fe.emitter.types, fieldType)
		if err != nil {
			return "", "", err
		}
		if valTy != fieldLLVM {
			valTy = fieldLLVM
		}
		off := layoutInfo.FieldOffsets[fieldIdx]
		bytePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, bytePtr)
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
	for i := range lit.Elems {
		if i >= len(layoutInfo.FieldOffsets) {
			return "", "", fmt.Errorf("tuple field %d out of range", i)
		}
		val, valTy, err := fe.emitValueOperand(&lit.Elems[i])
		if err != nil {
			return "", "", err
		}
		elemType := info.Elems[i]
		elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
		if err != nil {
			return "", "", err
		}
		if valTy != elemLLVM {
			valTy = elemLLVM
		}
		off := layoutInfo.FieldOffsets[i]
		bytePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, bytePtr)
	}
	return mem, "ptr", nil
}

func (fe *funcEmitter) emitArrayLit(lit *mir.ArrayLit, dstType types.TypeID) (val, ty string, err error) {
	if lit == nil {
		return "", "", fmt.Errorf("nil array literal")
	}
	elemType, dynamic, ok := arrayElemType(fe.emitter.types, dstType)
	if !ok || !dynamic {
		return "", "", fmt.Errorf("unsupported array literal type")
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
	length := len(lit.Elems)
	dataSize := stride * length

	dataPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", dataPtr, dataSize, elemAlign)
	headPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", headPtr, arrayHeaderSize, arrayHeaderAlign)

	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, headPtr, arrayLenOffset)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %d, ptr %s\n", length, lenPtr)
	capPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", capPtr, headPtr, arrayCapOffset)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %d, ptr %s\n", length, capPtr)
	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, headPtr, arrayDataOffset)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", dataPtr, dataPtrPtr)

	for i := range lit.Elems {
		val, valTy, err := fe.emitValueOperand(&lit.Elems[i])
		if err != nil {
			return "", "", err
		}
		if valTy != elemLLVM {
			valTy = elemLLVM
		}
		offset := i * stride
		elemPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", elemPtr, dataPtr, offset)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, elemPtr)
	}
	return headPtr, "ptr", nil
}
