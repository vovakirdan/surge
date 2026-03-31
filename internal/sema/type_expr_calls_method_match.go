package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) receiverTypeParamSubst(recv types.TypeID) map[string]symbols.TypeKey {
	if recv == types.NoTypeID || tc.types == nil {
		return nil
	}
	resolved := tc.resolveAlias(recv)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return nil
	}
	switch tt.Kind {
	case types.KindOwn, types.KindReference, types.KindPointer:
		if tt.Elem == types.NoTypeID {
			return nil
		}
		return tc.receiverTypeParamSubst(tt.Elem)
	}

	var typeArgs []types.TypeID
	var typeParams []types.TypeID
	var typeParamNames []source.StringID
	typeName := source.NoStringID
	switch tt.Kind {
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
			typeArgs = info.TypeArgs
			typeParams = info.TypeParams
			typeName = info.Name
		}
	case types.KindUnion:
		if info, ok := tc.types.UnionInfo(resolved); ok && info != nil {
			typeArgs = info.TypeArgs
			typeName = info.Name
		}
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
			typeArgs = info.TypeArgs
			typeName = info.Name
		}
	}
	if len(typeParams) > 0 {
		typeParamNames = make([]source.StringID, 0, len(typeParams))
		for _, param := range typeParams {
			typeParamNames = append(typeParamNames, tc.typeParamNames[param])
		}
	}
	if len(typeParamNames) == 0 && typeName != source.NoStringID {
		scope := tc.fileScope()
		if !scope.IsValid() {
			scope = tc.scopeOrFile(tc.currentScope())
		}
		symID := tc.lookupTypeSymbol(typeName, scope)
		if !symID.IsValid() {
			if anySymID := tc.lookupSymbolAny(typeName, scope); anySymID.IsValid() {
				if sym := tc.symbolFromID(anySymID); sym != nil && sym.Kind == symbols.SymbolType {
					symID = anySymID
				}
			}
		}
		if symID.IsValid() {
			if sym := tc.symbolFromID(symID); sym != nil {
				if len(sym.TypeParamSymbols) > 0 {
					typeParamNames = make([]source.StringID, 0, len(sym.TypeParamSymbols))
					for _, tp := range sym.TypeParamSymbols {
						typeParamNames = append(typeParamNames, tp.Name)
					}
				} else if len(sym.TypeParams) > 0 {
					typeParamNames = sym.TypeParams
				}
			}
		}
	}
	if len(typeParamNames) == 0 || len(typeArgs) != len(typeParamNames) {
		return nil
	}
	subst := make(map[string]symbols.TypeKey, len(typeParamNames))
	for i, paramName := range typeParamNames {
		if paramName == source.NoStringID {
			continue
		}
		name := tc.lookupName(paramName)
		if name == "" {
			continue
		}
		argKey := tc.typeKeyForType(typeArgs[i])
		if argKey == "" {
			continue
		}
		subst[name] = argKey
	}
	if len(subst) == 0 {
		return nil
	}
	return subst
}

func (tc *typeChecker) methodParamsMatchWithSubst(expected []symbols.TypeKey, args []types.TypeID, subst map[string]symbols.TypeKey) bool {
	if len(expected) != len(args) {
		return false
	}
	for i, arg := range args {
		if !tc.methodParamMatchesWithSubst(expected[i], arg, subst) {
			return false
		}
	}
	return true
}

func (tc *typeChecker) methodParamsMatchWithImplicitBorrow(expected []symbols.TypeKey, args []types.TypeID, argExprs []ast.ExprID, subst map[string]symbols.TypeKey, info *borrowMatchInfo) bool {
	if len(expected) != len(args) {
		return false
	}
	for i, arg := range args {
		expectedKey := substituteTypeKeyParams(expected[i], subst)
		if !tc.magicParamCompatible(expectedKey, arg, tc.typeKeyForType(arg)) {
			return false
		}
		expectedStr := strings.TrimSpace(string(expectedKey))
		if strings.HasPrefix(expectedStr, "&") && !tc.isReferenceType(arg) {
			if i >= len(argExprs) || !argExprs[i].IsValid() {
				continue
			}
			expectedType := tc.typeFromKey(expectedKey)
			if tc.isBorrowableStringLiteral(argExprs[i], expectedType) {
				continue
			}
			if tc.canMaterializeForRefString(argExprs[i], expectedType) {
				continue
			}
			if strings.HasPrefix(expectedStr, "&mut ") {
				if !tc.isAddressableExpr(argExprs[i]) {
					if info != nil {
						info.record(argExprs[i], true, borrowFailureNotAddressable)
					}
					return false
				}
				if !tc.isMutablePlaceExpr(argExprs[i]) {
					if info != nil {
						info.record(argExprs[i], true, borrowFailureImmutable)
					}
					return false
				}
			} else if !tc.isAddressableExpr(argExprs[i]) {
				if info != nil {
					info.record(argExprs[i], false, borrowFailureNotAddressable)
				}
				return false
			}
		}
	}
	return true
}

func (tc *typeChecker) methodParamMatches(expected symbols.TypeKey, arg types.TypeID) bool {
	return tc.methodParamMatchesWithSubst(expected, arg, nil)
}

func (tc *typeChecker) methodParamMatchesWithSubst(expected symbols.TypeKey, arg types.TypeID, subst map[string]symbols.TypeKey) bool {
	if expected == "" {
		return false
	}
	substituted := substituteTypeKeyParams(expected, subst)
	substitutedStr := string(substituted)

	argCopy := tc.isCopyType(arg)
	argOwnNonCopy := false
	if tc.types != nil {
		if tt, ok := tc.types.Lookup(tc.resolveAlias(arg)); ok && tt.Kind == types.KindOwn && !argCopy {
			argOwnNonCopy = true
		}
	}

	innerExpected := substituted
	if after, found := strings.CutPrefix(substitutedStr, "own "); found {
		innerExpected = symbols.TypeKey(strings.TrimSpace(after))
	}

	for _, cand := range tc.typeKeyCandidates(arg) {
		if typeKeyEqual(cand.key, substituted) {
			if argOwnNonCopy && !strings.HasPrefix(substitutedStr, "own ") {
				continue
			}
			return true
		}
		if innerExpected != substituted && typeKeyEqual(cand.key, innerExpected) && argCopy {
			return true
		}
	}

	if expectedType := tc.typeFromKey(substituted); expectedType != types.NoTypeID {
		if tc.methodResolvedTypeMatches(expectedType, arg) {
			return !argOwnNonCopy || strings.HasPrefix(substitutedStr, "own ")
		}
		if tc.isUnionMember(expectedType, arg) {
			return true
		}
	}
	return false
}

func (tc *typeChecker) methodResolvedTypeMatches(expectedType, actual types.TypeID) bool {
	if tc == nil || expectedType == types.NoTypeID || actual == types.NoTypeID {
		return false
	}
	if expectedType == actual {
		return true
	}
	expectedResolved := tc.resolveAlias(expectedType)
	actualResolved := tc.resolveAlias(actual)
	if expectedResolved == actualResolved {
		return true
	}
	expectedKey := tc.typeKeyForType(expectedType)
	actualKey := tc.typeKeyForType(actual)
	return expectedKey != "" && typeKeyEqual(expectedKey, actualKey)
}
