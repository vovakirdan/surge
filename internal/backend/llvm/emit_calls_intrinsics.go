package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("__len requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	dstType := types.NoTypeID
	if call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
		dstType = fe.f.Locals[call.Dst.Local].Type
	}
	targetType := operandValueType(fe.emitter.types, &call.Args[0])
	if targetType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			targetType = baseType
		}
	}
	handlePtr, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return true, err
	}
	switch {
	case isStringLike(fe.emitter.types, targetType):
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_string_len(ptr %s)\n", tmp, handlePtr)
		if err := fe.emitLenStore(call.Dst, dstType, tmp); err != nil {
			return true, err
		}
		return true, nil
	case isArrayLike(fe.emitter.types, targetType):
		lenVal := fe.emitArrayLen(handlePtr)
		if err := fe.emitLenStore(call.Dst, dstType, lenVal); err != nil {
			return true, err
		}
		return true, nil
	case isBytesViewType(fe.emitter.types, targetType):
		lenVal, err := fe.emitBytesViewLen(handlePtr, targetType)
		if err != nil {
			return true, err
		}
		if err := fe.emitLenStore(call.Dst, dstType, lenVal); err != nil {
			return true, err
		}
		return true, nil
	default:
		resolved := resolveValueType(fe.emitter.types, targetType)
		if fe.emitter.types != nil {
			if _, length, ok := fe.emitter.types.ArrayFixedInfo(resolved); ok {
				if err := fe.emitLenStore(call.Dst, dstType, fmt.Sprintf("%d", length)); err != nil {
					return true, err
				}
				return true, nil
			}
			if tt, ok := fe.emitter.types.Lookup(resolved); ok && tt.Kind == types.KindArray && tt.Count != types.ArrayDynamicLength {
				if err := fe.emitLenStore(call.Dst, dstType, fmt.Sprintf("%d", tt.Count)); err != nil {
					return true, err
				}
				return true, nil
			}
		}
		kind := "unknown"
		if fe.emitter != nil && fe.emitter.types != nil {
			if tt, ok := fe.emitter.types.Lookup(resolved); ok {
				kind = tt.Kind.String()
			}
		}
		return true, fmt.Errorf("unsupported __len target type#%d (%s)", targetType, kind)
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
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	switch name {
	case "__index":
		return true, fe.emitIndexGet(call)
	case "__index_set":
		return true, fe.emitIndexSet(call)
	default:
		return false, nil
	}
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
	if containerType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			containerType = baseType
		}
	}
	dstType := types.NoTypeID
	if call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
		dstType = fe.f.Locals[call.Dst.Local].Type
	}
	fixedElemType, fixedLen, fixedOK := arrayFixedInfo(fe.emitter.types, containerType)
	switch {
	case isStringLike(fe.emitter.types, containerType):
		strArg, err := fe.emitHandleOperandPtr(&call.Args[0])
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
		lenVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_string_len(ptr %s)\n", lenVal, strArg)
		idx64, err := fe.emitIndexToI64(0, idxVal, idxTy, call.Args[1].Type, lenVal)
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
		viewArg, err := fe.emitHandleOperandPtr(&call.Args[0])
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
	case fixedOK:
		arrArg, err := fe.emitHandleOperandPtr(&call.Args[0])
		if err != nil {
			return err
		}
		idxVal, idxTy, err := fe.emitValueOperand(&call.Args[1])
		if err != nil {
			return err
		}
		elemPtr, elemLLVM, err := fe.emitArrayFixedElemPtr(arrArg, idxVal, idxTy, call.Args[1].Type, fixedElemType, fixedLen)
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
	case isArrayLike(fe.emitter.types, containerType):
		elemType, _, ok := arrayElemType(fe.emitter.types, containerType)
		if !ok {
			return fmt.Errorf("unsupported __index target")
		}
		arrArg, err := fe.emitHandleOperandPtr(&call.Args[0])
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

func (fe *funcEmitter) emitLenStore(dst mir.Place, dstType types.TypeID, lenVal string) error {
	ptr, dstTy, err := fe.emitPlacePtr(dst)
	if err != nil {
		return err
	}
	if isBigUintType(fe.emitter.types, dstType) {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_from_u64(i64 %s)\n", tmp, lenVal)
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}
	if dstTy != "i64" {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to %s\n", tmp, lenVal, dstTy)
		lenVal = tmp
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, lenVal, ptr)
	return nil
}

func (fe *funcEmitter) emitIndexSet(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 3 {
		return fmt.Errorf("__index_set requires 3 arguments")
	}
	containerType := operandValueType(fe.emitter.types, &call.Args[0])
	if containerType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			containerType = baseType
		}
	}
	elemType, _, ok := arrayElemType(fe.emitter.types, containerType)
	if ok {
		arrArg, err := fe.emitHandleOperandPtr(&call.Args[0])
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
	fixedElemType, fixedLen, fixedOK := arrayFixedInfo(fe.emitter.types, containerType)
	if !fixedOK {
		return fmt.Errorf("unsupported __index_set target")
	}
	arrArg, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	idxVal, idxTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	elemPtr, elemLLVM, err := fe.emitArrayFixedElemPtr(arrArg, idxVal, idxTy, call.Args[1].Type, fixedElemType, fixedLen)
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
