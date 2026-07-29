package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) typeSelectExpr(id ast.ExprID, isRace bool, span source.Span) types.TypeID {
	if tc.builder == nil {
		return types.NoTypeID
	}

	var data *ast.ExprSelectData
	if isRace {
		if sel, ok := tc.builder.Exprs.Race(id); ok {
			data = sel
		}
	} else {
		if sel, ok := tc.builder.Exprs.Select(id); ok {
			data = sel
		}
	}
	if data == nil {
		return types.NoTypeID
	}

	if tc.awaitDepth == 0 {
		keyword := "select"
		if isRace {
			keyword = "race"
		}
		tc.report(diag.SemaIntrinsicBadContext, span, "%s can only be used in async context", keyword)
	}

	resultType := types.NoTypeID
	nothingType := types.NoTypeID
	if tc.types != nil {
		nothingType = tc.types.Builtins().Nothing
	}
	armTypes := make([]types.TypeID, len(data.Arms))
	defaultCount := 0

	// Exactly one arm wins at runtime, so each arm is a branch for move
	// tracking: a send arm's payload moves only where that arm won, stays
	// owned inside the other arms' bodies, and is maybe-moved after the
	// join (same discipline as compare arms).
	movedBefore := tc.snapshotMovedPlaces()
	movedArms := make([]map[Place]source.Span, len(data.Arms))
	armClosed := make([]bool, len(data.Arms))
	// Moves made while evaluating an arm's AWAIT expression, snapshotted
	// separately from the arm's full moved-set. Every arm's await is
	// evaluated before the select runs, so a move there — a send arm's
	// `own` payload above all — happens before any winner exists and is
	// unconditional, unlike a move in the arm's body.
	//
	// LOAD-BEARING ORDER: this is sound only because the await is typed
	// strictly BEFORE the arm body below. Fuse or reorder those two and
	// the snapshot silently starts capturing body moves, at which point
	// merging it for a closed arm stops meaning anything.
	movedAwait := make([]map[Place]source.Span, len(data.Arms))

	for i, arm := range data.Arms {
		tc.restoreMovedPlaces(movedBefore)
		if arm.IsDefault {
			defaultCount++
			if i != len(data.Arms)-1 {
				tc.report(diag.SemaError, arm.Span, "default arm must be last")
			}
		} else {
			if !tc.isSelectAwaitableExpr(arm.Await) {
				tc.report(diag.SemaTypeMismatch, tc.exprSpan(arm.Await), "select arm expects awaitable expression")
			} else {
				tc.typeSelectAwaitExpr(arm.Await)
			}
		}

		movedAwait[i] = tc.snapshotMovedPlaces()

		armResult := tc.typeExpr(arm.Result)
		armClosed[i] = tc.compareArmAbruptExit(arm.Result)
		movedArms[i] = tc.snapshotMovedPlaces()
		armTypes[i] = armResult
		if armResult != types.NoTypeID {
			switch {
			case resultType == types.NoTypeID:
				resultType = armResult
			case nothingType != types.NoTypeID && resultType == nothingType:
				resultType = armResult
			case nothingType != types.NoTypeID && armResult == nothingType:
				// nothing can flow into any other arm result
			case tc.typesAssignable(resultType, armResult, true):
				// arm result fits the current inferred type
			case tc.typesAssignable(armResult, resultType, true):
				// widen the result type to the new arm
				resultType = armResult
			default:
				tc.report(diag.SemaTypeMismatch, tc.exprSpan(arm.Result), "select arm type mismatch: expected %s, got %s", tc.typeLabel(resultType), tc.typeLabel(armResult))
			}
		}
	}

	var mergedMoves map[Place]source.Span
	for i := range data.Arms {
		if armClosed[i] {
			continue
		}
		if mergedMoves == nil {
			mergedMoves = movedArms[i]
			continue
		}
		mergedMoves = mergeMovedPlaces(mergedMoves, movedArms[i])
	}
	// A CLOSED arm (one whose body exits the FUNCTION — `return`, `panic`,
	// `exit`; note `break` leaves an arm OPEN) contributes nothing from its
	// body, because control never reaches the join through it. But its
	// await already ran: a `send(own x)` payload left the caller when the
	// select ran, before any winner existed. Without merging that back, the
	// binding stays live past the join, so the contract `send(own x)` states
	// — "if this arm loses, the value is reclaimed in the other arms and
	// cannot be used after the select" — silently did not hold for closed
	// arms, and no open arm was given the reclaim drop for that payload
	// either. Nothing merges when every arm is closed: the join is
	// unreachable, so the post-join set is moot.
	if mergedMoves != nil {
		for i := range data.Arms {
			if !armClosed[i] {
				continue
			}
			mergedMoves = mergeMovedPlaces(mergedMoves, movedAwait[i])
		}
	}
	if mergedMoves != nil {
		// Per-arm drop synthesis: a payload delivered by the winning send
		// arm is reclaimed at the end of every arm that did not deliver
		// it; after the join every such binding is in the union moved-set,
		// so using it stays a use-of-moved error.
		for i := range data.Arms {
			// LOAD-BEARING: a closed arm must never receive arm drops.
			// Its own abrupt exit already runs recordEarlyExitDrops
			// against this arm's moved-set, so adding an ArmDrops entry
			// here would drop the other arms' payloads a second time.
			// The merge above deliberately DOES read closed arms (their
			// await half only) while this loop deliberately does not.
			if armClosed[i] {
				continue
			}
			drops := tc.oneSidedDroppables(movedArms[i], mergedMoves)
			if len(drops) == 0 {
				continue
			}
			if tc.result.ArmDropsExpr == nil {
				tc.result.ArmDropsExpr = make(map[ast.ExprID][]symbols.SymbolID)
			}
			tc.result.ArmDropsExpr[data.Arms[i].Result] = drops
		}
		tc.movedPlaces = mergedMoves
	} else {
		tc.movedPlaces = movedBefore
	}

	if defaultCount > 1 {
		tc.report(diag.SemaError, span, "default arm must appear at most once")
	}

	if resultType != types.NoTypeID {
		for i, arm := range data.Arms {
			tc.recordNumericWidening(arm.Result, armTypes[i], resultType)
		}
	}
	tc.checkSelectFarArms(id, data, span)
	return resultType
}

