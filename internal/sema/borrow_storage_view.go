package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A `Range` a call hands back and a window of a FIXED array are views into
// storage, exactly as a BytesView is, and the checker holds them as it holds
// one: a view is a borrow of what it reads.
//
// Neither says so in its type. `Range<T>` is `{ __state: *byte }`, and the
// state points into the array it walks; the return-origin shape reads the raw
// pointer as free of any borrow, so `let it = ys.__range(); ys = [...]` ended
// the receiver's loan at the call and the cursor then read the freed buffer.
// A window of a fixed array is typed `T[]` like an owned array, and unlike a
// dynamic array's window it retains nothing: the fixed array has no header to
// register against, so `first(&xs[0])` followed by `xs.push(...)` read the
// element storage `rt_realloc` had moved. A DYNAMIC array's window keeps its
// base alive through the runtime's view registry and stays as it is.
//
// The rule is the BytesView one, asked of two more shapes: a call's `&`
// argument (or receiver) stays borrowed for as long as the result lives when
// the result can point into it. It asks shapes, not which argument the value
// came from, and is conservative in the same way.

// callResultKeepsLoan: a call's result can point into the storage its `&`
// argument of type actual refers to -- it carries a reference or a BytesView,
// it holds a range cursor or a raw pointer, or it holds a dynamic array whose
// element is the element of a fixed array the argument reaches (a window of it).
//
// callee, when known, is asked only one thing: whether it is core's
// `ArrayFixed::to_array`, which builds an owned copy and so is no window.
func (tc *typeChecker) callResultKeepsLoan(callee *symbols.Symbol, actual, result types.TypeID) bool {
	if result == types.NoTypeID {
		return false
	}
	if returnOriginTypeShape(tc.types, result, nil) == returnOriginCarriesRef {
		return true
	}
	if tc.typeHoldsStorageCursor(result) {
		return true
	}
	return !tc.isCoreFixedArrayCopy(callee) && tc.resultMayWindowFixedArray(actual, result)
}

// isCoreFixedArrayCopy certifies core's `ArrayFixed<T, N>::to_array` by its
// declaration: a method with a body declared in the core module, `self:
// &ArrayFixed<T,N>` its one parameter, `Array<T>` its result. Its body
// (core/array.sg) pushes a clone of every element into a fresh array, so the
// result points into nothing the receiver owns. A user function of the same
// name or shape is not certified: `fn f(c: &E[N]) -> E[]` may return a window,
// and the shape rule keeps its argument borrowed.
func (tc *typeChecker) isCoreFixedArrayCopy(sym *symbols.Symbol) bool {
	if sym == nil || sym.Signature == nil || sym.Kind != symbols.SymbolFunction ||
		sym.Flags&symbols.SymbolFlagMethod == 0 || !isCoreModulePath(sym.ModulePath) {
		return false
	}
	sig := sym.Signature
	return tc.lookupName(sym.Name) == "to_array" && sig.HasSelf && sig.HasBody && len(sig.Params) == 1 &&
		sig.Params[0] == "&ArrayFixed<T,N>" && sig.Result == "Array<T>" && sym.ReceiverKey == "ArrayFixed<T,N>"
}

// mayHoldStorageLoan: a binding of this type can hold a loan a view depends
// on -- a borrow-carrying type, a range cursor or raw pointer, or a dynamic
// array (which may be a window of a fixed array).
func (tc *typeChecker) mayHoldStorageLoan(t types.TypeID) bool {
	if t == types.NoTypeID {
		return false
	}
	return tc.mayCarryView(t) || tc.typeHoldsStorageCursor(t) || tc.typeReachesDynamicArray(t)
}

// typeHoldsStorageCursor: some part of the value is a `Range` or a raw
// pointer. Task and channel handles point at runtime objects of their own, and
// a BytesView is already a borrow by its mark, so neither is looked into.
func (tc *typeChecker) typeHoldsStorageCursor(t types.TypeID) bool {
	return tc.typeFinds(t, func(id types.TypeID, typ types.Type) (bool, bool) {
		if _, isRange := tc.types.RangeBoundType(id); isRange {
			return true, false
		}
		if tc.types.IsRuntimeHandleType(id) || tc.types.IsBorrowedView(id) {
			return false, false
		}
		return typ.Kind == types.KindPointer, true
	}, nil)
}

