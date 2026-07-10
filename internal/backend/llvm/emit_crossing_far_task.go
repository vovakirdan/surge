package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
)

func (fe *funcEmitter) emitFarTaskLifecycleCrossing(ins *mir.CrossingInstr, method, runtimeFn string) error {
	if ins == nil {
		return nil
	}
	if ins.ReadyBB == mir.NoBlockID || ins.PendBB == mir.NoBlockID {
		return fmt.Errorf("far Task.%s must be lowered inside an async suspend context", method)
	}
	pendingPtr, pendingTy, err := fe.emitPlacePtr(ins.Pending)
	if err != nil {
		return err
	}
	if pendingTy != "ptr" {
		return fmt.Errorf("far Task.%s pending slot must lower as ptr, got %s", method, pendingTy)
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
	receiverVal, receiverTy, err := fe.emitValueOperand(&ins.Receiver)
	if err != nil {
		return err
	}
	if receiverTy != "ptr" {
		return fmt.Errorf("far Task.%s receiver must lower as ptr, got %s", method, receiverTy)
	}
	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @%s(ptr %s, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		runtimeFn,
		receiverVal,
		pendingPtr,
		kindPtr,
		bitsPtr)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_far_task_handle_free(ptr %s)\n", receiverVal)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", initStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", retryBB)
	retryStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @%s(ptr null, ptr %s, ptr %s, ptr %s)\n",
		retryStatus,
		runtimeFn,
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
	if err := fe.emitFarTaskLifecycleErrorBlocks(method, errBB, statusVal); err != nil {
		return err
	}
	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitFarTaskLifecycleResult(ins *mir.CrossingInstr, resultBB, kindPtr, bitsPtr string) error {
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", resultBB)
	kindVal := fe.nextTemp()
	bitsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", kindVal, kindPtr)
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)

	resultType := ins.ResultType
	if resultType == 0 {
		var err error
		resultType, err = fe.placeBaseType(ins.Dst)
		if err != nil {
			return err
		}
	}
	successIdx, payloadType, err := fe.taskResultInfo(resultType)
	if err != nil {
		return err
	}
	resultSlot := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", resultSlot)
	successBB := fe.nextInlineBlock()
	cancelBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()
	isSuccess := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", isSuccess, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isSuccess, successBB, cancelBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", successBB)
	payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
	if err != nil {
		return err
	}
	successPtr, err := fe.emitTagValueSinglePayload(resultType, successIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", successPtr, resultSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cancelBB)
	cancelPtr, err := fe.emitTagValue(resultType, "Cancelled", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", cancelPtr, resultSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	resultVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", resultVal, resultSlot)
	dstPtr, dstTy, err := fe.emitPlacePtr(ins.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, resultVal, dstPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.ReadyBB)
	return nil
}

func (fe *funcEmitter) emitFarTaskLifecycleErrorBlocks(method, errBB, statusVal string) error {
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", errBB)
	invalidBB := fe.nextInlineBlock()
	shutdownBB := fe.nextInlineBlock()
	queueBB := fe.nextInlineBlock()
	refusedBB := fe.nextInlineBlock()
	staleBB := fe.nextInlineBlock()
	consumedBB := fe.nextInlineBlock()
	defaultBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%%s [\n", statusVal, defaultBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskInvalidArgument, invalidBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskDestinationShutdown, shutdownBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskQueueFull, queueBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskRefused, refusedBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskStaleToken, staleBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskConsumed, consumedBB)
	fmt.Fprintf(&fe.emitter.buf, "  ]\n")
	if err := fe.emitPanicBlock(invalidBB, fmt.Sprintf("far Task.%s requires an async task context", method)); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(shutdownBB, fmt.Sprintf("far Task.%s owner shard is shut down", method)); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(queueBB, fmt.Sprintf("far Task.%s transport queue is full", method)); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(refusedBB, fmt.Sprintf("far Task.%s remote task request was refused", method)); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(staleBB, fmt.Sprintf("far Task.%s handle is stale", method)); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(consumedBB, fmt.Sprintf("far Task.%s handle was already consumed", method)); err != nil {
		return err
	}
	return fe.emitPanicBlock(defaultBB, fmt.Sprintf("far Task.%s remote task request failed", method))
}
