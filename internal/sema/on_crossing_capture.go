package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// crossingArrayBehindHandleMessage is the one sentence both crossing gates say
// when a value reaches a dynamic array only through storage the relinquishing
// walk cannot step. subject is the clause that names the value and the thing it
// cannot do; keeper is who keeps the storage ("shard" for a crossing, "thread"
// for `blocking`); moment is the point the header would have had to be read by.
//
// One function because the two gates refuse ONE shape for one reason, and a
// reader who meets it at `blocking` and again at `on` should not have to decide
// whether two differently worded sentences mean the same thing.
func crossingArrayBehindHandleMessage(subject, keeper, moment string) string {
	return fmt.Sprintf(
		"%s: it holds a dynamic array in storage this %s keeps (a map's table, a channel's "+
			"ring, a task's result slot), so the runtime is never shown that array's header "+
			"before %s and cannot tell a view into another array's buffer from an array of its "+
			"own -- whoever takes it out on the far side would write through the view into the "+
			"buffer this %s is still reading. Take the array out of the container and cross it "+
			"on its own, or in a field of the value that crosses",
		subject, keeper, moment, keeper)
}

// typeFarHandleCall types a method call whose receiver is a `far T` handle and
// returns the call's result type. Outside a crossing it keeps Block 1's
// rejection (SEM3194). Inside an `on` crossing it enforces the owner anchor
// (SEM3150) and, for `far TcpConn`, the control-only whitelist (SEM3151).
func (tc *typeChecker) typeFarHandleCall(callID ast.ExprID, member *ast.ExprMemberData, receiverType types.TypeID, call *ast.ExprCallData, span source.Span) types.TypeID {
	methodName := tc.lookupName(member.Field)

	// Outside any crossing: acting through a far handle is rejected (Block 1).
	if len(tc.onCrossingStack) == 0 {
		tc.report(diag.SemaFarLocalOp, span, "operation on %s requires an accepted remote context", tc.typeLabel(receiverType))
		return types.NoTypeID
	}

	// The destination far handle anchors operations through that same handle only.
	frame := tc.onCrossingStack[len(tc.onCrossingStack)-1]
	tc.noteAnchorOpReceiver(member.Target)
	recvSym := tc.symbolForExpr(member.Target)
	if !frame.isFar || !recvSym.IsValid() || recvSym != frame.anchorSym {
		tc.report(diag.SemaOnAnchorUnproven, span, "this remote handle is not anchored by the current `on` destination")
		return types.NoTypeID
	}

	// `far TcpConn` is control-only: `close()` is the sole operation.
	if tc.typeNameIs(tc.farInner(receiverType), "TcpConn") && methodName != "close" {
		tc.report(diag.SemaOnTcpRemoteIO, span, "remote socket I/O through `far TcpConn` is not supported yet")
		return types.NoTypeID
	}

	// Anchored channel operations carry the local `Channel<T>` surface:
	// `send(own T) -> nothing`, `recv() -> Option<T>`, `close() -> nothing`.
	// The op executes owner-side against the resolved local channel, so the
	// signatures must not drift from `core/intrinsics.sg`.
	if element := tc.channelPayloadType(tc.farInner(receiverType)); element != types.NoTypeID {
		return tc.typeAnchoredChannelOp(callID, methodName, element, member, receiverType, recvSym, call, span)
	}

	// Accepted anchored operation: type argument expressions for the usual
	// checks; the crossing itself is compile-only.
	for _, arg := range call.Args {
		tc.typeExpr(arg.Value)
	}
	tc.recordActiveOnRemoteOp(CrossingRemoteOpInfo{
		Method:         methodName,
		CallExpr:       callID,
		Span:           span,
		ReceiverExpr:   member.Target,
		ReceiverSymbol: recvSym,
		ReceiverType:   receiverType,
	})
	return tc.types.Builtins().Nothing
}