// resultMayWindowFixedArray: the argument reaches a fixed array of E (through
// its reference, a field, an element) and the result holds a dynamic `E[]`.
func (tc *typeChecker) resultMayWindowFixedArray(actual, result types.TypeID) bool {
	var elems []types.TypeID
	tc.typeFinds(actual, func(id types.TypeID, _ types.Type) (bool, bool) {
		if elem, _, ok := tc.types.ArrayFixedInfo(id); ok {
			if elem = tc.resolveAlias(elem); !slices.Contains(elems, elem) {
				elems = append(elems, elem)
			}
		}
		return false, !tc.types.IsRuntimeHandleType(id)
	}, nil)
	if len(elems) == 0 {
		return false
	}
	return tc.typeFinds(result, func(id types.TypeID, _ types.Type) (bool, bool) {
		if elem, ok := tc.types.ArrayInfo(id); ok && slices.Contains(elems, tc.resolveAlias(elem)) {
			return true, false
		}
		return false, !tc.types.IsRuntimeHandleType(id)
	}, nil)
}

// typeReachesDynamicArray: some part of the value is a dynamic array.
func (tc *typeChecker) typeReachesDynamicArray(t types.TypeID) bool {
	return tc.typeFinds(t, func(id types.TypeID, _ types.Type) (bool, bool) {
		if _, ok := tc.types.ArrayInfo(id); ok {
			return true, false
		}
		return false, !tc.types.IsRuntimeHandleType(id)
	}, nil)
}

// typeFinds walks the parts of a value's type -- aliases, own/far/array/
// reference/pointer elements, union members and tag arguments, struct fields
// and type arguments, tuple elements -- and reports whether visit found one.
// visit also says whether to look inside the type it was handed. Unlike
// typeReaches, a type that cannot be looked into (a generic parameter) finds
// nothing: these rules keep a loan for a shape they can name.
func (tc *typeChecker) typeFinds(id types.TypeID, visit func(types.TypeID, types.Type) (found, descend bool), seen map[types.TypeID]bool) bool {
	if tc.types == nil || id == types.NoTypeID || seen[id] {
		return false
	}
	if seen == nil {
		seen = make(map[types.TypeID]bool)
	}
	seen[id] = true
	typ, ok := tc.types.Lookup(id)
	if !ok {
		return false
	}
	found, descend := visit(id, typ)
	if found {
		return true
	}
	if !descend {
		return false
	}
	var inner []types.TypeID
	switch typ.Kind {
	case types.KindOwn, types.KindFar, types.KindArray, types.KindReference, types.KindPointer:
		inner = append(inner, typ.Elem)
	case types.KindAlias:
		if target, found := tc.types.AliasTarget(id); found {
			inner = append(inner, target)
		}
	case types.KindUnion:
		if info, found := tc.types.UnionInfo(id); found && info != nil {
			for _, member := range info.Members {
				if member.Type != types.NoTypeID {
					inner = append(inner, member.Type)
				}
				inner = append(inner, member.TagArgs...)
			}
		}
	case types.KindStruct:
		if info, found := tc.types.StructInfo(id); found && info != nil {
			for _, field := range info.Fields {
				inner = append(inner, field.Type)
			}
			inner = append(inner, info.TypeArgs...)
		}
	case types.KindTuple:
		if info, found := tc.types.TupleInfo(id); found && info != nil {
			inner = append(inner, info.Elems...)
		}
	}
	for _, next := range inner {
		if tc.typeFinds(next, visit, seen) {
			return true
		}
	}
	return false
}