// checkSelectFarArms classifies the select's arms for the remote-select
// vertical: a select containing ANY far channel arm ships to the arms'
// owner shard as one anchored block, so it must contain ONLY far channel
// arms — local channels, tasks, timeouts, and default arms cannot join it
// (SEM3176 names the split). Owner-shard sameness across the far arms is
// runtime data inside the tokens and is enforced by the runtime call; the
// clean shape is recorded as a ChannelSelect crossing whose reply is the
// winner index.
func (tc *typeChecker) checkSelectFarArms(id ast.ExprID, data *ast.ExprSelectData, span source.Span) {
	if tc.types == nil {
		return
	}
	var farOps []CrossingRemoteOpInfo
	mixed := false
	for _, arm := range data.Arms {
		if arm.IsDefault {
			mixed = true
			continue
		}
		op, isFar := tc.selectFarArmInfo(arm.Await)
		if isFar {
			farOps = append(farOps, op)
		} else {
			mixed = true
		}
	}
	if len(farOps) == 0 {
		return
	}
	if mixed {
		for _, arm := range data.Arms {
			if arm.IsDefault {
				tc.report(diag.SemaSelectFarArmsSingleOwner, arm.Span,
					"a `select` with `far` channel arms ships to their owner shard as one block "+
						"and cannot take a default arm; give the remote channels their own `select`")
				continue
			}
			if _, isFar := tc.selectFarArmInfo(arm.Await); !isFar {
				tc.report(diag.SemaSelectFarArmsSingleOwner, arm.Span,
					"a `select` with `far` channel arms ships to their owner shard as one block; "+
						"this arm cannot join it — keep local channel, task, and timeout arms in "+
						"a local `select`, or split into one `select` per owner")
			}
		}
		return
	}
	indexType := tc.types.Builtins().Int32
	tc.recordCrossingLowering(&CrossingLoweringInfo{
		Kind:           CrossingLoweringChannelSelect,
		Expr:           id,
		Span:           span,
		Function:       tc.currentFnSym(),
		SuspendCapable: tc.awaitDepth > 0,
		RemoteOps:      farOps,
		PayloadType:    indexType,
		ResultType:     indexType,
	})
}

