package sema

import (
	"strconv"
	"strings"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) instantiationKey(symID symbols.SymbolID, args []types.TypeID) string {
	if !symID.IsValid() {
		return ""
	}
	var b strings.Builder
	b.WriteString(strconv.FormatUint(uint64(symID), 10))
	for _, arg := range args {
		b.WriteByte('#')
		b.WriteString(strconv.FormatUint(uint64(arg), 10))
	}
	return b.String()
}

func (tc *typeChecker) cachedInstantiation(key string) types.TypeID {
	if key == "" || tc.typeInstantiations == nil {
		return types.NoTypeID
	}
	if cached, ok := tc.typeInstantiations[key]; ok {
		return cached
	}
	return types.NoTypeID
}

func (tc *typeChecker) rememberInstantiation(key string, typeID types.TypeID) {
	if key == "" || typeID == types.NoTypeID || tc.typeInstantiations == nil {
		return
	}
	tc.typeInstantiations[key] = typeID
}

func (tc *typeChecker) instantiateType(symID symbols.SymbolID, args []types.TypeID) types.TypeID {
	key := tc.instantiationKey(symID, args)
	if cached := tc.cachedInstantiation(key); cached != types.NoTypeID {
		return cached
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return types.NoTypeID
	}
	item := tc.builder.Items.Get(sym.Decl.Item)
	if (item == nil || item.Kind != ast.ItemType) && (sym.Flags&symbols.SymbolFlagImported != 0 || sym.Flags&symbols.SymbolFlagBuiltin != 0) {
		if instantiated := tc.instantiateImportedType(sym, args); instantiated != types.NoTypeID {
			tc.rememberInstantiation(key, instantiated)
			return instantiated
		}
	}
	if item == nil || item.Kind != ast.ItemType {
		return types.NoTypeID
	}
	typeItem, ok := tc.builder.Items.Type(sym.Decl.Item)
	if !ok || typeItem == nil {
		return types.NoTypeID
	}

	var instantiated types.TypeID
	switch typeItem.Kind {
	case ast.TypeDeclStruct:
		instantiated = tc.instantiateStruct(typeItem, symID, args)
	case ast.TypeDeclAlias:
		instantiated = tc.instantiateAlias(typeItem, symID, args)
	case ast.TypeDeclUnion:
		instantiated = tc.instantiateUnion(typeItem, symID, args)
	default:
		instantiated = types.NoTypeID
	}
	tc.rememberInstantiation(key, instantiated)
	return instantiated
}

func (tc *typeChecker) instantiateImportedType(sym *symbols.Symbol, args []types.TypeID) types.TypeID {
	if tc.types == nil || sym == nil || sym.Type == types.NoTypeID {
		return types.NoTypeID
	}
	base := tc.resolveAlias(sym.Type)
	if info, ok := tc.types.UnionInfo(base); ok && info != nil {
		members := make([]types.UnionMember, len(info.Members))
		for i, member := range info.Members {
			members[i] = member
			members[i].Type = tc.substituteImportedType(member.Type, args)
			if len(member.TagArgs) > 0 {
				tagArgs := make([]types.TypeID, len(member.TagArgs))
				for j, arg := range member.TagArgs {
					tagArgs[j] = tc.substituteImportedType(arg, args)
				}
				members[i].TagArgs = tagArgs
			}
		}
		instantiated := tc.types.RegisterUnionInstance(info.Name, info.Decl, append([]types.TypeID(nil), args...))
		tc.types.SetUnionMembers(instantiated, members)
		if name := tc.lookupName(sym.Name); name != "" {
			tc.recordTypeName(instantiated, name)
		}
		return instantiated
	}
	if info, ok := tc.types.StructInfo(base); ok && info != nil {
		if len(info.TypeParams) == 0 {
			return base
		}
		if len(args) != len(info.TypeParams) {
			return types.NoTypeID
		}
		mapping := make(map[types.TypeID]types.TypeID, len(info.TypeParams))
		for i, param := range info.TypeParams {
			mapping[tc.resolveAlias(param)] = args[i]
		}
		fields := make([]types.StructField, len(info.Fields))
		for i, field := range info.Fields {
			fields[i] = types.StructField{
				Name: field.Name,
				Type: tc.substituteTypeParams(field.Type, mapping),
			}
		}
		instantiated := tc.types.RegisterStructInstance(info.Name, info.Decl, args)
		tc.types.SetStructFields(instantiated, fields)
		if len(info.ValueArgs) > 0 {
			tc.types.SetStructValueArgs(instantiated, info.ValueArgs)
		}
		if name := tc.lookupName(sym.Name); name != "" {
			tc.recordTypeName(instantiated, name)
		}
		return instantiated
	}
	return types.NoTypeID
}

func (tc *typeChecker) substituteImportedType(id types.TypeID, args []types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return id
	}
	resolved := tc.resolveAlias(id)
	if info, ok := tc.types.TypeParamInfo(resolved); ok && info != nil {
		if idx := int(info.Index); idx >= 0 && idx < len(args) && args[idx] != types.NoTypeID {
			return args[idx]
		}
		return id
	}
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return resolved
	}
	switch tt.Kind {
	case types.KindArray, types.KindPointer, types.KindReference, types.KindOwn:
		elem := tc.substituteImportedType(tt.Elem, args)
		if elem == tt.Elem {
			return resolved
		}
		clone := tt
		clone.Elem = elem
		return tc.types.Intern(clone)
	case types.KindConst:
		return resolved
	case types.KindStruct:
		if elem, ok := tc.arrayElemType(resolved); ok {
			inner := tc.substituteImportedType(elem, args)
			if inner == elem {
				return resolved
			}
			return tc.instantiateArrayType(inner)
		}
		return resolved
	default:
		return resolved
	}
}
