package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// onAnchorFrame records the destination of an active `on dst { ... }` crossing.
// A far-handle destination anchors operations through that same handle only; a
// `Placement` destination anchors no handle (anchorSym is invalid).
type onAnchorFrame struct {
	anchorSym symbols.SymbolID
	isFar     bool
	remoteOps []CrossingRemoteOpInfo
	// isSpawn marks a `spawn on dst { ... }` remote-spawn body (Block 3) so the
	// shared `return`-rejection site reports the spawn-on code (SEM3159) instead
	// of the immediate-crossing code (SEM3147).
	isSpawn bool
	// returnRejected records that a `return` was rejected inside this crossing
	// body (SEM3147), so the missing-`ret` and result-wrapping checks stay quiet.
	returnRejected bool
}

// typeExprOn type-checks an `on dst { ... }` placement crossing (Block 2)
// and yields `TaskResult<T>` where `T` is the body's `ret` value type.
func (tc *typeChecker) typeExprOn(id ast.ExprID, span source.Span) types.TypeID {
	data, ok := tc.builder.Exprs.On(id)
	if !ok || data == nil {
		return types.NoTypeID
	}
	checkpoint := tc.errorCheckpoint()
	discarded := tc.isExprDiscarded(id)
	siteOK := true

	// Note: `on` no longer requires an explicit `crosses` marker — the effect is
	// inferred by sema (SEM3162 retired with the `crosses` grammar removal).
	tc.markCurrentFunctionMayCross()

	// ON-CROSS-N002: `on` is legal only where suspension is legal (not inside
	// a `blocking { }` body).
	if tc.blockingDepth > 0 {
		tc.report(diag.SemaOnSuspendContext, span, "`on` is allowed only where suspension is legal")
		siteOK = false
	}

	// ON-NEST-N001/N002: a crossing body cannot open a second crossing.
	if len(tc.onCrossingStack) > 0 {
		tc.report(diag.SemaOnNested, span, "nested `on` crossing blocks are not allowed")
		siteOK = false
	}

	// ON-DST rows: type the destination and derive the anchor frame.
	frame, destInfo := tc.checkOnDestination(data.Dest)
	if destInfo.Kind == CrossingDestinationNone {
		siteOK = false
	}

	// Walk the body under a crossing return context so `ret` yields the result
	// and `return` is rejected (SEM3147); track the anchor for far-handle ops.
	var returns []collectedResult
	var bareRet []source.Span
	tc.pushReturnContext(returnCtxOnCrossing, types.NoTypeID, span, &returns, &bareRet)
	tc.onCrossingStack = append(tc.onCrossingStack, frame)
	tc.walkStmt(data.Body)
	last := len(tc.onCrossingStack) - 1
	frame = tc.onCrossingStack[last]
	returnRejected := frame.returnRejected
	tc.onCrossingStack = tc.onCrossingStack[:last]
	tc.popReturnContext()

	// ON-CAP rows: capture legality across the crossing boundary.
	captures, capturesOK := tc.checkOnCaptures(data.Body)
	if !capturesOK {
		siteOK = false
	}

	payload, sawRet := tc.unifyOnBodyResults(returns, bareRet)

	// ON-BODY-N002: a value-position crossing must produce a result with `ret`.
	// A rejected `return` (SEM3147) already explains a missing result.
	if !sawRet && !discarded && !returnRejected {
		tc.report(diag.SemaOnBodyMissingRet, span, "an `on` crossing block must produce its value with `ret`")
		siteOK = false
	}

	resultType := tc.taskResultType(payload, span)
	if resultType == types.NoTypeID {
		resultType = payload
	}
	if returnRejected {
		siteOK = false
	}
	if discarded {
		if siteOK && !tc.hasErrorsSince(checkpoint) {
			tc.recordCrossingLowering(&CrossingLoweringInfo{
				Kind:           crossingLoweringKindForOnDestination(destInfo),
				Expr:           id,
				Span:           span,
				Body:           data.Body,
				Function:       tc.currentFnSym(),
				Destination:    destInfo,
				Captures:       cloneCrossingCaptures(captures),
				RemoteOps:      cloneCrossingRemoteOps(frame.remoteOps),
				PayloadType:    payload,
				ResultType:     resultType,
				SuspendCapable: tc.awaitDepth > 0,
			})
		}
		return resultType
	}

	// A body that produced no proper result adopts the expected type so the
	// enclosing binding/return does not also report a cascading mismatch.
	if !sawRet || returnRejected {
		if expected := tc.expectedTypeForExpr(id); expected != types.NoTypeID {
			return expected
		}
		return resultType
	}

	// ON-BODY-N003/N004: the crossing evaluates to `TaskResult<T>`, not `T`.
	if override, ok := tc.checkOnResultWrapping(id, payload, resultType, span); ok {
		return override
	}
	if siteOK && !tc.hasErrorsSince(checkpoint) {
		tc.recordCrossingLowering(&CrossingLoweringInfo{
			Kind:           crossingLoweringKindForOnDestination(destInfo),
			Expr:           id,
			Span:           span,
			Body:           data.Body,
			Function:       tc.currentFnSym(),
			Destination:    destInfo,
			Captures:       cloneCrossingCaptures(captures),
			RemoteOps:      cloneCrossingRemoteOps(frame.remoteOps),
			PayloadType:    payload,
			ResultType:     resultType,
			SuspendCapable: tc.awaitDepth > 0,
		})
	}
	return resultType
}

