package llvm

import (
	"fmt"
	"strings"

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
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return true, err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, ptrVal, ptr, dstAlign)
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
	if name != "__len" && !strings.HasSuffix(name, ".__len") {
		return false, nil
	}
	// Always treat __len as intrinsic, even if a symbol exists in the module.
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
	if call.Args[0].Kind != mir.OperandConst {
		if call.Args[0].Kind == mir.OperandAddrOf || call.Args[0].Kind == mir.OperandAddrOfMut ||
			((call.Args[0].Kind == mir.OperandCopy || call.Args[0].Kind == mir.OperandCopyValue || call.Args[0].Kind == mir.OperandMove) &&
				fe.operandIsRef(&call.Args[0], call.Args[0].Type)) {
			if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
				targetType = baseType
			}
		} else if targetType == types.NoTypeID {
			if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
				targetType = baseType
			}
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

func (fe *funcEmitter) emitRangeIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "__range" {
		return false, nil
	}
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("__range requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	iterType := operandValueType(fe.emitter.types, &call.Args[0])
	if iterType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			iterType = baseType
		}
	}
	if _, dynamic, ok := arrayElemType(fe.emitter.types, iterType); ok {
		iterPtr, _, err := fe.emitArrayIterInit(&call.Args[0], iterType, dynamic)
		if err != nil {
			return true, err
		}
		ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
		if err != nil {
			return true, err
		}
		if !isStorageRun(dstTy) {
			dstTy = handleType
		}
		fe.emitValueStore(dstTy, iterPtr, ptr, dstAlign)
		return true, nil
	}
	return true, fmt.Errorf("__range requires array")
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
			fe.emitRangeObjectFree(rangeVal)
			ptr, dstTy, dstAlign, placeErr := fe.emitPlaceStorage(call.Dst)
			if placeErr != nil {
				return placeErr
			}
			if !isStorageRun(dstTy) {
				dstTy = handleType
			}
			fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
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
		ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "i32" {
			dstTy = "i32"
		}
		fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
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
		ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != ty {
			dstTy = ty
		}
		fe.emitValueStore(dstTy, val, ptr, dstAlign)
		return nil
	case fixedOK:
		arrArg, err := fe.emitHandleOperandPtr(&call.Args[0])
		if err != nil {
			return err
		}
		indexType := operandValueType(fe.emitter.types, &call.Args[1])
		if isRangeType(fe.emitter.types, indexType) {
			rangeVal, _, rangeErr := fe.emitOperand(&call.Args[1])
			if rangeErr != nil {
				return rangeErr
			}
			tmp, sliceErr := fe.emitArrayFixedSlice(arrArg, rangeVal, fixedElemType, fixedLen)
			if sliceErr != nil {
				return sliceErr
			}
			ptr, dstTy, dstAlign, placeErr := fe.emitPlaceStorage(call.Dst)
			if placeErr != nil {
				return placeErr
			}
			if !isStorageRun(dstTy) {
				dstTy = handleType
			}
			fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
			return nil
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
		ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != ty {
			dstTy = ty
		}
		fe.emitValueStore(dstTy, val, ptr, dstAlign)
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
		indexType := operandValueType(fe.emitter.types, &call.Args[1])
		if isRangeType(fe.emitter.types, indexType) {
			rangeVal, _, rangeErr := fe.emitOperand(&call.Args[1])
			if rangeErr != nil {
				return rangeErr
			}
			tmp, sliceErr := fe.emitArraySlice(arrArg, rangeVal, elemType)
			if sliceErr != nil {
				return sliceErr
			}
			ptr, dstTy, dstAlign, placeErr := fe.emitPlaceStorage(call.Dst)
			if placeErr != nil {
				return placeErr
			}
			if !isStorageRun(dstTy) {
				dstTy = handleType
			}
			fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
			return nil
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
		ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != ty {
			dstTy = ty
		}
		fe.emitValueStore(dstTy, val, ptr, dstAlign)
		return nil
	default:
		return fmt.Errorf("unsupported __index target")
	}
}

