package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitArrayIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	switch name {
	case "rt_array_reserve":
		return true, fe.emitArrayReserve(call)
	case "rt_array_push":
		return true, fe.emitArrayPush(call)
	case "rt_array_pop":
		return true, fe.emitArrayPop(call)
	default:
		return false, nil
	}
}

func (fe *funcEmitter) arrayElemLayout(op *mir.Operand) (elemType types.TypeID, elemLLVM string, stride int, align int, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return types.NoTypeID, "", 0, 0, fmt.Errorf("missing type interner")
	}
	if op == nil {
		return types.NoTypeID, "", 0, 0, fmt.Errorf("nil operand")
	}
	arrType := operandValueType(fe.emitter.types, op)
	if arrType == types.NoTypeID && op.Kind != mir.OperandConst {
		if baseType, baseErr := fe.placeBaseType(op.Place); baseErr == nil {
			arrType = baseType
		}
	}
	elemType, dynamic, ok := arrayElemType(fe.emitter.types, arrType)
	if !ok || !dynamic {
		return types.NoTypeID, "", 0, 0, fmt.Errorf("rt_array_* requires a dynamic array")
	}
	elemLLVM, err = llvmValueType(fe.emitter.types, elemType)
	if err != nil {
		return types.NoTypeID, "", 0, 0, err
	}
	elemSize, elemAlign, err := llvmTypeSizeAlign(elemLLVM)
	if err != nil {
		return types.NoTypeID, "", 0, 0, err
	}
	if elemAlign <= 0 {
		elemAlign = 1
	}
	stride = roundUpInt(elemSize, elemAlign)
	return elemType, elemLLVM, stride, elemAlign, nil
}

func (fe *funcEmitter) emitGrowArrayCapacity(currentCap, minCap string) (string, error) {
	capPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", capPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", currentCap, capPtr)
	minPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", minPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", minCap, minPtr)

	cur := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", cur, capPtr)
	ltOne := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 1\n", ltOne, cur)
	initCap := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, i64 1, i64 %s\n", initCap, ltOne, cur)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", initCap, capPtr)

	loop := fe.nextInlineBlock()
	body := fe.nextInlineBlock()
	done := fe.nextInlineBlock()

	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", loop)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", loop)
	cur = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", cur, capPtr)
	min := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", min, minPtr)
	ready := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sge i64 %s, %s\n", ready, cur, min)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", ready, done, body)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", body)
	next := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, 2\n", next, cur)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", next, capPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", loop)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", done)
	result := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", result, capPtr)
	return result, nil
}

func (fe *funcEmitter) emitArrayReserve(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_array_reserve requires 2 arguments")
	}
	_, _, stride, elemAlign, err := fe.arrayElemLayout(&call.Args[0])
	if err != nil {
		return err
	}
	handlePtr, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	capVal, capTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	newCap, err := fe.coerceIntToI64(capVal, capTy, call.Args[1].Type)
	if err != nil {
		return err
	}

	head := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", head, handlePtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, head, arrayLenOffset)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", lenVal, lenPtr)
	capPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", capPtr, head, arrayCapOffset)
	curCap := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", curCap, capPtr)

	noGrow := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sle i64 %s, %s\n", noGrow, newCap, curCap)
	done := fe.nextInlineBlock()
	grow := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", noGrow, done, grow)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", grow)

	needLen := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, %s\n", needLen, newCap, lenVal)
	adjCap := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, i64 %s, i64 %s\n", adjCap, needLen, lenVal, newCap)
	grown, err := fe.emitGrowArrayCapacity(curCap, adjCap)
	if err != nil {
		return err
	}

	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, head, arrayDataOffset)
	dataPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)
	oldSize := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", oldSize, curCap, stride)
	newSize := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", newSize, grown, stride)
	newData := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_realloc(ptr %s, i64 %s, i64 %s, i64 %d)\n", newData, dataPtr, oldSize, newSize, elemAlign)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", newData, dataPtrPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", grown, capPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", done)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", done)
	return nil
}

func (fe *funcEmitter) emitArrayPush(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_array_push requires 2 arguments")
	}
	_, elemLLVM, stride, elemAlign, err := fe.arrayElemLayout(&call.Args[0])
	if err != nil {
		return err
	}
	handlePtr, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	val, valTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if valTy != elemLLVM {
		valTy = elemLLVM
	}

	head := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", head, handlePtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, head, arrayLenOffset)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", lenVal, lenPtr)
	capPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", capPtr, head, arrayCapOffset)
	curCap := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", curCap, capPtr)

	needGrow := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i64 %s, %s\n", needGrow, lenVal, curCap)
	grow := fe.nextInlineBlock()
	cont := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", needGrow, grow, cont)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", grow)
	minCap := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = add i64 %s, 1\n", minCap, lenVal)
	grown, err := fe.emitGrowArrayCapacity(curCap, minCap)
	if err != nil {
		return err
	}
	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, head, arrayDataOffset)
	dataPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)
	oldSize := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", oldSize, curCap, stride)
	newSize := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", newSize, grown, stride)
	newData := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_realloc(ptr %s, i64 %s, i64 %s, i64 %d)\n", newData, dataPtr, oldSize, newSize, elemAlign)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", newData, dataPtrPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", grown, capPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", cont)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)

	dataPtrPtr = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, head, arrayDataOffset)
	dataPtr = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)
	offset := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", offset, lenVal, stride)
	elemPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", elemPtr, dataPtr, offset)
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, elemPtr)
	newLen := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = add i64 %s, 1\n", newLen, lenVal)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", newLen, lenPtr)
	return nil
}

func (fe *funcEmitter) emitArrayPop(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("rt_array_pop requires 1 argument")
	}
	_, elemLLVM, stride, _, err := fe.arrayElemLayout(&call.Args[0])
	if err != nil {
		return err
	}
	handlePtr, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}

	head := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", head, handlePtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, head, arrayLenOffset)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", lenVal, lenPtr)

	isEmpty := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i64 %s, 0\n", isEmpty, lenVal)
	empty := fe.nextInlineBlock()
	nonEmpty := fe.nextInlineBlock()
	done := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isEmpty, empty, nonEmpty)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", empty)
	if call.HasDst {
		dstType, err := fe.placeBaseType(call.Dst)
		if err != nil {
			return err
		}
		nothingVal, err := fe.emitTagValue(dstType, "nothing", symbols.NoSymbolID, nil)
		if err != nil {
			return err
		}
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, nothingVal, ptr)
	}
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", done)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", nonEmpty)
	newLen := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = sub i64 %s, 1\n", newLen, lenVal)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", newLen, lenPtr)
	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, head, arrayDataOffset)
	dataPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)
	offset := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", offset, newLen, stride)
	elemPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", elemPtr, dataPtr, offset)
	if call.HasDst {
		elemVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", elemVal, elemLLVM, elemPtr)
		dstType, err := fe.placeBaseType(call.Dst)
		if err != nil {
			return err
		}
		tagIndex, meta, err := fe.emitter.tagCaseMeta(dstType, "Some", symbols.NoSymbolID)
		if err != nil {
			return err
		}
		if len(meta.PayloadTypes) != 1 {
			return fmt.Errorf("tag %q expects 1 payload value, got %d", meta.TagName, len(meta.PayloadTypes))
		}
		tagVal, err := fe.emitTagValueSinglePayload(dstType, tagIndex, meta.PayloadTypes[0], elemVal, elemLLVM)
		if err != nil {
			return err
		}
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tagVal, ptr)
	}
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", done)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", done)
	return nil
}
