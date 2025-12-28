package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) unwrapGroupExpr(id ast.ExprID) ast.ExprID {
	if !id.IsValid() || tc.builder == nil {
		return id
	}
	for {
		group, ok := tc.builder.Exprs.Group(id)
		if !ok || group == nil {
			return id
		}
		id = group.Inner
	}
}

func (tc *typeChecker) implicitCopyFromRef(expected, actual types.TypeID) bool {
	if expected == types.NoTypeID || actual == types.NoTypeID || tc.types == nil {
		return false
	}
	actual = tc.resolveAlias(actual)
	tt, ok := tc.types.Lookup(actual)
	if !ok || tt.Kind != types.KindReference {
		return false
	}
	elem := tc.resolveAlias(tt.Elem)
	if !tc.isCopyType(elem) {
		return false
	}
	expected = tc.resolveAlias(expected)
	if expInfo, ok := tc.types.Lookup(expected); ok && expInfo.Kind == types.KindOwn {
		return elem == tc.resolveAlias(expInfo.Elem)
	}
	if expInfo, ok := tc.types.Lookup(expected); ok && (expInfo.Kind == types.KindReference || expInfo.Kind == types.KindPointer) {
		return false
	}
	return elem == expected
}

func (tc *typeChecker) implicitCopyFromRefParam(param symbols.TypeKey, actual types.TypeID) bool {
	paramStr := strings.TrimSpace(string(param))
	if paramStr == "" || strings.HasPrefix(paramStr, "&") {
		return false
	}
	if strings.HasPrefix(paramStr, "own ") {
		paramStr = strings.TrimSpace(strings.TrimPrefix(paramStr, "own "))
	}
	if actual == types.NoTypeID || tc.types == nil {
		return false
	}
	actual = tc.resolveAlias(actual)
	tt, ok := tc.types.Lookup(actual)
	if !ok || tt.Kind != types.KindReference {
		return false
	}
	elem := tt.Elem
	if !tc.isCopyType(elem) {
		return false
	}
	return tc.methodParamMatches(symbols.TypeKey(paramStr), elem)
}

func (tc *typeChecker) dropBorrowForExpr(expr ast.ExprID, span source.Span, note string) {
	if tc.borrow == nil || !expr.IsValid() {
		return
	}
	bid := tc.borrow.ExprBorrow(expr)
	if bid == NoBorrowID {
		return
	}
	var place Place
	if info := tc.borrow.Info(bid); info != nil {
		place = info.Place
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:   BorrowEvBorrowEnd,
		Borrow: bid,
		Place:  place,
		Span:   span,
		Scope:  tc.currentScope(),
		Note:   note,
	})
	tc.borrow.DropBorrow(bid)
}

func (tc *typeChecker) dropImplicitBorrow(expr ast.ExprID, expected, actual types.TypeID, span source.Span) {
	if !tc.implicitCopyFromRef(expected, actual) {
		return
	}
	expr = tc.unwrapGroupExpr(expr)
	if expr.IsValid() && tc.builder != nil {
		if idx, ok := tc.builder.Exprs.Index(expr); ok && idx != nil {
			tc.dropBorrowForExpr(idx.Target, span, "implicit_deref")
			return
		}
	}
	tc.dropBorrowForExpr(expr, span, "implicit_deref")
}

func (tc *typeChecker) dropImplicitBorrowForValueParam(expr ast.ExprID, param symbols.TypeKey, actual types.TypeID, span source.Span) {
	if !tc.implicitCopyFromRefParam(param, actual) {
		return
	}
	expr = tc.unwrapGroupExpr(expr)
	if expr.IsValid() && tc.builder != nil {
		if idx, ok := tc.builder.Exprs.Index(expr); ok && idx != nil {
			tc.dropBorrowForExpr(idx.Target, span, "implicit_deref")
			return
		}
	}
	tc.dropBorrowForExpr(expr, span, "implicit_deref")
}

