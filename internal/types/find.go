package types //nolint:revive

import (
	"slices"

	"surge/internal/source"
)

// FindStructInstance returns a struct TypeID whose name and type arguments match args.
func (in *Interner) FindStructInstance(name source.StringID, args []TypeID) (TypeID, bool) {
	if in == nil || name == source.NoStringID {
		return NoTypeID, false
	}
	for id := TypeID(1); int(id) < len(in.types); id++ {
		if in.types[id].Kind != KindStruct {
			continue
		}
		info, ok := in.StructInfo(id)
		if !ok || info == nil {
			continue
		}
		if info.Name != name {
			continue
		}
		if slices.Equal(info.TypeArgs, args) {
			return id, true
		}
	}
	return NoTypeID, false
}

// FindUnionInstance returns a union TypeID whose name and type arguments match args.
func (in *Interner) FindUnionInstance(name source.StringID, args []TypeID) (TypeID, bool) {
	if in == nil || name == source.NoStringID {
		return NoTypeID, false
	}
	for id := TypeID(1); int(id) < len(in.types); id++ {
		if in.types[id].Kind != KindUnion {
			continue
		}
		info, ok := in.UnionInfo(id)
		if !ok || info == nil {
			continue
		}
		if info.Name != name {
			continue
		}
		if slices.Equal(info.TypeArgs, args) {
			return id, true
		}
	}
	return NoTypeID, false
}

// FindAliasInstance returns an alias TypeID whose name and type arguments match args.
func (in *Interner) FindAliasInstance(name source.StringID, args []TypeID) (TypeID, bool) {
	if in == nil || name == source.NoStringID {
		return NoTypeID, false
	}
	for id := TypeID(1); int(id) < len(in.types); id++ {
		if in.types[id].Kind != KindAlias {
			continue
		}
		info, ok := in.AliasInfo(id)
		if !ok || info == nil {
			continue
		}
		if info.Name != name {
			continue
		}
		if slices.Equal(info.TypeArgs, args) {
			return id, true
		}
	}
	return NoTypeID, false
}
