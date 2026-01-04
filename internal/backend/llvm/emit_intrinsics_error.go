package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitExitIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "exit" {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("exit requires 1 argument")
	}
	argVal, _, argType, err := fe.emitToSource(&call.Args[0])
	if err != nil {
		return true, err
	}
	errType := argType
	if fe.emitter != nil && fe.emitter.types != nil {
		if tt, ok := fe.emitter.types.Lookup(resolveAliasAndOwn(fe.emitter.types, argType)); ok && tt.Kind == types.KindUnion {
			if unionErr, uErr := fe.erringErrorType(argType); uErr == nil {
				errType = unionErr
			}
		}
	}
	msgVal, codeVal, codeLLVM, codeType, err := fe.emitErrorLikeFields(argVal, errType)
	if err != nil {
		return true, err
	}
	maxIndex := int64(^uint64(0) >> 1)
	var code64 string
	switch {
	case isBigUintType(fe.emitter.types, codeType):
		code64, err = fe.emitCheckedBigUintToU64(codeVal, "exit code out of range")
		if err != nil {
			return true, err
		}
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, code64, maxIndex)
		fail := fe.nextInlineBlock()
		cont := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", tooHigh, fail, cont)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
		if panicErr := fe.emitPanicNumeric("exit code out of range"); panicErr != nil {
			return true, panicErr
		}
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	case isBigIntType(fe.emitter.types, codeType):
		code64, err = fe.emitCheckedBigIntToI64(codeVal, "exit code out of range")
		if err != nil {
			return true, err
		}
		neg := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", neg, code64)
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sgt i64 %s, %d\n", tooHigh, code64, maxIndex)
		oob := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", oob, neg, tooHigh)
		fail := fe.nextInlineBlock()
		cont := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", oob, fail, cont)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
		if panicErr := fe.emitPanicNumeric("exit code out of range"); panicErr != nil {
			return true, panicErr
		}
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	default:
		info, ok := intInfo(fe.emitter.types, codeType)
		if !ok {
			return true, fmt.Errorf("exit code must be an integer")
		}
		code64, err = fe.coerceIntToI64(codeVal, codeLLVM, codeType)
		if err != nil {
			return true, err
		}
		if info.signed {
			neg := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", neg, code64)
			tooHigh := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sgt i64 %s, %d\n", tooHigh, code64, maxIndex)
			oob := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", oob, neg, tooHigh)
			fail := fe.nextInlineBlock()
			cont := fe.nextInlineBlock()
			fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", oob, fail, cont)
			fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
			if panicErr := fe.emitPanicNumeric("exit code out of range"); panicErr != nil {
				return true, panicErr
			}
			fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
		} else {
			tooHigh := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, code64, maxIndex)
			fail := fe.nextInlineBlock()
			cont := fe.nextInlineBlock()
			fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", tooHigh, fail, cont)
			fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
			if panicErr := fe.emitPanicNumeric("exit code out of range"); panicErr != nil {
				return true, panicErr
			}
			fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
		}
	}
	msgAddr := fe.emitHandleAddr(msgVal)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_string_len_bytes(ptr %s)\n", lenVal, msgAddr)
	isEmpty := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i64 %s, 0\n", isEmpty, lenVal)
	skip := fe.nextInlineBlock()
	write := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isEmpty, skip, write)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", write)
	msgPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_ptr(ptr %s)\n", msgPtr, msgAddr)
	fmt.Fprintf(&fe.emitter.buf, "  call i64 @rt_write_stderr(ptr %s, i64 %s)\n", msgPtr, lenVal)
	lastIdx := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = sub i64 %s, 1\n", lastIdx, lenVal)
	lastPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", lastPtr, msgPtr, lastIdx)
	lastByte := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", lastByte, lastPtr)
	isNL := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 10\n", isNL, lastByte)
	writeNL := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isNL, skip, writeNL)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", writeNL)
	nlHandle, _, err := fe.emitStringConst("\n")
	if err != nil {
		return true, err
	}
	nlAddr := fe.emitHandleAddr(nlHandle)
	nlPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_ptr(ptr %s)\n", nlPtr, nlAddr)
	fmt.Fprintf(&fe.emitter.buf, "  call i64 @rt_write_stderr(ptr %s, i64 1)\n", nlPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", skip)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", skip)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_exit(i64 %s)\n", code64)
	return true, nil
}

