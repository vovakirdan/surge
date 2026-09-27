package sema

import (
	"slices"
	"strings"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A statement temporary's loan ends with its statement. `xs[1].__len()`
// borrows `xs` for the element reference `xs[1]` (the index keeps its target
// borrowed, because its result is a reference), and nothing ever names that
// reference again: the method reads it and hands back a value that carries no
// borrow. Registered at the block, the loan lived to the block's end, so
// `let n = xs[1].__len(); @drop xs;` -- and `own xs`, `xs = ...` -- were
// refused although nothing could still read `xs`.
//
// The loan is noted when the temporary is consumed that way
// (noteIndexTemporaryArg) and released when its statement ends
// (releaseStatementTemporaryLoans, from popTempFrame) -- an `if` or `while`
// condition's when the condition is typed (releaseConditionTemporaries) --
// unless a binding holds it by then. Every way a loan escapes into a binding records a holder -- a
// reference binding (bindingBorrow), a view, an aggregate, a compare arm or a
// compare value (viewLoans) -- and a loan reborrowed through it (a child) is
// not released either.

// noteIndexTemporaryArg: the argument at param (the receiver is 0 for a
// method) is an index temporary (`xs[1]`, `o.xs[1]`, `xs[1][2]`) taken by
// reference, and the call cannot keep it: its result holds no reference, no
// view and no raw pointer (`Range<T>` is `{ __state: *byte }` and points into
// the element), and no other parameter can hold a mutable reference or a raw
// pointer (resolved through aliases: `type MR = &mut &string`), so the callee
// has nowhere to store it. The loans the index chain keeps on its targets are
// noted for release at the statement's end.
func (tc *typeChecker) noteIndexTemporaryArg(arg ast.ExprID, sig *symbols.FunctionSignature, param int, result types.TypeID) {
	if tc.borrow == nil || tc.builder == nil || len(tc.tempFrames) == 0 || sig == nil || param < 0 || param >= len(sig.Params) {
		return
	}
	if !tc.paramTakesReference(sig.Params[param]) {
		return
	}
	for i, p := range sig.Params {
		if i != param && tc.paramCanStoreReference(p) {
			return
		}
	}
	if returnOriginTypeShape(tc.types, result, nil) != returnOriginRefFree ||
		tc.typeReaches(result, func(t types.Type) bool { return t.Kind == types.KindPointer }, nil) {
		return
	}
	frame := &tc.tempFrames[len(tc.tempFrames)-1]
	for expr := tc.unwrapGroupExpr(arg); expr.IsValid(); {
		index, ok := tc.builder.Exprs.Index(expr)
		if !ok || index == nil {
			return
		}
		target := tc.unwrapGroupExpr(index.Target)
		if bid := tc.borrow.ExprBorrow(target); bid != NoBorrowID && !slices.Contains(frame.loans, bid) {
			frame.loans = append(frame.loans, bid)
		}
		expr = target
	}
}

// paramTakesReference: the parameter is a reference (`&T`, `&mut T`), spelled
// or behind an alias.
func (tc *typeChecker) paramTakesReference(key symbols.TypeKey) bool {
	if strings.HasPrefix(strings.TrimSpace(string(key)), "&") {
		return true
	}
	id := tc.resolveAlias(tc.typeFromKey(key))
	t, ok := tc.types.Lookup(id)
	return id != types.NoTypeID && ok && t.Kind == types.KindReference
}

// paramCanStoreReference: a parameter through which a callee could store the
// temporary -- a type that reaches a mutable reference or a raw pointer, or one
// that cannot be resolved here (a generic parameter, for one), which is
// assumed to.
func (tc *typeChecker) paramCanStoreReference(key symbols.TypeKey) bool {
	text := strings.TrimSpace(string(key))
	if strings.HasPrefix(text, "&mut") {
		return true
	}
	id := tc.typeFromKey(key)
	if id == types.NoTypeID {
		return !strings.HasPrefix(text, "&")
	}
	return tc.typeReaches(id, func(t types.Type) bool {
		return t.Kind == types.KindPointer || (t.Kind == types.KindReference && t.Mutable)
	}, nil)
}

// typeReaches: some type inside id -- through aliases, own/far/array/reference
// elements, union members, struct fields and type arguments, tuple elements --
// satisfies hit. A type that cannot be looked into (a generic parameter, a
// function, an unknown kind) counts as reaching.
func (tc *typeChecker) typeReaches(id types.TypeID, hit func(types.Type) bool, seen map[types.TypeID]bool) bool {
	if tc.types == nil || id == types.NoTypeID {
		return true
	}
	if seen[id] {
		return false
	}
	if seen == nil {
		seen = make(map[types.TypeID]bool)
	}
	seen[id] = true
	t, ok := tc.types.Lookup(id)
	if !ok {
		return true
	}
	if hit(t) {
		return true
	}
	var inner []types.TypeID
	switch t.Kind {
	case types.KindUnit, types.KindNothing, types.KindBool, types.KindConst,
		types.KindString, types.KindInt, types.KindUint, types.KindFloat, types.KindEnum:
		return false
	case types.KindOwn, types.KindFar, types.KindArray, types.KindReference, types.KindPointer:
		inner = append(inner, t.Elem)
	case types.KindAlias:
		target, found := tc.types.AliasTarget(id)
		if !found {
			return true
		}
		inner = append(inner, target)
	case types.KindUnion:
		info, found := tc.types.UnionInfo(id)
		if !found || info == nil {
			return true
		}
		for _, member := range info.Members {
			if member.Type != types.NoTypeID {
				inner = append(inner, member.Type)
			}
			inner = append(inner, member.TagArgs...)
		}
	case types.KindStruct:
		info, found := tc.types.StructInfo(id)
		if !found || info == nil {
			return true
		}
		for _, field := range info.Fields {
			inner = append(inner, field.Type)
		}
		inner = append(inner, info.TypeArgs...)
	case types.KindTuple:
		info, found := tc.types.TupleInfo(id)
		if !found || info == nil {
			return true
		}
		inner = append(inner, info.Elems...)
	default:
		return true
	}
	for _, next := range inner {
		if tc.typeReaches(next, hit, seen) {
			return true
		}
	}
	return false
}

// releaseStatementTemporaryLoans ends, at its statement's end, each noted loan
// that is still live and that no binding holds.
func (tc *typeChecker) releaseStatementTemporaryLoans(loans []BorrowID) {
	if tc.borrow == nil {
		return
	}
	for _, bid := range loans {
		info := tc.borrow.Info(bid)
		if info == nil || tc.borrow.exprBorrow[info.Life.FromExpr] != bid || tc.statementLoanHeld(bid) {
			continue
		}
		place, span := info.Place, info.Span
		tc.borrow.DropBorrow(bid)
		tc.recordBorrowEvent(&BorrowEvent{
			Kind:   BorrowEvBorrowEnd,
			Borrow: bid,
			Place:  place,
			Span:   span,
			Scope:  tc.currentScope(),
			Note:   "statement_temporary",
		})
	}
}

// releaseConditionTemporaries: an `if` or `while` condition is a value that
// carries no borrow, so the loans its temporaries noted end before the body.
func (tc *typeChecker) releaseConditionTemporaries() {
	if len(tc.tempFrames) == 0 {
		return
	}
	frame := &tc.tempFrames[len(tc.tempFrames)-1]
	if frame.tainted {
		return
	}
	tc.releaseStatementTemporaryLoans(frame.loans)
	frame.loans = nil
}

// statementLoanHeld: a binding holds the loan (its reference borrow, or a
// loan its value depends on), or another loan was reborrowed through it.
func (tc *typeChecker) statementLoanHeld(bid BorrowID) bool {
	for _, held := range tc.bindingBorrow {
		if held == bid {
			return true
		}
	}
	for _, loans := range tc.viewLoans {
		if slices.Contains(loans, bid) {
			return true
		}
	}
	for _, info := range tc.borrow.infos {
		if info.Parent == bid {
			return true
		}
	}
	return false
}