// selectFarArmInfo classifies one select arm's await expression: a recv or
// send member call whose receiver is a far channel yields the crossing
// remote-op record for the arm.
func (tc *typeChecker) selectFarArmInfo(exprID ast.ExprID) (CrossingRemoteOpInfo, bool) {
	exprID = tc.unwrapSelectAwaitExpr(exprID)
	if !exprID.IsValid() || tc.builder == nil {
		return CrossingRemoteOpInfo{}, false
	}
	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil || expr.Kind != ast.ExprCall {
		return CrossingRemoteOpInfo{}, false
	}
	call, ok := tc.builder.Exprs.Call(exprID)
	if !ok || call == nil {
		return CrossingRemoteOpInfo{}, false
	}
	member, ok := tc.builder.Exprs.Member(call.Target)
	if !ok || member == nil {
		return CrossingRemoteOpInfo{}, false
	}
	name := tc.lookupName(member.Field)
	if name != "recv" && name != "send" {
		return CrossingRemoteOpInfo{}, false
	}
	recvType := tc.typeExpr(member.Target)
	if tc.channelPayloadType(tc.farInner(recvType)) == types.NoTypeID {
		return CrossingRemoteOpInfo{}, false
	}
	op := CrossingRemoteOpInfo{
		Method:         name,
		Span:           tc.exprSpan(exprID),
		ReceiverExpr:   member.Target,
		ReceiverSymbol: tc.symbolForExpr(member.Target),
		ReceiverType:   recvType,
	}
	if name == "send" && len(call.Args) == 1 {
		op.ValueExpr = call.Args[0].Value
	}
	return op, true
}

func (tc *typeChecker) isSelectAwaitableExpr(exprID ast.ExprID) bool {
	if !exprID.IsValid() || tc.builder == nil {
		return false
	}
	exprID = tc.unwrapSelectAwaitExpr(exprID)
	if !exprID.IsValid() {
		return false
	}
	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return false
	}
	switch expr.Kind {
	case ast.ExprCall:
		call, ok := tc.builder.Exprs.Call(exprID)
		if !ok || call == nil {
			return false
		}
		if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
			name := tc.lookupName(member.Field)
			recvType := tc.typeExpr(member.Target)
			// Far channel arms are awaitable too: the remote-select vertical
			// ships them owner-side (checkSelectFarArms owns the shape rule).
			isChannelArm := tc.isChannelType(recvType) ||
				tc.channelPayloadType(tc.farInner(recvType)) != types.NoTypeID
			switch name {
			case "await":
				return len(call.Args) == 0 && tc.isTaskType(recvType)
			case "recv":
				return len(call.Args) == 0 && isChannelArm
			case "send":
				return len(call.Args) == 1 && isChannelArm
			}
		}
		if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
			name := tc.lookupName(ident.Name)
			switch name {
			case "await":
				if len(call.Args) != 1 {
					return false
				}
				argType := tc.typeExpr(call.Args[0].Value)
				return tc.isTaskType(argType)
			case "timeout":
				if len(call.Args) != 2 {
					return false
				}
				argType := tc.typeExpr(call.Args[0].Value)
				return tc.isTaskType(argType)
			}
		}
	case ast.ExprAwait:
		if data, ok := tc.builder.Exprs.Await(exprID); ok && data != nil {
			return tc.isTaskType(tc.typeExpr(data.Value))
		}
	}
	return false
}

