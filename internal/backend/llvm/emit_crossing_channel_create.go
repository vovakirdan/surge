package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/sema"
)

// emitChannelCreateCrossing lowers `channel_on(dst, capacity)`: allocate the
// caller-side handle token, send one create request to the destination shard,
// suspend on the single reply, and leave the filled token pointer in the
// destination place. The producer returns the handle directly (not a
// TaskResult), so every failure status maps to a deterministic panic.
func (fe *funcEmitter) emitChannelCreateCrossing(ins *mir.CrossingInstr) error {
	if ins == nil {
		return nil
	}
	if ins.ReadyBB == mir.NoBlockID || ins.PendBB == mir.NoBlockID {
		return fmt.Errorf("channel_create must be lowered inside an async suspend context")
	}
	if ins.Destination.Kind != sema.CrossingDestinationPlacement {
		return fmt.Errorf("channel_create expects a Placement destination")
	}

	pendingPtr, pendingTy, err := fe.emitPlacePtr(ins.Pending)
	if err != nil {
		return err
	}
	if pendingTy != "ptr" {
		return fmt.Errorf("channel_create pending slot must lower as ptr, got %s", pendingTy)
	}
	handleSlot, handleSlotTy, err := fe.emitPlacePtr(ins.Handle)
	if err != nil {
		return err
	}
	if handleSlotTy != "ptr" {
		return fmt.Errorf("channel_create handle slot must lower as ptr, got %s", handleSlotTy)
	}

	kindPtr := fe.nextTemp()
	statusSlot := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8, align %d\n", kindPtr, 1)
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
	placementVal, placementTy, err := fe.emitValueOperand(&ins.Destination.Value)
	if err != nil {
		return err
	}
	if placementTy != "i64" {
		return fmt.Errorf("channel_create placement must lower as i64, got %s", placementTy)
	}
	capacityVal, err := fe.emitUintOperandToI64(&ins.Receiver, "channel_on capacity out of range")
	if err != nil {
		return err
	}
	// The element type crosses as its id -- EVERY element type, a scalar
	// included. The owner shard turns the id back into the exact descriptor
	// the element's storage was laid out with before it sizes a cell; a
	// scalar sent as "no descriptor" used to be given a machine word instead,
	// which is the wrong width for anything narrower than one.
	payloadID := fe.emitter.registerCrossingPayloadType(ins.PayloadType)
	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_channel_create(i64 %s, i64 %s, i64 %d, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		placementVal,
		capacityVal,
		payloadID,
		pendingPtr,
		handlePtr,
		kindPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", initStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", retryBB)
	retryHandlePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", retryHandlePtr, handleSlot)
	retryStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_channel_create(i64 0, i64 0, i64 0, ptr %s, ptr %s, ptr %s)\n",
		retryStatus,
		pendingPtr,
		retryHandlePtr,
		kindPtr)
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
	dstPtr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		return fmt.Errorf("channel_create handle destination must lower as ptr, got %s", dstTy)
	}
	fe.emitValueStore(dstTy, readyHandlePtr, dstPtr, dstAlign)
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
	unsupportedBB := fe.nextInlineBlock()
	defaultBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%%s [\n", statusVal, defaultBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskInvalidArgument, invalidBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskDestinationShutdown, shutdownBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskQueueFull, queueBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskRefused, refusedBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskUnsupportedPlacement, unsupportedBB)
	fmt.Fprintf(&fe.emitter.buf, "  ]\n")
	if err := fe.emitPanicBlock(invalidBB, "channel_on placement is invalid"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(shutdownBB, "channel_on destination shard is shut down"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(queueBB, "channel_on transport queue is full"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(refusedBB, "channel_on create request was refused"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(unsupportedBB, "channel_on placement is not supported by this backend"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(defaultBB, "channel_on create request failed"); err != nil {
		return err
	}

	fe.blockTerminated = true
	return nil
}