// typeAnchoredChannelOp types one anchored channel operation with local
// parity. Arity and value-type failures name the expected local signature so
// the fix is readable from the diagnostic alone.
func (tc *typeChecker) typeAnchoredChannelOp(
	callID ast.ExprID,
	methodName string,
	element types.TypeID,
	member *ast.ExprMemberData,
	receiverType types.TypeID,
	recvSym symbols.SymbolID,
	call *ast.ExprCallData,
	span source.Span,
) types.TypeID {
	record := func() {
		tc.recordActiveOnRemoteOp(CrossingRemoteOpInfo{
			Method:         methodName,
			CallExpr:       callID,
			Span:           span,
			ReceiverExpr:   member.Target,
			ReceiverSymbol: recvSym,
			ReceiverType:   receiverType,
		})
	}
	switch methodName {
	case "send":
		if len(call.Args) != 1 {
			tc.report(diag.SemaOnChannelOp, span,
				"anchored `send` takes exactly one value of the channel element type `%s`",
				tc.typeLabel(element))
			return types.NoTypeID
		}
		argType := tc.typeExprWithExpected(call.Args[0].Value, element)
		// Asked BEFORE assignability, because the question below would not stop
		// a reference and the reference is the whole defect: typesAssignable
		// DEREFS a reference to a Copy element, so `&int` and `own &int` both
		// satisfy `int` outright.
		if !tc.checkAnchoredSendPayloadIsAValue(argType, element, tc.exprSpan(call.Args[0].Value)) {
			return types.NoTypeID
		}
		// The local signature takes `own T`, and typesAssignable already reads a
		// moved owned value at its nominal type. It used to be asked a second
		// time of `tc.valueType(argType)`, which strips `&` as well as `own`;
		// censused rather than reasoned about, the only payloads that fallback
		// admitted ALONE were `own &T` for a non-Copy `T` -- `own &string`,
		// `own &Pair`, `own &[int]` -- and the question above now refuses all
		// three for their reference. Everything else it was carrying it was not
		// carrying: `ch.send(own xs)` over a far `Channel<int[]>`, `ch.send(own
		// name)` and `ch.send(own f)` all still compile with it gone.
		if argType != types.NoTypeID && !tc.typesAssignable(element, argType, true) {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(call.Args[0].Value),
				"anchored `send` value must be `%s` (the channel element type), got `%s`",
				tc.typeLabel(element), tc.typeLabel(argType))
			return types.NoTypeID
		}
		if !tc.checkAnchoredSendGivesThePayloadAway(call.Args[0].Value, element, tc.exprSpan(call.Args[0].Value)) {
			return types.NoTypeID
		}
		record()
		return tc.types.Builtins().Nothing
	case "recv":
		if len(call.Args) != 0 {
			tc.report(diag.SemaOnChannelOp, span, "anchored `recv` takes no arguments")
			return types.NoTypeID
		}
		record()
		return tc.resolveOptionType(element, span, tc.scopeOrFile(tc.currentScope()))
	case "close":
		if len(call.Args) != 0 {
			tc.report(diag.SemaOnChannelOp, span, "anchored `close` takes no arguments")
			return types.NoTypeID
		}
		record()
		return tc.types.Builtins().Nothing
	default:
		tc.report(diag.SemaOnChannelOp, span,
			"`%s` is not an anchored channel operation; `on ch` supports `send`, `recv`, and `close`",
			methodName)
		return types.NoTypeID
	}
}

func (tc *typeChecker) recordActiveOnRemoteOp(op CrossingRemoteOpInfo) {
	if tc == nil || len(tc.onCrossingStack) == 0 {
		return
	}
	last := len(tc.onCrossingStack) - 1
	tc.onCrossingStack[last].remoteOps = append(tc.onCrossingStack[last].remoteOps, op)
	if op.CallExpr.IsValid() {
		if tc.result.CrossingDispatchCalls == nil {
			tc.result.CrossingDispatchCalls = make(map[ast.ExprID]struct{})
		}
		tc.result.CrossingDispatchCalls[op.CallExpr] = struct{}{}
	}
}

