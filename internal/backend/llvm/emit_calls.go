package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitCall(ins *mir.Instr) error {
	call := &ins.Call
	if handled, err := fe.emitTagConstructor(call); handled {
		return err
	}
	if handled, err := fe.emitLenIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitIndexIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitMagicIntrinsic(call); handled {
		return err
	}
	callee, sig, err := fe.resolveCallee(call)
	if err != nil {
		return err
	}
	args := make([]string, 0, len(call.Args))
	for i := range call.Args {
		val, ty, err := fe.emitOperand(&call.Args[i])
		if err != nil {
			return err
		}
		args = append(args, fmt.Sprintf("%s %s", ty, val))
	}
	callStmt := fmt.Sprintf("call %s @%s(%s)", sig.ret, callee, strings.Join(args, ", "))
	if call.HasDst {
		if sig.ret == "void" {
			return fmt.Errorf("call has destination but returns void: %s", callee)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s\n", tmp, callStmt)
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != sig.ret {
			dstTy = sig.ret
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}
	fmt.Fprintf(&fe.emitter.buf, "  %s\n", callStmt)
	return nil
}

func (fe *funcEmitter) emitTagConstructor(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym || !call.Callee.Sym.IsValid() {
		return false, nil
	}
	sym := fe.emitter.symFor(call.Callee.Sym)
	if sym == nil || sym.Kind != symbols.SymbolTag {
		return false, nil
	}
	if !call.HasDst {
		return true, fmt.Errorf("tag constructor requires a destination")
	}
	dstType := fe.f.Locals[call.Dst.Local].Type
	tagName := call.Callee.Name
	if tagName == "" {
		tagName = fe.symbolName(call.Callee.Sym)
	}
	ptrVal, err := fe.emitTagValue(dstType, tagName, call.Callee.Sym, call.Args)
	if err != nil {
		return true, err
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, ptrVal, ptr)
	return true, nil
}

func (fe *funcEmitter) emitLenIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "__len" {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("__len requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	targetType := operandValueType(fe.emitter.types, &call.Args[0])
	switch {
	case isStringLike(fe.emitter.types, targetType):
		argVal, _, err := fe.emitOperand(&call.Args[0])
		if err != nil {
			return true, err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_string_len(ptr %s)\n", tmp, argVal)
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return true, err
		}
		if dstTy != "i64" {
			dstTy = "i64"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return true, nil
	case isArrayLike(fe.emitter.types, targetType):
		argVal, _, err := fe.emitOperand(&call.Args[0])
		if err != nil {
			return true, err
		}
		lenVal := fe.emitArrayLen(argVal)
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return true, err
		}
		if dstTy != "i64" {
			dstTy = "i64"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, lenVal, ptr)
		return true, nil
	case isBytesViewType(fe.emitter.types, targetType):
		argVal, _, err := fe.emitOperand(&call.Args[0])
		if err != nil {
			return true, err
		}
		lenVal, err := fe.emitBytesViewLen(argVal, targetType)
		if err != nil {
			return true, err
		}
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return true, err
		}
		if dstTy != "i64" {
			dstTy = "i64"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, lenVal, ptr)
		return true, nil
	default:
		return true, fmt.Errorf("unsupported __len target")
	}
}

func (fe *funcEmitter) emitIndexIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	switch name {
	case "__index":
		return true, fe.emitIndexGet(call)
	case "__index_set":
		return true, fe.emitIndexSet(call)
	default:
		return false, nil
	}
}

func (fe *funcEmitter) emitMagicIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	switch name {
	case "__add", "__sub", "__mul", "__div", "__mod",
		"__bit_and", "__bit_or", "__bit_xor", "__shl", "__shr",
		"__eq", "__ne", "__lt", "__le", "__gt", "__ge":
		if !fe.canEmitMagicBinary(call) {
			return false, nil
		}
		return true, fe.emitMagicBinaryIntrinsic(call, name)
	case "__pos", "__neg", "__not":
		if !fe.canEmitMagicUnary(call) {
			return false, nil
		}
		return true, fe.emitMagicUnaryIntrinsic(call, name)
	default:
		return false, nil
	}
}

func (fe *funcEmitter) canEmitMagicBinary(call *mir.CallInstr) bool {
	if call == nil || len(call.Args) != 2 {
		return false
	}
	leftType := operandValueType(fe.emitter.types, &call.Args[0])
	rightType := operandValueType(fe.emitter.types, &call.Args[1])
	if isStringLike(fe.emitter.types, leftType) || isStringLike(fe.emitter.types, rightType) {
		return isStringLike(fe.emitter.types, leftType) && isStringLike(fe.emitter.types, rightType)
	}
	_, okLeft := intInfo(fe.emitter.types, leftType)
	_, okRight := intInfo(fe.emitter.types, rightType)
	return okLeft && okRight
}

func (fe *funcEmitter) canEmitMagicUnary(call *mir.CallInstr) bool {
	if call == nil || len(call.Args) != 1 {
		return false
	}
	operandType := operandValueType(fe.emitter.types, &call.Args[0])
	_, ok := intInfo(fe.emitter.types, operandType)
	return ok
}

