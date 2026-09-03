package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// typeExprSpawnOn type-checks a `spawn on dst { ... }` remote spawn (
// Block 3). It shares the ExprOn node with the immediate crossing but, instead
// of running the body and yielding `TaskResult<T>`, it creates placed work and
// yields a strictly-affine `far Task<T>` handle whose payload `T` is the body's
// `ret` value type. The handle follows the same must-await-or-return lifecycle
// as a local `Task<T>` (SemaTaskNotAwaited on drop; SemaUseAfterMove on reuse).
func (tc *typeChecker) typeExprSpawnOn(id ast.ExprID, span source.Span) types.TypeID {
	data, ok := tc.builder.Exprs.On(id)
	if !ok || data == nil {
		return types.NoTypeID
	}
	checkpoint := tc.errorCheckpoint()
	siteOK := true

	// S07: `@local spawn on` mixes a local-only handle with remote placement.
	if tc.spawnOnHasAttr(data, "local") {
		tc.report(diag.SemaLocalSpawnOn, span,
			"`@local spawn on` is invalid; local spawn and remote placement are mutually exclusive")
		siteOK = false
	}

	// Note: the `crosses` effect is inferred by sema (not demanded from the
	// programmer), so `spawn on` does not require an explicit `crosses` marker
	// (X03/SEM3162 retired per the crosses-inference design change).
	tc.markCurrentFunctionMayCross()

	// A crossing body cannot open a second crossing; the uniform SemaOnNested
	// rule covers `on`/`spawn on` in any combination (matrix has no nested
	// spawn-on row, so this is a conservative superset).
	if len(tc.onCrossingStack) > 0 {
		tc.report(diag.SemaOnNested, span, "nested `on` crossing blocks are not allowed")
		siteOK = false
	}

	// Destination typing (D01-D13) with the spawn-on diagnostic set.
	destInfo := tc.checkSpawnOnDestination(data.Dest)
	if destInfo.Kind != CrossingDestinationPlacement {
		siteOK = false
	}

	// Walk the body under a crossing return context so `ret` yields the payload
	// and `return` is rejected (SEM3159); mark the frame so the shared
	// return-rejection site emits the spawn-on code.
	var returns []collectedResult
	var bareRet []source.Span
	tc.pushReturnContext(returnCtxOnCrossing, types.NoTypeID, span, &returns, &bareRet)
	tc.onCrossingStack = append(tc.onCrossingStack, onAnchorFrame{isSpawn: true})
	// The body's exits stop at this boundary rather than at the enclosing
	// function: a `ret` frees what the BODY built and what MOVED into it,
	// never the caller's bindings.
	tc.pushDropScope(true)
	tc.registerCrossingBodyOwnership(data.Body, symbols.NoSymbolID)
	tc.walkStmt(data.Body)
	tc.popDropScope()
	last := len(tc.onCrossingStack) - 1
	returnRejected := tc.onCrossingStack[last].returnRejected
	tc.onCrossingStack = tc.onCrossingStack[:last]
	tc.popReturnContext()

	// Capture legality across the shard boundary (C01-C10).
	captures, capturesOK := tc.checkOnCaptures(data.Body, symbols.NoSymbolID)
	if !capturesOK {
		siteOK = false
	}

	payload, sawRet := tc.unifyBodyResults("on-crossing", returns, bareRet)

	// B05: a `spawn on` body must always produce its result with `ret` (unlike a
	// discarded `on` crossing, a dropped `far Task` is still a lifecycle error).
	if !sawRet && !returnRejected {
		tc.report(diag.SemaSpawnOnBodyMissingRet, span, "a `spawn on` block must produce its result with `ret`")
		siteOK = false
	}

	// B06: statements after the body's `ret` are unreachable.
	if !tc.checkSpawnOnUnreachableRet(data.Body) {
		siteOK = false
	}

	resultType := tc.farTaskType(payload, span)
	if returnRejected {
		siteOK = false
	}

	// Affine must-await lifecycle: register a well-formed handle so a drop
	// without await/return reports SemaTaskNotAwaited (L01) and a return
	// transfers ownership (L04). A malformed body already reported its own
	// error, so it is not also tracked as a leaked handle.
	if tc.taskTracker != nil && sawRet && !returnRejected && resultType != types.NoTypeID {
		tc.taskTracker.SpawnTask(id, span, tc.currentScope(), tc.asyncBlockDepth > 0, false)
	}

	// A body that produced no proper result adopts the expected type so the
	// enclosing binding/return does not also report a cascading mismatch.
	if !sawRet || returnRejected {
		if expected := tc.expectedTypeForExpr(id); expected != types.NoTypeID {
			return expected
		}
	}
	if siteOK && !tc.hasErrorsSince(checkpoint) {
		tc.recordCrossingLowering(&CrossingLoweringInfo{
			Kind:           CrossingLoweringSpawnOn,
			Expr:           id,
			Span:           span,
			Body:           data.Body,
			Function:       tc.currentFnSym(),
			SuspendCapable: tc.awaitDepth > 0,
			Destination:    destInfo,
			Captures:       cloneCrossingCaptures(captures),
			PayloadType:    payload,
			ResultType:     resultType,
			HandleType:     resultType,
		})
	}
	return resultType
}

