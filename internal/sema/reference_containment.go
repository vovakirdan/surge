package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// References carry no lifetime of their own: they are valid only while the
// borrowed value's scope is alive, and the borrow checker tracks them only in
// local bindings. A reference stored inside an aggregate (struct field, tag
// payload, tuple/array/map element) escapes that tracking — the aggregate can
// outlive the borrowed value and dangle. Until aggregates can carry loans,
// aggregates hold owned values only.
//
// rejectRefInAggregate reports the violation and returns true when t is a
// reference type; container names the aggregate position for the message.
func (tc *typeChecker) rejectRefInAggregate(t types.TypeID, span source.Span, container string) bool {
	if t == types.NoTypeID || tc.types == nil {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(t))
	if !ok || tt.Kind != types.KindReference {
		return false
	}
	tc.reportRefInAggregate(tc.typeLabel(t), tc.typeLabel(tt.Elem), span, container)
	return true
}

// checkBorrowEscapeOnReturn rejects returning a reference whose loan provably
// roots in storage that dies with this call frame: a local binding, or an
// owned (by-value) parameter. References with unknown provenance — above all
// a `&T` parameter returned as `&T` (`fn ident(x: &T) -> &T`) — stay allowed;
// the existing call-result loan propagation covers those.
func (tc *typeChecker) checkBorrowEscapeOnReturn(expr ast.ExprID, ty types.TypeID, span source.Span) {
	if tc.borrow == nil || !tc.isReferenceType(ty) {
		return
	}
	inner := tc.unwrapGroupExpr(expr)
	bid := tc.borrow.ExprBorrow(inner)
	if bid == NoBorrowID {
		if symID := tc.symbolForExpr(inner); symID.IsValid() && tc.bindingBorrow != nil {
			bid = tc.bindingBorrow[symID]
		}
	}
	if bid == NoBorrowID {
		bid = tc.inheritedBorrowForExpr(inner)
	}
	if bid == NoBorrowID {
		return
	}
	info := tc.borrow.Info(bid)
	if info == nil || !info.Place.Base.IsValid() {
		return
	}
	sym := tc.symbolFromID(info.Place.Base)
	if sym == nil {
		return
	}
	var storage string
	switch sym.Kind {
	case symbols.SymbolLet:
		storage = "local"
	case symbols.SymbolParam:
		// A reference parameter's target lives in the caller; only an
		// owned (by-value) parameter dies with this frame.
		if tc.isReferenceType(tc.bindingType(info.Place.Base)) {
			return
		}
		storage = "by-value parameter"
	default:
		return
	}
	name := tc.lookupName(sym.Name)
	if tc.reporter == nil {
		tc.report(diag.SemaBorrowEscapesReturn, span,
			"cannot return a borrow of %s '%s': it is freed when the function returns", storage, name)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaBorrowEscapesReturn, span,
		fmt.Sprintf("cannot return a borrow of %s '%s': it is freed when the function returns", storage, name))
	if b == nil {
		return
	}
	if sym.Span != (source.Span{}) {
		b.WithNote(sym.Span, fmt.Sprintf("'%s' lives only inside this function", name))
	}
	b.WithNote(span, fmt.Sprintf(
		"hint: return the value itself to move it out, or a copy: %s.__clone()", name))
	b.Emit()
}

func (tc *typeChecker) reportRefInAggregate(label, elemLabel string, span source.Span, container string) {
	article := "a"
	if container != "" {
		switch container[0] {
		case 'a', 'e', 'i', 'o', 'u':
			article = "an"
		}
	}
	if tc.reporter == nil {
		tc.report(diag.SemaRefInAggregate, span, "%s %s cannot hold a reference (%s)", article, container, label)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaRefInAggregate, span,
		fmt.Sprintf("%s %s cannot hold a reference (%s)", article, container, label))
	if b == nil {
		return
	}
	b.WithNote(span, fmt.Sprintf(
		"a reference is only valid while the borrowed value's scope is alive; a %s can outlive that scope and dangle",
		container))
	b.WithNote(span, fmt.Sprintf(
		"hint: store an owned %s instead — move the value in, or copy it with .__clone(); to lend a value to a function, pass %s as a parameter",
		elemLabel, label))
	b.Emit()
}
