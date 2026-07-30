package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/trace"
	"surge/internal/types"
)

func (tc *typeChecker) typeExpr(id ast.ExprID) types.TypeID {
	if !id.IsValid() {
		return types.NoTypeID
	}
	if ty, ok := tc.result.ExprTypes[id]; ok {
		return ty
	}
	expr := tc.builder.Exprs.Get(id)
	if expr == nil {
		return types.NoTypeID
	}

	tc.exprDepth++
	defer func() { tc.exprDepth-- }()

	var span *trace.Span
	if tc.tracer != nil && tc.tracer.Level() >= trace.LevelDebug && tc.exprDepth <= 20 {
		span = trace.Begin(tc.tracer, trace.ScopeNode, "type_expr", 0)
		span.WithExtra("kind", fmt.Sprintf("%d", expr.Kind))
		span.WithExtra("depth", fmt.Sprintf("%d", tc.exprDepth))
	}

	var ty types.TypeID
	defer func() {
		if span != nil {
			if ty != types.NoTypeID {
				span.WithExtra("result", tc.typeLabel(ty))
			}
			span.End("")
		}
	}()

	// A projection READ asks about the place it names, not about its base: with
	// `o.inner` given away, `o.label` is still there to read. Only the outermost
	// projection asks — inner levels are the base chain of this one — and an
	// assignment target is not a read at all.
	//
	// Asked BEFORE the expression is typed, because typing it can perform the
	// very move being asked about. An index whose `__index` takes `self` by value
	// consumes its receiver while being typed, so a check afterwards found the
	// move this expression had just made and reported the expression as a use of
	// what it was itself giving away — `b[0]` on such a type was rejected outright
	// (RV2-DEBT-090). Reading before typing also matches evaluation order: the
	// place is read at this point, and whatever the operation then does with it
	// happens after.
	tc.checkProjectionReadBeforeEvaluation(id, expr.Kind, expr.Span)

	switch expr.Kind {
	case ast.ExprIdent:
		ty = tc.typeExprIdent(id, expr.Span)
	case ast.ExprLit:
		ty = tc.typeExprLiteral(id)
	case ast.ExprGroup:
		ty = tc.typeExprGroup(id)
	case ast.ExprUnary:
		if data, ok := tc.builder.Exprs.Unary(id); ok && data != nil {
			ty = tc.typeUnary(id, expr.Span, data)
		}
	case ast.ExprBinary:
		if data, ok := tc.builder.Exprs.Binary(id); ok && data != nil {
			ty = tc.typeBinary(id, expr.Span, data)
		}
	case ast.ExprTernary:
		ty = tc.typeExprTernary(id, expr.Span)
	case ast.ExprCall:
		if call, ok := tc.builder.Exprs.Call(id); ok && call != nil {
			ty = tc.typeExprCall(id, expr.Span, call)
		}
	case ast.ExprArray:
		ty = tc.typeExprArray(id, expr.Span)
	case ast.ExprMap:
		ty = tc.typeExprMap(id, expr.Span)
	case ast.ExprRangeLit:
		ty = tc.typeExprRange(id, expr.Span)
	case ast.ExprTuple:
		ty = tc.typeExprTuple(id)
	case ast.ExprIndex:
		ty = tc.typeExprIndex(id, expr.Span)
	case ast.ExprMember:
		ty = tc.typeExprMember(id, expr.Span)
	case ast.ExprTupleIndex:
		ty = tc.typeExprTupleIndex(id, expr.Span)
	case ast.ExprAwait:
		ty = tc.typeExprAwait(id, expr.Span)
	case ast.ExprCast:
		ty = tc.typeExprCast(id, expr.Span)
	case ast.ExprCompare:
		ty = tc.typeExprCompare(id, expr.Span)
	case ast.ExprSelect:
		ty = tc.typeSelectExpr(id, false, expr.Span)
	case ast.ExprRace:
		ty = tc.typeSelectExpr(id, true, expr.Span)
	case ast.ExprParallel:
		if par, ok := tc.builder.Exprs.Parallel(id); ok && par != nil {
			tc.reporter.Report(diag.FutParallelNotSupported, diag.SevError, expr.Span, "'parallel' requires multi-threading (v2+)", nil, nil)
		}
	case ast.ExprAsync:
		ty = tc.typeExprAsync(id, expr.Span)
	case ast.ExprBlocking:
		ty = tc.typeExprBlocking(id, expr.Span)
	case ast.ExprOn:
		// Single divergence gate for the shared ExprOn node: `spawn on dst { ... }`
		// (Spawn flag set) is the Block 3 remote-spawn variant yielding `far Task<T>`;
		// plain `on dst { ... }` is the Block 2 immediate crossing yielding `TaskResult<T>`.
		if data, okOn := tc.builder.Exprs.On(id); okOn && data != nil && data.Spawn {
			ty = tc.typeExprSpawnOn(id, expr.Span)
		} else {
			ty = tc.typeExprOn(id, expr.Span)
		}
	case ast.ExprTask:
		if task, ok := tc.builder.Exprs.Task(id); ok && task != nil {
			ty = tc.typeSpawnExpr(id, expr.Span, task.Value, false)
		}
	case ast.ExprSpawn:
		if spawn, ok := tc.builder.Exprs.Spawn(id); ok && spawn != nil {
			ty = tc.typeSpawnExpr(id, expr.Span, spawn.Value, tc.spawnHasAttr(id, "local"))
		}
	case ast.ExprSpread:
		tc.typeExprSpread(id)
	case ast.ExprStruct:
		ty = tc.typeExprStruct(id, expr.Span)
	case ast.ExprBlock:
		if block, ok := tc.builder.Exprs.Block(id); ok && block != nil {
			ty = tc.typeBlockExpr(id, block)
		}
	default:
	}

	tc.result.ExprTypes[id] = ty
	tc.noteTempCandidate(id, expr.Kind, ty)
	return ty
}