// checkOnCaptures enforces capture legality for values crossing an `on`
// boundary (ON-CAP rows). The capture-effect diagnostics are owned by Block 4;
// Block 2 emits them at the crossing site so the invariants hold now.
//
// anchorSym is the destination handle of an `on far_handle` block, or invalid.
// The anchor crosses in its own mode, CrossingCaptureAnchorLease: the caller
// keeps the handle and its drop, the body reaches the channel through the
// lease the runtime pins on the owner shard for the block's lifetime
// (rt_far_channel_pin, taken at dispatch and dropped at the reply edge), and
// the body's copy of the token is a borrowing read it never dereferences and
// never drops. One owner; checkAnchorLeaseUses keeps the body from treating
// the lease as a value.
func (tc *typeChecker) checkOnCaptures(body ast.StmtID, anchorSym symbols.SymbolID) ([]CrossingCaptureInfo, bool) {
	ok := true
	var captures []CrossingCaptureInfo
	for _, cap := range tc.collectBlockingCaptures(body) {
		capType := tc.bindingType(cap.symID)
		if capType == types.NoTypeID {
			continue
		}
		if anchorSym.IsValid() && cap.symID == anchorSym {
			name := ""
			if sym := tc.symbolFromID(cap.symID); sym != nil {
				name = tc.lookupName(sym.Name)
			}
			captures = append(captures, CrossingCaptureInfo{
				Symbol:  cap.symID,
				Name:    name,
				Expr:    cap.exprID,
				Span:    cap.span,
				Type:    capType,
				Mode:    CrossingCaptureAnchorLease,
				Verdict: CrossingCaptureAnchorLeased,
			})
			continue
		}
		mode, verdict, accepted := tc.classifyOnCapture(capType, cap)
		if !accepted {
			ok = false
			continue
		}
		// A capture that MOVES across the boundary ends the caller's binding
		// here, and until this was written that was true of the lowering
		// only: the state took the value, the body unpacked it, and the
		// caller's binding stayed live anyway. So the caller could still read
		// it, mutate it — giving two answers for one value — or move it a
		// second time, all without a diagnostic, and it was the caller's
		// scope-exit drop that happened to reclaim the capture.
		//
		// A Copy capture leaves the caller's binding intact by definition. An
		// owned value and a far handle both move: the body owns what it
		// unpacks (registerCrossingBodyOwnership), and for a far handle that
		// is the lease itself, given back by the body's drop of it. The one
		// far handle that is not moved is the anchor, filtered out above.
		if mode == CrossingCaptureMoveOwned || mode == CrossingCaptureMoveFarHandle {
			tc.observeMove(cap.exprID, cap.span)
		}
		name := ""
		if sym := tc.symbolFromID(cap.symID); sym != nil {
			name = tc.lookupName(sym.Name)
		}
		captures = append(captures, CrossingCaptureInfo{
			Symbol:  cap.symID,
			Name:    name,
			Expr:    cap.exprID,
			Span:    cap.span,
			Type:    capType,
			Mode:    mode,
			Verdict: verdict,
		})
	}
	return captures, ok
}

