package llvm

import (
	"fmt"

	"surge/internal/mir"
)

// emitChannelShareCrossing lowers `ch.share()`: allocate the caller-side
// sibling token, send one share request to the source token's owner shard,
// suspend on the single reply, and leave the filled sibling pointer in the
// destination place. The mint returns the handle directly (not a
// TaskResult), so every failure status maps to a deterministic panic — the
// stale case names the released source lease.
func (fe *funcEmitter) emitChannelShareCrossing(ins *mir.CrossingInstr) error {
	if ins == nil {
		return nil
	}
	if ins.ReadyBB == mir.NoBlockID || ins.PendBB == mir.NoBlockID {
		return fmt.Errorf("channel_share must be lowered inside an async suspend context")
	}
	pendingPtr, pendingTy, err := fe.emitPlacePtr(ins.Pending)
	if err != nil {
		return err
	}
	if pendingTy != "ptr" {
		return fmt.Errorf("channel_share pending slot must lower as ptr, got %s", pendingTy)
	}
	handleSlot, handleSlotTy, err := fe.emitPlacePtr(ins.Handle)
	if err != nil {
		return err
	}
	if handleSlotTy != "ptr" {
		return fmt.Errorf("channel_share handle slot must lower as ptr, got %s", handleSlotTy)
	}

	kindPtr := fe.nextTemp()
	bitsPtr := fe.nextTemp()
	statusSlot := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8, align %d\n", kindPtr, 1)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", bitsPtr, alignWord)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i32, align %d\n", statusSlot, 4)

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
	allocStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_channel_handle_alloc(ptr %s)\n", allocStatus, handleSlot)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", allocStatus, statusSlot)
	allocOK := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i32 %s, %d\n", allocOK, allocStatus, rtRemoteTaskOK)
	allocReadyBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf,
		"  br i1 %s, label %%%s, label %%%s\n", allocOK, allocReadyBB, statusBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", allocReadyBB)
	handlePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handlePtr, handleSlot)
	sourceVal, sourceTy, err := fe.emitValueOperand(&ins.Receiver)
	if err != nil {
		return err
	}
	if sourceTy != "ptr" {
		return fmt.Errorf("channel_share source handle must lower as ptr, got %s", sourceTy)
	}
	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_channel_share(ptr %s, ptr %s, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		sourceVal,
		pendingPtr,
		handlePtr,
		kindPtr,
		bitsPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", initStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", retryBB)
	retryHandlePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", retryHandlePtr, handleSlot)
	retryStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_channel_share(ptr null, ptr %s, ptr %s, ptr %s, ptr %s)\n",
		retryStatus,
		pendingPtr,
		retryHandlePtr,
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
	readyBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isOK, readyBB, errBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
	readyHandlePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", readyHandlePtr, handleSlot)
	dstPtr, dstTy, err := fe.emitPlacePtr(ins.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		return fmt.Errorf("channel_share handle destination must lower as ptr, got %s", dstTy)
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", readyHandlePtr, dstPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.ReadyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", errBB)
	errorHandlePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", errorHandlePtr, handleSlot)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_far_channel_handle_free(ptr %s)\n", errorHandlePtr)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", handleSlot)
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
	if err := fe.emitPanicBlock(invalidBB, "share requires an async task context and a channel handle"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(shutdownBB, "share destination shard is shut down"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(queueBB, "share transport queue is full"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(refusedBB, "share request was refused"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(staleBB, "share source lease was already released; a released holder cannot propagate access"); err != nil {
		return err
	}
	return fe.emitPanicBlock(defaultBB, "share request failed")
}
