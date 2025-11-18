package sema

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type resultKey struct {
	ok  types.TypeID
	err types.TypeID
}

func (tc *typeChecker) resolveResultType(okType, errType types.TypeID, span source.Span, scope symbols.ScopeID) types.TypeID {
	if okType == types.NoTypeID || errType == types.NoTypeID || tc.builder == nil || tc.types == nil {
		return types.NoTypeID
	}
	key := resultKey{ok: okType, err: errType}
	if res := tc.resultTypes[key]; res != types.NoTypeID {
		return res
	}
	name := tc.builder.StringsInterner.Intern("Result")
	okTag := tc.builder.StringsInterner.Intern("Ok")
	errTag := tc.builder.StringsInterner.Intern("Err")
	id := tc.types.RegisterUnionInstance(name, span, []types.TypeID{okType, errType})
	tc.types.SetUnionMembers(id, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: okTag, TagArgs: []types.TypeID{okType}},
		{Kind: types.UnionMemberTag, TagName: errTag, TagArgs: []types.TypeID{errType}},
	})
	tc.resultTypes[key] = id
	tc.setSymbolTypeByName("Result", id)
	return id
}

func (tc *typeChecker) optionPayload(id types.TypeID) (types.TypeID, bool) {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID, false
	}
	id = tc.resolveAlias(id)
	info, ok := tc.types.UnionInfo(id)
	if !ok || info == nil || len(info.TypeArgs) != 1 {
		return types.NoTypeID, false
	}
	if tc.lookupName(info.Name) != "Option" {
		return types.NoTypeID, false
	}
	return info.TypeArgs[0], true
}

func (tc *typeChecker) resultPayload(id types.TypeID) (okType, errType types.TypeID, ok bool) {
	if id == types.NoTypeID || tc.types == nil {
		return 0, 0, false
	}
	id = tc.resolveAlias(id)
	info, okInfo := tc.types.UnionInfo(id)
	if !okInfo || info == nil || len(info.TypeArgs) != 2 {
		return 0, 0, false
	}
	if tc.lookupName(info.Name) != "Result" {
		return 0, 0, false
	}
	return info.TypeArgs[0], info.TypeArgs[1], true
}

func (tc *typeChecker) resolveOptionType(inner types.TypeID, span source.Span, scope symbols.ScopeID) types.TypeID {
	if inner == types.NoTypeID || tc.builder == nil || tc.types == nil {
		return types.NoTypeID
	}
	if existing := tc.optionTypes[inner]; existing != types.NoTypeID {
		return existing
	}
	name := tc.builder.StringsInterner.Intern("Option")
	some := tc.builder.StringsInterner.Intern("Some")
	id := tc.types.RegisterUnionInstance(name, span, []types.TypeID{inner})
	tc.types.SetUnionMembers(id, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: some, TagArgs: []types.TypeID{inner}},
		{Kind: types.UnionMemberNothing, Type: tc.types.Builtins().Nothing},
	})
	tc.optionTypes[inner] = id
	tc.setSymbolTypeByName("Option", id)
	return id
}

func (tc *typeChecker) resolveErrorType(span source.Span, scope symbols.ScopeID) types.TypeID {
	if tc.types == nil || tc.builder == nil {
		return types.NoTypeID
	}
	if tc.errorType != types.NoTypeID {
		return tc.errorType
	}
	errName := tc.builder.StringsInterner.Intern("Error")
	stringID := tc.builder.StringsInterner.Intern("string")
	uintID := tc.builder.StringsInterner.Intern("uint")
	errType := tc.types.RegisterStruct(errName, span)
	tc.types.SetStructFields(errType, []types.StructField{
		{Name: stringID, Type: tc.types.Builtins().String},
		{Name: uintID, Type: tc.types.Builtins().Uint},
	})
	tc.errorType = errType
	tc.setSymbolTypeByName("Error", errType)
	return errType
}

func (tc *typeChecker) setSymbolTypeByName(literal string, typeID types.TypeID) {
	if typeID == types.NoTypeID || tc.builder == nil || tc.symbols == nil || tc.symbols.Table == nil {
		return
	}
	nameID := tc.builder.StringsInterner.Intern(literal)
	symID := tc.lookupTypeSymbol(nameID, tc.fileScope())
	if !symID.IsValid() {
		return
	}
	tc.assignSymbolType(symID, typeID)
}
