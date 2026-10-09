package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// A field read through a reference to a plain struct is typed as a borrow of the
// field, a sub-place of the referent, so it has exactly the reference's origins.
// The target is either a named reference or a member/index projection whose
// evaluated owner is already exact. The field is declared reference-free, or
// is the exact marked BytesView whose by-value load has a separate transfer, so
// the projection itself loads nothing from the referent. A loan-carrier field
// is admitted when its borrowed-content shape is otherwise reference-free: the
// borrow names the field's place, never its contents, and the checker keeps the
// referent borrowed while a fixed-array window or cursor result lives. Contents
// are read only by containerLoans/backing transfers, which still refuse a base
// they cannot prove.
func (b *returnOriginBody) memberBorrowsReferent(id ast.ExprID, data *ast.ExprMemberData, owner returnOriginValue) bool {
	u := b.function.unit
	in := u.Sema.TypeInterner
	targetID := b.ungroup(data.Target)
	node := u.Builder.Exprs.Get(targetID)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprIdent:
		// Keep the established one-level certificate unchanged.
	case ast.ExprMember:
		if !returnOriginExactProjectionOwner(owner) {
			return false
		}
	case ast.ExprIndex:
		if _, reason := b.analyzer.indexOperation(b.function, targetID); reason != "" || !returnOriginExactProjectionOwner(owner) {
			return false
		}
	default:
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
		if field.Type != result.Elem || returnOriginIsReference(in, field.Type) {
			return false
		}
		if !in.IsBorrowedView(returnOriginResolveAlias(in, field.Type)) &&
			returnOriginTypeShape(in, field.Type, nil) != returnOriginRefFree {
			return false
		}
	}
	return matches == 1
}

// A composed projection may reuse only a complete storage owner produced by
// the preceding certified projection. Calls, captures, external-cell contents
// and backing-content selectors keep their own transfer obligations.
func returnOriginExactProjectionOwner(owner returnOriginValue) bool {
	if !owner.normal || len(owner.roots) == 0 || len(owner.callables) != 0 {
		return false
	}
	for _, root := range owner.roots {
		if root.expired {
			return false
		}
		switch root.kind {
		case returnOriginLocal:
		case returnOriginParam:
			if root.selector != returnOriginInputValue {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// returnOriginDynamicArray says whether a type is an owned `T[]`/`Array<T>`,
// whose elements live in a heap buffer rather than inline in the holder.
func returnOriginDynamicArray(in *types.Interner, id types.TypeID) bool {
	c, canonical := returnOriginContainer(in, id)
	return canonical && !c.reference && c.family == in.ArrayNominalType()
}
