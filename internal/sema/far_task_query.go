package sema

import "surge/internal/types"

// IsDirectFarTaskType reports the direct runtime-owned `far Task<T>` handle
// shape. Containers intentionally do not match: Task 9's lease seam tracks one
// direct handle pointer at a time.
func (r *Result) IsDirectFarTaskType(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil || id == types.NoTypeID {
		return false
	}
	typesIn := r.TypeInterner
	outer, ok := typesIn.Lookup(id)
	if !ok || outer.Kind != types.KindFar || typesIn.Strings == nil {
		return false
	}
	inner, ok := typesIn.Lookup(outer.Elem)
	if !ok {
		return false
	}
	name := ""
	switch inner.Kind {
	case types.KindStruct:
		info, found := typesIn.StructInfo(outer.Elem)
		if !found || info == nil {
			return false
		}
		name, ok = typesIn.Strings.Lookup(info.Name)
	case types.KindAlias:
		info, found := typesIn.AliasInfo(outer.Elem)
		if !found || info == nil {
			return false
		}
		name, ok = typesIn.Strings.Lookup(info.Name)
	default:
		return false
	}
	return ok && name == "Task"
}
