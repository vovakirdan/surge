package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// A task's result is storage the task owns, described by one descriptor, and
// these are the two things a call site needs to say so: which descriptor the
// task is created with, and where the value it produces already lives.
//
// This is what replaces emitValueToI64 / emitI64ToValue on the task paths. Those
// existed because the runtime carried a result through a machine word, so a
// composite had to be boxed on the way in and adopted on the way out. The
// runtime carries storage now, and the box goes with it.

// taskResultOpsOperand renders the descriptor operand for a task whose result
// is `resultType`.
//
// A type with no bytes -- a task that produces no value -- answers `ptr null`,
// which is a shape and not an omission: the task's slot stays empty and
// rt_async_return refuses to publish into it. Any other type must have a
// descriptor, and the module refuses rather than falling back to carrying the
// value as an opaque word, which would lose the drop and the clone the type
// actually has (owner ruling 2026-08-25).
func (fe *funcEmitter) taskResultOpsOperand(resultType types.TypeID) (string, error) {
	if resultType == types.NoTypeID {
		return "ptr null", nil
	}
	llvmTy, err := fe.emitter.llvmType(resultType)
	if err == nil && llvmTy == "void" {
		return "ptr null", nil
	}
	if !fe.emitter.valueOpsRegistryHas(resultType) {
		return "", fmt.Errorf(
			"llvm: a task result of type#%d has no operation descriptor, so the task could not "+
				"be created with one; note: every registry type gets a descriptor, so this type "+
				"never reached the operation census as a value",
			resultType)
	}
	return "ptr @" + valueOpsSymbol(resultType), nil
}

// taskResultType names what a Task<T> handle's T is.
func (fe *funcEmitter) taskResultType(taskType types.TypeID) (types.TypeID, error) {
	if fe.emitter == nil || fe.emitter.types == nil || taskType == types.NoTypeID {
		return types.NoTypeID, nil
	}
	return taskPayloadType(fe.emitter.types, taskType)
}

// emitTaskCreateIntrinsic adds the result descriptor to __task_create, and to
// __task_create_affine, the same constructor for a task that borrows its
// creator's frame (the lowering chose between them; see
// mir.asyncTaskConstructorName).
//
// The lowering builds the call with the two arguments it can name from MIR --
// the poll id and the state box -- and the descriptor is the emitter's to add,
// because it is the emitter that knows the symbol a type's descriptor became.
func (fe *funcEmitter) emitTaskCreateIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeValue {
		return false, nil
	}
	ctor := stripGenericSuffix(call.Callee.Name)
	if ctor != "__task_create" && ctor != "__task_create_affine" {
		return false, nil
	}
	if len(call.Args) != 2 {
		return true, fmt.Errorf("%s expects 2 lowered arguments, got %d", ctor, len(call.Args))
	}
	pollID, _, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return true, err
	}
	state, _, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return true, err
	}
	taskType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return true, err
	}
	resultType, err := fe.taskResultType(taskType)
	if err != nil {
		return true, err
	}
	operand, err := fe.taskResultOpsOperand(resultType)
	if err != nil {
		return true, err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(i64 %s, ptr %s, %s)\n",
		tmp, ctor, pollID, state, operand)
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return true, err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
	return true, nil
}

// emitTaskPayloadSlot reserves storage for the value an await or a poll is about
// to receive, sized and aligned for the payload itself.
//
// It is the channel's emitChannelPayloadSlot in every respect, which is the
// point: a task result and a channel element are the same question -- where does
// a value the runtime is about to hand me live -- and they now have the same
// answer.
func (fe *funcEmitter) emitTaskPayloadSlot(payloadType types.TypeID) (ptr, storageTy string, err error) {
	return fe.emitChannelPayloadSlot(payloadType)
}

// emitTaskPayloadValue reads back what an await wrote into the slot, in whatever
// form the rest of the emitter expects to hand on.
func (fe *funcEmitter) emitTaskPayloadValue(
	payloadType types.TypeID,
	storageTy, ptr string,
) (val, opTy string, err error) {
	return fe.emitChannelPayloadValue(payloadType, storageTy, ptr)
}