// checkProjectionReadBeforeEvaluation asks the use-after-move question for a
// projection READ, before the expression it names is evaluated. See the call
// site for why the order matters.
func (tc *typeChecker) checkProjectionReadBeforeEvaluation(id ast.ExprID, kind ast.ExprKind, span source.Span) {
	if tc.placeBaseDepth != 0 || tc.assignmentLHSDepth != 0 {
		return
	}
	switch kind {
	case ast.ExprMember, ast.ExprIndex, ast.ExprTupleIndex:
		tc.checkPlaceUseAfterMove(id, span)
	case ast.ExprUnary:
		// A dereference is a projection too — `resolvePlace` has always resolved
		// it — so `*p` asks about the place it names rather than relying on `p`
		// being checked as a value.
		if unary, ok := tc.builder.Exprs.Unary(id); ok && unary != nil && unary.Op == ast.ExprUnaryDeref {
			tc.checkPlaceUseAfterMove(id, span)
		}
	}
}

// typeExprAsPlaceBase types the TARGET of a projection. The target is walked to
// reach a place, not read as a value, so the move checks inside it stay quiet
// and the enclosing projection asks the question once for the whole path.
//
// Scoped to the target alone on purpose: `o.field[idx]` must still check `idx`,
// which is an ordinary value read that happens to sit inside a projection.
func (tc *typeChecker) typeExprAsPlaceBase(id ast.ExprID) types.TypeID {
	tc.placeBaseDepth++
	ty := tc.typeExpr(id)
	tc.placeBaseDepth--
	return ty
}

func (tc *typeChecker) typeExprAssignLHS(id ast.ExprID) types.TypeID {
	tc.assignmentLHSDepth++
	ty := tc.typeExpr(id)
	tc.assignmentLHSDepth--
	if tc.builder != nil && tc.isReferenceType(ty) {
		exprID := tc.unwrapGroupExpr(id)
		if idx, ok := tc.builder.Exprs.Index(exprID); ok && idx != nil {
			if elem, ok := tc.elementType(ty); ok {
				return elem
			}
		}
	}
	return ty
}

func (tc *typeChecker) typeSpawnExpr(exprID ast.ExprID, span source.Span, value ast.ExprID, local bool) types.TypeID {
	exprType := tc.typeExpr(value)
	tc.observeMove(value, tc.exprSpan(value))
	tc.enforceSpawn(value, local)

	var ty types.TypeID
	if tc.isTaskType(exprType) {
		ty = exprType
		if tc.isCheckpointCall(value) {
			tc.warn(diag.SemaSpawnCheckpointUseless, span,
				"spawn checkpoint() has no effect; use checkpoint().await() or ignore the result")
		}
	} else if exprType != types.NoTypeID {
		tc.report(diag.SemaSpawnNotTask, span,
			"spawn requires async function call or Task<T> expression, got %s",
			tc.typeLabel(exprType))
		ty = types.NoTypeID
	}

	if tc.taskTracker != nil && ty != types.NoTypeID {
		inAsyncBlock := tc.asyncBlockDepth > 0
		tc.taskTracker.SpawnTask(exprID, span, tc.currentScope(), inAsyncBlock, local)
	}

	return ty
}
