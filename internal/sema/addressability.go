package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

type borrowFailureKind uint8

const (
	borrowFailureNone borrowFailureKind = iota
	borrowFailureNotAddressable
	borrowFailureImmutable
)

type borrowMatchInfo struct {
	expr ast.ExprID
	mut  bool
	kind borrowFailureKind
}

func (info *borrowMatchInfo) record(expr ast.ExprID, mut bool, kind borrowFailureKind) {
	if info == nil || !expr.IsValid() || info.expr.IsValid() || kind == borrowFailureNone {
		return
	}
	info.expr = expr
	info.mut = mut
	info.kind = kind
}

func (tc *typeChecker) isAddressableExpr(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil || tc.result == nil {
		return false
	}
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() {
		return false
	}
	if node := tc.builder.Exprs.Get(expr); node != nil && node.Kind == ast.ExprIndex {
		if !tc.isReferenceType(tc.result.ExprTypes[expr]) {
			return false
		}
	}
	_, ok := tc.resolvePlace(expr)
	return ok
}

func (tc *typeChecker) reportBorrowNonAddressable(expr ast.ExprID, mut bool) {
	if !expr.IsValid() {
		return
	}
	msg := "cannot take reference to temporary value; bind it to a variable first"
	if mut {
		msg = "cannot take mutable reference to temporary value; bind it to a variable first"
	}
	if b := diag.ReportError(tc.reporter, diag.SemaBorrowNonAddressable, tc.exprSpan(expr), msg); b != nil {
		note := "bind the value to a variable first, then borrow it (e.g. `let tmp = <expr>; let r: &T = &tmp;`)"
		if mut {
			note = "bind the value to a variable first, then borrow it mutably (e.g. `let tmp = <expr>; let r: &mut T = &mut tmp;`)"
		}
		b.WithNote(tc.exprSpan(expr), note)
		b.Emit()
	}
}

func (tc *typeChecker) reportBorrowImmutable(expr ast.ExprID) {
	if !expr.IsValid() {
		return
	}
	desc, ok := tc.resolvePlace(expr)
	if ok {
		place := tc.canonicalPlace(desc)
		if place.IsValid() {
			tc.report(diag.SemaBorrowImmutable, tc.exprSpan(expr), "cannot take mutable borrow of %s", tc.placeLabel(place))
			return
		}
	}
	tc.report(diag.SemaBorrowImmutable, tc.exprSpan(expr), "cannot take mutable borrow of immutable value")
}

func (tc *typeChecker) reportBorrowFailure(info *borrowMatchInfo) {
	if info == nil || !info.expr.IsValid() {
		return
	}
	switch info.kind {
	case borrowFailureImmutable:
		tc.reportBorrowImmutable(info.expr)
	default:
		tc.reportBorrowNonAddressable(info.expr, info.mut)
	}
}

func (tc *typeChecker) isMutablePlaceExpr(expr ast.ExprID) bool {
	desc, ok := tc.resolvePlace(expr)
	if !ok {
		return false
	}
	return tc.isMutableBinding(desc.Base)
}

func (tc *typeChecker) isMutableBinding(symID symbols.SymbolID) bool {
	if !symID.IsValid() {
		return false
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return false
	}
	if sym.Flags&symbols.SymbolFlagMutable != 0 {
		return true
	}
	if tc.types == nil {
		return false
	}
	ty := tc.bindingType(symID)
	if ty == types.NoTypeID {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(ty))
	if !ok {
		return false
	}
	return tt.Kind == types.KindReference && tt.Mutable
}

func (tc *typeChecker) isStringLiteralExpr(expr ast.ExprID) bool {
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() || tc.builder == nil {
		return false
	}
	lit, ok := tc.builder.Exprs.Literal(expr)
	return ok && lit != nil && lit.Kind == ast.ExprLitString
}

func (tc *typeChecker) isStringType(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	return tc.resolveAlias(id) == tc.types.Builtins().String
}

func (tc *typeChecker) isBorrowableStringLiteral(expr ast.ExprID, expected types.TypeID) bool {
	if !tc.isStringLiteralExpr(expr) || expected == types.NoTypeID || tc.types == nil {
		return false
	}
	expected = tc.resolveAlias(expected)
	tt, ok := tc.types.Lookup(expected)
	if !ok || tt.Kind != types.KindReference || tt.Mutable {
		return false
	}
	return tc.isStringType(tt.Elem)
}

func (tc *typeChecker) canMaterializeForRefString(expr ast.ExprID, expected types.TypeID) bool {
	if expr == ast.NoExprID || expected == types.NoTypeID || tc.types == nil || tc.result == nil {
		return false
	}
	expected = tc.resolveAlias(expected)
	tt, ok := tc.types.Lookup(expected)
	if !ok || tt.Kind != types.KindReference || tt.Mutable {
		return false
	}
	if !tc.isStringType(tt.Elem) {
		return false
	}
	actual := tc.result.ExprTypes[expr]
	if actual == types.NoTypeID {
		return false
	}
	if tc.isReferenceType(actual) {
		return false
	}
	if tc.resolveAlias(actual) != tc.types.Builtins().String {
		return false
	}
	return !tc.isAddressableExpr(expr)
}
