package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// typeFarHandleCall types a method call whose receiver is a `far T` handle and
// returns the call's result type. Outside a crossing it keeps Block 1's
// rejection (SEM3142). Inside an `on` crossing it enforces the owner anchor
// (SEM3150) and, for `far TcpConn`, the control-only whitelist (SEM3151).
func (tc *typeChecker) typeFarHandleCall(member *ast.ExprMemberData, receiverType types.TypeID, call *ast.ExprCallData, span source.Span) types.TypeID {
	methodName := tc.lookupName(member.Field)

	// Outside any crossing: acting through a far handle is rejected (Block 1).
	if len(tc.onCrossingStack) == 0 {
		tc.report(diag.SemaFarLocalOp, span, "operation on %s requires an accepted remote context", tc.typeLabel(receiverType))
		return types.NoTypeID
	}

	// The destination far handle anchors operations through that same handle only.
	frame := tc.onCrossingStack[len(tc.onCrossingStack)-1]
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
		return tc.typeAnchoredChannelOp(methodName, element, member, receiverType, recvSym, call, span)
	}

	// Accepted anchored operation: type argument expressions for the usual
	// checks; the crossing itself is compile-only.
	for _, arg := range call.Args {
		tc.typeExpr(arg.Value)
	}
	tc.recordActiveOnRemoteOp(CrossingRemoteOpInfo{
		Method:         methodName,
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
		// The local signature takes `own T`: moved owned values match the
		// element on their nominal type.
		if argType != types.NoTypeID && !tc.typesAssignable(element, argType, true) &&
			!tc.typesAssignable(element, tc.valueType(argType), true) {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(call.Args[0].Value),
				"anchored `send` value must be `%s` (the channel element type), got `%s`",
				tc.typeLabel(element), tc.typeLabel(argType))
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
}

// checkOnCaptures enforces capture legality for values crossing an `on`
// boundary (ON-CAP rows). The capture-effect diagnostics are owned by Block 4;
// Block 2 emits them at the crossing site so the invariants hold now.
func (tc *typeChecker) checkOnCaptures(body ast.StmtID) ([]CrossingCaptureInfo, bool) {
	ok := true
	var captures []CrossingCaptureInfo
	for _, cap := range tc.collectBlockingCaptures(body) {
		capType := tc.bindingType(cap.symID)
		if capType == types.NoTypeID {
			continue
		}
		mode, verdict, accepted := tc.classifyOnCapture(capType, cap.span)
		if !accepted {
			ok = false
			continue
		}
		captures = append(captures, CrossingCaptureInfo{
			Symbol:  cap.symID,
			Expr:    cap.exprID,
			Span:    cap.span,
			Type:    capType,
			Mode:    mode,
			Verdict: verdict,
		})
	}
	return captures, ok
}

func (tc *typeChecker) classifyOnCapture(capType types.TypeID, span source.Span) (CrossingCaptureMode, CrossingCaptureVerdict, bool) {
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
	// Copy values, including `Placement`, may cross freely (ON-CAP-V001/V004).
	if !owned && tc.result != nil && tc.result.IsCopyType(capType) {
		if tc.isPlacementType(capType) {
			return CrossingCaptureCopy, CrossingCapturePlacementCopy, true
		}
		return CrossingCaptureCopy, CrossingCaptureCopyValue, true
	}
	// Owned captures are judged on their nominal type (strip the `own` wrapper).
	nominal := tc.valueType(capType)
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
	default:
		// ON-CAP-N005: unmarked owned user values are not shard-movable.
		tc.report(diag.SemaCrossNotShardMovable, span,
			"this owned value is not shard-movable; mark its type `@shard_movable` to cross it")
		return 0, 0, false
	}
}

func (tc *typeChecker) isOwnType(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(id))
	return ok && tt.Kind == types.KindOwn
}