func (fe *funcEmitter) emitIndexGet(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("__index requires 2 arguments")
	}
	if !call.HasDst {
		return nil
	}
	containerType := operandValueType(fe.emitter.types, &call.Args[0])
	dstType := types.NoTypeID
	if call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
		dstType = fe.f.Locals[call.Dst.Local].Type
	}
	switch {
	case isStringLike(fe.emitter.types, containerType):
		strArg, _, err := fe.emitOperand(&call.Args[0])
		if err != nil {
			return err
		}
		indexType := operandValueType(fe.emitter.types, &call.Args[1])
		if isRangeType(fe.emitter.types, indexType) {
			rangeVal, _, rangeErr := fe.emitOperand(&call.Args[1])
			if rangeErr != nil {
				return rangeErr
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_slice(ptr %s, ptr %s)\n", tmp, strArg, rangeVal)
			ptr, dstTy, placeErr := fe.emitPlacePtr(call.Dst)
			if placeErr != nil {
				return placeErr
			}
			if dstTy != "ptr" {
				dstTy = "ptr"
			}
			fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
			return nil
		}
		idxVal, idxTy, err := fe.emitValueOperand(&call.Args[1])
		if err != nil {
			return err
		}
		idx64, err := fe.coerceIntToI64(idxVal, idxTy, call.Args[1].Type)
		if err != nil {
			return err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i32 @rt_string_index(ptr %s, i64 %s)\n", tmp, strArg, idx64)
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "i32" {
			dstTy = "i32"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	case isBytesViewType(fe.emitter.types, containerType):
		viewArg, _, err := fe.emitOperand(&call.Args[0])
		if err != nil {
			return err
		}
		idxVal, idxTy, err := fe.emitValueOperand(&call.Args[1])
		if err != nil {
			return err
		}
		val, ty, err := fe.emitBytesViewIndex(viewArg, containerType, idxVal, idxTy, call.Args[1].Type)
		if err != nil {
			return err
		}
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != ty {
			dstTy = ty
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, val, ptr)
		return nil
	case isArrayLike(fe.emitter.types, containerType):
		elemType, _, ok := arrayElemType(fe.emitter.types, containerType)
		if !ok {
			return fmt.Errorf("unsupported __index target")
		}
		arrArg, _, err := fe.emitOperand(&call.Args[0])
		if err != nil {
			return err
		}
		idxVal, idxTy, err := fe.emitValueOperand(&call.Args[1])
		if err != nil {
			return err
		}
		elemPtr, elemLLVM, err := fe.emitArrayElemPtr(arrArg, idxVal, idxTy, call.Args[1].Type, elemType)
		if err != nil {
			return err
		}
		val := ""
		ty := ""
		if isRefType(fe.emitter.types, dstType) {
			val = elemPtr
			ty = "ptr"
		} else {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, elemLLVM, elemPtr)
			val = tmp
			ty = elemLLVM
		}
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != ty {
			dstTy = ty
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, val, ptr)
		return nil
	default:
		return fmt.Errorf("unsupported __index target")
	}
}

func (fe *funcEmitter) emitIndexSet(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 3 {
		return fmt.Errorf("__index_set requires 3 arguments")
	}
	containerType := operandValueType(fe.emitter.types, &call.Args[0])
	if !isArrayLike(fe.emitter.types, containerType) {
		return fmt.Errorf("unsupported __index_set target")
	}
	elemType, _, ok := arrayElemType(fe.emitter.types, containerType)
	if !ok {
		return fmt.Errorf("unsupported __index_set target")
	}
	arrArg, _, err := fe.emitOperand(&call.Args[0])
	if err != nil {
		return err
	}
	idxVal, idxTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	elemPtr, elemLLVM, err := fe.emitArrayElemPtr(arrArg, idxVal, idxTy, call.Args[1].Type, elemType)
	if err != nil {
		return err
	}
	val, valTy, err := fe.emitValueOperand(&call.Args[2])
	if err != nil {
		return err
	}
	if valTy != elemLLVM {
		valTy = elemLLVM
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, elemPtr)
	return nil
}

func (fe *funcEmitter) emitMagicBinaryIntrinsic(call *mir.CallInstr, name string) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("%s requires 2 arguments", name)
	}
	if !call.HasDst {
		return nil
	}
	leftType := operandValueType(fe.emitter.types, &call.Args[0])
	rightType := operandValueType(fe.emitter.types, &call.Args[1])
	if isStringLike(fe.emitter.types, leftType) || isStringLike(fe.emitter.types, rightType) {
		if !isStringLike(fe.emitter.types, leftType) || !isStringLike(fe.emitter.types, rightType) {
			return fmt.Errorf("mixed string and non-string operands")
		}
		leftPtr, err := fe.emitOperandAddr(&call.Args[0])
		if err != nil {
			return err
		}
		rightPtr, err := fe.emitOperandAddr(&call.Args[1])
		if err != nil {
			return err
		}
		tmp := fe.nextTemp()
		resultTy := ""
		switch name {
		case "__add":
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_concat(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
			resultTy = "ptr"
		case "__eq":
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
			resultTy = "i1"
		case "__ne":
			eqTmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", eqTmp, leftPtr, rightPtr)
			fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, eqTmp)
			resultTy = "i1"
		default:
			return fmt.Errorf("unsupported string op %s", name)
		}
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != resultTy {
			dstTy = resultTy
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}

	leftVal, leftTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	rightVal, rightTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if leftTy != rightTy {
		return fmt.Errorf("binary operand type mismatch: %s vs %s", leftTy, rightTy)
	}
	info, ok := intInfo(fe.emitter.types, leftType)
	if !ok && leftTy != "ptr" {
		return fmt.Errorf("unsupported numeric op on type")
	}
	resultTy := leftTy
	tmp := fe.nextTemp()
	switch name {
	case "__add", "__sub", "__mul", "__div", "__mod", "__bit_and", "__bit_or", "__bit_xor", "__shl", "__shr":
		if !ok {
			return fmt.Errorf("unsupported numeric op on type")
		}
		opcode := ""
		switch name {
		case "__add":
			opcode = "add"
		case "__sub":
			opcode = "sub"
		case "__mul":
			opcode = "mul"
		case "__div":
			if info.signed {
				opcode = "sdiv"
			} else {
				opcode = "udiv"
			}
		case "__mod":
			if info.signed {
				opcode = "srem"
			} else {
				opcode = "urem"
			}
		case "__bit_and":
			opcode = "and"
		case "__bit_or":
			opcode = "or"
		case "__bit_xor":
			opcode = "xor"
		case "__shl":
			opcode = "shl"
		case "__shr":
			if info.signed {
				opcode = "ashr"
			} else {
				opcode = "lshr"
			}
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, opcode, leftTy, leftVal, rightVal)
	case "__eq", "__ne", "__lt", "__le", "__gt", "__ge":
		pred := ""
		switch name {
		case "__eq":
			pred = "eq"
		case "__ne":
			pred = "ne"
		case "__lt":
			if info.signed {
				pred = "slt"
			} else {
				pred = "ult"
			}
		case "__le":
			if info.signed {
				pred = "sle"
			} else {
				pred = "ule"
			}
		case "__gt":
			if info.signed {
				pred = "sgt"
			} else {
				pred = "ugt"
			}
		case "__ge":
			if info.signed {
				pred = "sge"
			} else {
				pred = "uge"
			}
		}
		if leftTy == "ptr" {
			if name != "__eq" && name != "__ne" {
				return fmt.Errorf("unsupported pointer comparison %s", name)
			}
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s ptr %s, %s\n", tmp, pred, leftVal, rightVal)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s %s %s, %s\n", tmp, pred, leftTy, leftVal, rightVal)
		}
		resultTy = "i1"
	default:
		return fmt.Errorf("unsupported magic binary op %s", name)
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != resultTy {
		dstTy = resultTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	return nil
}

func (fe *funcEmitter) emitMagicUnaryIntrinsic(call *mir.CallInstr, name string) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("%s requires 1 argument", name)
	}
	if !call.HasDst {
		return nil
	}
	val, ty, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	switch name {
	case "__pos":
		tmp = val
	case "__neg":
		info, ok := intInfo(fe.emitter.types, operandValueType(fe.emitter.types, &call.Args[0]))
		if !ok || !info.signed {
			return fmt.Errorf("unsupported unary minus type")
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = sub %s 0, %s\n", tmp, ty, val)
	case "__not":
		if ty != "i1" {
			return fmt.Errorf("unary not requires i1, got %s", ty)
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, val)
	default:
		return fmt.Errorf("unsupported magic unary op %s", name)
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != ty {
		dstTy = ty
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	return nil
}

func (fe *funcEmitter) resolveCallee(call *mir.CallInstr) (string, funcSig, error) {
	if call == nil {
		return "", funcSig{}, fmt.Errorf("nil call")
	}
	if call.Callee.Kind == mir.CalleeSym {
		if call.Callee.Sym.IsValid() {
			if fe.emitter.mod != nil {
				if id, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
					name := fe.emitter.funcNames[id]
					sig := fe.emitter.funcSigs[id]
					return name, sig, nil
				}
			}
		}
		name := call.Callee.Name
		if name == "" {
			name = fe.symbolName(call.Callee.Sym)
		}
		if sig, ok := fe.emitter.runtimeSigs[name]; ok {
			return name, sig, nil
		}
		return "", funcSig{}, fmt.Errorf("unknown external function %q", name)
	}
	return "", funcSig{}, fmt.Errorf("unsupported callee kind %v", call.Callee.Kind)
}

func (fe *funcEmitter) symbolName(symID symbols.SymbolID) string {
	if !symID.IsValid() || fe.emitter.syms == nil || fe.emitter.syms.Symbols == nil || fe.emitter.syms.Strings == nil {
		return ""
	}
	sym := fe.emitter.syms.Symbols.Get(symID)
	if sym == nil {
		return ""
	}
	return fe.emitter.syms.Strings.MustLookup(sym.Name)
}

func stripGenericSuffix(name string) string {
	if idx := strings.Index(name, "::<"); idx >= 0 {
		return name[:idx]
	}
	return name
}
