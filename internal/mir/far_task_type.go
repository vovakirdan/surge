package mir

import "surge/internal/types"

// IsDirectFarTaskType reports the concrete runtime-owned `far Task<T>` shape.
// It intentionally does not recurse through containers: lease seam
// only owns direct handles that the native ABI represents as one pointer.
func IsDirectFarTaskType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	outer, ok := typesIn.Lookup(id)
	if !ok || outer.Kind != types.KindFar {
		return false
	}
	inner, ok := typesIn.Lookup(outer.Elem)
	if !ok {
		return false
	}
	if typesIn.Strings == nil {
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
