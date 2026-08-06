package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitArrayPop(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("rt_array_pop requires 1 argument")
	}
	elemType, elemLLVM, stride, _, err := fe.arrayElemLayout(&call.Args[0])
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
	capPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", capPtr, head, arrayCapOffset)
	curCap := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", curCap, capPtr)
	if err := fe.emitArrayViewResizeGuard(head); err != nil {
		return err
	}

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
		tagVal, err := fe.emitTagValueSinglePayload(dstType, tagIndex, meta.PayloadTypes[0], elemVal, elemLLVM, elemType)
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

func (fe *funcEmitter) emitArrayGetMut(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_array_get_mut requires 2 arguments")
	}
	if !call.HasDst {
		return nil
	}
	containerType := operandValueType(fe.emitter.types, &call.Args[0])
	if containerType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			containerType = baseType
		}
	}

	arrArg, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	idxVal, idxTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}

	var elemPtr string
	if fixedElemType, fixedLen, fixedOK := arrayFixedInfo(fe.emitter.types, containerType); fixedOK {
		elemPtr, _, err = fe.emitArrayFixedElemPtr(arrArg, idxVal, idxTy, call.Args[1].Type, fixedElemType, fixedLen)
		if err != nil {
			return err
		}
	} else if elemType, dynamic, ok := arrayElemType(fe.emitter.types, containerType); ok && dynamic {
		elemPtr, _, err = fe.emitArrayElemPtr(arrArg, idxVal, idxTy, call.Args[1].Type, elemType)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("rt_array_get_mut requires array")
	}

	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, elemPtr, ptr)
	return nil
}
