package sema

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) resolveResultType(okType, errType types.TypeID, span source.Span, scope symbols.ScopeID) types.TypeID {
	if okType == types.NoTypeID || errType == types.NoTypeID || tc.builder == nil {
		return types.NoTypeID
	}
	name := tc.builder.StringsInterner.Intern("Result")
	args := []types.TypeID{okType, errType}
	return tc.resolveNamedType(name, args, nil, span, scope)
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
	if tc.lookupTypeName(id, info.Name) != "Option" {
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
	if tc.lookupTypeName(id, info.Name) != "Result" {
		return 0, 0, false
	}
	return info.TypeArgs[0], info.TypeArgs[1], true
}

func (tc *typeChecker) resolveOptionType(inner types.TypeID, span source.Span, scope symbols.ScopeID) types.TypeID {
	if inner == types.NoTypeID || tc.builder == nil {
		return types.NoTypeID
	}
	name := tc.builder.StringsInterner.Intern("Option")
	args := []types.TypeID{inner}
	return tc.resolveNamedType(name, args, nil, span, scope)
}

func (tc *typeChecker) resolveErrorType(span source.Span, scope symbols.ScopeID) types.TypeID {
	if tc.builder == nil {
		return types.NoTypeID
	}
	errName := tc.builder.StringsInterner.Intern("Error")
	return tc.resolveNamedType(errName, nil, nil, span, scope)
}
