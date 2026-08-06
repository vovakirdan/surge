package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// emitChannelSelectCrossing lowers a remote select: build the arm tables
// (far-channel token pointers, arm kinds, send payloads), ship ONE anchored
// select request to the arms' owner shard, suspend on the single reply, and
// store the winner index into the select_index destination — the same value
// a local rt_select_poll would have produced, so the arm dispatch that
// follows is shared with the local path. A Cancelled reply re-enters the
// pend edge: rt_async_yield's cancellation check completes the caller as
// Cancelled exactly like a local select's parked path.
func (fe *funcEmitter) emitChannelSelectCrossing(ins *mir.CrossingInstr) error {
	if ins == nil {
		return nil
	}
	if ins.BodyFuncID == mir.NoFuncID {
		return fmt.Errorf("channel_select missing selector poll function")
	}
	if ins.ReadyBB == mir.NoBlockID || ins.PendBB == mir.NoBlockID {
		return fmt.Errorf("channel_select must be lowered inside an async suspend context")
	}
	armCount := len(ins.RemoteOps)
	if armCount == 0 {
		return fmt.Errorf("channel_select requires at least one arm")
	}
	pendingPtr, pendingTy, err := fe.emitPlacePtr(ins.Pending)
	if err != nil {
		return err
	}
	if pendingTy != "ptr" {
		return fmt.Errorf("channel_select pending slot must lower as ptr, got %s", pendingTy)
	}

	kindPtr := fe.nextTemp()
	bitsPtr := fe.nextTemp()
	statusSlot := fe.nextTemp()
	anchorsPtr := fe.nextTemp()
	armKindsPtr := fe.nextTemp()
	armBitsPtr := fe.nextTemp()
	armDropIDsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8, align %d\n", kindPtr, 1)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", bitsPtr, alignWord)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i32, align %d\n", statusSlot, 4)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x ptr], align %d\n", anchorsPtr, armCount, alignPtr)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i8], align %d\n", armKindsPtr, armCount, 1)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i64], align %d\n", armBitsPtr, armCount, alignWord)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i64], align %d\n", armDropIDsPtr, armCount, alignWord)

	// The arm tables are built ONCE, on the true first attempt. A resumed
	// retry re-enters this same block, and rt_far_channel_select's own retry
	// branch returns on `*pending != NULL` before it reads anchors, kinds,
	// send_bits, or send_drop_fn_ids at all — so anything re-evaluated here
	// per round is silently discarded.
	//
	// What actually gets re-evaluated is narrower than it looks, and the
	// distinction is worth stating so nobody re-derives it and deletes this
	// split: a PLACE operand (a call result, a binding, a lease-minting
	// receiver) is temp'd into a preceding block by splitAsyncAwaits, so
	// re-entry only re-loads it and costs nothing. A CONST operand is
	// embedded in the crossing instruction itself and lowers inline, right
	// here — and several const kinds ALLOCATE: a `int`/`uint`/`float`
	// literal outside the fixnum inline range calls rt_bigint_from_literal
	// and friends, a string const calls rt_string_from_bytes. Measured: a
	// bignum-literal send arm leaks exactly one orphaned bigint per retry
	// round without this split (72 bytes/2 blocks vs 36/1 with it).
	//
	// That makes this the same category as emitChannelCreateCrossing's
	// split, whose initBB allocates a handle — conditional on operand form
	// here rather than unconditional, but the same hazard.
	statusBB := fe.nextInlineBlock()
	initBB := fe.nextInlineBlock()
	retryBB := fe.nextInlineBlock()
	pendingVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", pendingVal, pendingPtr)
	isRetry := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne ptr %s, null\n", isRetry, pendingVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isRetry, retryBB, initBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", initBB)
	for i := range ins.RemoteOps {
		op := &ins.RemoteOps[i]
		anchorSlot := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 %d\n",
			anchorSlot, armCount, anchorsPtr, i)
		recvVal, recvTy, recvErr := fe.emitValueOperand(&op.Receiver)
		if recvErr != nil {
			return recvErr
		}
		if recvTy != "ptr" {
			return fmt.Errorf("channel_select arm handle must lower as ptr, got %s", recvTy)
		}
		fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", recvVal, anchorSlot)

		kindSlot := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 %d\n",
			kindSlot, armCount, armKindsPtr, i)
		armKind := 1 // SELECT_CHAN_RECV (rt_select_poll ABI)
		if op.Method == "send" {
			armKind = 2 // SELECT_CHAN_SEND
		}
		fmt.Fprintf(&fe.emitter.buf, "  store i8 %d, ptr %s\n", armKind, kindSlot)

		bitsSlot := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 %d\n",
			bitsSlot, armCount, armBitsPtr, i)
		dropIDSlot := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 %d\n",
			dropIDSlot, armCount, armDropIDsPtr, i)
		if op.Method == "send" {
			val, valTy, valErr := fe.emitValueOperand(&op.Value)
			if valErr != nil {
				return valErr
			}
			valueType := operandValueType(fe.emitter.types, &op.Value)
			bitsVal, bitsErr := fe.emitValueToI64(val, valTy, valueType)
			if bitsErr != nil {
				return bitsErr
			}
			fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", bitsVal, bitsSlot)
			dropID := types.TypeID(0)
			if fe.emitter.typeOwnsHeap(valueType) {
				dropID = fe.emitter.registerCrossingDropResult(valueType)
			}
			fmt.Fprintf(&fe.emitter.buf, "  store i64 %d, ptr %s\n", dropID, dropIDSlot)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", bitsSlot)
			fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", dropIDSlot)
		}
	}
	anchorsBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 0\n",
		anchorsBase, armCount, anchorsPtr)
	kindsBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0\n",
		kindsBase, armCount, armKindsPtr)
	bitsBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 0\n",
		bitsBase, armCount, armBitsPtr)
	dropIDsBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 0\n",
		dropIDsBase, armCount, armDropIDsPtr)

	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_channel_select(ptr %s, ptr %s, ptr %s, ptr %s, i64 %d, i64 0, "+
			"i64 %d, ptr null, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		anchorsBase,
		kindsBase,
		bitsBase,
		dropIDsBase,
		armCount,
		ins.BodyFuncID,
		pendingPtr,
		kindPtr,
		bitsPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", initStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	// Retry: the pending already owns the shipped arm table (payloads
	// included), so every arm-describing argument is passed inert — matching
	// the C retry branch, which reads none of them.
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", retryBB)
	retryBitsBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 0\n",
		retryBitsBase, armCount, armBitsPtr)
	retryStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_channel_select(ptr null, ptr null, ptr %s, ptr null, i64 %d, i64 0, "+
			"i64 0, ptr null, ptr %s, ptr %s, ptr %s)\n",
		retryStatus,
		retryBitsBase,
		armCount,
		pendingPtr,
		kindPtr,
		bitsPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", retryStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	pendingBB := fe.nextInlineBlock()
	doneBB := fe.nextInlineBlock()
	errBB := fe.nextInlineBlock()

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
	replyBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isOK, replyBB, errBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", replyBB)
	replyKind := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", replyKind, kindPtr)
	isWinner := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", isWinner, replyKind)
	winnerBB := fe.nextInlineBlock()
	cancelledBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isWinner, winnerBB, cancelledBB)

	// Cancelled reply: the caller itself is being cancelled (the only route
	// that cancels the selector body); re-entering the pend edge lets
	// rt_async_yield's cancellation check finish the task, matching the
	// local select's parked-path behavior byte for byte.
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cancelledBB)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.PendBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", winnerBB)
	winnerBits := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", winnerBits, bitsPtr)
	if handbackErr := fe.emitChannelSelectHandback(ins, armBitsPtr, winnerBits); handbackErr != nil {
		return handbackErr
	}
	dstType, err := fe.placeBaseType(ins.Dst)
	if err != nil {
		return err
	}
	val, valTy, err := fe.emitI64ToValue(winnerBits, dstType)
	if err != nil {
		return err
	}
	dstPtr, dstTy, err := fe.emitPlacePtr(ins.Dst)
	if err != nil {
		return err
	}
	if dstTy != valTy {
		dstTy = valTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, val, dstPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.ReadyBB)

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
	if err := fe.emitPanicBlock(invalidBB,
		"remote select arms must be far channels sharing one owner shard; "+
			"split into one select per owner"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(shutdownBB, "remote select destination shard is shut down"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(queueBB, "remote select transport queue is full"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(refusedBB, "remote select request was refused"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(staleBB,
		"remote select arm lease was already released; a released holder cannot select"); err != nil {
		return err
	}
	return fe.emitPanicBlock(defaultBB, "remote select request failed")
}

// emitChannelSelectHandback materializes the MIR ReturnPlace definitions on a
// genuine winner reply. The runtime has already copied every non-committed
// SEND payload back into armBits and relinquished the pending's drop
// obligations. A committed SEND stays with its channel and is deliberately
// skipped; all other returned roots are restored before ReadyBB so sema's
// ordinary losing-arm drops see exactly the owners MIR promises.
func (fe *funcEmitter) emitChannelSelectHandback(
	ins *mir.CrossingInstr,
	armBitsPtr string,
	winnerBits string,
) error {
	if ins == nil {
		return nil
	}
	armCount := len(ins.RemoteOps)
	for i := range ins.RemoteOps {
		op := &ins.RemoteOps[i]
		if op.ReturnPlace == nil {
			continue
		}
		if op.Method != "send" || op.Value.Kind != mir.OperandMove ||
			op.ReturnPlace.Kind != mir.PlaceLocal || len(op.ReturnPlace.Proj) != 0 ||
			op.Value.Place.Kind != mir.PlaceLocal || len(op.Value.Place.Proj) != 0 ||
			op.Value.Place.Local != op.ReturnPlace.Local {
			return fmt.Errorf("channel_select return place %d is not an exact SEND MOVE", i)
		}
		restoreBB := fe.nextInlineBlock()
		nextBB := fe.nextInlineBlock()
		committed := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i64 %s, %d\n", committed, winnerBits, i)
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", committed, nextBB, restoreBB)

		fmt.Fprintf(&fe.emitter.buf, "%s:\n", restoreBB)
		bitsSlot := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 %d\n",
			bitsSlot, armCount, armBitsPtr, i)
		returnedBits := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", returnedBits, bitsSlot)
		placeType, err := fe.placeBaseType(*op.ReturnPlace)
		if err != nil {
			return err
		}
		returned, returnedTy, err := fe.emitI64ToValue(returnedBits, placeType)
		if err != nil {
			return err
		}
		placePtr, placeTy, err := fe.emitPlacePtr(*op.ReturnPlace)
		if err != nil {
			return err
		}
		if placeTy != returnedTy {
			return fmt.Errorf("channel_select return place %d lowers as %s, returned %s", i, placeTy, returnedTy)
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", placeTy, returned, placePtr)
		fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", nextBB)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", nextBB)
	}
	return nil
}