func (tc *typeChecker) typeSelectAwaitExpr(exprID ast.ExprID) {
	if !exprID.IsValid() || tc.builder == nil {
		return
	}
	exprID = tc.unwrapSelectAwaitExpr(exprID)
	if !exprID.IsValid() {
		return
	}
	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return
	}

	switch expr.Kind {
	case ast.ExprCall:
		call, ok := tc.builder.Exprs.Call(exprID)
		if !ok || call == nil {
			return
		}
		if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
			name := tc.lookupName(member.Field)
			switch name {
			case "await":
				recvType := tc.typeExpr(member.Target)
				if !tc.isTaskType(recvType) {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(member.Target), "await expects Task<T>, got %s", tc.typeLabel(recvType))
				}
				if tc.taskTracker != nil {
					tc.trackTaskAwait(member.Target)
				}
			case "recv":
				recvType := tc.typeExpr(member.Target)
				if !tc.isChannelType(recvType) &&
					tc.channelPayloadType(tc.farInner(recvType)) == types.NoTypeID {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(member.Target), "recv expects Channel<T>, got %s", tc.typeLabel(recvType))
				}
			case "send":
				recvType := tc.typeExpr(member.Target)
				if !tc.isChannelType(recvType) &&
					tc.channelPayloadType(tc.farInner(recvType)) == types.NoTypeID {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(member.Target), "send expects Channel<T>, got %s", tc.typeLabel(recvType))
				}
				if len(call.Args) > 0 {
					tc.typeExpr(call.Args[0].Value)
					tc.checkChannelSendValue(call.Args[0].Value, tc.exprSpan(call.Args[0].Value))
					tc.checkSelectSendPayloadOwnership(recvType, call.Args[0].Value)
				}
			}
			return
		}

		if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
			name := tc.lookupName(ident.Name)
			switch name {
			case "await":
				if len(call.Args) == 0 {
					return
				}
				argType := tc.typeExpr(call.Args[0].Value)
				if !tc.isTaskType(argType) {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(call.Args[0].Value), "await expects Task<T>, got %s", tc.typeLabel(argType))
				}
				if tc.taskTracker != nil {
					tc.trackTaskAwait(call.Args[0].Value)
				}
			case "timeout":
				if len(call.Args) == 0 {
					return
				}
				argType := tc.typeExpr(call.Args[0].Value)
				if !tc.isTaskType(argType) {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(call.Args[0].Value), "timeout expects Task<T>, got %s", tc.typeLabel(argType))
				}
				if len(call.Args) > 1 {
					tc.typeExpr(call.Args[1].Value)
				}
			}
		}
	case ast.ExprAwait:
		if data, ok := tc.builder.Exprs.Await(exprID); ok && data != nil {
			argType := tc.typeExpr(data.Value)
			if !tc.isTaskType(argType) {
				tc.report(diag.SemaTypeMismatch, tc.exprSpan(data.Value), "await expects Task<T>, got %s", tc.typeLabel(argType))
			}
			if tc.taskTracker != nil {
				tc.trackTaskAwait(data.Value)
			}
		}
	}
}

