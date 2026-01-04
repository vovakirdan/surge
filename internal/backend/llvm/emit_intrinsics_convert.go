package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

type floatMeta struct {
	bits int
}

func floatInfo(typesIn *types.Interner, id types.TypeID) (floatMeta, bool) {
	if typesIn == nil {
		return floatMeta{}, false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	if !ok || tt.Kind != types.KindFloat || tt.Width == types.WidthAny {
		return floatMeta{}, false
	}
	return floatMeta{bits: widthBits(tt.Width)}, true
}

func isBoolType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindBool
}

func (fe *funcEmitter) emitToIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "__to" {
		return false, nil
	}
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("__to requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	dstType := fe.f.Locals[call.Dst.Local].Type
	srcVal, srcLLVM, srcType, err := fe.emitToSource(&call.Args[0])
	if err != nil {
		return true, err
	}

	var outVal, outTy string
	switch {
	case isStringLike(fe.emitter.types, dstType):
		outVal, outTy, err = fe.emitToString(srcVal, srcLLVM, srcType)
	case isStringLike(fe.emitter.types, srcType):
		outVal, outTy, _, err = fe.emitParseStringValue(srcVal, dstType)
	default:
		outVal, outTy, err = fe.emitNumericCast(srcVal, srcLLVM, srcType, dstType)
	}
	if err != nil {
		return true, err
	}

	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}
	if dstTy != outTy {
		outTy = dstTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", outTy, outVal, ptr)
	return true, nil
}

func (fe *funcEmitter) emitFromStrIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "from_str" {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("from_str requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	dstType := fe.f.Locals[call.Dst.Local].Type
	successIdx, successMeta, err := fe.emitter.tagCaseMeta(dstType, "Success", symbols.NoSymbolID)
	if err != nil {
		return true, err
	}
	if len(successMeta.PayloadTypes) != 1 {
		return true, fmt.Errorf("from_str requires single payload Success tag")
	}
	targetType := successMeta.PayloadTypes[0]
	strVal, _, srcType, err := fe.emitToSource(&call.Args[0])
	if err != nil {
		return true, err
	}
	if !isStringLike(fe.emitter.types, srcType) {
		return true, fmt.Errorf("from_str requires string argument")
	}

	var parsedVal, parsedTy, okVal string
	if isStringLike(fe.emitter.types, targetType) {
		parsedVal = strVal
		parsedTy = "ptr"
		okVal = "1"
	} else {
		parsedVal, parsedTy, okVal, err = fe.emitParseStringValue(strVal, targetType)
		if err != nil {
			return true, err
		}
	}

	okBB := fe.nextInlineBlock()
	errBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()

	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, okBB, errBB)

	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
	tagVal, err := fe.emitTagValueSinglePayload(dstType, successIdx, targetType, parsedVal, parsedTy)
	if err != nil {
		return true, err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tagVal, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", errBB)
	errType, err := fe.erringErrorType(dstType)
	if err != nil {
		return true, err
	}
	msgVal, _, err := fe.emitStringConst("parse error")
	if err != nil {
		return true, err
	}
	errVal, err := fe.emitErrorLikeValue(errType, msgVal, "ptr", "1", "i64")
	if err != nil {
		return true, err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, errVal, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	return true, nil
}

func (fe *funcEmitter) emitToSource(op *mir.Operand) (val, llvmTy string, typeID types.TypeID, err error) {
	if op == nil {
		return "", "", types.NoTypeID, fmt.Errorf("nil operand")
	}
	val, llvmTy, err = fe.emitOperand(op)
	if err != nil {
		return "", "", types.NoTypeID, err
	}
	typeID = operandValueType(fe.emitter.types, op)
	if typeID == types.NoTypeID && op.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(op.Place); err == nil {
			typeID = baseType
		}
	}
	if isRefType(fe.emitter.types, op.Type) {
		elemType, ok := derefType(fe.emitter.types, op.Type)
		if ok {
			llvmElem, err := llvmValueType(fe.emitter.types, elemType)
			if err != nil {
				return "", "", types.NoTypeID, err
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, llvmElem, val)
			val = tmp
			llvmTy = llvmElem
			typeID = elemType
		}
	}
	return val, llvmTy, typeID, nil
}