func (tc *typeChecker) classifyOnCapture(capType types.TypeID, capture blockingCapture) (CrossingCaptureMode, CrossingCaptureVerdict, bool) {
	span := capture.span
	// Borrowed captures are rejected on the surface type (ON-CAP-N001/N002).
	if tc.isReferenceType(capType) {
		tc.report(diag.SemaCrossBorrowCapture, span, "borrowed values cannot cross shard boundaries")
		return 0, 0, false
	}
	// Far handles move in (affine); the remote resource stays put (ON-CAP-V003).
	if tc.isFarType(capType) {
		return CrossingCaptureMoveFarHandle, CrossingCaptureFarHandle, true
	}
	owned := tc.isOwnType(capType)
	// Owned captures are judged on their nominal type (strip the `own` wrapper).
	nominal := tc.valueType(capType)
	// A dynamic array carries no attribute of its own, so the attribute arms
	// below can only ever say "unmarked" about one. The question an array
	// actually answers is about its ELEMENT, and the type axis has answered it
	// that way since the `@shard_movable` field validator first read an array
	// field as the element it holds (isShardMovableMemberType).
	arrayElem, isDynArray := types.NoTypeID, false
	if tc.types != nil {
		arrayElem, isDynArray = tc.types.DynamicArrayElem(nominal)
	}
	// ON-CAP-N006, and it is asked FIRST for an array because it is the operative
	// fact about one. `Channel<float>[]` answers the counted-block question below
	// as well -- a channel's ring holds counted values -- and answering there
	// named a reason a reader cannot act on: "use `float64` for the values it
	// holds" produces `Channel<float64>[]`, which is refused all over again,
	// because what refuses an array of channels is that no array of channels
	// crosses, whatever the payload. The element is the declaration a reader can
	// change, so the element answers.
	//
	// `Array<T>` is a spelling rather than a declaration, so the refusal never
	// blames the array for carrying no marker -- there is nothing to mark.
	if isDynArray && !tc.shardMovableElement(arrayElem) {
		tc.report(diag.SemaCrossNotShardMovable, span,
			"`%s` cannot cross a shard boundary: its element `%s` may not move between "+
				"shards, and an array crosses only when every element it holds may. Hold "+
				"elements that travel -- a fixed-width value, a `string`, or a type you have "+
				"marked `@shard_movable` -- or leave the array on this shard and cross what "+
				"you need out of it",
			types.Label(tc.types, nominal), types.Label(tc.types, arrayElem))
		return 0, 0, false
	}
	// An arbitrary-precision scalar is Copy, but its word is a reference into a
	// counted heap block, and the count is deliberately not atomic. A capture
	// that holds one -- copied, or moved while a sibling binding still holds
	// the block (`own P{ v: a }` retains `a`'s block into the field) -- is made
	// PRIVATE in the relinquishing operand before the state ships: the lowering
	// un-shares every counted leaf the walk can reach, and a dynamic array's
	// buffer is walked element by element by the runtime in that operand, so
	// the body's state and the caller's bindings never name one block from two
	// shards.
	//
	// What no walk reaches is refused here, in words that say why: a map's
	// table and a channel's ring are storage this shard keeps and the handle
	// only names, with no per-element walk into them. Union payloads count on
	// both sides of that question; the Copy-only ContainsRefCountedScalar does
	// not walk them.
	if tc.result != nil && tc.result.CountedBlockStaysShared(capType) {
		tc.report(diag.SemaCrossNotShardMovable, span, "%s", tc.result.CountedBlockRefusalMessage(
			tc.builder.StringsInterner, capType,
			fmt.Sprintf("`%s` cannot cross a shard boundary", types.Label(tc.types, tc.valueType(capType))),
			tc.countedOnCaptureAllowsWidthRepair(capType, capture)))
		return 0, 0, false
	}
	// The same stop, asked about arrays. An array carries no count, so the
	// question above never named one -- and a SLICE of an array is a header
	// pointing into somebody else's buffer, a fact only the runtime's view
	// registry holds. Where the walk reaches the array it hands the runtime
	// the slot and the refusal is the runtime's; where the array sits behind a
	// handle the walk never reaches it, so the refusal has to be here.
	if tc.result != nil && tc.result.DynamicArrayStaysUnchecked(capType) {
		tc.report(diag.SemaCrossNotShardMovable, span, "%s", crossingArrayBehindHandleMessage(
			fmt.Sprintf("`%s` cannot cross a shard boundary",
				types.Label(tc.types, tc.valueType(capType))),
			"shard", "the value ships"))
		return 0, 0, false
	}
	// Copy values, including `Placement`, may cross freely (ON-CAP-V001/V004).
	if !owned && tc.result != nil && tc.result.IsCopyType(capType) {
		if tc.isPlacementType(capType) {
			return CrossingCaptureCopy, CrossingCapturePlacementCopy, true
		}
		return CrossingCaptureCopy, CrossingCaptureCopyValue, true
	}
	switch {
	case tc.typeHasAttr(nominal, "shard_pinned"):
		// ON-CAP-N004: shard-pinned resources cannot cross as owned values.
		tc.report(diag.SemaCrossPinnedCapture, span,
			"this operation would move a shard-pinned resource; use a far handle or explicit migration")
		return 0, 0, false
	case tc.typeHasAttr(nominal, "nosend"):
		// ON-CAP-N003: `@nosend` forbids crossing task/shard boundaries.
		tc.report(diag.SemaCrossNosendCapture, span,
			"`@nosend` values cannot cross task or shard boundaries outside `@local spawn`")
		return 0, 0, false
	case tc.typeHasAttr(nominal, "shard_movable"):
		// ON-CAP-V002: owned shard-movable values may cross.
		return CrossingCaptureMoveOwned, CrossingCaptureOwnedShardMovable, true
	case tc.typeHasAttr(nominal, "send"):
		// C08: `@send` alone is not sufficient for shard movement.
		tc.report(diag.SemaShardMovableSendInsufficient, span,
			"`@send` is not sufficient for shard movement; add `@shard_movable`")
		return 0, 0, false
	case owned && tc.typeHasAttr(nominal, "copy"):
		// LOCALITY-007/008: `@copy` alone allows copying, not owned migration.
		tc.report(diag.SemaShardMovableCopyInsufficient, span,
			"`@copy` is not sufficient for shard movement; add `@shard_movable`")
		return 0, 0, false
	case owned && tc.result != nil && tc.result.IsCopyType(nominal):
		// Owned builtin Copy values do not need a user shard-movement marker.
		return CrossingCaptureMoveOwned, CrossingCaptureOwnedBuiltinCopy, true
	case isDynArray && tc.isArrayViewBinding(capture.symID):
		// ON-CAP-N007: a VIEW is a window onto ANOTHER array's buffer, and that
		// array stays here. ON-CAP-V005 below accepts an array because its
		// ELEMENTS may travel; for a view that is true of the elements and false
		// of the storage they live in, so crossing one would hand the destination
		// shard a pointer into a buffer the origin shard still owns, still reads
		// through its base, and still frees. Two shards, one mutable buffer.
		//
		// This rule is KINDNESS, not the guarantee. The guarantee is the
		// runtime's: every crossing of a dynamic array hands its header to
		// `rt_array_unshare_walk`, whatever the element type, and the walk asks
		// the view registry and refuses a view -- and a base some view still
		// reads -- by name. So a view this checker cannot see still stops: one a
		// callee returned, one read out of a struct field or passed in as a
		// parameter, one pushed or assigned into a holder. What THIS arm buys is
		// the moment and the words: a compile error naming the binding, before
		// the program is built and run into a VM1003 panic.
		//
		// The two do not answer about the same set, and neither is the other's
		// subset. This one reads a MAY fact over every path, so it refuses a
		// binding a later assignment always overwrites; the runtime reads the
		// header in hand.
		tc.reportCrossingViewCapture(capture.symID, span)
		return 0, 0, false
	case isDynArray && tc.holdsAnArrayView(capture.symID):
		// ON-CAP-N008: the capture is not a view; it HOLDS one. `let v: int[] =
		// base[[1..3]]; let xs: int[][] = [v];` builds a fresh outer array whose
		// ELEMENT windows a buffer the origin shard keeps, and moving `xs` moves
		// that window with it.
		//
		// V005 asks about the element TYPE, which is the travel question, and a
		// view answers it the same way its base does. This is the STORAGE
		// question, and only the VALUE can answer that one.
		//
		// Kindness in front of the same guarantee N007 stands in front of: the
		// walk recurses into an inner array, so the held window meets the view
		// registry at run time whether or not this arm saw it. The routes it
		// cannot see -- `xs.push(v)`, `xs[0] = v`, `pair.0 = v` -- are exactly
		// the ones the runtime now catches, each with a VM1003 panic at 2 and at
		// 8 shards.
		tc.reportCrossingHeldViewCapture(capture.symID, span)
		return 0, 0, false
	case isDynArray:
		// ON-CAP-V005: a dynamic array crosses when every element it holds may
		// move between shards. The array itself needs no marker -- there is
		// nothing to mark, `[T]` being a spelling rather than a declaration --
		// and the counted blocks its buffer holds were already made private
		// above, element by element, by the walk the relinquishing operand runs.
		//
		// Reaching here means four questions were already answered, in this
		// order and for this reason: the ELEMENT question (ON-CAP-N006) first,
		// because for an array it is the operative fact and its way out is the
		// only one an array can take; then the counted-block one, which owns
		// every shape that is NOT an array; then the array-behind-a-handle one,
		// which owns the container an array hides in and never fires on an array
		// itself; then the two view arms. So every element travels, no storage
		// the walk cannot step is in the way, and the value is neither a view nor
		// the holder of one this checker can see.
		//
		// The verdict is its own, not the `@shard_movable` one: recording an
		// `int[]` as accepted "because its type is marked" would be false at the
		// one place the record is read to explain the decision.
		return CrossingCaptureMoveOwned, CrossingCaptureOwnedMovableElements, true
	default:
		// ON-CAP-N005: unmarked owned user values are not shard-movable.
		tc.report(diag.SemaCrossNotShardMovable, span,
			"this owned value is not shard-movable; mark its type `@shard_movable` to cross it")
		return 0, 0, false
	}
}