// borrowForInIterable: `for x in xs` walks xs through a `__range()` cursor the
// loop makes for itself (hir normalizeIterFor), so the loop body is where that
// cursor lives, and xs stays borrowed for the whole body exactly as it does in
// `for x in &xs`. Without the loan `for x in xs { xs.push(x); }` compiled and
// the cursor read the buffer the push had reallocated. A loop over a range, a
// call's result or a reference binding takes no loan here. The loan is ended
// by endForInIterable when the body has been walked.
func (tc *typeChecker) borrowForInIterable(iterable ast.ExprID, iterableType types.TypeID) ast.ExprID {
	expr := tc.unwrapGroupExpr(iterable)
	if tc.borrow == nil || !expr.IsValid() || tc.isBorrowExpr(expr) ||
		tc.isReferenceType(iterableType) || !tc.isArrayOrFixedType(iterableType) ||
		tc.borrow.ExprBorrow(expr) != NoBorrowID {
		return ast.NoExprID
	}
	if _, isPlace := tc.resolvePlace(expr); !isPlace {
		return ast.NoExprID
	}
	tc.handleBorrow(expr, tc.exprSpan(expr), ast.ExprUnaryRef, expr)
	return expr
}

// endForInIterable ends the loop's loan on its iterable.
func (tc *typeChecker) endForInIterable(expr ast.ExprID) {
	if expr.IsValid() {
		tc.dropBorrowForExpr(expr, tc.exprSpan(expr), "for_in_iterable")
	}
}

// borrowIndexThroughReference: an element reference `p[1]` handed through a
// call whose result carries it, where the index's target is a reference
// (a `&mut string[]` parameter) and so took no loan of its own. The element
// lives in p's referent, and a binding that stores the result -- `{ r =
// ident(p[1]); }` -- must keep that referent borrowed, or `*p = [...]` frees
// what `r` reads. A shared child loan is taken on the target through p, noted
// as a statement temporary: it ends with the statement unless a binding holds
// it (indexResultLoan finds it for inheritedBorrowForCall).
func (tc *typeChecker) borrowIndexThroughReference(expr ast.ExprID, span source.Span) {
	if tc.builder == nil || len(tc.tempFrames) == 0 {
		return
	}
	index, ok := tc.builder.Exprs.Index(expr)
	if !ok || index == nil || tc.indexResultLoan(expr) != NoBorrowID {
		return
	}
	target := index.Target
	if _, isPlace := tc.resolvePlace(target); !isPlace {
		return
	}
	tc.handleBorrow(target, span, ast.ExprUnaryRef, target)
	bid := tc.borrow.ExprBorrow(target)
	if bid == NoBorrowID {
		return
	}
	frame := &tc.tempFrames[len(tc.tempFrames)-1]
	if !slices.Contains(frame.loans, bid) {
		frame.loans = append(frame.loans, bid)
	}
}

// fixedWindowIndexLoan: `xs[0][[0..2]]` is a window of a fixed array, and the
// loan the index keeps on its target is what the window reads through, so a
// binding that stores the window -- from an inner block too -- holds it. A
// dynamic array's window retains its base and needs none.
func (tc *typeChecker) fixedWindowIndexLoan(data *ast.ExprIndexData) BorrowID {
	if tc.borrow == nil || tc.result == nil || data == nil {
		return NoBorrowID
	}
	container := tc.result.ExprTypes[data.Target]
	if !tc.isArrayRangeIndex(container, tc.result.ExprTypes[data.Index]) {
		return NoBorrowID
	}
	if _, _, fixed := tc.arrayFixedInfo(tc.valueType(container)); !fixed {
		return NoBorrowID
	}
	if bid := tc.borrow.ExprBorrow(data.Target); bid != NoBorrowID {
		return bid
	}
	// A target that is itself a reference -- an element `xs[0]`, a reference
	// binding -- took no loan of its own: the window reads through the one it
	// stands on.
	if bid := tc.indexResultLoan(tc.unwrapGroupExpr(data.Target)); bid != NoBorrowID {
		return bid
	}
	return tc.inheritedBorrowForExpr(data.Target)
}
