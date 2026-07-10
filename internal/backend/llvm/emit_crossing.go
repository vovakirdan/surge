package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/sema"
)

const (
	rtRemoteSpawnOK                   = 0
	rtRemoteSpawnPending              = 1
	rtRemoteSpawnInvalidArgument      = 2
	rtRemoteSpawnDestinationShutdown  = 3
	rtRemoteSpawnQueueFull            = 4
	rtRemoteSpawnRefused              = 5
	rtRemoteSpawnUnsupportedPlacement = 7
	rtRemoteSpawnInvalidPlacement     = 8

	rtRemoteTaskOK                   = 0
	rtRemoteTaskPending              = 1
	rtRemoteTaskInvalidArgument      = 2
	rtRemoteTaskDestinationShutdown  = 3
	rtRemoteTaskQueueFull            = 4
	rtRemoteTaskRefused              = 5
	rtRemoteTaskStaleToken           = 6
	rtRemoteTaskConsumed             = 7
	rtRemoteTaskUnsupportedPlacement = 8
)

func (fe *funcEmitter) emitInstrCrossing(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	switch ins.Crossing.Kind {
	case sema.CrossingLoweringSpawnOn:
		return fe.emitSpawnOnCrossing(&ins.Crossing)
	case sema.CrossingLoweringOnPlacement:
		return fe.emitImmediateOnCrossing(&ins.Crossing)
	case sema.CrossingLoweringChannelCreate:
		return fe.emitChannelCreateCrossing(&ins.Crossing)
	case sema.CrossingLoweringFarTaskAwait:
		return fe.emitFarTaskLifecycleCrossing(&ins.Crossing, "await", "rt_far_task_await")
	case sema.CrossingLoweringFarTaskCancel:
		return fe.emitFarTaskLifecycleCrossing(&ins.Crossing, "cancel", "rt_far_task_cancel")
	default:
		return fmt.Errorf("crossing %s is not implemented in LLVM backend", mirCrossingKindNameForLLVM(ins.Crossing.Kind))
	}
}

func mirCrossingKindNameForLLVM(kind sema.CrossingLoweringKind) string {
	switch kind {
	case sema.CrossingLoweringOnPlacement:
		return "on"
	case sema.CrossingLoweringOnFarHandle:
		return "on_far_handle"
	case sema.CrossingLoweringSpawnOn:
		return "spawn_on"
	case sema.CrossingLoweringFarTaskAwait:
		return "far_task_await"
	case sema.CrossingLoweringFarTaskCancel:
		return "far_task_cancel"
	case sema.CrossingLoweringChannelCreate:
		return "channel_create"
	default:
		return fmt.Sprintf("kind_%d", kind)
	}
}

func (fe *funcEmitter) emitSpawnOnCrossing(ins *mir.CrossingInstr) error {
	if ins == nil {
		return nil
	}
	if ins.BodyFuncID == mir.NoFuncID {
		return fmt.Errorf("spawn_on missing remote poll function")
	}
	if ins.ReadyBB == mir.NoBlockID || ins.PendBB == mir.NoBlockID {
		return fmt.Errorf("spawn_on must be lowered inside an async suspend context")
	}
	if ins.Destination.Kind != sema.CrossingDestinationPlacement {
		return fmt.Errorf("spawn_on expects a Placement destination")
	}

	pendingPtr, pendingTy, err := fe.emitPlacePtr(ins.Pending)
	if err != nil {
		return err
	}
	if pendingTy != "ptr" {
		return fmt.Errorf("spawn_on pending slot must lower as ptr, got %s", pendingTy)
	}
	handleSlot, handleSlotTy, err := fe.emitPlacePtr(ins.Handle)
	if err != nil {
		return err
	}
	if handleSlotTy != "ptr" {
		return fmt.Errorf("spawn_on handle slot must lower as ptr, got %s", handleSlotTy)
	}
	statusSlot := fe.nextTemp()
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
	allocStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_task_handle_alloc(ptr %s)\n", allocStatus, handleSlot)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", allocStatus, statusSlot)
	allocOK := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i32 %s, %d\n", allocOK, allocStatus, rtRemoteSpawnOK)
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
		return fmt.Errorf("spawn_on placement must lower as i64, got %s", placementTy)
	}
	stateVal := "null"
	if len(ins.State.Fields) > 0 {
		stateVal, placementTy, err = fe.emitStructLit(&ins.State)
		if err != nil {
			return err
		}
		if placementTy != "ptr" {
			return fmt.Errorf("spawn_on state must lower as ptr, got %s", placementTy)
		}
	}
	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_remote_spawn_publish_placement(i64 %s, i64 %d, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		placementVal,
		ins.BodyFuncID,
		stateVal,
		pendingPtr,
		handlePtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", initStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", retryBB)
	retryHandlePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", retryHandlePtr, handleSlot)
	retryStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_remote_spawn_publish_placement(i64 0, i64 %d, ptr null, ptr %s, ptr %s)\n",
		retryStatus,
		ins.BodyFuncID,
		pendingPtr,
		retryHandlePtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", retryStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", statusBB)
	statusVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i32, ptr %s\n", statusVal, statusSlot)
	isPending := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i32 %s, %d\n", isPending, statusVal, rtRemoteSpawnPending)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isPending, pendingBB, doneBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", pendingBB)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.PendBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", doneBB)
	isOK := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i32 %s, %d\n", isOK, statusVal, rtRemoteSpawnOK)
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
		return fmt.Errorf("spawn_on handle destination must lower as ptr, got %s", dstTy)
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", readyHandlePtr, dstPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.ReadyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", errBB)
	errorHandlePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", errorHandlePtr, handleSlot)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_far_task_handle_free(ptr %s)\n", errorHandlePtr)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", handleSlot)
	shutdownBB := fe.nextInlineBlock()
	queueBB := fe.nextInlineBlock()
	refusedBB := fe.nextInlineBlock()
	unsupportedPlacementBB := fe.nextInlineBlock()
	invalidPlacementBB := fe.nextInlineBlock()
	invalidBB := fe.nextInlineBlock()
	defaultBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%%s [\n", statusVal, defaultBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteSpawnInvalidArgument, invalidBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteSpawnDestinationShutdown, shutdownBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteSpawnQueueFull, queueBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteSpawnRefused, refusedBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteSpawnUnsupportedPlacement, unsupportedPlacementBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteSpawnInvalidPlacement, invalidPlacementBB)
	fmt.Fprintf(&fe.emitter.buf, "  ]\n")
	if err := fe.emitPanicBlock(invalidBB, "spawn on remote publish requires an async task context"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(shutdownBB, "spawn on destination is shut down"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(queueBB, "spawn on remote publish queue is full"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(refusedBB, "spawn on remote publish was refused"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(unsupportedPlacementBB, "spawn on placement is not supported by this backend"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(invalidPlacementBB, "spawn on placement is invalid"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(defaultBB, "spawn on remote publish failed"); err != nil {
		return err
	}

	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitPanicBlock(label, msg string) error {
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", label)
	return fe.emitPanic(msg)
}

func (fe *funcEmitter) emitPanic(msg string) error {
	ptr, dataLen, err := fe.emitBytesConst(msg)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_panic(ptr %s, i64 %d)\n", ptr, dataLen)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}