// registerCrossingBodyOwnership makes the crossing body the owner of every
// capture that MOVED into it, which is the other half of marking the caller's
// binding moved in checkOnCaptures: someone has to drop the value, and the
// caller has just stopped.
//
// Only owned moves qualify, matching the move marking exactly. A Copy capture
// leaves the caller's binding intact and its duplicate is reclaimed by the
// unpacking site (`rewriteSpawnOnPollReturns`); a far handle's lease travels
// with the handle.
//
// "Matching the move marking exactly" is a claim about a set, and the set is
// what ON-CAP-V005 widened: a dynamic array moves into the body under a binding
// whose type is `[T]`, not `own [T]` -- an array literal cannot even be bound as
// `own int[]` -- so asking `isOwnType` about it says no, the body registers
// nothing, and NOBODY drops what the caller has just stopped owning. A body that
// only READS its captured array leaked the header, the buffer, and every private
// clone the un-share walk had just made (24 direct + 24 indirect bytes for a
// bare `int[]`, 228 for a `float[3]`, measured); a body that handed the array to
// an owning callee happened to be clean, which is why the first measurements
// missed it. The question is asked here the way the gate asks it.
//
// This must run BEFORE the body is walked so a `ret` inside it collects the
// capture as a live obligation. The capture set is recomputed here rather than
// threaded from checkOnCaptures, which runs after the walk: collectBlockingCaptures
// is a pure syntactic scan, and moving the classification earlier would reorder
// its diagnostics.
func (tc *typeChecker) registerCrossingBodyOwnership(body ast.StmtID, anchorSym symbols.SymbolID) {
	for _, cap := range tc.collectBlockingCaptures(body) {
		if anchorSym.IsValid() && cap.symID == anchorSym {
			// The anchor is leased to the body, not moved into it; the caller
			// keeps the handle and its drop (checkOnCaptures).
			continue
		}
		capType := tc.bindingType(cap.symID)
		// A far handle moves in like an owned value: the body is the one
		// holder of the lease from here on, and its scope exit gives it back.
		if tc.isFarType(capType) {
			tc.registerDroppableBinding(cap.symID)
			continue
		}
		// A dynamic array accepted by ON-CAP-V005 moves in the same way, under a
		// binding that never wore the `own` wrapper.
		if tc.crossingCaptureMovesAsDynamicArray(capType) {
			tc.registerDroppableBinding(cap.symID)
			continue
		}
		if !tc.isOwnType(capType) || !tc.paramTransfersOwnership(capType) {
			continue
		}
		tc.registerDroppableBinding(cap.symID)
	}
}

