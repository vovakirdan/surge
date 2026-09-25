package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// Tuples: the element read `t.N` (expression kind 12). A tuple pattern in `let`
// is refused by the checker (SemaLetTuplePattern, let_forms.go); the statement
// walk keeps its own row, "destructuring needs projected origin facts", as the
// second fence (return_origin_stmt.go).
//
// A tuple's value does not keep its elements' origins apart: the constructor
// joins every child's value into one (return_origin_constructors.go,
// `contents = contents.join(next.value)`), and a tuple whose type is
// reference-free keeps none of them, because each loan-carrying child is
// refused there by the G6-i loan-discard guard. An element therefore holds at
// most what its tuple holds:
//
//   - an element whose type can hold no reference and no storage loan keeps
//     nothing, as a reference-free member does (return_origin_expr.go, the
//     ExprMember case);
//   - any other element keeps every root of its tuple, a superset of its own
//     roots that never drops one: a tuple holding `Some::<&int>(&x)` hands the
//     root `x` to `t.0`;
//   - an element reached through a reference or a raw pointer lives in the
//     referent, whose contents this frame has no origin for, so such an element
//     keeps a named refusal unless it can hold nothing;
//   - an element whose type is unresolved, or a function value, keeps a named
//     refusal.
//
// The element's storage is its tuple's storage (the referent's when the tuple
// is reached through a reference), so `&t.0` borrows the tuple's owner exactly
// as `&t` does.

const (
	returnOriginTupleUnknownRefusal  = "tuple element needs its concrete element type"
	returnOriginTupleCallableRefusal = "tuple element that holds a callable needs its callable transfer"
	returnOriginTupleReferentRefusal = "tuple element read through a reference needs its referent's contents"
)

// returnOriginTupleSubject resolves the tuple a projection reads. indirect reports
// that the tuple is reached through a reference or a raw pointer, so its elements
// live in a referent rather than in the subject's own storage.
func returnOriginTupleSubject(in *types.Interner, id types.TypeID) (info *types.TupleInfo, indirect, ok bool) {
	seen := make(map[types.TypeID]bool)
	for id != types.NoTypeID && !seen[id] {
		seen[id] = true
		if target, found := in.AliasTarget(id); found {
			id = target
			continue
		}
		typ, found := in.Lookup(id)
		if !found {
			return nil, false, false
		}
		switch typ.Kind {
		case types.KindOwn:
			id = typ.Elem
		case types.KindReference, types.KindPointer:
			indirect = true
			id = typ.Elem
		case types.KindTuple:
			info, ok = in.TupleInfo(id)
			return info, indirect, ok && info != nil
		default:
			return nil, false, false
		}
	}
	return nil, false, false
}

// tupleElementValue is the value one element of a tuple holds, given the value of
// the tuple itself. It reports the refusal reason when the element keeps one.
func (b *returnOriginBody) tupleElementValue(elem types.TypeID, tuple returnOriginValue, indirect bool) (value returnOriginValue, reason string) {
	if returnOriginFnInfo(b.function.unit.Sema.TypeInterner, elem) != nil {
		return returnOriginValue{}, returnOriginTupleCallableRefusal
	}
	switch returnOriginView(b.function).shape(elem) {
	case returnOriginRefFree:
		if !b.analyzer.loanCarrier(elem) {
			return returnOriginValueOf(), ""
		}
	case returnOriginCarriesRef:
	default:
		return returnOriginValue{}, returnOriginTupleUnknownRefusal
	}
	return tuple.clone(), ""
}

// tupleIndex is `t.N`: the target is walked by its own transfer, the element
// keeps its tuple's storage, and its value is tupleElementValue's.
func (b *returnOriginBody) tupleIndex(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	span := u.Builder.Exprs.Get(id).Span
	data, ok := u.Builder.Exprs.TupleIndex(id)
	if !ok || data == nil {
		return b.unknownExpr(env, span, returnOriginTupleUnknownRefusal), nil
	}
	out, err := b.expr(data.Target, env, targets)
	if err != nil || !out.flow.normal.reachable {
		return out, err
	}
	info, indirect, found := returnOriginTupleSubject(u.Sema.TypeInterner, u.Sema.ExprTypes[data.Target])
	if !found || int(data.Index) >= len(info.Elems) || info.Elems[data.Index] != u.Sema.ExprTypes[id] {
		b.pending(span, returnOriginTupleUnknownRefusal)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		return out, nil
	}
	if indirect {
		// Through a reference the element lives in the referent, as a member does.
		out.storage = out.value.clone()
	}
	value, reason := b.tupleElementValue(info.Elems[data.Index], out.value, indirect)
	if reason != "" {
		b.pending(span, reason)
		value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	out.value = value
	return out, nil
}
