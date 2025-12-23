package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) markArrayViewExpr(expr ast.ExprID) {
	if !expr.IsValid() || tc.arrayViewExprs == nil {
		return
	}
	tc.arrayViewExprs[expr] = struct{}{}
}

func (tc *typeChecker) markArrayViewBinding(symID symbols.SymbolID, isView bool) {
	if !symID.IsValid() || tc.arrayViewBindings == nil {
		return
	}
	if isView {
		tc.arrayViewBindings[symID] = struct{}{}
		return
	}
	delete(tc.arrayViewBindings, symID)
}

func (tc *typeChecker) isArrayViewExpr(expr ast.ExprID) bool {
	if !expr.IsValid() {
		return false
	}
	expr = tc.unwrapArrayViewExpr(expr)
	if expr.IsValid() && tc.arrayViewExprs != nil {
		if _, ok := tc.arrayViewExprs[expr]; ok {
			return true
		}
	}
	symID := tc.symbolForExpr(expr)
	if symID.IsValid() {
		if _, ok := tc.arrayViewBindings[symID]; ok {
			return true
		}
	}
	return false
}

func (tc *typeChecker) unwrapArrayViewExpr(expr ast.ExprID) ast.ExprID {
	if !expr.IsValid() || tc.builder == nil || tc.builder.Exprs == nil {
		return expr
	}
	for {
		if group, ok := tc.builder.Exprs.Group(expr); ok && group != nil {
			expr = group.Inner
			continue
		}
		if unary, ok := tc.builder.Exprs.Unary(expr); ok && unary != nil {
			if unary.Op == ast.ExprUnaryRef || unary.Op == ast.ExprUnaryRefMut {
				expr = unary.Operand
				continue
			}
		}
		break
	}
	return expr
}

func (tc *typeChecker) isArrayOrFixedType(id types.TypeID) bool {
	if id == types.NoTypeID {
		return false
	}
	base := tc.valueType(id)
	if base == types.NoTypeID {
		return false
	}
	if tc.isArrayType(base) {
		return true
	}
	_, _, ok := tc.arrayFixedInfo(base)
	return ok
}

func (tc *typeChecker) isArrayRangeIndex(container, index types.TypeID) bool {
	if container == types.NoTypeID || index == types.NoTypeID || tc.types == nil {
		return false
	}
	base := tc.valueType(container)
	if base == types.NoTypeID {
		return false
	}
	if _, ok := tc.arrayElemType(base); !ok {
		if _, _, ok := tc.arrayFixedInfo(base); !ok {
			return false
		}
	}
	payload, ok := tc.rangePayload(index)
	if !ok {
		return false
	}
	intType := tc.types.Builtins().Int
	return intType != types.NoTypeID && tc.sameType(payload, intType)
}

func (tc *typeChecker) reportArrayViewResize(span source.Span, op string) {
	if op == "" {
		tc.report(diag.SemaTypeMismatch, span, "array view is not resizable")
		return
	}
	tc.report(diag.SemaTypeMismatch, span, "array view is not resizable; %s requires an owned array", op)
}

func (tc *typeChecker) updateArrayViewBindingFromAssign(left, right ast.ExprID) {
	if !left.IsValid() || !right.IsValid() || tc.builder == nil || tc.builder.Exprs == nil {
		return
	}
	if _, ok := tc.builder.Exprs.Ident(left); !ok {
		return
	}
	symID := tc.symbolForExpr(left)
	if !symID.IsValid() {
		return
	}
	tc.markArrayViewBinding(symID, tc.isArrayViewExpr(right))
}

func (tc *typeChecker) checkArrayViewResizeMethod(receiverExpr ast.ExprID, name string, receiverType types.TypeID, span source.Span) {
	if name != "push" && name != "pop" && name != "reserve" {
		return
	}
	if !tc.isArrayType(tc.valueType(receiverType)) {
		return
	}
	if !tc.isArrayViewExpr(receiverExpr) {
		return
	}
	opSpan := span
	if recvSpan := tc.exprSpan(receiverExpr); recvSpan != (source.Span{}) {
		opSpan = recvSpan
	}
	tc.reportArrayViewResize(opSpan, name)
}

func (tc *typeChecker) markArrayViewMethodCall(callID ast.ExprID, name string, receiverType types.TypeID, args []types.TypeID) {
	if callID == ast.NoExprID || !tc.isArrayOrFixedType(receiverType) {
		return
	}
	if name != "slice" && name != "__index" {
		return
	}
	if len(args) == 0 {
		return
	}
	payload, ok := tc.rangePayload(args[0])
	if !ok || tc.types == nil {
		return
	}
	intType := tc.types.Builtins().Int
	if intType == types.NoTypeID || !tc.sameType(payload, intType) {
		return
	}
	tc.markArrayViewExpr(callID)
}

func (tc *typeChecker) checkArrayViewResizeCall(name string, args []callArg, span source.Span) {
	switch name {
	case "rt_array_reserve", "rt_array_push", "rt_array_pop",
		"array_reserve", "array_push", "array_pop":
	default:
		return
	}
	if len(args) == 0 {
		return
	}
	if !tc.isArrayViewExpr(args[0].expr) {
		return
	}
	opSpan := span
	if argSpan := tc.exprSpan(args[0].expr); argSpan != (source.Span{}) {
		opSpan = argSpan
	}
	tc.reportArrayViewResize(opSpan, name)
}