// crossingCaptureMovesAsDynamicArray is ON-CAP-V005's question asked without a
// diagnostic, so the gate that ACCEPTS the capture and the registration that
// makes the body drop it read one predicate rather than two that can drift.
//
// It repeats the arms classifyOnCapture reaches first, because reaching V005 is
// what it means to be accepted by it: a borrow is refused on its surface type, a
// far handle moves in its own mode, an element that may not travel is refused
// for the element, a counted block no walk reaches is refused after that, and an
// array behind a handle after that again. The attribute arms in between need no
// repeating -- `[T]` is a spelling, so it carries no attribute and is not Copy,
// and none of them can claim a dynamic array.
//
// A capture this answers true about but the gate then refuses (a view,
// ON-CAP-N007, or the holder of one, ON-CAP-N008) registers a drop for a program
// that does not compile, which costs nothing; answering false about one the gate
// accepts is the leak, so the doubt falls that way on purpose.
func (tc *typeChecker) crossingCaptureMovesAsDynamicArray(capType types.TypeID) bool {
	if tc == nil || tc.types == nil || capType == types.NoTypeID {
		return false
	}
	if tc.isReferenceType(capType) || tc.isFarType(capType) {
		return false
	}
	elem, isDynArray := tc.types.DynamicArrayElem(tc.valueType(capType))
	if !isDynArray {
		return false
	}
	if tc.result != nil && tc.result.CountedBlockStaysShared(capType) {
		return false
	}
	if tc.result != nil && tc.result.DynamicArrayStaysUnchecked(capType) {
		return false
	}
	return tc.shardMovableElement(elem)
}