// crossingResultTypeID names the type of a body's result for a crossing to
// carry, in the only form that survives one: a number.
//
// A body that produces no value answers 0, which the destination resolves to
// the opaque machine word -- an empty slot describing a word, which a body that
// publishes nothing never fills. Any other type must have a descriptor, and the
// module refuses rather than letting the destination fall back to a word for a
// value that owns something (owner ruling 2026-08-25).
func (fe *funcEmitter) crossingResultTypeID(payloadType types.TypeID) (types.TypeID, error) {
	if payloadType == types.NoTypeID {
		return types.TypeID(0), nil
	}
	if llvmTy, err := fe.emitter.llvmType(payloadType); err == nil && llvmTy == "void" {
		return types.TypeID(0), nil
	}
	if !fe.emitter.valueOpsRegistryHas(payloadType) {
		return types.TypeID(0), fmt.Errorf(
			"llvm: a crossing body's result of type#%d has no operation descriptor, so the "+
				"destination shard could not be told what it will hold; note: every registry "+
				"type gets a descriptor, so this type never reached the operation census as a "+
				"value",
			payloadType)
	}
	return payloadType, nil
}

// asyncReturnWidensBareMember answers the same three questions
// widenAsyncReturnBareMember does before it materialises anything: the body's
// result is a union, this value is not already that union, and it is one of the
// union's members.
func (fe *funcEmitter) asyncReturnWidensBareMember(valueType types.TypeID) bool {
	if fe == nil || fe.f == nil || valueType == types.NoTypeID {
		return false
	}
	result := resolveValueType(fe.emitter.types, fe.f.Result)
	if result == types.NoTypeID || !isUnionType(fe.emitter.types, result) {
		return false
	}
	if resolveValueType(fe.emitter.types, valueType) == result {
		return false
	}
	_, ok := fe.emitter.unionMemberFor(result, valueType)
	return ok
}

func (fe *funcEmitter) widenAsyncReturnBareMember(val, valTy string, valueType types.TypeID) (storage, storageLLVM string, unionType types.TypeID, widened bool, err error) {
	if fe == nil || fe.f == nil || valueType == types.NoTypeID {
		return "", "", types.NoTypeID, false, nil
	}
	result := resolveValueType(fe.emitter.types, fe.f.Result)
	if result == types.NoTypeID || !isUnionType(fe.emitter.types, result) {
		return "", "", types.NoTypeID, false, nil
	}
	if resolveValueType(fe.emitter.types, valueType) == result {
		return "", "", types.NoTypeID, false, nil
	}
	if _, ok := fe.emitter.unionMemberFor(result, valueType); !ok {
		return "", "", types.NoTypeID, false, nil
	}
	mem, err := fe.emitValueStorage(result)
	if err != nil {
		return "", "", types.NoTypeID, false, err
	}
	facts, err := fe.emitter.layoutOf(result)
	if err != nil {
		return "", "", types.NoTypeID, false, err
	}
	align := facts.Align
	if align == 0 {
		align = 1
	}
	materialised, err := fe.emitUnionMaterialiseBareMember(mem, align, result, val, valTy, valueType)
	if err != nil {
		return "", "", types.NoTypeID, false, err
	}
	if !materialised {
		return "", "", types.NoTypeID, false, nil
	}
	resultLLVM, err := fe.emitter.llvmType(result)
	if err != nil {
		return "", "", types.NoTypeID, false, err
	}
	return mem, resultLLVM, result, true, nil
}