// checkOnDestination validates an `on` destination against the accepted set
// (`Placement` value, `far Channel<T>`, control-only `far TcpConn`) and returns
// the anchor frame for far-handle destinations.
func (tc *typeChecker) checkOnDestination(dest ast.ExprID) (onAnchorFrame, CrossingDestinationInfo) {
	var frame onAnchorFrame
	info := CrossingDestinationInfo{Expr: dest}
	if !dest.IsValid() {
		// `on blocking { }`: the parser already emitted FUT7012.
		return frame, info
	}
	// Type-name and bare-function destinations are not value expressions and are
	// detected structurally before value typing.
	if sym := tc.destIdentSymbol(dest); sym != nil {
		switch sym.Kind {
		case symbols.SymbolType:
			tc.report(diag.SemaOnDestTypeName, tc.exprSpan(dest), "a type name is not a placement target")
			return frame, info
		case symbols.SymbolFunction:
			tc.report(diag.SemaOnDestBareFn, tc.exprSpan(dest),
				"a function value is not a placement target; call it if it returns `Placement`")
			return frame, info
		}
	}
	destType := tc.typeExpr(dest)
	if destType == types.NoTypeID {
		return frame, info
	}
	info.Type = destType
	if tc.isFarType(destType) {
		if tc.isTaskType(tc.farInner(destType)) {
			tc.report(diag.SemaOnDestFarTask, tc.exprSpan(dest),
				"`far Task<T>` is not an `on` destination; use `await()` or `cancel()`")
			return frame, info
		}
		frame.isFar = true
		frame.anchorSym = tc.symbolForExpr(dest)
		info.Kind = CrossingDestinationFarHandle
		info.AnchorSymbol = frame.anchorSym
		info.OwnerAnchored = frame.anchorSym.IsValid()
		return frame, info
	}
	if tc.isPlacementType(destType) {
		info.Kind = CrossingDestinationPlacement
		return frame, info
	}
	tc.report(diag.SemaOnDestNotPlacement, tc.exprSpan(dest),
		"`on` destination must be a `Placement` value or an accepted `far` handle")
	return frame, info
}

func crossingLoweringKindForOnDestination(dest CrossingDestinationInfo) CrossingLoweringKind {
	if dest.Kind == CrossingDestinationFarHandle {
		return CrossingLoweringOnFarHandle
	}
	return CrossingLoweringOnPlacement
}

// destIdentSymbol resolves a bare-identifier destination to its symbol, or nil
// when the destination is not a plain identifier.
func (tc *typeChecker) destIdentSymbol(dest ast.ExprID) *symbols.Symbol {
	d := tc.unwrapGroupExpr(dest)
	node := tc.builder.Exprs.Get(d)
	if node == nil || node.Kind != ast.ExprIdent {
		return nil
	}
	return tc.symbolFromID(tc.symbolForExpr(d))
}

// unifyOnBodyResults folds the `ret` value types of a crossing body into a
// single payload type (defaulting to `nothing`), reporting whether any `ret`
// appeared.
func (tc *typeChecker) unifyOnBodyResults(returns []collectedResult, bareRet []source.Span) (types.TypeID, bool) {
	nothing := tc.types.Builtins().Nothing
	sawRet := len(returns) > 0 || len(bareRet) > 0
	payload := types.NoTypeID
	have := false
	for _, r := range returns {
		if r.typ == types.NoTypeID {
			continue
		}
		if !have {
			payload = r.typ
			have = true
			continue
		}
		switch {
		case tc.typesAssignable(payload, r.typ, true):
		case tc.typesAssignable(r.typ, payload, true):
			payload = r.typ
		default:
			tc.report(diag.SemaTypeMismatch, r.span,
				"on-crossing result type mismatch: expected %s, got %s", tc.typeLabel(payload), tc.typeLabel(r.typ))
		}
	}
	if !have {
		payload = nothing
	}
	return payload, sawRet
}

// checkOnResultWrapping rejects using the crossing result as the unwrapped `T`
// instead of `TaskResult<T>` (ON-BODY-N003/N004, SEM3149). When it fires it
// returns the expected type to suppress a cascading type-mismatch.
func (tc *typeChecker) checkOnResultWrapping(id ast.ExprID, payload, resultType types.TypeID, span source.Span) (types.TypeID, bool) {
	expected := tc.expectedTypeForExpr(id)
	if expected == types.NoTypeID || expected == tc.types.Builtins().Nothing {
		return types.NoTypeID, false
	}
	if tc.typesAssignable(expected, resultType, true) {
		return types.NoTypeID, false
	}
	if payload != types.NoTypeID && tc.typesAssignable(expected, payload, true) {
		tc.report(diag.SemaOnResultTaskResult, span, "`on` evaluates to `TaskResult<T>`, not `T`")
		return expected, true
	}
	return types.NoTypeID, false
}

// isPlacementType reports whether id is the intrinsic `Placement` type.
func (tc *typeChecker) isPlacementType(id types.TypeID) bool {
	return tc != nil && tc.types != nil && tc.types.IsRuntimePlacementType(id)
}

// farInner returns the handle type wrapped by a `far T`, or NoTypeID.
func (tc *typeChecker) farInner(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok || tt.Kind != types.KindFar {
		return types.NoTypeID
	}
	return tt.Elem
}