// checkAnchorLeaseUses enforces what the anchor of an `on far_handle` block is
// inside the block: the receiver of its channel operation, and nothing else.
//
// The body reaches the channel through the owner-side pin, never through the
// caller's handle; the handle is not shipped and the caller keeps its drop.
// Reading the anchor as a VALUE inside the block -- binding it, passing it on,
// returning it -- would give the body a second holder of a lease it does not
// own, released twice or after the caller freed it.
func (tc *typeChecker) checkAnchorLeaseUses(body ast.StmtID, frame *onAnchorFrame) bool {
	if frame == nil || !frame.anchorSym.IsValid() {
		return true
	}
	receivers := make(map[ast.ExprID]struct{}, len(frame.remoteOps)+len(frame.opReceivers))
	for i := range frame.remoteOps {
		receivers[frame.remoteOps[i].ReceiverExpr] = struct{}{}
	}
	for _, receiver := range frame.opReceivers {
		receivers[receiver] = struct{}{}
	}
	ok := true
	tc.scanCapturedIdents(body, func(symID symbols.SymbolID, exprID ast.ExprID, span source.Span) {
		if symID != frame.anchorSym {
			return
		}
		if _, isReceiver := receivers[exprID]; isReceiver {
			return
		}
		if ok {
			tc.report(diag.SemaOnAnchorLeaseMisuse, span,
				"the `on` block holds this handle as a lease for its channel operation only; "+
					"it cannot be bound, passed on or returned inside the block -- the handle stays with the caller")
		}
		ok = false
	})
	return ok
}

