package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
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

	for i, arm := range data.Arms {
		if arm.IsDefault {
			defaultCount++
			if i != len(data.Arms)-1 {
				tc.report(diag.SemaError, arm.Span, "default arm must be last")
			}
		} else {
			tc.typeExpr(arm.Await)
			if !tc.isSelectAwaitableExpr(arm.Await) {
				tc.report(diag.SemaTypeMismatch, tc.exprSpan(arm.Await), "select arm expects awaitable expression")
			}
		}

		armResult := tc.typeExpr(arm.Result)
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

	if defaultCount > 1 {
		tc.report(diag.SemaError, span, "default arm must appear at most once")
	}

	if resultType != types.NoTypeID {
		for i, arm := range data.Arms {
			tc.recordNumericWidening(arm.Result, armTypes[i], resultType)
		}
	}
	return resultType
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
			switch name {
			case "await":
				return len(call.Args) == 0 && tc.isTaskType(recvType)
			case "recv":
				return len(call.Args) == 0 && tc.isChannelType(recvType)
			case "send":
				return len(call.Args) == 1 && tc.isChannelType(recvType)
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
	if stmt == nil || stmt.Kind != ast.StmtReturn {
		return ast.NoExprID
	}
	ret := tc.builder.Stmts.Return(block.Stmts[0])
	if ret == nil || !ret.Expr.IsValid() {
		return ast.NoExprID
	}
	return ret.Expr
}