// The producing end: a task's own return moves its value into the slot the
// task was created with, which is where every asker later reads it from.
func (fe *funcEmitter) emitTermAsyncReturn(term *mir.Terminator) error {
	if term == nil {
		return nil
	}
	stateVal, stateTy, err := fe.emitValueOperand(&term.AsyncReturn.State)
	if err != nil {
		return err
	}
	// The frame goes back HERE, on the ordinary return, and it says SPENT: the
	// captures were unpacked into locals at entry and the drops that reclaim
	// them ran before this terminator, so the release hands the storage
	// straight to the allocator without walking a member.
	//
	// This used to be the free builtin the lowering appended, and when the
	// frame gained a word that answers for itself the builtin went with it --
	// but the reader of the word was wired only to the cancelled and abandoned
	// paths, and nothing gave a SUCCESSFUL body's frame back at all. The
	// runtime cannot do it in `rt_async_return`: that entry point never learns
	// the frame's type, and `mark_done` only releases what a yield stashed.
	frameType, frameErr := fe.suspensionFrameTypeOf(&term.AsyncReturn.State)
	if frameErr != nil {
		return frameErr
	}
	if frameType != types.NoTypeID {
		ops, opsErr := fe.emitter.frameOpsSymbol(frameType)
		if opsErr != nil {
			return opsErr
		}
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_frame_release(ptr @%s, ptr %s)\n", ops, stateVal)
	}
	if stateTy != "ptr" {
		return fmt.Errorf("async_return expects state pointer, got %s", stateTy)
	}
	// The runtime takes the ADDRESS of the value and moves it into the task's
	// own result slot. What used to happen here instead was a conversion to a
	// machine word, which meant allocating a box for anything wider than one --
	// the representation this step exists to remove.
	sourceVal := "null"
	if term.AsyncReturn.HasValue {
		valueType := operandValueType(fe.emitter.types, &term.AsyncReturn.Value)
		if valueType == types.NoTypeID && term.AsyncReturn.Value.Kind != mir.OperandConst {
			if baseType, baseErr := fe.placeBaseType(term.AsyncReturn.Value.Place); baseErr == nil {
				valueType = baseType
			}
		}
		// The widening path needs the VALUE and the ordinary path needs its
		// ADDRESS, and asking for both materialises a constant twice -- which
		// for a string is two allocations where the program wrote one, and the
		// first is leaked. So the question is decided before either is asked.
		if !fe.asyncReturnWidensBareMember(valueType) {
			// A far Task handle returned from an async body hands its lease
			// back before the value moves: the hook is about the HANDLE, not
			// about how the value travels, so it survives the flip unchanged.
			if mir.IsDirectFarTaskType(fe.emitter.types, valueType) {
				handle, handleTy, handleErr := fe.emitValueOperand(&term.AsyncReturn.Value)
				if handleErr != nil {
					return handleErr
				}
				if handleTy != "ptr" {
					return fmt.Errorf("far Task async return must lower as ptr, got %s", handleTy)
				}
				fmt.Fprintf(&fe.emitter.buf,
					"  call void @rt_far_task_prepare_return(ptr %s)\n", handle)
			}
			// The value is moved out of where it already lives; giving it a
			// second home first would give one obligation two owners.
			ptr, _, storageErr := fe.emitOperandStorage(&term.AsyncReturn.Value)
			if storageErr != nil {
				return storageErr
			}
			fmt.Fprintf(&fe.emitter.buf, "  call void @rt_async_return(ptr %s, ptr %s)\n",
				stateVal, ptr)
			fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
			return nil
		}
		val, valTy, err := fe.emitValueOperand(&term.AsyncReturn.Value)
		if err != nil {
			return err
		}
		// A bare member returned from an async body is still a member of the
		// result union, and it has to be widened HERE. The sibling arm that
		// returns a tag arrives as a cast to the union; the bare arm arrives
		// as the member itself, so without this the reader is handed a member
		// where it expects a union and reads the payload at the wrong offset.
		// Widening already materialises the union into storage, so what comes
		// back is the address the move needs.
		widened, _, _, ok, wErr := fe.widenAsyncReturnBareMember(val, valTy, valueType)
		if wErr != nil {
			return wErr
		}
		if !ok {
			return fmt.Errorf(
				"async_return: type#%d reads as a bare union member and did not widen", valueType)
		}
		sourceVal = widened
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_async_return(ptr %s, ptr %s)\n", stateVal, sourceVal)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}
