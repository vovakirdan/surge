package llvm

import (
	"fmt"

	"surge/internal/mir"
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
	winnerPtr := fe.nextTemp()
	statusSlot := fe.nextTemp()
	anchorsPtr := fe.nextTemp()
	armKindsPtr := fe.nextTemp()
	// One ADDRESS per arm rather than one word: the runtime moves a SEND
	// payload out of the caller's own storage when the select arms, and moves a
	// losing one back into it on the terminal retry. The word this replaces
	// could only carry a pointer, which is why a payload wider than one had to
	// be boxed to travel at all.
	armValuesPtr := fe.nextTemp()
	armTypeIDsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8, align %d\n", kindPtr, 1)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", winnerPtr, alignWord)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i32, align %d\n", statusSlot, 4)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x ptr], align %d\n", anchorsPtr, armCount, alignPtr)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i8], align %d\n", armKindsPtr, armCount, 1)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x ptr], align %d\n", armValuesPtr, armCount, alignPtr)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i64], align %d\n", armTypeIDsPtr, armCount, alignWord)

	// The arm tables are built ONCE, on the true first attempt. A resumed
	// retry re-enters this same block, and rt_far_channel_select's own retry
	// branch returns on `*pending != NULL` before it reads anchors, kinds,
	// send addresses, or payload type ids at all — so anything re-evaluated here
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

		valueSlot := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 %d\n",
			valueSlot, armCount, armValuesPtr, i)
		typeIDSlot := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 %d\n",
			typeIDSlot, armCount, armTypeIDsPtr, i)
		if op.Method == "send" {
			// Where the payload ALREADY is. A place operand has an address of
			// its own and the runtime moves straight out of it; a constant gets
			// staging storage here, which the move empties exactly as it would
			// empty a binding.
			addr, addrErr := fe.emitChannelValueAddress(&op.Value)
			if addrErr != nil {
				return addrErr
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", addr, valueSlot)
			// The element's TYPE, not a drop id: it names the descriptor that
			// moves this payload and destroys it, and it is passed for every
			// SEND arm rather than only for the ones that own heap, because a
			// descriptor is how the runtime knows the WIDTH as well.
			//
			// It comes from the CHANNEL, not from the value: the cell this
			// stages into is an element of that channel, and only the channel
			// can say how wide an element is. Asking the operand would let a
			// literal's own type size a cell the channel's element does not
			// fit -- the storage-flip defect shape, in one line.
			elementType := resolveValueType(fe.emitter.types, fe.channelElementTypeOf(&op.Receiver))
			fmt.Fprintf(&fe.emitter.buf, "  store i64 %d, ptr %s\n", elementType, typeIDSlot)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", valueSlot)
			fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", typeIDSlot)
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
	valuesBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 0\n",
		valuesBase, armCount, armValuesPtr)
	typeIDsBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 0\n",
		typeIDsBase, armCount, armTypeIDsPtr)

	initStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_channel_select(ptr %s, ptr %s, ptr %s, ptr %s, i64 %d, i64 0, "+
			"i64 %d, ptr null, ptr %s, ptr %s, ptr %s)\n",
		initStatus,
		anchorsBase,
		kindsBase,
		valuesBase,
		typeIDsBase,
		armCount,
		ins.BodyFuncID,
		pendingPtr,
		kindPtr,
		winnerPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", initStatus, statusSlot)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", statusBB)

	// Retry: the pending already owns the staged payloads, so every other
	// arm-describing argument is passed inert — matching the C retry branch,
	// which reads none of them.
	//
	// The value array is the exception and it is rebuilt HERE rather than
	// carried over, because this block is re-entered on a fresh poll: the
	// addresses the arming call staged from belonged to that poll's frame, and
	// what a losing payload must be moved back into is THIS poll's storage for
	// the same MIR place.
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", retryBB)
	if addrErr := fe.emitChannelSelectReturnAddresses(ins, armValuesPtr); addrErr != nil {
		return addrErr
	}
	retryValuesBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 0\n",
		retryValuesBase, armCount, armValuesPtr)
	retryStatus := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call i32 @rt_far_channel_select(ptr null, ptr null, ptr %s, ptr null, i64 %d, i64 0, "+
			"i64 0, ptr null, ptr %s, ptr %s, ptr %s)\n",
		retryStatus,
		retryValuesBase,
		armCount,
		pendingPtr,
		kindPtr,
		winnerPtr)
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
	winnerIndex := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", winnerIndex, winnerPtr)
	// No handback to emit: every losing payload was MOVED back into its own MIR
	// place by the call above, which is the place sema's losing-arm drops
	// already name. The winner's place was emptied by the staging move and
	// stays empty, so nothing there is dropped twice.
	if err := fe.emitSelectWinnerIndex(winnerIndex, ins.Dst); err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.ReadyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", errBB)
	// No arm for a full transport queue: a saturated lane parks the task
	// (PENDING) rather than refusing it, so the status never reaches here.
	invalidBB := fe.nextInlineBlock()
	shutdownBB := fe.nextInlineBlock()
	refusedBB := fe.nextInlineBlock()
	staleBB := fe.nextInlineBlock()
	defaultBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%%s [\n", statusVal, defaultBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskInvalidArgument, invalidBB)
	fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%%s\n", rtRemoteTaskDestinationShutdown, shutdownBB)
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
	if err := fe.emitPanicBlock(refusedBB, "remote select request was refused"); err != nil {
		return err
	}
	if err := fe.emitPanicBlock(staleBB,
		"remote select arm lease was already released; a released holder cannot select"); err != nil {
		return err
	}
	return fe.emitPanicBlock(defaultBB, "remote select request failed")
}

// emitChannelSelectReturnAddresses fills the arm value table with WHERE each
// losing SEND payload must be moved back to.
//
// It is the retry half of the same array the arming call staged from, and it
// exists as its own pass because the two run in different polls: the arming
// call read the caller's storage on the poll that armed the select, and this
// names the storage for the SAME MIR place on the poll that finishes it. The
// runtime keeps neither address, so nothing here can outlive its frame.
//
// The shape is checked rather than assumed: MIR promises a SEND arm's return
// place is the very local the payload moved out of, and a lowering that
// silently disagreed would restore a value into storage nobody drops.
func (fe *funcEmitter) emitChannelSelectReturnAddresses(
	ins *mir.CrossingInstr,
	armValuesPtr string,
) error {
	if ins == nil {
		return nil
	}
	armCount := len(ins.RemoteOps)
	for i := range ins.RemoteOps {
		op := &ins.RemoteOps[i]
		valueSlot := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 %d\n",
			valueSlot, armCount, armValuesPtr, i)
		if op.Method != "send" || op.ReturnPlace == nil {
			// A RECV arm, or a SEND whose payload MIR gives no place to return
			// to. Either way there is nowhere to move a loser back into, and
			// the runtime destroys it in its own cell instead.
			fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", valueSlot)
			continue
		}
		if op.Value.Kind != mir.OperandMove ||
			op.ReturnPlace.Kind != mir.PlaceLocal || len(op.ReturnPlace.Proj) != 0 ||
			op.Value.Place.Kind != mir.PlaceLocal || len(op.Value.Place.Proj) != 0 ||
			op.Value.Place.Local != op.ReturnPlace.Local {
			return fmt.Errorf("channel_select return place %d is not an exact SEND MOVE", i)
		}
		placePtr, _, _, err := fe.emitPlaceStorage(*op.ReturnPlace)
		if err != nil {
			return err
		}
		fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", placePtr, valueSlot)
	}
	return nil
}
