package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// Tuples: the element read `t.N` (expression kind 12) and the destructuring
// `let (a, b) = v`.
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
//     root `x` to `t.0` and to the name that destructuring binds it to;
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
	returnOriginDestructureRefusal   = "destructuring needs projected origin facts"
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
func (b *returnOriginBody) tupleElementValue(elem types.TypeID, tuple returnOriginValue, indirect bool) (returnOriginValue, string) {
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
	if indirect {
		return returnOriginValue{}, returnOriginTupleReferentRefusal
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

// destructure binds every name of the tuple pattern `pattern` to its element of
// a subject of type subject whose value is value. Only the patterns the checker
// accepts are read (bindTuplePattern: identifiers and nested tuples); anything
// else keeps the destructuring row. A name with no symbol is `_` and binds nothing.
// A refused element still binds its names, to an unknown value, so every later
// read stays unfinished instead of reading an unbound name.
func (b *returnOriginBody) destructure(span source.Span, pattern ast.ExprID, subject types.TypeID, value returnOriginValue, env returnOriginEnv) returnOriginEnv {
	u := b.function.unit
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	tuple, ok := u.Builder.Exprs.Tuple(pattern)
	info, indirect, found := returnOriginTupleSubject(u.Sema.TypeInterner, subject)
	if !ok || tuple == nil || !found || len(tuple.Elements) != len(info.Elems) {
		b.pending(span, returnOriginDestructureRefusal)
		return b.bindPatternUnknown(pattern, env)
	}
	for i, elem := range tuple.Elements {
		node := u.Builder.Exprs.Get(elem)
		elemType := info.Elems[i]
		switch {
		case node != nil && node.Kind == ast.ExprTuple:
			inner, reason := b.tupleElementValue(elemType, value, indirect)
			if reason != "" {
				b.pending(span, reason)
				inner = unknown
			}
			env = b.destructure(span, elem, elemType, inner, env)
		case node != nil && node.Kind == ast.ExprIdent:
			symID := u.Symbols.ExprSymbols[elem]
			sym := u.Symbols.Table.Symbols.Get(symID)
			if sym == nil {
				if !b.wildcardIdent(elem) {
					b.pending(span, returnOriginDestructureRefusal)
				}
				continue // `_` names nobody and binds nothing
			}
			bound, reason := b.tupleElementValue(elemType, value, indirect)
			if typ, present := u.Sema.BindingTypes[symID]; !present || typ != elemType {
				reason = returnOriginDestructureRefusal
			}
			if reason != "" {
				b.pending(span, reason)
				bound = unknown
			}
			env = env.assign(symID, sym.Scope, bound)
		default:
			b.pending(span, returnOriginDestructureRefusal)
			env = b.bindPatternUnknown(elem, env)
		}
	}
	return env
}

// wildcardIdent reports whether id is the identifier `_`.
func (b *returnOriginBody) wildcardIdent(id ast.ExprID) bool {
	u := b.function.unit
	ident, ok := u.Builder.Exprs.Ident(id)
	if !ok || ident == nil {
		return false
	}
	name, _ := u.Builder.StringsInterner.Lookup(ident.Name)
	return name == "_"
}

// bindPatternUnknown binds every name under pattern to an unknown value.
func (b *returnOriginBody) bindPatternUnknown(pattern ast.ExprID, env returnOriginEnv) returnOriginEnv {
	u := b.function.unit
	node := u.Builder.Exprs.Get(pattern)
	if node == nil {
		return env
	}
	if node.Kind == ast.ExprTuple {
		if tuple, ok := u.Builder.Exprs.Tuple(pattern); ok && tuple != nil {
			for _, elem := range tuple.Elements {
				env = b.bindPatternUnknown(elem, env)
			}
		}
		return env
	}
	symID := u.Symbols.ExprSymbols[pattern]
	if sym := u.Symbols.Table.Symbols.Get(symID); sym != nil {
		env = env.assign(symID, sym.Scope, returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
	}
	return env
}
