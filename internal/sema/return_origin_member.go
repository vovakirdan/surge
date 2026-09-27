package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// A field read through a reference to a plain struct is typed as a borrow of the
// field, a sub-place of the referent, so it has exactly the reference's origins.
// One level only: the target is a named reference, and the field is declared
// reference-free, so nothing is loaded from the referent. A dynamic array field
// is admitted too: the borrow names the field's place, never its heap contents,
// and the loans those contents keep are read only where the place is loaded or
// passed on, by containerLoans or the backing-call targets, which refuse a base
// they cannot prove. Any other loan carrier keeps the refusal: a fixed array's
// view or a cursor over it points into the referent's own storage, and the
// checker does not keep a caller's argument borrowed while such a result lives.
func (b *returnOriginBody) memberBorrowsReferent(id ast.ExprID, data *ast.ExprMemberData) bool {
	u := b.function.unit
	in := u.Sema.TypeInterner
	if node := u.Builder.Exprs.Get(data.Target); node == nil || node.Kind != ast.ExprIdent {
		return false
	}
	target, ok := in.Lookup(returnOriginResolveAlias(in, u.Sema.ExprTypes[data.Target]))
	result, borrowed := in.Lookup(returnOriginResolveAlias(in, u.Sema.ExprTypes[id]))
	if !ok || !borrowed || target.Kind != types.KindReference || result.Kind != types.KindReference {
		return false
	}
	info, found := in.StructInfo(target.Elem)
	if !found || info == nil || !returnOriginPlainStruct(b.function, info) {
		return false
	}
	if _, nominal := returnOriginNominalShape(in, target.Elem, info, nil); nominal {
		return false
	}
	matches := 0
	for _, field := range info.Fields {
		if field.Name != data.Field {
			continue
		}
		matches++
		if field.Type != result.Elem || returnOriginIsReference(in, field.Type) ||
			returnOriginTypeShape(in, field.Type, nil) != returnOriginRefFree || b.analyzer.loanCarrier(field.Type) && !returnOriginDynamicArray(in, field.Type) {
			return false
		}
	}
	return matches == 1
}

// returnOriginDynamicArray says whether a type is an owned `T[]`/`Array<T>`,
// whose elements live in a heap buffer rather than inline in the holder.
func returnOriginDynamicArray(in *types.Interner, id types.TypeID) bool {
	c, canonical := returnOriginContainer(in, id)
	return canonical && !c.reference && c.family == in.ArrayNominalType()
}
