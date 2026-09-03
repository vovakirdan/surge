package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/sema"
	"surge/internal/types"
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
	case sema.CrossingLoweringOnFarHandle:
		return fe.emitAnchoredOnCrossing(&ins.Crossing)
	case sema.CrossingLoweringChannelCreate:
		return fe.emitChannelCreateCrossing(&ins.Crossing)
	case sema.CrossingLoweringChannelShare:
		return fe.emitChannelShareCrossing(&ins.Crossing)
	case sema.CrossingLoweringChannelSelect:
		return fe.emitChannelSelectCrossing(&ins.Crossing)
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
	case sema.CrossingLoweringChannelShare:
		return "channel_share"
	case sema.CrossingLoweringChannelSelect:
		return "channel_select"
	default:
		return fmt.Sprintf("kind_%d", kind)
	}
}

// crossingStateTypeID names the state box a crossing ships, as the number the
// runtime turns back into that type's DESCRIPTOR.
//
// It refuses a type the operation census never saw. The runtime destroys an
// abandoned state box through the descriptor -- members, then storage at the
// width the descriptor states -- so a type with no descriptor would be freed
// at the wrong width, silently, on a path that only runs when something has
// already gone wrong. The refusal is here, where the type is still legible.
func (fe *funcEmitter) crossingStateTypeID(stateType types.TypeID) (types.TypeID, error) {
	resolved := resolveValueType(fe.emitter.types, stateType)
	if resolved == types.NoTypeID {
		return types.NoTypeID, nil
	}
	if !fe.emitter.valueOpsRegistryHas(resolved) {
		return types.NoTypeID, fmt.Errorf(
			"llvm: a crossing ships a state of type#%d with no operation descriptor, so an "+
				"abandoned one could not be reclaimed at its own width; note: every registry "+
				"type gets a descriptor, so this type never reached the operation census",
			resolved)
	}
	return resolved, nil
}

// emitCrossingCloneCounter charges the one duplication a crossing performs.
// A capture whose type is Copy is duplicated into the state block -- the
// source stays usable, the block gets its own bytes -- and nothing else on a
// crossing copies: owned captures move, an anchor is leased. The count is
// emitted in the crossing's initial block only, because a retry poll ships
// no state; it is the width of the copied captures, as the runtime's
// resident-byte telemetry reports it (rt_resident_bytes.h).
func (fe *funcEmitter) emitCrossingCloneCounter(ins *mir.CrossingInstr) error {
	var total uint64
	for i := range ins.Captures {
		capture := &ins.Captures[i]
		if capture.Mode != sema.CrossingCaptureCopy {
			continue
		}
		resolved := resolveValueType(fe.emitter.types, capture.Type)
		if resolved == types.NoTypeID {
			continue
		}
		layoutInfo, err := fe.emitter.layoutOf(resolved)
		if err != nil {
			return err
		}
		total += layoutInfo.Size
	}
	if total == 0 {
		return nil
	}
	fmt.Fprintf(&fe.emitter.buf,
		"  call void @rt_resident_bytes_record_crossing_clone(i64 %d)\n", total)
	return nil
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
	if err := fe.emitCrossingCloneCounter(ins); err != nil {
		return err
	}
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
	stateTypeID := types.TypeID(0)
	if len(ins.State.Fields) > 0 {
		stateVal, placementTy, err = fe.emitStructLit(&ins.State)
		if err != nil {
			return err
		}
		if placementTy != "ptr" {
			return fmt.Errorf("spawn_on state must lower as ptr, got %s", placementTy)
		}
		stateType, stateErr := fe.crossingStateTypeID(ins.State.TypeID)
		if stateErr != nil {
			return stateErr
		}
		stateTypeID = stateType
	}
	// The body's RESULT TYPE, as a number. A crossing carries no pointers, so
	// the descriptor cannot travel; the id can, and the owner side turns it
	// back into one (rt_channel_element_ops_for). That descriptor is what the
	// body's own result slot is bound with, which is how an abandoned reply is
	// reclaimed on the owner shard now (RV2-DEBT-053a): by the slot's dispose
	// rather than by a numbered drop threaded beside the value. PayloadType is
	// the body's returned reply value; ResultType is the far Task<T> handle.
	resultTypeID, err := fe.crossingResultTypeID(ins.PayloadType)
	if err != nil {
		return err
	}
	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_remote_spawn_publish_placement(i64 %s, i64 %d, i64 %d, i64 %d, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		placementVal,
		stateTypeID,
		resultTypeID,
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
		"  %s = call i32 @rt_remote_spawn_publish_placement(i64 0, i64 0, i64 0, i64 %d, ptr null, ptr %s, ptr %s)\n",
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
	dstPtr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		return fmt.Errorf("spawn_on handle destination must lower as ptr, got %s", dstTy)
	}
	fe.emitValueStore(dstTy, readyHandlePtr, dstPtr, dstAlign)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.ReadyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", errBB)
	errorHandlePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", errorHandlePtr, handleSlot)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_far_task_handle_free(ptr %s)\n", errorHandlePtr)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", handleSlot)
	// No arm for a full transport queue: a saturated data lane parks the
	// publishing task (it answers PENDING) rather than refusing it, so the
	// status never reaches compiled code.
	shutdownBB := fe.nextInlineBlock()
	refusedBB := fe.nextInlineBlock()
	unsupportedPlacementBB := fe.nextInlineBlock()
	invalidPlacementBB := fe.nextInlineBlock()
	invalidBB := fe.nextInlineBlock()
	defaultBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%%s [\n", statusVal, defaultBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteSpawnInvalidArgument, invalidBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteSpawnDestinationShutdown, shutdownBB)
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
