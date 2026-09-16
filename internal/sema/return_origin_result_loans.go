package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// erasedType says whether every reader drops the roots of a value of this type:
// it holds no borrow and is no loan carrier (I1; return_origin_expr.go:44–47).
func (b *returnOriginBody) erasedType(id types.TypeID) bool {
	return returnOriginTypeShape(b.function.unit.Sema.TypeInterner, id, nil) == returnOriginRefFree && !b.analyzer.loanCarrier(id)
}

// holdsLoan says whether a value of this concrete type can keep a storage loan:
// the type is, or stores without a reference, pointer or function, an array or
// cursor. A type that still names a template parameter answers false; its
// instance caller decides, and the summary carries the roots there.
func (b *returnOriginBody) holdsLoan(id types.TypeID) bool {
	return b.loanWalk(id, false, b.analyzer.loanCarrier)
}

// dropsLoan says whether a part stored in this type, or the type itself, is erased
// while it can keep a loan, so reading that part out drops the loan. Concrete parts
// around a template parameter still answer: only the parameter itself is skipped.
func (b *returnOriginBody) dropsLoan(id types.TypeID) bool {
	return b.loanWalk(id, true, func(part types.TypeID) bool { return b.erasedType(part) && b.holdsLoan(part) })
}

// erasesLoans is Part A's result test: a reader of the value or of one of its
// parts drops roots, so a loan must be refused where it enters.
func (b *returnOriginBody) erasesLoans(id types.TypeID) bool {
	return b.erasedType(id) || b.dropsLoan(id)
}

// loanWalk answers whether visit holds for id or for a part stored in it. Without
// parts a type naming a template parameter answers false as a whole; with parts
// only the parameter does, since it holds nothing but that parameter's values.
func (b *returnOriginBody) loanWalk(id types.TypeID, parts bool, visit func(types.TypeID) bool) bool {
	in := b.function.unit.Sema.TypeInterner
	if !parts && types.ContainsGenericParam(in, id) {
		return false
	}
	seen := make(map[types.TypeID]bool)
	var walk func(types.TypeID) bool
	walk = func(id types.TypeID) bool {
		typ, ok := in.Lookup(id)
		if !ok || seen[id] || typ.Kind == types.KindReference || typ.Kind == types.KindPointer || typ.Kind == types.KindFn {
			return false
		}
		seen[id] = true
		if visit(id) {
			return true
		}
		children, _ := returnOriginTypeChildren(b.function, id)
		if c, canonical := returnOriginContainer(in, id); canonical {
			children = []types.TypeID{c.element}
		} else if payloads, handle := in.RuntimeHandlePayloads(id); handle {
			children = append(children, payloads...)
		}
		return slices.ContainsFunc(children, walk)
	}
	return walk(id)
}

// guardErasedResult is G6-v at a call. A generic source body's by-value formal
// skips G6-ii because its template keeps the actual's loan in V(i), but a call
// whose value, or a concrete part of it, is erased hands that loan to a reader
// that drops it. Each by-value actual that can keep a loan raises the
// loan-discard refusal here; the roots stay, so a direct return names the owner.
func (b *returnOriginBody) guardErasedResult(id ast.ExprID, summary returnOriginValue, params []types.TypeID, slots []returnOriginArgument,
	actuals []returnOriginValue, span source.Span,
) {
	u := b.function.unit
	if !b.erasesLoans(u.Sema.ExprTypes[id]) {
		return
	}
	for _, root := range summary.roots {
		i := int(root.param)
		// A reference formal's loan in an erased result was made in the callee's
		// body, whose own producer guard refused it (I1′ there).
		if root.kind != returnOriginParam || i >= len(params) || i >= len(slots) || i >= len(actuals) || returnOriginIsReference(u.Sema.TypeInterner, params[i]) {
			continue
		}
		if slices.ContainsFunc(slots[i].exprs, func(expr ast.ExprID) bool {
			return b.shape(expr) == returnOriginRefFree || b.loanWalk(u.Sema.ExprTypes[expr], true, b.analyzer.loanCarrier)
		}) {
			b.discardLoans(actuals[i], span)
		}
	}
}
