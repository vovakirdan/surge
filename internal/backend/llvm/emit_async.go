package llvm

import (
	"fmt"
	"sort"
	"strings"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func isPollFunc(f *mir.Func) bool {
	if f == nil || f.Name == "" {
		return false
	}
	return strings.HasSuffix(f.Name, "$poll")
}

func isBlockingFunc(f *mir.Func) bool {
	if f == nil || f.Name == "" {
		return false
	}
	return strings.HasPrefix(f.Name, "__blocking_block$")
}

func (e *Emitter) emitPollDispatch() error {
	if e == nil || e.mod == nil {
		return nil
	}
	pollIDs := make([]mir.FuncID, 0)
	for _, __id := range e.mod.SortedFuncIDs() {
		id := __id
		f := e.mod.Funcs[__id]
		if isPollFunc(f) {
			pollIDs = append(pollIDs, id)
		}
	}
	sort.Slice(pollIDs, func(i, j int) bool { return pollIDs[i] < pollIDs[j] })

	fmt.Fprintf(&e.buf, "define void @__surge_poll_call(i64 %%id) {\n")
	fmt.Fprintf(&e.buf, "entry:\n")
	e.emitTraceBoundaryMarker()
	fmt.Fprintf(&e.buf, "  switch i64 %%id, label %%poll_default [\n")
	for _, id := range pollIDs {
		fmt.Fprintf(&e.buf, "    i64 %d, label %%poll.%d\n", id, id)
	}
	fmt.Fprintf(&e.buf, "  ]\n")

	for _, id := range pollIDs {
		f := e.mod.Funcs[id]
		if f == nil {
			continue
		}
		name := e.funcNames[id]
		sig, ok := e.funcSigs[id]
		if !ok {
			return fmt.Errorf("missing poll function signature for %s", f.Name)
		}
		if len(sig.params) != 0 {
			return fmt.Errorf("poll function %s must not have parameters", f.Name)
		}
		lowered, err := e.loweredSignature(&sig)
		if err != nil {
			return fmt.Errorf("call contract for %s: %w", f.Name, err)
		}
		fmt.Fprintf(&e.buf, "poll.%d:\n", id)
		switch {
		case lowered.sret:
			// The dispatch discards a poll's result, but the callee still needs
			// somewhere legal to write one. The destination is this frame's,
			// like every other caller-owned destination.
			dst := fmt.Sprintf("%%poll.ret.%d", id)
			fmt.Fprintf(&e.buf, "  %s = alloca %s, align %d\n", dst, lowered.retStorage, lowered.retAlign)
			fmt.Fprintf(&e.buf, "  call void @%s(ptr sret(%s) align %d %s)\n",
				name, lowered.retStorage, lowered.retAlign, dst)
		case lowered.ret == "void":
			fmt.Fprintf(&e.buf, "  call void @%s()\n", name)
		default:
			fmt.Fprintf(&e.buf, "  call %s @%s()\n", lowered.ret, name)
		}
		fmt.Fprintf(&e.buf, "  ret void\n")
	}

	fmt.Fprintf(&e.buf, "poll_default:\n")
	if sc, ok := e.stringConsts["missing poll function"]; ok && sc.globalName != "" {
		fmt.Fprintf(&e.buf, "  call void @rt_panic(ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d)\n", sc.arrayLen, sc.globalName, sc.dataLen)
	}
	fmt.Fprintf(&e.buf, "  unreachable\n")
	fmt.Fprintf(&e.buf, "}\n\n")

	return nil
}
func (fe *funcEmitter) emitInstrAwait(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	val, err := fe.emitTaskHandleOperand(&ins.Await.Task)
	if err != nil {
		return fmt.Errorf("await expects Task pointer: %w", err)
	}
	kindPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8, align %d\n", kindPtr, 1)

	resultType, err := fe.placeBaseType(ins.Await.Dst)
	if err != nil {
		return err
	}
	successIdx, payloadType, err := fe.taskResultInfo(resultType)
	if err != nil {
		return err
	}
	// The awaited value is moved into storage this frame owns, at the payload's
	// own width. The word this replaces could only carry a pointer, so anything
	// wider had to be boxed by the producer and adopted here.
	payloadPtr, payloadStorageTy, err := fe.emitTaskPayloadSlot(payloadType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_task_await(ptr %s, ptr %s, ptr %s)\n",
		val, kindPtr, payloadPtr)
	kindVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", kindVal, kindPtr)
	resultPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr, align %d\n", resultPtr, alignPtr)
	successBB := fe.nextInlineBlock()
	cancelBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()
	isSuccess := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", isSuccess, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isSuccess, successBB, cancelBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", successBB)
	payloadVal, payloadTy, err := fe.emitTaskPayloadValue(payloadType, payloadStorageTy, payloadPtr)
	if err != nil {
		return err
	}
	successPtr, err := fe.emitTagValueSinglePayload(resultType, successIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", successPtr, resultPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cancelBB)
	cancelPtr, err := fe.emitTagValue(resultType, "Cancelled", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", cancelPtr, resultPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	resultVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", resultVal, resultPtr)
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.Await.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, resultVal, ptr, dstAlign)
	return nil
}

