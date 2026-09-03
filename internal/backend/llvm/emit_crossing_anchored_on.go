package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// emitAnchoredOnCrossing lowers `on ch { ... }` to the anchored immediate
// execute/reply runtime call: the destination is the far channel token (the
// runtime routes to the anchor's owner shard and pins the registry entry
// before the body exists), one request, one reply, suspend on the reply,
// and `TaskResult<T>` materialization from the reply's result kind and the
// typed result slot the reply names.
func (fe *funcEmitter) emitAnchoredOnCrossing(ins *mir.CrossingInstr) error {
	if ins == nil {
		return nil
	}
	if ins.BodyFuncID == mir.NoFuncID {
		return fmt.Errorf("anchored on crossing missing destination poll function")
	}
	if ins.ReadyBB == mir.NoBlockID || ins.PendBB == mir.NoBlockID {
		return fmt.Errorf("anchored on crossing must be lowered inside an async suspend context")
	}
	if ins.Destination.Kind != sema.CrossingDestinationFarHandle {
		return fmt.Errorf("anchored on crossing expects a far-handle destination")
	}
	pendingPtr, pendingTy, err := fe.emitPlacePtr(ins.Pending)
	if err != nil {
		return err
	}
	if pendingTy != "ptr" {
		return fmt.Errorf("anchored on crossing pending slot must lower as ptr, got %s", pendingTy)
	}

	kindPtr := fe.nextTemp()
	statusSlot := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8, align %d\n", kindPtr, 1)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i32, align %d\n", statusSlot, 4)
	// Storage for the crossing's result, at the payload's own width. The
	// runtime reads it only on the terminal call; see emitTaskPayloadSlot.
	crossingResultType := ins.ResultType
	if crossingResultType == types.NoTypeID {
		crossingResultType, err = fe.placeBaseType(ins.Dst)
		if err != nil {
			return err
		}
	}
	_, crossingPayloadType, err := fe.taskResultInfo(crossingResultType)
	if err != nil {
		return err
	}
	payloadPtr, payloadStorageTy, err := fe.emitTaskPayloadSlot(crossingPayloadType)
	if err != nil {
		return err
	}
	// The body's result type as a number, for the destination shard to turn
	// back into a descriptor: a crossing carries no pointers.
	resultTypeID, err := fe.crossingResultTypeID(crossingPayloadType)
	if err != nil {
		return err
	}

	pendingVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", pendingVal, pendingPtr)
	isRetry := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne ptr %s, null\n", isRetry, pendingVal)

	initBB := fe.nextInlineBlock()
	retryBB := fe.nextInlineBlock()
	statusBB := fe.nextInlineBlock()
	pendingBB := fe.nextInlineBlock()
	doneBB := fe.nextInlineBlock()
	errBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isRetry, retryBB, initBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", initBB)
	anchorVal, anchorTy, err := fe.emitValueOperand(&ins.Destination.Value)
	if err != nil {
		return err
	}
	if anchorTy != "ptr" {
		return fmt.Errorf("anchored on crossing anchor must lower as ptr, got %s", anchorTy)
	}
	stateVal := "null"
	stateTypeID := types.TypeID(0)
	if len(ins.State.Fields) > 0 {
		var stateTy string
		stateVal, stateTy, err = fe.emitStructLit(&ins.State)
		if err != nil {
			return err
		}
		if stateTy != "ptr" {
			return fmt.Errorf("anchored on crossing state must lower as ptr, got %s", stateTy)
		}
		stateType, stateErr := fe.crossingStateTypeID(ins.State.TypeID)
		if stateErr != nil {
			return stateErr
		}
		stateTypeID = stateType
	}
	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_immediate_on_execute_anchored(ptr %s, i64 %d, i64 %d, i64 %d, ptr %s, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		anchorVal,
		stateTypeID,
		resultTypeID,
		ins.BodyFuncID,
		stateVal,
		pendingPtr,
		kindPtr,
		payloadPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", initStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", retryBB)
	retryStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_immediate_on_execute_anchored(ptr null, i64 0, i64 0, i64 %d, ptr null, ptr %s, ptr %s, ptr %s)\n",
		retryStatus,
		ins.BodyFuncID,
		pendingPtr,
		kindPtr,
		payloadPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", retryStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", statusBB)
	statusVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i32, ptr %s\n", statusVal, statusSlot)
	isPending := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i32 %s, %d\n", isPending, statusVal, rtRemoteTaskPending)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isPending, pendingBB, doneBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", pendingBB)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.PendBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", doneBB)
	isOK := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i32 %s, %d\n", isOK, statusVal, rtRemoteTaskOK)
	resultBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isOK, resultBB, errBB)

	if err := fe.emitFarTaskLifecycleResult(ins, resultBB, kindPtr, payloadPtr, payloadStorageTy); err != nil {
		return err
	}
	if err := fe.emitAnchoredOnErrorBlocks(errBB, statusVal); err != nil {
		return err
	}
	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitAnchoredOnErrorBlocks(errBB, statusVal string) error {
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", errBB)
	invalidBB := fe.nextInlineBlock()
	shutdownBB := fe.nextInlineBlock()
	queueBB := fe.nextInlineBlock()
	refusedBB := fe.nextInlineBlock()
	staleBB := fe.nextInlineBlock()
	defaultBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%%s [\n", statusVal, defaultBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskInvalidArgument, invalidBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskDestinationShutdown, shutdownBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskQueueFull, queueBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskRefused, refusedBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskStaleToken, staleBB)
	fmt.Fprintf(&fe.emitter.buf, "  ]\n")
	if err := fe.emitPanicBlock(invalidBB, "anchored on requires an async task context and a channel anchor"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(shutdownBB, "anchored on destination shard is shut down"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(queueBB, "anchored on transport queue is full"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(refusedBB, "anchored on execute request was refused"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(staleBB, "anchored channel is gone: the far channel was released before the block could run"); err != nil {
		return err
	}
	return fe.emitPanicBlock(defaultBB, "anchored on execute request failed")
}

// emitAnchoredChannelSend publishes the sent value into an allocation the
// transport owns before handing it over.
//
// The anchored send is the one storing runtime call that reaches the backend as
// an ordinary named call rather than as an instruction of its own, so nothing
// downstream would otherwise widen its argument. The channel queues what it is
// given and the value outlives this frame, so passing the operand as emitted
// would hand the runtime an address in storage this frame owns — and the
// matching receive adopts, which means it would free that address. This is the
// copy-in leg of the same transport pair `emitAnchoredChanRecv` completes.
func (fe *funcEmitter) emitAnchoredChannelSend(call *mir.CallInstr) (bool, error) {
	if call == nil || fe == nil {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	if stripGenericSuffix(name) != "rt_anchored_channel_send" {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("rt_anchored_channel_send expects 1 argument, got %d", len(call.Args))
	}
	srcPtr, err := fe.emitChannelValueAddress(&call.Args[0])
	if err != nil {
		return true, err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_anchored_channel_send(ptr %s)\n", srcPtr)
	return true, nil
}

// emitAnchoredChanRecv materializes `Option<T>` from the anchored receive
// helper. The parked case never reaches this code (the helper yields and the
// body re-enters from the top), so the emit is straight-line: 1 delivers a
// value, anything else is the closed outcome.
func (fe *funcEmitter) emitAnchoredChanRecv(ins *mir.Instr) error {
	// The element type comes from the DESTINATION here, not from the channel
	// operand: an anchored receive has no channel operand at all -- the channel
	// comes from the binding the anchored body was entered with -- so the only
	// place the element is named is the Option this receive fills.
	dstType, err := fe.placeBaseType(ins.ChanRecv.Dst)
	if err != nil {
		return err
	}
	someIdx, someMeta, err := fe.emitter.tagCaseMeta(dstType, "Some", symbols.NoSymbolID)
	if err != nil {
		return err
	}
	if len(someMeta.PayloadTypes) != 1 {
		return fmt.Errorf("Option::Some expects single payload")
	}
	payloadType := someMeta.PayloadTypes[0]

	payloadPtr, payloadStorageTy, err := fe.emitChannelPayloadSlot(payloadType)
	if err != nil {
		return err
	}
	statusVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_anchored_channel_recv(ptr %s)\n", statusVal, payloadPtr)

	valueBB := fe.nextInlineBlock()
	closedBB := fe.nextInlineBlock()
	joinBB := fe.nextInlineBlock()
	hasValue := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", hasValue, statusVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", hasValue, valueBB, closedBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", valueBB)
	payloadVal, payloadTy, err := fe.emitChannelPayloadValue(payloadType, payloadStorageTy, payloadPtr)
	if err != nil {
		return err
	}
	somePtr, err := fe.emitTagValueSinglePayload(dstType, someIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	dstPtr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.ChanRecv.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, somePtr, dstPtr, dstAlign)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", joinBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", closedBB)
	nonePtr, err := fe.emitTagValue(dstType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	dstPtr, dstTy, err = fe.emitPlacePtr(ins.ChanRecv.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, nonePtr, dstPtr, dstAlign)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", joinBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", joinBB)
	return nil
}