// spawnOnHasAttr reports whether the `spawn on` node carries the named attribute
// (e.g. `@local`), mirroring spawnHasAttr for the ExprOn payload.
func (tc *typeChecker) spawnOnHasAttr(data *ast.ExprOnData, name string) bool {
	if data == nil || tc.builder == nil || tc.builder.Items == nil || data.AttrCount == 0 || !data.AttrStart.IsValid() {
		return false
	}
	for _, attr := range tc.builder.Items.CollectAttrs(data.AttrStart, data.AttrCount) {
		if strings.EqualFold(tc.lookupName(attr.Name), name) {
			return true
		}
	}
	return false
}

// farTaskType builds `far Task<payload>` for a `spawn on` result, mirroring the
// `far` construction in resolveFarTypeExpr over the nominal `Task` type.
func (tc *typeChecker) farTaskType(payload types.TypeID, span source.Span) types.TypeID {
	inner := tc.taskType(payload, span)
	if inner == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	return tc.types.Intern(types.MakeFar(inner))
}

// isFarTaskType reports whether id is a `far Task<T>` remote-task handle.
func (tc *typeChecker) isFarTaskType(id types.TypeID) bool {
	return tc.isFarType(id) && tc.isTaskType(tc.farInner(id))
}

// typeFarTaskCall types a `far Task<T>` `.await()` / `.cancel()` call. Both
// operations require a `crosses` context (T07/T08), consume the affine handle
// (a second use is a use-after-move, L02/L03), and satisfy the must-await
// lifecycle. `.await()` yields `TaskResult<T>` (T10); `.cancel()` yields
// `TaskResult<nothing>` (T06). The result differs from a local `Task` extern
// method (own self vs `&self`), so it is synthesized here rather than resolved
// against the intrinsic.
func (tc *typeChecker) typeFarTaskCall(id ast.ExprID, member *ast.ExprMemberData, receiverType types.TypeID, call *ast.ExprCallData, span source.Span, method string, checkpoint int) types.TypeID {
	// Note: the `crosses` effect is inferred, so await/cancel on a `far Task<T>`
	// no longer requires an explicit `crosses` marker (T07/T08/SEM3164 retired
	// per the crosses-inference design change).
	tc.markCurrentFunctionMayCross()

	// await/cancel take no arguments, but type any present for the usual checks.
	for _, arg := range call.Args {
		tc.typeExpr(arg.Value)
	}

	// Affine: both operations consume the handle (own self). Mark the receiver
	// moved so a later use is a use-after-move, and satisfy the must-await
	// lifecycle so the handle is not reported as dropped.
	tc.observeMove(member.Target, tc.exprSpan(member.Target))
	if tc.taskTracker != nil {
		target := tc.unwrapGroupExpr(member.Target)
		if node := tc.builder.Exprs.Get(target); node != nil && node.Kind == ast.ExprIdent {
			if symID := tc.symbolForExpr(target); symID.IsValid() {
				tc.taskTracker.MarkAwaited(symID)
			}
		} else {
			tc.taskTracker.MarkAwaitedByExpr(target)
		}
	}

	// Record the call site so the backend guard can report the operation as
	// unavailable in the current backend (FUT7016 / FUT7017) until Phase 4.
	if tc.result != nil {
		if method == "cancel" {
			tc.result.FarTaskCancelSpans = append(tc.result.FarTaskCancelSpans, span)
		} else {
			tc.result.FarTaskAwaitSpans = append(tc.result.FarTaskAwaitSpans, span)
		}
	}

	payload := tc.types.Builtins().Nothing
	if method == "await" {
		if inner := tc.taskPayloadType(tc.farInner(receiverType)); inner != types.NoTypeID {
			payload = inner
		}
	}
	resultType := tc.taskResultType(payload, span)
	kind := CrossingLoweringFarTaskAwait
	if method == "cancel" {
		kind = CrossingLoweringFarTaskCancel
	}
	if !tc.hasErrorsSince(checkpoint) {
		tc.recordCrossingLowering(&CrossingLoweringInfo{
			Kind:           kind,
			Expr:           id,
			Span:           span,
			Function:       tc.currentFnSym(),
			SuspendCapable: tc.awaitDepth > 0,
			ReceiverExpr:   member.Target,
			ReceiverSymbol: tc.symbolForExpr(member.Target),
			ReceiverType:   receiverType,
			ConsumesHandle: true,
			PayloadType:    payload,
			ResultType:     resultType,
		})
	}
	return resultType
}