func (fe *funcEmitter) emitInstrPoll(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	val, err := fe.emitTaskHandleOperand(&ins.Poll.Task)
	if err != nil {
		return fmt.Errorf("poll expects Task pointer: %w", err)
	}
	resultTypeForSlot, err := fe.placeBaseType(ins.Poll.Dst)
	if err != nil {
		return err
	}
	_, pollPayloadType, err := fe.taskResultInfo(resultTypeForSlot)
	if err != nil {
		return err
	}
	// Sized for the payload rather than for a machine word: see
	// emitTaskPayloadSlot.
	payloadPtr, payloadStorageTy, err := fe.emitTaskPayloadSlot(pollPayloadType)
	if err != nil {
		return err
	}
	kindVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_task_poll(ptr %s, ptr %s)\n",
		kindVal, val, payloadPtr)
	pendingCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 0\n", pendingCond, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb.inline.poll_done%d\n", pendingCond, ins.Poll.PendBB, fe.inlineBlock)

	doneBB := fmt.Sprintf("bb.inline.poll_done%d", fe.inlineBlock)
	fe.inlineBlock++
	successBB := fe.nextInlineBlock()
	cancelBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", doneBB)
	successCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", successCond, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", successCond, successBB, cancelBB)

	resultType, err := fe.placeBaseType(ins.Poll.Dst)
	if err != nil {
		return err
	}
	successIdx, payloadType, err := fe.taskResultInfo(resultType)
	if err != nil {
		return err
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", successBB)
	payloadVal, payloadTy, err := fe.emitTaskPayloadValue(payloadType, payloadStorageTy, payloadPtr)
	if err != nil {
		return err
	}
	successPtr, err := fe.emitTagValueSinglePayload(resultType, successIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.Poll.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, successPtr, ptr, dstAlign)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Poll.ReadyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cancelBB)
	cancelPtr, err := fe.emitTagValue(resultType, "Cancelled", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	ptr, dstTy, err = fe.emitPlacePtr(ins.Poll.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, cancelPtr, ptr, dstAlign)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Poll.ReadyBB)

	fe.blockTerminated = true
	return nil
}
func (fe *funcEmitter) emitInstrTimeout(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	val, err := fe.emitTaskHandleOperand(&ins.Timeout.Task)
	if err != nil {
		return fmt.Errorf("timeout expects Task pointer: %w", err)
	}
	ms64, err := fe.emitUintOperandToI64(&ins.Timeout.Ms, "timeout duration out of range")
	if err != nil {
		return err
	}
	timeoutResultType, err := fe.placeBaseType(ins.Timeout.Dst)
	if err != nil {
		return err
	}
	_, timeoutPayloadType, err := fe.taskResultInfo(timeoutResultType)
	if err != nil {
		return err
	}
	// Sized for the payload rather than for a machine word: see
	// emitTaskPayloadSlot. A timeout poll takes the SAME result out of the
	// SAME slot an await does, so it cannot be the one surface that still
	// asks for a word.
	payloadPtr, payloadStorageTy, err := fe.emitTaskPayloadSlot(timeoutPayloadType)
	if err != nil {
		return err
	}
	kindVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_timeout_poll(ptr %s, i64 %s, ptr %s)\n", kindVal, val, ms64, payloadPtr)
	pendingCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 0\n", pendingCond, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb.inline.timeout_done%d\n", pendingCond, ins.Timeout.PendBB, fe.inlineBlock)

	doneBB := fmt.Sprintf("bb.inline.timeout_done%d", fe.inlineBlock)
	fe.inlineBlock++
	successBB := fe.nextInlineBlock()
	cancelBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", doneBB)
	successCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", successCond, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", successCond, successBB, cancelBB)

	resultType := timeoutResultType
	successIdx, payloadType, err := fe.taskResultInfo(resultType)
	if err != nil {
		return err
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", successBB)
	payloadVal, payloadTy, err := fe.emitTaskPayloadValue(payloadType, payloadStorageTy, payloadPtr)
	if err != nil {
		return err
	}
	successPtr, err := fe.emitTagValueSinglePayload(resultType, successIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.Timeout.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, successPtr, ptr, dstAlign)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Timeout.ReadyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cancelBB)
	cancelPtr, err := fe.emitTagValue(resultType, "Cancelled", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	ptr, dstTy, err = fe.emitPlacePtr(ins.Timeout.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, cancelPtr, ptr, dstAlign)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Timeout.ReadyBB)

	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitInstrSelect(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	armCount := len(ins.Select.Arms)
	if armCount == 0 {
		return fmt.Errorf("select expects at least one arm")
	}
	defaultIndex := int64(-1)
	for i := range ins.Select.Arms {
		arm := &ins.Select.Arms[i]
		switch arm.Kind {
		case mir.SelectArmDefault:
			if defaultIndex >= 0 {
				return fmt.Errorf("select has multiple default arms")
			}
			defaultIndex = int64(i)
		case mir.SelectArmTask, mir.SelectArmChanRecv, mir.SelectArmChanSend, mir.SelectArmTimeout:
			// handled below
		default:
			return fmt.Errorf("unsupported select arm kind %v", arm.Kind)
		}
	}

	kindsPtr := fe.nextTemp()
	handlesPtr := fe.nextTemp()
	valuesPtr := fe.nextTemp()
	msPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i8], align %d\n", kindsPtr, armCount, 1)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x ptr], align %d\n", handlesPtr, armCount, alignPtr)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i64], align %d\n", valuesPtr, armCount, alignWord)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i64], align %d\n", msPtr, armCount, alignWord)
	for i := range ins.Select.Arms {
		arm := &ins.Select.Arms[i]
		kindPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 %d\n", kindPtr, armCount, kindsPtr, i)
		fmt.Fprintf(&fe.emitter.buf, "  store i8 %d, ptr %s\n", arm.Kind, kindPtr)

		handlePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 %d\n", handlePtr, armCount, handlesPtr, i)
		switch arm.Kind {
		case mir.SelectArmTask, mir.SelectArmTimeout:
			val, err := fe.emitTaskHandleOperand(&arm.Task)
			if err != nil {
				return fmt.Errorf("select expects Task pointer: %w", err)
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", val, handlePtr)
		case mir.SelectArmChanRecv, mir.SelectArmChanSend:
			chVal, err := fe.emitChannelHandle(&arm.Channel)
			if err != nil {
				return err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", chVal, handlePtr)
		default:
			fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", handlePtr)
		}

		// The arm table carries ADDRESSES now, not words: a send arm's value
		// stays where the caller has it and the runtime moves from there.
		valuePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 %d\n", valuePtr, armCount, valuesPtr, i)
		if arm.Kind == mir.SelectArmChanSend {
			srcPtr, err := fe.emitChannelValueAddress(&arm.Value)
			if err != nil {
				return err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", srcPtr, valuePtr)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", valuePtr)
		}

		msElemPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 %d\n", msElemPtr, armCount, msPtr, i)
		if arm.Kind == mir.SelectArmTimeout {
			ms64, err := fe.emitUintOperandToI64(&arm.Ms, "timeout duration out of range")
			if err != nil {
				return err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", ms64, msElemPtr)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", msElemPtr)
		}
	}

	kindsBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0\n", kindsBase, armCount, kindsPtr)
	handlesBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 0\n", handlesBase, armCount, handlesPtr)
	valuesBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 0\n", valuesBase, armCount, valuesPtr)
	msBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 0\n", msBase, armCount, msPtr)
	idxVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_select_poll(i64 %d, ptr %s, ptr %s, ptr %s, ptr %s, i64 %d)\n",
		idxVal, armCount, kindsBase, handlesBase, valuesBase, msBase, defaultIndex)
	pendingCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", pendingCond, idxVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb.inline.select_ready%d\n", pendingCond, ins.Select.PendBB, fe.inlineBlock)

	readyBB := fmt.Sprintf("bb.inline.select_ready%d", fe.inlineBlock)
	fe.inlineBlock++
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
	if err := fe.emitSelectWinnerIndex(idxVal, ins.Select.Dst); err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Select.ReadyBB)

	fe.blockTerminated = true
	return nil
}