func (tc *typeChecker) dropImplicitBorrowForRefParam(expr ast.ExprID, param symbols.TypeKey, actual, result types.TypeID, span source.Span) {
	paramStr := strings.TrimSpace(string(param))
	if paramStr == "" || !strings.HasPrefix(paramStr, "&") {
		return
	}
	expr = tc.unwrapGroupExpr(expr)
	if tc.isReferenceType(actual) && !tc.isBorrowExpr(expr) {
		return
	}
	if tc.isReferenceType(result) {
		return
	}
	tc.dropBorrowForExpr(expr, span, "temp_borrow")
}

func (tc *typeChecker) isBorrowExpr(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil || node.Kind != ast.ExprUnary {
		return false
	}
	unary := tc.builder.Exprs.Unaries.Get(uint32(node.Payload))
	if unary == nil {
		return false
	}
	return unary.Op == ast.ExprUnaryRef || unary.Op == ast.ExprUnaryRefMut
}

func (tc *typeChecker) dropImplicitBorrowsForCall(sym *symbols.Symbol, args []callArg, result types.TypeID) {
	if sym == nil || sym.Signature == nil {
		return
	}
	sig := sym.Signature

	hasNamed := false
	for _, arg := range args {
		if arg.name != source.NoStringID {
			hasNamed = true
			break
		}
	}
	ordered := args
	if hasNamed {
		if reordered, ok := tc.reorderArgsForSignature(sig, args); ok {
			ordered = reordered
		}
	}

	variadicIndex := -1
	for i, v := range sig.Variadic {
		if v {
			variadicIndex = i
			break
		}
	}

	for i, arg := range ordered {
		paramIndex := i
		if variadicIndex >= 0 && i >= variadicIndex {
			paramIndex = variadicIndex
		}
		if paramIndex >= len(sig.Params) {
			continue
		}
		expectedType := tc.typeFromKey(sig.Params[paramIndex])
		if expectedType == types.NoTypeID {
			continue
		}
		tc.dropImplicitBorrow(arg.expr, expectedType, arg.ty, tc.exprSpan(arg.expr))
		tc.dropImplicitBorrowForRefParam(arg.expr, sig.Params[paramIndex], arg.ty, result, tc.exprSpan(arg.expr))
		tc.dropImplicitBorrowForValueParam(arg.expr, sig.Params[paramIndex], arg.ty, tc.exprSpan(arg.expr))
	}
}

func (tc *typeChecker) applyCallOwnership(sym *symbols.Symbol, args []callArg) bool {
	if sym == nil || sym.Signature == nil {
		return false
	}
	sig := sym.Signature

	hasNamed := false
	for _, arg := range args {
		if arg.name != source.NoStringID {
			hasNamed = true
			break
		}
	}
	ordered := args
	if hasNamed {
		if reordered, ok := tc.reorderArgsForSignature(sig, args); ok {
			ordered = reordered
		} else {
			return false
		}
	}

	variadicIndex := -1
	for i, v := range sig.Variadic {
		if v {
			variadicIndex = i
			break
		}
	}

	for i, arg := range ordered {
		paramIndex := i
		if variadicIndex >= 0 && i >= variadicIndex {
			paramIndex = variadicIndex
		}
		if paramIndex >= len(sig.Params) {
			continue
		}
		tc.applyParamOwnership(sig.Params[paramIndex], arg.expr, arg.ty, tc.exprSpan(arg.expr))
	}
	return true
}

func (tc *typeChecker) applyParamOwnershipForType(expected types.TypeID, expr ast.ExprID, actual types.TypeID, span source.Span) {
	if expected == types.NoTypeID || !expr.IsValid() || tc.types == nil {
		return
	}
	expected = tc.resolveAlias(expected)
	tt, ok := tc.types.Lookup(expected)
	if !ok {
		tc.observeMove(expr, span)
		return
	}
	if tt.Kind == types.KindReference {
		if tc.isReferenceType(actual) {
			return
		}
		if !tt.Mutable && tc.canMaterializeForRefString(expr, expected) {
			return
		}
		op := ast.ExprUnaryRef
		if tt.Mutable {
			op = ast.ExprUnaryRefMut
		}
		tc.handleBorrow(expr, span, op, expr)
		return
	}
	tc.observeMove(expr, span)
}
