package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
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

	// `far TcpConn` is control-only in Epic 11: `close()` is the sole operation.
	if tc.typeNameIs(tc.farInner(receiverType), "TcpConn") && methodName != "close" {
		tc.report(diag.SemaOnTcpRemoteIO, span, "remote socket I/O through `far TcpConn` is not supported yet")
		return types.NoTypeID
	}

	// Accepted anchored operation: type argument expressions for the usual
	// checks; the crossing itself is compile-only in Epic 11.
	for _, arg := range call.Args {
		tc.typeExpr(arg.Value)
	}
	return tc.types.Builtins().Nothing
}

// checkOnCaptures enforces capture legality for values crossing an `on`
// boundary (ON-CAP rows). The capture-effect diagnostics are owned by Block 4;
// Block 2 emits them at the crossing site so the invariants hold now.
func (tc *typeChecker) checkOnCaptures(body ast.StmtID) {
	for _, cap := range tc.collectBlockingCaptures(body) {
		capType := tc.bindingType(cap.symID)
		if capType == types.NoTypeID {
			continue
		}
		tc.classifyOnCapture(capType, cap.span)
	}
}

func (tc *typeChecker) classifyOnCapture(capType types.TypeID, span source.Span) {
	// Borrowed captures are rejected on the surface type (ON-CAP-N001/N002).
	if tc.isReferenceType(capType) {
		tc.report(diag.SemaCrossBorrowCapture, span, "borrowed values cannot cross shard boundaries")
		return
	}
	// Far handles move in (affine); the remote resource stays put (ON-CAP-V003).
	if tc.isFarType(capType) {
		return
	}
	// Copy values, including `Placement`, may cross freely (ON-CAP-V001/V004).
	if tc.result != nil && tc.result.IsCopyType(capType) {
		return
	}
	// Owned captures are judged on their nominal type (strip the `own` wrapper).
	nominal := tc.valueType(capType)
	switch {
	case tc.typeHasAttr(nominal, "shard_pinned"):
		// ON-CAP-N004: shard-pinned resources cannot cross as owned values.
		tc.report(diag.SemaCrossPinnedCapture, span,
			"this operation would move a shard-pinned resource; use a far handle or explicit migration")
	case tc.typeHasAttr(nominal, "nosend"):
		// ON-CAP-N003: `@nosend` forbids crossing task/shard boundaries.
		tc.report(diag.SemaCrossNosendCapture, span,
			"`@nosend` values cannot cross task or shard boundaries outside `@local spawn`")
	case tc.typeHasAttr(nominal, "shard_movable"):
		// ON-CAP-V002: owned shard-movable values may cross.
	default:
		// ON-CAP-N005: unmarked owned user values are not shard-movable.
		tc.report(diag.SemaCrossNotShardMovable, span,
			"this owned value is not shard-movable; mark its type `@shard_movable` to cross it")
	}
}
