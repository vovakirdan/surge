package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

const (
	arrayIterSize      = 24
	arrayIterAlign     = 8
	arrayIterDataOff   = 0
	arrayIterIndexOff  = 8
	arrayIterLengthOff = 16
)

func (fe *funcEmitter) emitIterInit(init *mir.IterInit) (val, ty string, err error) {
	if init == nil {
		return "", "", fmt.Errorf("nil iter_init")
	}
	iterType := operandValueType(fe.emitter.types, &init.Iterable)
	if iterType == types.NoTypeID && init.Iterable.Kind != mir.OperandConst {
		if baseType, baseErr := fe.placeBaseType(init.Iterable.Place); baseErr == nil {
			iterType = baseType
		}
	}
	if isRangeType(fe.emitter.types, iterType) {
		return fe.emitValueOperand(&init.Iterable)
	}
	if _, dynamic, ok := arrayElemType(fe.emitter.types, iterType); ok {
		return fe.emitArrayIterInit(&init.Iterable, iterType, dynamic)
	}
	return "", "", fmt.Errorf("unsupported iter_init iterable type")
}

func (fe *funcEmitter) emitIterNext(next *mir.IterNext) (val, ty string, err error) {
	if next == nil {
		return "", "", fmt.Errorf("nil iter_next")
	}
	iterVal, iterTy, err := fe.emitValueOperand(&next.Iter)
	if err != nil {
		return "", "", err
	}
	if iterTy != "ptr" {
		return "", "", fmt.Errorf("iter_next requires ptr, got %s", iterTy)
	}
	iterType := operandValueType(fe.emitter.types, &next.Iter)
	elemType, ok := rangeElemType(fe.emitter.types, iterType)
	if !ok {
		return "", "", fmt.Errorf("iter_next requires Range<T> type")
	}
	optType, ok := optionTypeForElem(fe.emitter.types, elemType)
	if !ok {
		return "", "", fmt.Errorf("missing Option<T> type for iter_next")
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

	idxPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", idxPtr, iterVal, arrayIterIndexOff)
	idxVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", idxVal, idxPtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, iterVal, arrayIterLengthOff)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", lenVal, lenPtr)

	done := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sge i64 %s, %s\n", done, idxVal, lenVal)

	resPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", resPtr)

	emptyBB := fe.nextInlineBlock()
	nonEmptyBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", done, emptyBB, nonEmptyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", emptyBB)
	nothingVal, err := fe.emitTagValue(optType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return "", "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nothingVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", nonEmptyBB)
	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, iterVal, arrayIterDataOff)
	dataPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)
	offset := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", offset, idxVal, stride)
	elemPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", elemPtr, dataPtr, offset)
	elemVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", elemVal, elemLLVM, elemPtr)
	nextIdx := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = add i64 %s, 1\n", nextIdx, idxVal)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", nextIdx, idxPtr)

	someIndex, meta, err := fe.emitter.tagCaseMeta(optType, "Some", symbols.NoSymbolID)
	if err != nil {
		return "", "", err
	}
	if len(meta.PayloadTypes) != 1 {
		return "", "", fmt.Errorf("tag %q expects 1 payload value, got %d", meta.TagName, len(meta.PayloadTypes))
	}
	someVal, err := fe.emitTagValueSinglePayload(optType, someIndex, meta.PayloadTypes[0], elemVal, elemLLVM)
	if err != nil {
		return "", "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", someVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	out := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", out, resPtr)
	return out, "ptr", nil
}

func (fe *funcEmitter) emitArrayIterInit(op *mir.Operand, arrType types.TypeID, dynamic bool) (val, ty string, err error) {
	if fe.emitter == nil || fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	if op == nil {
		return "", "", fmt.Errorf("nil iterable operand")
	}
	handlePtr, err := fe.emitHandleOperandPtr(op)
	if err != nil {
		return "", "", err
	}

	var dataPtr string
	var lenVal string
	if dynamic {
		lenVal = fe.emitArrayLen(handlePtr)
		head := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", head, handlePtr)
		dataPtrPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, head, arrayDataOffset)
		dataPtr = fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)
	} else {
		head := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", head, handlePtr)
		dataPtr = head
		if _, length, ok := arrayFixedInfo(fe.emitter.types, arrType); ok {
			lenVal = fmt.Sprintf("%d", length)
		} else if tt, ok := fe.emitter.types.Lookup(resolveValueType(fe.emitter.types, arrType)); ok && tt.Kind == types.KindArray && tt.Count != types.ArrayDynamicLength {
			lenVal = fmt.Sprintf("%d", tt.Count)
		} else {
			return "", "", fmt.Errorf("missing fixed array length for iter_init")
		}
	}

	iterPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", iterPtr, arrayIterSize, arrayIterAlign)

	storeData := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", storeData, iterPtr, arrayIterDataOff)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", dataPtr, storeData)
	storeIndex := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", storeIndex, iterPtr, arrayIterIndexOff)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", storeIndex)
	storeLen := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", storeLen, iterPtr, arrayIterLengthOff)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", lenVal, storeLen)

	return iterPtr, "ptr", nil
}

func rangeElemType(typesIn *types.Interner, id types.TypeID) (types.TypeID, bool) {
	if typesIn == nil || typesIn.Strings == nil || id == types.NoTypeID {
		return types.NoTypeID, false
	}
	id = resolveValueType(typesIn, id)
	info, ok := typesIn.StructInfo(id)
	if !ok || info == nil || len(info.TypeArgs) != 1 {
		return types.NoTypeID, false
	}
	if typesIn.Strings.MustLookup(info.Name) != "Range" {
		return types.NoTypeID, false
	}
	return info.TypeArgs[0], true
}

func optionTypeForElem(typesIn *types.Interner, elem types.TypeID) (types.TypeID, bool) {
	if typesIn == nil || typesIn.Strings == nil || elem == types.NoTypeID {
		return types.NoTypeID, false
	}
	optionName := typesIn.Strings.Intern("Option")
	id, ok := typesIn.FindUnionInstance(optionName, []types.TypeID{elem})
	return id, ok
}
