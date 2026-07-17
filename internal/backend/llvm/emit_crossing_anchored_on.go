package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/sema"
	"surge/internal/symbols"
)

// emitAnchoredOnCrossing lowers `on ch { ... }` to the anchored immediate
// execute/reply runtime call: the destination is the far channel token (the
// runtime routes to the anchor's owner shard and pins the registry entry
// before the body exists), one request, one reply, suspend on the reply,
// and `TaskResult<T>` materialization from the reply's result kind/bits.
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
	bitsPtr := fe.nextTemp()
	statusSlot := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8\n", kindPtr)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", bitsPtr)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i32\n", statusSlot)

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
	stateDropID := mir.FuncID(0)
	if len(ins.State.Fields) > 0 {
		var stateTy string
		stateVal, stateTy, err = fe.emitStructLit(&ins.State)
		if err != nil {
			return err
		}
		if stateTy != "ptr" {
			return fmt.Errorf("anchored on crossing state must lower as ptr, got %s", stateTy)
		}
		if regErr := fe.emitter.registerCrossingDropState(ins.BodyFuncID, ins.State.TypeID); regErr != nil {
			return regErr
		}
		stateDropID = ins.BodyFuncID
	}
	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_immediate_on_execute_anchored(ptr %s, i64 %d, i64 %d, ptr %s, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		anchorVal,
		stateDropID,
		ins.BodyFuncID,
		stateVal,
		pendingPtr,
		kindPtr,
		bitsPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", initStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", retryBB)
	retryStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_immediate_on_execute_anchored(ptr null, i64 0, i64 %d, ptr null, ptr %s, ptr %s, ptr %s)\n",
		retryStatus,
		ins.BodyFuncID,
		pendingPtr,
		kindPtr,
		bitsPtr)
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

	if err := fe.emitFarTaskLifecycleResult(ins, resultBB, kindPtr, bitsPtr); err != nil {
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

// emitAnchoredChanRecv materializes `Option<T>` from the anchored receive
// helper. The parked case never reaches this code (the helper yields and the
// body re-enters from the top), so the emit is straight-line: 1 delivers a
// value, anything else is the closed outcome.
func (fe *funcEmitter) emitAnchoredChanRecv(ins *mir.Instr) error {
	bitsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", bitsPtr)
	statusVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_anchored_channel_recv(ptr %s)\n", statusVal, bitsPtr)

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

	valueBB := fe.nextInlineBlock()
	closedBB := fe.nextInlineBlock()
	joinBB := fe.nextInlineBlock()
	hasValue := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", hasValue, statusVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", hasValue, valueBB, closedBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", valueBB)
	bitsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)
	payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
	if err != nil {
		return err
	}
	somePtr, err := fe.emitTagValueSinglePayload(dstType, someIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	dstPtr, dstTy, err := fe.emitPlacePtr(ins.ChanRecv.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, somePtr, dstPtr)
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
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, nonePtr, dstPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", joinBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", joinBB)
	return nil
}
