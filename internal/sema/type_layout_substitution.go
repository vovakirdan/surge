package sema

import (
	"surge/internal/symbols"
	"surge/internal/types"
)

// collectLayoutSubstitutions finds only parameters owned by one concrete
// function instantiation. The returned overlay is consumed by layout's proof
// engine; it never materializes types in the canonical interner.
func collectLayoutSubstitutions(
	typesIn *types.Interner,
	owner symbols.SymbolID,
	args []types.TypeID,
	root types.TypeID,
) map[types.TypeID]types.TypeID {
	if typesIn == nil || owner == 0 || root == types.NoTypeID || len(args) == 0 {
		return nil
	}
	out := make(map[types.TypeID]types.TypeID, len(args))
	seen := make(map[types.TypeID]struct{}, 32)
	var walk func(types.TypeID)
	walkList := func(ids []types.TypeID) {
		for _, id := range ids {
			walk(id)
		}
	}
	walk = func(id types.TypeID) { //nolint:gocyclo
		if id == types.NoTypeID {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		t, ok := typesIn.Lookup(id)
		if !ok {
			return
		}
		switch t.Kind {
		case types.KindGenericParam:
			info, infoOK := typesIn.TypeParamInfo(id)
			if !infoOK || info == nil || symbols.SymbolID(info.Owner) != owner {
				return
			}
			index := int(info.Index)
			if index >= 0 && index < len(args) && args[index] != types.NoTypeID && args[index] != id {
				out[id] = args[index]
			}
		case types.KindPointer, types.KindReference, types.KindOwn, types.KindFar, types.KindArray:
			walk(t.Elem)
		case types.KindTuple:
			if info, infoOK := typesIn.TupleInfo(id); infoOK && info != nil {
				walkList(info.Elems)
			}
		case types.KindFn:
			if info, infoOK := typesIn.FnInfo(id); infoOK && info != nil {
				walkList(info.Params)
				walk(info.Result)
			}
		case types.KindStruct:
			if info, infoOK := typesIn.StructInfo(id); infoOK && info != nil {
				walkList(info.TypeArgs)
				walk(info.BaseType)
				for _, field := range info.Fields {
					walk(field.Type)
				}
			}
		case types.KindUnion:
			if info, infoOK := typesIn.UnionInfo(id); infoOK && info != nil {
				walkList(info.TypeArgs)
				for _, member := range info.Members {
					walk(member.Type)
					walkList(member.TagArgs)
				}
			}
		case types.KindAlias:
			if info, infoOK := typesIn.AliasInfo(id); infoOK && info != nil {
				walkList(info.TypeArgs)
				walk(info.Target)
			}
		case types.KindEnum:
			if info, infoOK := typesIn.EnumInfo(id); infoOK && info != nil {
				walkList(info.TypeArgs)
				walk(info.BaseType)
			}
		}
	}
	walk(root)
	if len(out) == 0 {
		return nil
	}
	return out
}
