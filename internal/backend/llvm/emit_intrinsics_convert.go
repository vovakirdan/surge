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

	parseKind := fe.parseKindForType(targetType)

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
	tagVal, err := fe.emitTagValueSinglePayload(dstType, successIdx, targetType, parsedVal, parsedTy, targetType)
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
	msgVal, err := fe.emitParseErrorMessage(strVal, parseKind)
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

func (fe *funcEmitter) parseKindForType(typeID types.TypeID) string {
	if isBigIntType(fe.emitter.types, typeID) {
		return "int"
	}
	if isBigUintType(fe.emitter.types, typeID) {
		return "uint"
	}
	if isBigFloatType(fe.emitter.types, typeID) {
		return "float"
	}
	if isBoolType(fe.emitter.types, typeID) {
		return "bool"
	}
	if info, ok := intInfo(fe.emitter.types, typeID); ok {
		if info.signed {
			return "int"
		}
		return "uint"
	}
	if _, ok := floatInfo(fe.emitter.types, typeID); ok {
		return "float"
	}
	return ""
}

func (fe *funcEmitter) emitParseErrorMessage(strVal, kind string) (string, error) {
	var middle string
	switch kind {
	case "int":
		middle = "\\\" as int: invalid numeric format: \\\""
	case "uint":
		middle = "\\\" as uint: invalid numeric format: \\\""
	case "float":
		middle = "\\\" as float: invalid numeric format: \\\""
	default:
		msgVal, _, err := fe.emitStringConst("parse error")
		return msgVal, err
	}
	prefixVal, _, err := fe.emitStringConst("failed to parse \\\"")
	if err != nil {
		return "", err
	}
	middleVal, _, err := fe.emitStringConst(middle)
	if err != nil {
		return "", err
	}
	suffixVal, _, err := fe.emitStringConst("\\\"")
	if err != nil {
		return "", err
	}
	return fe.emitStringConcatAll(prefixVal, strVal, middleVal, strVal, suffixVal)
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