// registerAsyncBodyOwnership is registerCrossingBodyOwnership for a local async
// block, and differs in exactly one predicate: there is no `own` requirement.
//
// A crossing capture must be an owned move because it travels to another shard.
// A local async block's capture is a by-value PARAMETER of the synthetic function
// the block becomes, so the question is the one a parameter already answers —
// does passing it transfer ownership — and that is paramTransfersOwnership, the
// same predicate registerDroppableParams uses.
//
// This is one half of a pair and is useless alone. The caller's binding is marked
// moved in typeExprAsync; registering here without that marking makes both sides
// drop, and marking there without registering here makes neither. RV2-DEBT-079
// and RV2-DEBT-081 record the crossing's first attempt at this pairing turning
// into an invalid read plus an invalid free for exactly that reason.
func (tc *typeChecker) registerAsyncBodyOwnership(body ast.StmtID) {
	for _, cap := range tc.collectBlockingCaptures(body) {
		if !tc.paramTransfersOwnership(tc.bindingType(cap.symID)) {
			continue
		}
		tc.registerDroppableBinding(cap.symID)
	}
}

// registerBlockingBodyOwnership is registerAsyncBodyOwnership for a `blocking`
// body, and asks the same predicate for the same reason: the body becomes a
// function whose one parameter is the packed state, and unpacking that state
// spends each field exactly as a by-value argument is spent (MIR's
// `blockingCaptureInfo.Transfers` answers from `byValueArgContract`, which is
// this predicate's lowering-side twin).
//
// The job that carries the state destroys it through its own descriptor, and
// marks it SPENT before the body runs (`rt_async_blocking.c`), so a capture the
// body only reads has exactly one owner left -- the body -- and this is where it
// is told so. Registering here without the worker's claim would be a double
// free; the claim without this registration abandons the capture once per
// execution, which is the leak this registration closes.
//
// A reference-counted capture needs the SECOND predicate, and for the same
// reason an async function's reference-counted parameter does: the state literal
// RETAINED it into the field, and "does not transfer" is the right answer only
// where a caller is still holding the reference the callee reads through. Here
// the frame is that holder, and the frame is never walked -- so the body's
// return is the one place that reference can be given back, and nothing else in
// the tree is going to do it.
func (tc *typeChecker) registerBlockingBodyOwnership(body ast.StmtID) {
	for _, cap := range tc.collectBlockingCaptures(body) {
		ty := tc.bindingType(cap.symID)
		if !tc.paramTransfersOwnership(ty) && !tc.captureIsRetainedIntoBlockingFrame(ty) {
			continue
		}
		tc.registerDroppableBinding(cap.symID)
	}
}

// captureIsRetainedIntoBlockingFrame is, for a `blocking` capture, the retention
// an `async fn` frame has for a reference-counted parameter: a reference-counted
// value the state literal read into a frame that outlives the read.
//
// It takes no function, because unlike a parameter there is no non-frame case to
// exclude -- a capture is by definition read into a frame. Both reference-counted
// families reach an accepted program: a HANDLE, and a SCALAR whose block the
// relinquishing operand makes private before the frame is submitted (the loop
// in typeExprBlocking refuses only a scalar no walk reaches, inside a map's
// table or a channel's ring; an array's buffer is walked by the runtime).
// Either way the body owes the field's one reference back, and this
// registration is what makes it pay.
//
// Deliberately not shared with registerAsyncBodyOwnership above, which still
// asks only the transfer predicate. A local `async` block's frame is reclaimed
// on its own protocol, so whether it abandons a retained capture the same way is
// a separate derivation, on its own evidence, which this change does not make.
func (tc *typeChecker) captureIsRetainedIntoBlockingFrame(id types.TypeID) bool {
	if tc.types == nil {
		return false
	}
	return tc.isDroppableType(id) && tc.types.IsRefCounted(tc.resolveAlias(id))
}

func (tc *typeChecker) isOwnType(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(id))
	return ok && tt.Kind == types.KindOwn
}
