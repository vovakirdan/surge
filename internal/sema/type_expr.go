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
