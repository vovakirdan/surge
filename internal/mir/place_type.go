package mir

import "surge/internal/types"

// placeTypeIn walks a place's projections to the type it names.
//
// It is free-standing rather than a lowerer method because two very different
// callers need the same walk: the lowering, which is building the place, and
// any pass reading a finished function that has to ask what a projected
// destination holds.
func placeTypeIn(typesIn *types.Interner, f *Func, globals []Global, place Place) (types.TypeID, bool) {
	if typesIn == nil || !place.IsValid() {
		return types.NoTypeID, false
	}

	var cur types.TypeID
	switch place.Kind {
	case PlaceLocal:
		idx := int(place.Local)
		if f == nil || idx < 0 || idx >= len(f.Locals) {
			return types.NoTypeID, false
		}
		cur = f.Locals[idx].Type
	case PlaceGlobal:
		idx := int(place.Global)
		if idx < 0 || idx >= len(globals) {
			return types.NoTypeID, false
		}
		cur = globals[idx].Type
	default:
		return types.NoTypeID, false
	}

	for _, proj := range place.Proj {
		next, ok := projectedPlaceTypeIn(typesIn, cur, proj)
		if !ok {
			return types.NoTypeID, false
		}
		cur = next
	}
	return cur, true
}

func projectedPlaceTypeIn(typesIn *types.Interner, cur types.TypeID, proj PlaceProj) (types.TypeID, bool) {
	switch proj.Kind {
	case PlaceProjDeref:
		return derefPlaceTypeIn(typesIn, cur)
	case PlaceProjField:
		return fieldPlaceTypeIn(typesIn, cur, proj)
	case PlaceProjIndex:
		return indexPlaceTypeIn(typesIn, cur)
	default:
		return types.NoTypeID, false
	}
}

func derefPlaceTypeIn(typesIn *types.Interner, id types.TypeID) (types.TypeID, bool) {
	tt, ok := typesIn.Lookup(resolveAlias(typesIn, id))
	if !ok {
		return types.NoTypeID, false
	}
	switch tt.Kind {
	case types.KindOwn, types.KindPointer, types.KindReference:
		return tt.Elem, true
	default:
		return types.NoTypeID, false
	}
}

func fieldPlaceTypeIn(typesIn *types.Interner, id types.TypeID, proj PlaceProj) (types.TypeID, bool) {
	id = resolveAliasType(typesIn, id)
	if info, ok := typesIn.StructInfo(id); ok && info != nil {
		fieldIdx := proj.FieldIdx
		if fieldIdx >= 0 && fieldIdx < len(info.Fields) {
			return info.Fields[fieldIdx].Type, true
		}
		if proj.FieldName != "" && typesIn.Strings != nil {
			for i, field := range info.Fields {
				name, ok := typesIn.Strings.Lookup(field.Name)
				if ok && name == proj.FieldName {
					fieldIdx = i
					break
				}
			}
		}
		if fieldIdx >= 0 && fieldIdx < len(info.Fields) {
			return info.Fields[fieldIdx].Type, true
		}
	}
	if info, ok := typesIn.TupleInfo(id); ok && info != nil {
		fieldIdx := proj.FieldIdx
		if fieldIdx >= 0 && fieldIdx < len(info.Elems) {
			return info.Elems[fieldIdx], true
		}
	}
	return types.NoTypeID, false
}

func indexPlaceTypeIn(typesIn *types.Interner, id types.TypeID) (types.TypeID, bool) {
	id = resolveAliasType(typesIn, id)
	if elem, ok := typesIn.ArrayInfo(id); ok {
		return elem, true
	}
	if elem, _, ok := typesIn.ArrayFixedInfo(id); ok {
		return elem, true
	}
	tt, ok := typesIn.Lookup(id)
	if !ok || tt.Kind != types.KindArray {
		return types.NoTypeID, false
	}
	return tt.Elem, true
}
