package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// handleLen handles the __len intrinsic.
func (vm *VM) handleLen(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "__len requires a destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "__len requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	if arg.Kind == VKRef || arg.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(arg.Loc)
		if loadErr != nil {
			return loadErr
		}
		arg = v
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	switch arg.Kind {
	case VKHandleString:
		obj := vm.Heap.Get(arg.H)
		u64, err := safecast.Conv[uint64](vm.stringCPLen(obj))
		if err != nil {
			return vm.eb.invalidNumericConversion("string length out of range")
		}
		val := vm.makeBigUint(dstType, bignum.UintFromUint64(u64))
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	case VKHandleArray:
		view, vmErr := vm.arrayViewFromHandle(arg.H)
		if vmErr != nil {
			return vmErr
		}
		u64, err := safecast.Conv[uint64](view.length)
		if err != nil {
			return vm.eb.invalidNumericConversion("array length out of range")
		}
		val := vm.makeBigUint(dstType, bignum.UintFromUint64(u64))
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	case VKHandleStruct:
		info, vmErr := vm.bytesViewLayout(arg.TypeID)
		if vmErr != nil {
			return vmErr
		}
		if !info.ok {
			return vm.eb.typeMismatch("len-compatible", arg.Kind.String())
		}
		obj := vm.Heap.Get(arg.H)
		if obj.Kind != OKStruct {
			return vm.eb.typeMismatch("struct", fmt.Sprintf("%v", obj.Kind))
		}
		if info.ownerIdx < 0 || info.ownerIdx >= len(obj.Fields) || info.lenIdx < 0 || info.lenIdx >= len(obj.Fields) {
			return vm.eb.makeError(PanicOutOfBounds, "bytes view layout mismatch")
		}
		lenVal, vmErr := vm.cloneForShare(obj.Fields[info.lenIdx])
		if vmErr != nil {
			return vmErr
		}
		lenVal.TypeID = dstType
		if vmErr := vm.writeLocal(frame, dstLocal, lenVal); vmErr != nil {
			vm.dropValue(lenVal)
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   lenVal,
		})
	default:
		return vm.eb.typeMismatch("len-compatible", arg.Kind.String())
	}
	return nil
}

// handleClone handles the __clone intrinsic.
func (vm *VM) handleClone(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "__clone requires a destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "__clone requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	if arg.Kind == VKRef || arg.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(arg.Loc)
		if loadErr != nil {
			return loadErr
		}
		arg = v
	}
	if arg.Kind != VKHandleString {
		return vm.eb.typeMismatch("string", arg.Kind.String())
	}
	clone, vmErr := vm.cloneForShare(arg)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	if vmErr := vm.writeLocal(frame, dstLocal, clone); vmErr != nil {
		vm.dropValue(clone)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   clone,
	})
	return nil
}

// handleCloneValue handles the clone intrinsic for Copy types.
func (vm *VM) handleCloneValue(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "clone requires a destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "clone requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsArg := arg.IsHeap()
	defer func() {
		if ownsArg {
			vm.dropValue(arg)
		}
	}()
	replaceArg := func(next Value, nextOwned bool) {
		if ownsArg {
			vm.dropValue(arg)
		}
		arg = next
		ownsArg = nextOwned && arg.IsHeap()
	}
	if arg.Kind == VKRef || arg.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(arg.Loc)
		if loadErr != nil {
			return loadErr
		}
		replaceArg(v, false)
	}
	if arg.IsHeap() {
		var cloneErr *VMError
		clone, cloneErr := vm.cloneForShare(arg)
		if cloneErr != nil {
			return cloneErr
		}
		replaceArg(clone, true)
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	if vm.Types != nil && dstType != types.NoTypeID {
		if !vm.Types.IsCopy(resolveAlias(vm.Types, dstType)) {
			return vm.eb.makeError(PanicTypeMismatch, "clone requires a Copy type")
		}
	}
	arg.TypeID = dstType
	if vmErr := vm.writeLocal(frame, dstLocal, arg); vmErr != nil {
		return vmErr
	}
	ownsArg = false
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   frame.Locals[dstLocal].V,
	})
	return nil
}

