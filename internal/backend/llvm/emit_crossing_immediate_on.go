package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// emitImmediateOnCrossing lowers `on placement { ... }` to the dedicated
// immediate execute/reply runtime call: one request, one reply, suspend on
// the reply, and `TaskResult<T>` materialization from the reply's result
// kind and the typed result slot the reply names. No far Task handle is
// materialized.
func (fe *funcEmitter) emitImmediateOnCrossing(ins *mir.CrossingInstr) error {
	if ins == nil {
		return nil
	}
	if ins.BodyFuncID == mir.NoFuncID {
		return fmt.Errorf("on crossing missing destination poll function")
	}
	if ins.ReadyBB == mir.NoBlockID || ins.PendBB == mir.NoBlockID {
		return fmt.Errorf("on crossing must be lowered inside an async suspend context")
	}
	pendingPtr, pendingTy, err := fe.emitPlacePtr(ins.Pending)
	if err != nil {
		return err
	}
	if pendingTy != "ptr" {
		return fmt.Errorf("on crossing pending slot must lower as ptr, got %s", pendingTy)
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
	placementVal, placementTy, err := fe.emitValueOperand(&ins.Destination.Value)
	if err != nil {
		return err
	}
	if placementTy != "i64" {
		return fmt.Errorf("on crossing placement must lower as i64, got %s", placementTy)
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
			return fmt.Errorf("on crossing state must lower as ptr, got %s", stateTy)
		}
		stateType, stateErr := fe.crossingStateTypeID(ins.State.TypeID)
		if stateErr != nil {
			return stateErr
		}
		stateTypeID = stateType
	}
	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_immediate_on_execute(i64 %s, i64 %d, i64 %d, i64 %d, ptr %s, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		placementVal,
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
		"  %s = call i32 @rt_immediate_on_execute(i64 0, i64 0, i64 0, i64 %d, ptr null, ptr %s, ptr %s, ptr %s)\n",
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
	if err := fe.emitImmediateOnErrorBlocks(errBB, statusVal); err != nil {
		return err
	}
	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitImmediateOnErrorBlocks(errBB, statusVal string) error {
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", errBB)
	invalidBB := fe.nextInlineBlock()
	shutdownBB := fe.nextInlineBlock()
	queueBB := fe.nextInlineBlock()
	refusedBB := fe.nextInlineBlock()
	staleBB := fe.nextInlineBlock()
	unsupportedBB := fe.nextInlineBlock()
	defaultBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%%s [\n", statusVal, defaultBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskInvalidArgument, invalidBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskDestinationShutdown, shutdownBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskQueueFull, queueBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskRefused, refusedBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskStaleToken, staleBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskUnsupportedPlacement, unsupportedBB)
	fmt.Fprintf(&fe.emitter.buf, "  ]\n")
	if err := fe.emitPanicBlock(invalidBB, "on crossing requires an async task context"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(shutdownBB, "on destination shard is shut down"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(queueBB, "on transport queue is full"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(refusedBB, "on execute request was refused"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(staleBB, "on execute token is stale"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(unsupportedBB, "on placement is not supported by this backend"); err != nil {
		return err
	}
	return fe.emitPanicBlock(defaultBB, "on execute request failed")
}