// checkSpawnOnDestination validates a `spawn on` destination. Unlike an `on`
// crossing, a `spawn on` accepts only a `Placement` value — every `far` handle
// is rejected (D10-D12), and non-placement values/literals are rejected (D06,
// D09) with the spawn-on diagnostic set.
func (tc *typeChecker) checkSpawnOnDestination(dest ast.ExprID) CrossingDestinationInfo {
	info := CrossingDestinationInfo{Expr: dest}
	if !dest.IsValid() {
		// `spawn on { }` / `spawn on blocking { }`: the parser already emitted
		// SYN2033 / FUT7013.
		return info
	}
	// Type-name and bare-function destinations are not value expressions and are
	// detected structurally before value typing.
	if sym := tc.destIdentSymbol(dest); sym != nil {
		switch sym.Kind {
		case symbols.SymbolType:
			tc.report(diag.SemaSpawnOnDestTypeName, tc.exprSpan(dest), "a type name is not a `spawn on` placement target")
			return info
		case symbols.SymbolFunction:
			tc.report(diag.SemaSpawnOnDestBareFn, tc.exprSpan(dest),
				"a bare function name is not a `spawn on` placement target; call it if it returns `Placement`")
			return info
		}
	}
	destType := tc.typeExpr(dest)
	if destType == types.NoTypeID {
		return info
	}
	info.Type = destType
	if tc.isFarType(destType) {
		if tc.isTaskType(tc.farInner(destType)) {
			tc.report(diag.SemaSpawnOnDestFarTask, tc.exprSpan(dest),
				"`far Task<T>` is not a `spawn on` destination; use `await()` or `cancel()`")
			return info
		}
		tc.report(diag.SemaSpawnOnDestFarHandle, tc.exprSpan(dest),
			"`far` handle destinations are invalid for `spawn on`; use `on` for immediate handle operations")
		return info
	}
	if tc.isPlacementType(destType) {
		info.Kind = CrossingDestinationPlacement
		return info
	}
	tc.report(diag.SemaSpawnOnDestNotPlacement, tc.exprSpan(dest),
		"`spawn on` destination must be a `Placement` value")
	return info
}

// checkSpawnOnUnreachableRet reports the first statement that follows the body's
// first `ret` as unreachable (B06). Only the body block's top-level statements
// are considered; a `ret` produces the remote task result, so nothing may run
// after it.
func (tc *typeChecker) checkSpawnOnUnreachableRet(body ast.StmtID) bool {
	if tc.builder == nil || tc.builder.Stmts == nil {
		return true
	}
	block := tc.builder.Stmts.Block(body)
	if block == nil {
		return true
	}
	sawRet := false
	for _, stmtID := range block.Stmts {
		stmt := tc.builder.Stmts.Get(stmtID)
		if stmt == nil {
			continue
		}
		if sawRet {
			tc.report(diag.SemaSpawnOnUnreachableAfterRet, stmt.Span, "unreachable statement after `ret` in a `spawn on` block")
			return false
		}
		if stmt.Kind == ast.StmtRet {
			sawRet = true
		}
	}
	return true
}