// handleIndex handles the __index intrinsic.
func (vm *VM) handleIndex(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "__index requires a destination")
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "__index requires 2 arguments")
	}
	objVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(objVal)
	idxVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(idxVal)
	if objVal.Kind == VKRef || objVal.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(objVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		objVal = v
	}
	res, vmErr := vm.evalIndex(objVal, idxVal)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	if res.TypeID == types.NoTypeID {
		res.TypeID = frame.Locals[dstLocal].TypeID
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		if res.IsHeap() {
			vm.dropValue(res)
		}
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   res,
	})
	return nil
}

// handleTo handles the __to intrinsic.
func (vm *VM) handleTo(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "__to requires a destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "__to requires 1 argument")
	}
	srcVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsSrc := srcVal.IsHeap()
	if srcVal.Kind == VKRef || srcVal.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(srcVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		srcVal = v
		ownsSrc = false
	}
	dstLocal := call.Dst.Local
	dstTy := frame.Locals[dstLocal].TypeID

	converted, vmErr := vm.evalIntrinsicTo(srcVal, dstTy)
	if vmErr != nil {
		if ownsSrc {
			vm.dropValue(srcVal)
		}
		return vmErr
	}
	vmErr = vm.writeLocal(frame, dstLocal, converted)
	if vmErr != nil {
		if ownsSrc && !(srcVal.IsHeap() && converted.IsHeap() && srcVal.Kind == converted.Kind && srcVal.H == converted.H) {
			vm.dropValue(srcVal)
		}
		if !ownsSrc && converted.IsHeap() {
			vm.dropValue(converted)
		}
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   converted,
	})
	if ownsSrc && !(srcVal.IsHeap() && converted.IsHeap() && srcVal.Kind == converted.Kind && srcVal.H == converted.H) {
		vm.dropValue(srcVal)
	}
	return nil
}

func (vm *VM) handleMagicBinary(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite, name string, op ast.ExprBinaryOp) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, name+" requires a destination")
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, name+" requires 2 arguments")
	}
	left, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(left)
	right, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(right)
	if left.Kind == VKRef || left.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(left.Loc)
		if loadErr != nil {
			return loadErr
		}
		left = v
	}
	if right.Kind == VKRef || right.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(right.Loc)
		if loadErr != nil {
			return loadErr
		}
		right = v
	}
	res, vmErr := vm.evalBinaryOp(op, left, right)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	if res.TypeID == types.NoTypeID {
		res.TypeID = frame.Locals[dstLocal].TypeID
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		if res.IsHeap() {
			vm.dropValue(res)
		}
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   res,
	})
	return nil
}

func (vm *VM) handleMagicUnary(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite, name string, op ast.ExprUnaryOp) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, name+" requires a destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, name+" requires 1 argument")
	}
	operand, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsOperand := operand.IsHeap()
	if operand.Kind == VKRef || operand.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(operand.Loc)
		if loadErr != nil {
			return loadErr
		}
		operand = v
		ownsOperand = false
	}
	res, vmErr := vm.evalUnaryOp(op, operand)
	if vmErr != nil {
		if ownsOperand {
			vm.dropValue(operand)
		}
		return vmErr
	}
	dstLocal := call.Dst.Local
	if res.TypeID == types.NoTypeID {
		res.TypeID = frame.Locals[dstLocal].TypeID
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		if res.IsHeap() && res != operand {
			vm.dropValue(res)
		}
		if ownsOperand {
			vm.dropValue(operand)
		}
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   res,
	})
	if ownsOperand && res != operand {
		vm.dropValue(operand)
	}
	return nil
}

func resolveAlias(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		tt, ok := typesIn.Lookup(id)
		if !ok || tt.Kind != types.KindAlias {
			return id
		}
		target, ok := typesIn.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
		seen++
	}
	return id
}
