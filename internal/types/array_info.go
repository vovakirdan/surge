package types //nolint:revive

import "fortio.org/safecast"

// ArrayNominalType returns the built-in nominal Array<T> TypeID, if it was registered.
func (in *Interner) ArrayNominalType() TypeID {
	if in == nil {
		return NoTypeID
	}
	return in.arrayType
}

// ArrayFixedNominalType returns the built-in nominal ArrayFixed<T, N> TypeID, if it was registered.
func (in *Interner) ArrayFixedNominalType() TypeID {
	if in == nil {
		return NoTypeID
	}
	return in.arrayFixedType
}

// ArrayInfo returns (elem, true) if id is an instantiation of the built-in Array<T>.
func (in *Interner) ArrayInfo(id TypeID) (elem TypeID, ok bool) {
	if in == nil || id == NoTypeID || in.arrayType == NoTypeID {
		return NoTypeID, false
	}
	id = resolveAliasAndOwn(in, id)
	info, ok := in.StructInfo(id)
	if !ok || info == nil || len(info.TypeArgs) != 1 {
		return NoTypeID, false
	}
	baseInfo, ok := in.StructInfo(in.arrayType)
	if !ok || baseInfo == nil {
		return NoTypeID, false
	}
	if info.Name != baseInfo.Name {
		return NoTypeID, false
	}
	return info.TypeArgs[0], true
}

// ArrayFixedInfo returns (elem, length, true) if id is an instantiation of the built-in ArrayFixed<T, N>.
func (in *Interner) ArrayFixedInfo(id TypeID) (elem TypeID, length uint32, ok bool) {
	if in == nil || id == NoTypeID || in.arrayFixedType == NoTypeID {
		return NoTypeID, 0, false
	}
	id = resolveAliasAndOwn(in, id)
	info, okInfo := in.StructInfo(id)
	if !okInfo || info == nil || len(info.TypeArgs) == 0 {
		return NoTypeID, 0, false
	}
	baseInfo, okBase := in.StructInfo(in.arrayFixedType)
	if !okBase || baseInfo == nil {
		return NoTypeID, 0, false
	}
	if info.Name != baseInfo.Name {
		return NoTypeID, 0, false
	}

	elem = info.TypeArgs[0]

	if vals := in.StructValueArgs(id); len(vals) > 0 {
		if vals[0] <= uint64(^uint32(0)) {
			if n, err := safecast.Conv[uint32](vals[0]); err == nil {
				return elem, n, true
			}
		}
		return NoTypeID, 0, false
	}

	// Fallback: derive from second type argument (usually a const uint32).
	if len(info.TypeArgs) > 1 {
		if t, ok := in.Lookup(info.TypeArgs[1]); ok && t.Kind == KindConst {
			return elem, t.Count, true
		}
	}
	return NoTypeID, 0, false
}

func resolveAliasAndOwn(in *Interner, id TypeID) TypeID {
	if in == nil {
		return id
	}
	seen := make(map[TypeID]struct{}, 8)
	for id != NoTypeID {
		if _, ok := seen[id]; ok {
			return id
		}
		seen[id] = struct{}{}
		tt, ok := in.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case KindAlias:
			target, ok := in.AliasTarget(id)
			if !ok || target == NoTypeID {
				return id
			}
			id = target
		case KindOwn:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}