func (fe *funcEmitter) erringErrorType(typeID types.TypeID) (types.TypeID, error) {
	if fe.emitter == nil || fe.emitter.types == nil {
		return types.NoTypeID, fmt.Errorf("missing type interner")
	}
	typeID = resolveValueType(fe.emitter.types, typeID)
	info, ok := fe.emitter.types.UnionInfo(typeID)
	if !ok || info == nil {
		return types.NoTypeID, fmt.Errorf("missing union info for type#%d", typeID)
	}
	for _, member := range info.Members {
		if member.Kind == types.UnionMemberType {
			return member.Type, nil
		}
	}
	return types.NoTypeID, fmt.Errorf("tag Erring missing error variant")
}

func (fe *funcEmitter) emitErrorLikeValue(errType types.TypeID, msgVal, msgTy, codeVal, codeTy string) (string, error) {
	layoutInfo, err := fe.emitter.layoutOf(errType)
	if err != nil {
		return "", err
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

	msgIdx, msgFieldType, err := fe.structFieldInfo(errType, mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: "message", FieldIdx: -1})
	if err != nil {
		return "", err
	}
	codeIdx, codeFieldType, err := fe.structFieldInfo(errType, mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: "code", FieldIdx: -1})
	if err != nil {
		return "", err
	}
	if msgIdx < 0 || msgIdx >= len(layoutInfo.FieldOffsets) || codeIdx < 0 || codeIdx >= len(layoutInfo.FieldOffsets) {
		return "", fmt.Errorf("error-like layout mismatch")
	}
	msgLLVM, err := llvmValueType(fe.emitter.types, msgFieldType)
	if err != nil {
		return "", err
	}
	codeLLVM, err := llvmValueType(fe.emitter.types, codeFieldType)
	if err != nil {
		return "", err
	}
	if msgTy != msgLLVM {
		msgTy = msgLLVM
	}
	if codeTy != codeLLVM {
		codeTy = codeLLVM
	}
	msgPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", msgPtr, mem, layoutInfo.FieldOffsets[msgIdx])
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", msgTy, msgVal, msgPtr)

	codePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", codePtr, mem, layoutInfo.FieldOffsets[codeIdx])
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", codeTy, codeVal, codePtr)
	return mem, nil
}

func (fe *funcEmitter) emitErrorLikeFields(errPtr string, errType types.TypeID) (msgVal, codeVal, codeLLVM string, codeType types.TypeID, err error) {
	layoutInfo, err := fe.emitter.layoutOf(errType)
	if err != nil {
		return "", "", "", types.NoTypeID, err
	}
	msgIdx, msgFieldType, err := fe.structFieldInfo(errType, mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: "message", FieldIdx: -1})
	if err != nil {
		return "", "", "", types.NoTypeID, err
	}
	codeIdx, codeFieldType, err := fe.structFieldInfo(errType, mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: "code", FieldIdx: -1})
	if err != nil {
		return "", "", "", types.NoTypeID, err
	}
	if msgIdx < 0 || msgIdx >= len(layoutInfo.FieldOffsets) || codeIdx < 0 || codeIdx >= len(layoutInfo.FieldOffsets) {
		return "", "", "", types.NoTypeID, fmt.Errorf("error-like layout mismatch")
	}
	msgLLVM, err := llvmValueType(fe.emitter.types, msgFieldType)
	if err != nil {
		return "", "", "", types.NoTypeID, err
	}
	codeLLVM, err = llvmValueType(fe.emitter.types, codeFieldType)
	if err != nil {
		return "", "", "", types.NoTypeID, err
	}
	msgPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", msgPtr, errPtr, layoutInfo.FieldOffsets[msgIdx])
	msgVal = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", msgVal, msgLLVM, msgPtr)

	codePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", codePtr, errPtr, layoutInfo.FieldOffsets[codeIdx])
	codeVal = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", codeVal, codeLLVM, codePtr)
	return msgVal, codeVal, codeLLVM, codeFieldType, nil
}
