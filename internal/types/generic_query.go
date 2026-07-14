package types

// ContainsGenericParam reports whether the type mentions any generic
// parameter (directly or through element/field/argument structure) —
// i.e. it is not yet a concrete monomorphized type.
func ContainsGenericParam(typesIn *Interner, id TypeID) bool {
	return containsGenericParam(typesIn, id, make(map[TypeID]struct{}))
}

func containsGenericParam(typesIn *Interner, id TypeID, seen map[TypeID]struct{}) bool {
	if typesIn == nil || id == NoTypeID {
		return false
	}
	if _, ok := seen[id]; ok {
		return false
	}
	seen[id] = struct{}{}

	tt, ok := typesIn.Lookup(id)
	if !ok {
		return false
	}

	switch tt.Kind {
	case KindGenericParam:
		return true

	case KindPointer, KindReference, KindOwn, KindArray:
		return containsGenericParam(typesIn, tt.Elem, seen)

	case KindTuple:
		info, ok := typesIn.TupleInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, el := range info.Elems {
			if containsGenericParam(typesIn, el, seen) {
				return true
			}
		}
		return false

	case KindFn:
		info, ok := typesIn.FnInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, p := range info.Params {
			if containsGenericParam(typesIn, p, seen) {
				return true
			}
		}
		return containsGenericParam(typesIn, info.Result, seen)

	case KindStruct:
		info, ok := typesIn.StructInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, a := range info.TypeArgs {
			if containsGenericParam(typesIn, a, seen) {
				return true
			}
		}
		for _, f := range typesIn.StructFields(id) {
			if containsGenericParam(typesIn, f.Type, seen) {
				return true
			}
		}
		return false

	case KindUnion:
		info, ok := typesIn.UnionInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, a := range info.TypeArgs {
			if containsGenericParam(typesIn, a, seen) {
				return true
			}
		}
		for _, m := range info.Members {
			if containsGenericParam(typesIn, m.Type, seen) {
				return true
			}
			for _, a := range m.TagArgs {
				if containsGenericParam(typesIn, a, seen) {
					return true
				}
			}
		}
		return false

	case KindAlias:
		info, ok := typesIn.AliasInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, a := range info.TypeArgs {
			if containsGenericParam(typesIn, a, seen) {
				return true
			}
		}
		return containsGenericParam(typesIn, info.Target, seen)

	default:
		return false
	}
}
