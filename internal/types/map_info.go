package types

// MapNominalType returns the built-in nominal Map<K, V> TypeID, if registered.
func (in *Interner) MapNominalType() TypeID {
	if in == nil {
		return NoTypeID
	}
	return in.mapType
}

// MapInfo returns (key, value, true) if id is an instantiation of Map<K, V>.
func (in *Interner) MapInfo(id TypeID) (key TypeID, value TypeID, ok bool) {
	if in == nil || id == NoTypeID || in.mapType == NoTypeID {
		return NoTypeID, NoTypeID, false
	}
	id = resolveAliasAndOwn(in, id)
	info, ok := in.StructInfo(id)
	if !ok || info == nil || len(info.TypeArgs) != 2 {
		return NoTypeID, NoTypeID, false
	}
	baseInfo, ok := in.StructInfo(in.mapType)
	if !ok || baseInfo == nil {
		return NoTypeID, NoTypeID, false
	}
	if info.Name != baseInfo.Name {
		return NoTypeID, NoTypeID, false
	}
	return info.TypeArgs[0], info.TypeArgs[1], true
}