// abandonedStateDropID resolves the drop-fn id the runtime passes to
// __surge_drop_abandoned_state_call if a cancellation abandons this
// suspend-point state box without ever resuming compiled code to free it.
// The state is always a heap-boxed struct (buildAsyncPendingBlocks packs
// live locals into one every time), so — unlike a task result, which may be
// inert Copy bits with no box at all — there is always something to
// register here.
func (fe *funcEmitter) abandonedStateDropID(state *mir.Operand) (types.TypeID, error) {
	stateType, err := fe.suspensionFrameTypeOf(state)
	if err != nil {
		return types.NoTypeID, err
	}
	return fe.emitter.registerAbandonedStateDrop(stateType), nil
}

// suspensionFrameTypeOf names the frame a suspension terminator is holding,
// for the terminators that reclaim it themselves rather than registering it.
func (fe *funcEmitter) suspensionFrameTypeOf(state *mir.Operand) (types.TypeID, error) {
	stateType, err := fe.placeBaseType(state.Place)
	if err != nil || stateType == types.NoTypeID {
		return types.NoTypeID, fmt.Errorf("async state missing type info")
	}
	return stateType, nil
}

func (fe *funcEmitter) emitTermAsyncYield(term *mir.Terminator) error {
	if term == nil {
		return nil
	}
	stateVal, stateTy, err := fe.emitValueOperand(&term.AsyncYield.State)
	if err != nil {
		return err
	}
	if stateTy != "ptr" {
		return fmt.Errorf("async_yield expects state pointer, got %s", stateTy)
	}
	dropID, err := fe.abandonedStateDropID(&term.AsyncYield.State)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_async_yield(ptr %s, i64 %d)\n", stateVal, dropID)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}

// widenAsyncReturnBareMember materialises a bare union member into the async
// body's declared result union.
//
// It answers false for everything else, including the ordinary case where the
// value already IS the union, so the caller can use it unconditionally.
func (fe *funcEmitter) emitTermAsyncReturnCancelled(term *mir.Terminator) error {
	if term == nil {
		return nil
	}
	stateVal, stateTy, err := fe.emitValueOperand(&term.AsyncReturnCancelled.State)
	if err != nil {
		return err
	}
	if stateTy != "ptr" {
		return fmt.Errorf("async_cancel expects state pointer, got %s", stateTy)
	}
	frameType, err := fe.suspensionFrameTypeOf(&term.AsyncReturnCancelled.State)
	if err != nil {
		return err
	}
	// This frame's payload was read out at the top of the poll and the locals
	// that took those bytes have been released since, so what sits here is a
	// duplicate of values that are already gone: give back the storage and
	// walk nothing. Handing it to the runtime's abandoned-frame release
	// instead would free them a second time, because that release is written
	// for the frame a yield packs and abandons, which owns what it holds.
	//
	// Zero says there is nothing left to reclaim. Nothing reads the state
	// pointer after a cancelled outcome — the runtime's cancelled arm goes
	// straight to completion — so passing the freed pointer alongside it
	// keeps the call shape without anyone dereferencing it.
	fmt.Fprintf(&fe.emitter.buf, "  call void @%s(ptr %s)\n",
		fe.emitter.requireSuspensionFrameRelease(frameType), stateVal)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_async_return_cancelled(ptr %s, i64 0)\n", stateVal)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}
