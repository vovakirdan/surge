package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A compare that only BORROWS its subject binds each payload as the subject's
// storage under another name: the binding is an arm-local slot holding the
// payload's handle, and the arm frees nothing of it (payloadStaysWithBorrowedOwner,
// published by the checker as BorrowedPayloadBindings). An element read through
// such a binding therefore points into storage the subject's owner keeps:
//
//	fn first_or(opt: &Option<uint64[]>, fallback: &uint64) -> uint64 {
//	    let got: &uint64 = compare *opt { Some(inner) => &inner[0]; _ => fallback; };
//	    return *got;
//	}
//
// Only the element is answered this way. `&inner` itself still names the
// arm-local slot, and a fixed array's elements live inside that slot, so both
// keep the binding as their owner.

// compareSubjectOwner answers the storage a borrowed subject reads: the
// referent of a reference-typed subject, or the place the subject expression
// names.
func (b *returnOriginBody) compareSubjectOwner(subject ast.ExprID, value *returnOriginExprResult) returnOriginValue {
	in := b.function.unit.Sema.TypeInterner
	if typ, ok := in.Lookup(resolveAlias(in, b.function.unit.Sema.ExprTypes[subject])); ok && typ.Kind == types.KindReference {
		return value.value.clone()
	}
	return value.storage.clone()
}

// notePayloadOwners records owner for each binding the checker published as
// reading a borrowed subject's payload.
func (b *returnOriginBody) notePayloadOwners(bindings []symbols.SymbolID, owner returnOriginValue) {
	published := b.function.unit.Sema.BorrowedPayloadBindings
	if !owner.normal || len(owner.roots) == 0 {
		return
	}
	for _, id := range bindings {
		if _, ok := published[id]; !ok {
			continue
		}
		if b.payloadOwners == nil {
			b.payloadOwners = make(map[symbols.SymbolID]returnOriginValue)
		}
		b.payloadOwners[id] = owner.clone()
	}
}

// payloadElementOwner answers the owner of a growable array element read
// directly through such a binding.
func (b *returnOriginBody) payloadElementOwner(target ast.ExprID, primitive returnOriginIndexType) (returnOriginValue, bool) {
	u := b.function.unit
	if primitive.reference || primitive.family != u.Sema.TypeInterner.ArrayNominalType() {
		return returnOriginValue{}, false
	}
	target = b.ungroup(target)
	if node := u.Builder.Exprs.Get(target); node == nil || node.Kind != ast.ExprIdent {
		return returnOriginValue{}, false
	}
	owner, ok := b.payloadOwners[u.Symbols.ExprSymbols[target]]
	if !ok {
		return returnOriginValue{}, false
	}
	return owner.clone(), true
}