func (fe *funcEmitter) emitLenStore(dst mir.Place, dstType types.TypeID, lenVal string) error {
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(dst)
	if err != nil {
		return err
	}
	if isBigUintType(fe.emitter.types, dstType) {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_from_u64(i64 %s)\n", tmp, lenVal)
		if !isStorageRun(dstTy) {
			dstTy = handleType
		}
		fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
		return nil
	}
	if dstTy != "i64" {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to %s\n", tmp, lenVal, dstTy)
		lenVal = tmp
	}
	fe.emitValueStore(dstTy, lenVal, ptr, dstAlign)
	return nil
}

// releaseDisplacedElement frees whatever an element slot is holding, just
// before a store lands on it (RV2-DEBT-204).
//
// The VM has always done this — `viewElemStore` and `storeStorage` read the
// previous value, write, and drop what they read — while both branches of
// `emitIndexSet` ended in a bare store, so `xs[i] = v` leaked the displaced
// element on the native lane only. `drop_elem.typeN` is the right primitive
// rather than a new one: it exists to drop a single element slot given its
// pointer, which is what `rt_array_free_elems` calls it for.
//
// ORDER. Callers must have fully evaluated the right-hand side before calling
// this, and must store after it. Both branches below already evaluated `val`
// first, so the sequence is the one the whole-binding path settled on: RHS
// evaluated, displaced value freed, store lands.
//
// WHAT MAKES THIS SAFE IS NOT LOCAL, AND THAT IS WHY IT IS WRITTEN DOWN. A
// release before a store double-frees if the same value can reach the store
// from the slot being released, or if the slot was already emptied, or if some
// OTHER binding still owns what the slot holds. All three are refused today,
// and sema is what refuses them, not this function:
//
//	xs[0] = xs[0]  and  xs[0] = xs[1]
//	    "cannot mutate 'xs'[?] while it is shared-borrowed"
//	@drop xs[0]
//	    "cannot take `xs[?]` out of `xs`: it names an element, and what would
//	     be left cannot be listed"
//	let a = ...; let xs = [a, ...]; ... a ...
//	    "use of moved value 'a'" — RV2-DEBT-209, and the reason this release
//	    could not land until that one did: the leak it removes was the only
//	    thing keeping a second owner from freeing the same value.
//
// The first two are pinned by golden fixtures — element_self_assign_forbidden,
// element_cross_assign_forbidden and element_move_out_forbidden under
// testdata/golden/hir_borrow/invalid, recording SEM3019 and SEM3143 verbatim.
// Relax any of these rules and something fails in the same commit that relaxes
// it, rather than this becoming a double free nobody attributed.
func (fe *funcEmitter) releaseDisplacedElement(elemPtr string, elemType types.TypeID) {
	if !fe.emitter.typeOwnsHeap(elemType) {
		return
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @%s(ptr %s)\n",
		fe.emitter.requireDropElemGlue(elemType), elemPtr)
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
	elemType, dynamic, ok := arrayElemType(fe.emitter.types, containerType)
	if ok && dynamic {
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
		val, _, err := fe.emitValueOperand(&call.Args[2])
		if err != nil {
			return err
		}
		align, alignErr := fe.emitter.storageAlignOf(elemType, elemLLVM)
		if alignErr != nil {
			return alignErr
		}
		fe.releaseDisplacedElement(elemPtr, elemType)
		fe.emitValueStore(elemLLVM, val, elemPtr, align)
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
	val, _, err := fe.emitValueOperand(&call.Args[2])
	if err != nil {
		return err
	}
	fixedAlign, fixedAlignErr := fe.emitter.storageAlignOf(fixedElemType, elemLLVM)
	if fixedAlignErr != nil {
		return fixedAlignErr
	}
	fe.releaseDisplacedElement(elemPtr, fixedElemType)
	fe.emitValueStore(elemLLVM, val, elemPtr, fixedAlign)
	return nil
}
