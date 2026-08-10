package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// taskResultServing works out how a task must serve its result once a second
// handle exists: the function that builds each asker its own value, and the id
// that releases the one original the task goes on holding.
//
// A result whose bits own nothing and travel in nothing needs neither. Any
// number of askers can read the same bits and none of them reclaims anything,
// so the pair is empty and the task keeps handing the bits over as before.
func (fe *funcEmitter) taskResultServing(taskType types.TypeID) (copyFn string, releaseID types.TypeID, err error) {
	payload, err := taskPayloadType(fe.emitter.types, taskType)
	if err != nil {
		return "", types.NoTypeID, err
	}
	if !fe.emitter.payloadNeedsRuntimeRelease(payload) {
		return "null", types.NoTypeID, nil
	}
	name := fe.emitter.requireCopyResultGlue(payload)
	return "@" + name, fe.emitter.registerCrossingDropResult(payload), nil
}

// taskPayloadType reads the T out of a Task<T>.
func taskPayloadType(typesIn *types.Interner, taskType types.TypeID) (types.TypeID, error) {
	resolved := resolveAliasAndOwn(typesIn, taskType)
	if info, ok := typesIn.AliasInfo(resolved); ok && info != nil && len(info.TypeArgs) == 1 {
		return info.TypeArgs[0], nil
	}
	if info, ok := typesIn.StructInfo(resolved); ok && info != nil && len(info.TypeArgs) == 1 {
		return info.TypeArgs[0], nil
	}
	return types.NoTypeID, fmt.Errorf("task handle type#%d has no single result type", taskType)
}

func (fe *funcEmitter) emitCloneValueIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "clone" {
		return false, nil
	}
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if !call.HasDst {
		return true, fmt.Errorf("clone requires a destination")
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("clone requires 1 argument")
	}
	dstType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return true, err
	}
	if isTaskType(fe.emitter.types, dstType) {
		val, valErr := fe.emitTaskHandleOperand(&call.Args[0])
		if valErr != nil {
			return true, valErr
		}
		copyFn, releaseID, resErr := fe.taskResultServing(dstType)
		if resErr != nil {
			return true, resErr
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_task_clone(ptr %s, ptr %s, i64 %d)\n",
			tmp, val, copyFn, releaseID)
		ptr, dstTy, dstAlign, ptrErr := fe.emitPlaceStorage(call.Dst)
		if ptrErr != nil {
			return true, ptrErr
		}
		if !isStorageRun(dstTy) {
			dstTy = handleType
		}
		fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
		return true, nil
	}
	if fe.emitter != nil && fe.emitter.types != nil && dstType != types.NoTypeID {
		if !fe.emitter.types.IsCopy(resolveAliasAndOwn(fe.emitter.types, dstType)) {
			return true, fmt.Errorf("clone requires a Copy type")
		}
	}
	val, valTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return true, err
	}
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return true, err
	}
	if isStorageRun(dstTy) {
		// A composite is duplicated by its own generated glue, into the
		// destination's storage. A byte copy alone would be wrong for any
		// member the two copies must not both own — which is the whole reason
		// the glue exists — and the argument here is a borrow, so its storage
		// is not this operation's to hand on.
		fmt.Fprintf(&fe.emitter.buf, "  call void @%s(ptr %s, ptr %s)\n",
			fe.emitter.requireCloneGlue(resolveValueType(fe.emitter.types, dstType)), ptr, val)
		return true, nil
	}
	// A borrowed argument is read through before it is stored. Whether it IS
	// borrowed is a question only the MIR can answer: `int`, `float` and every
	// other handle-carried scalar is itself a pointer here, so `&int` and `int`
	// are both `ptr` in the emitted text and comparing the two spellings hands
	// the ADDRESS on where the value was asked for.
	if !isRefType(fe.emitter.types, dstType) &&
		(fe.operandIsReference(&call.Args[0]) || (valTy == "ptr" && dstTy != "ptr")) {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s, align %d\n", tmp, dstTy, val, dstAlign)
		val = tmp
	}
	// A counted scalar is shared, not copied, so the destination has to take a
	// reference of its own. MIR wraps the result in `retain`/`drop`, which is a
	// net zero on top of whatever this produces, so the one reference the
	// destination goes on holding has to come from here.
	if fe.emitter.types.IsRefCountedScalar(resolveValueType(fe.emitter.types, dstType)) {
		fe.emitRetainValue(val, dstTy)
	}
	fe.emitValueStore(dstTy, val, ptr, dstAlign)
	return true, nil
}

// operandIsReference reports whether the value this operand emits is an address
// standing in for something else, rather than the thing itself. It asks the
// operand — the address-taking kinds always are one, and any other kind is one
// exactly when its own type is a reference — because the emitted LLVM type
// cannot distinguish the two for a scalar that is carried in a pointer.
func (fe *funcEmitter) operandIsReference(op *mir.Operand) bool {
	if op == nil || fe == nil || fe.emitter == nil {
		return false
	}
	switch op.Kind {
	case mir.OperandAddrOf, mir.OperandAddrOfMut:
		return true
	case mir.OperandConst:
		return false
	}
	typeID := op.Type
	if typeID == types.NoTypeID {
		baseType, err := fe.placeBaseType(op.Place)
		if err != nil {
			return false
		}
		typeID = baseType
	}
	return isRefType(fe.emitter.types, typeID)
}