// checkSelectSendPayloadOwnership applies direct-send ownership rules to a
// select send arm, local or far. The winning arm delivers the payload, so
// ownership visibly leaves the caller: the payload must match the channel
// payload type, a non-copy payload must be spelled `own <binding>`, and the
// move is observed in the current arm's moved-set so the losing arms
// reclaim the value and any use after the select join is a use-of-moved
// error. chanType may be a local Channel<T> or a far channel wrapping one;
// the payload resolves through whichever shape applies.
func (tc *typeChecker) checkSelectSendPayloadOwnership(chanType types.TypeID, valueExpr ast.ExprID) {
	if !valueExpr.IsValid() || tc.builder == nil {
		return
	}
	payloadType := tc.channelPayloadType(chanType)
	if payloadType == types.NoTypeID {
		payloadType = tc.channelPayloadType(tc.farInner(chanType))
	}
	span := tc.exprSpan(valueExpr)
	valueType := tc.result.ExprTypes[valueExpr]
	if payloadType != types.NoTypeID && valueType != types.NoTypeID &&
		!tc.typesAssignable(payloadType, tc.valueType(valueType), true) &&
		!tc.typesAssignable(tc.valueType(payloadType), tc.valueType(valueType), true) {
		tc.report(diag.SemaTypeMismatch, span, "select send arm payload: expected %s, got %s",
			tc.typeLabel(payloadType), tc.typeLabel(valueType))
		return
	}
	checkType := valueType
	if checkType == types.NoTypeID {
		checkType = payloadType
	}
	if checkType == types.NoTypeID || tc.isCopyType(tc.valueType(checkType)) {
		return
	}
	unary, isUnary := tc.builder.Exprs.Unary(valueExpr)
	if !isUnary || unary == nil || unary.Op != ast.ExprUnaryOwn {
		tc.report(diag.SemaSelectSendOwnMarker, span,
			"select send arm takes ownership of its payload: write `send(own ...)`; if this arm loses, the value is reclaimed in the other arms and cannot be used after the select")
		return
	}
	desc, ok := tc.resolvePlace(unary.Operand)
	if !ok || !desc.Base.IsValid() || len(desc.Segments) != 0 {
		tc.report(diag.SemaSelectSendPayloadNotBinding, span,
			"select send payload must be a whole owned binding so the losing arms can reclaim it: bind the value first (`let job = ...;` then `send(own job)`)")
		return
	}
	// A binding that merely ALIASES container-owned state (its value came
	// from a field, element, or deref read) is not this arm's to give away.
	// Both outcomes corrupt: if the arm wins, the payload is delivered while
	// the container still owns it; if it loses, the reclaim drop synthesized
	// into the other arms frees memory the container still owns. Rejected
	// here for the same reason a projection is rejected just above — this is
	// that hazard one binding removed.
	if tc.isAliasedBinding(desc.Base) {
		tc.report(diag.SemaSelectSendPayloadNotBinding, span,
			"select send payload is borrowed from a container and cannot be given away: "+
				"this binding reads out of a field, element, or deref, so the container still owns "+
				"the value — copy or move it out of the container first, then `send(own ...)` that")
		return
	}
	// This move happens while the arm's AWAIT is being typed, which is
	// what makes it unconditional: the payload is handed to rt_select_poll
	// (or shipped in the far arm table) before any winner exists. The arm
	// merge in typeSelectExpr relies on that position to honour the move
	// even for an arm whose body exits abruptly.
	tc.observeMove(valueExpr, span)
}

func (tc *typeChecker) unwrapSelectAwaitExpr(exprID ast.ExprID) ast.ExprID {
	for exprID.IsValid() {
		expr := tc.builder.Exprs.Get(exprID)
		if expr == nil {
			return ast.NoExprID
		}
		switch expr.Kind {
		case ast.ExprGroup:
			group, ok := tc.builder.Exprs.Group(exprID)
			if !ok || group == nil || !group.Inner.IsValid() {
				return ast.NoExprID
			}
			exprID = group.Inner
		case ast.ExprBlock:
			return tc.selectAwaitExprFromBlock(exprID)
		default:
			return exprID
		}
	}
	return ast.NoExprID
}

func (tc *typeChecker) selectAwaitExprFromBlock(exprID ast.ExprID) ast.ExprID {
	block, ok := tc.builder.Exprs.Block(exprID)
	if !ok || block == nil || len(block.Stmts) != 1 {
		return ast.NoExprID
	}
	stmt := tc.builder.Stmts.Get(block.Stmts[0])
	if stmt == nil {
		return ast.NoExprID
	}
	switch stmt.Kind {
	case ast.StmtReturn:
		ret := tc.builder.Stmts.Return(block.Stmts[0])
		if ret == nil || !ret.Expr.IsValid() {
			return ast.NoExprID
		}
		return ret.Expr
	case ast.StmtRet:
		ret := tc.builder.Stmts.Ret(block.Stmts[0])
		if ret == nil || !ret.Expr.IsValid() {
			return ast.NoExprID
		}
		return ret.Expr
	default:
		return ast.NoExprID
	}
}
