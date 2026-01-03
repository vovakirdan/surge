package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) magicResultForIndex(container, index types.TypeID) types.TypeID {
	if container == types.NoTypeID {
		return types.NoTypeID
	}
	intType := types.NoTypeID
	if tc.types != nil {
		intType = tc.types.Builtins().Int
	}
	for _, recv := range tc.typeKeyCandidates(container) {
		if recv.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recv.key, "__index")
		for _, sig := range methods {
			if sig == nil || len(sig.Params) < 2 {
				continue
			}
			if !tc.selfParamCompatible(container, sig.Params[0], recv.key) {
				continue
			}
			subst := tc.methodSubst(container, recv.key, sig)
			expectedIndex := substituteTypeKeyParams(sig.Params[1], subst)
			if !tc.magicParamCompatible(expectedIndex, index, tc.typeKeyForType(index)) {
				continue
			}
			resultKey := substituteTypeKeyParams(sig.Result, subst)
			res := tc.typeFromKey(resultKey)
			if res == types.NoTypeID {
				if elem, ok := tc.elementType(recv.base); ok && tc.types != nil {
					resultStr := strings.TrimSpace(string(resultKey))
					if strings.HasPrefix(resultStr, "&") {
						mut := strings.HasPrefix(resultStr, "&mut ")
						inner := strings.TrimSpace(strings.TrimPrefix(resultStr, "&mut "))
						if inner == resultStr {
							inner = strings.TrimSpace(strings.TrimPrefix(resultStr, "&"))
						}
						if inner == "T" || typeKeyEqual(symbols.TypeKey(inner), tc.typeKeyForType(elem)) {
							return tc.types.Intern(types.MakeReference(elem, mut))
						}
					}
				}
				if elem, ok := tc.elementType(recv.base); ok {
					if payload, ok := tc.rangePayload(index); ok && intType != types.NoTypeID && tc.sameType(payload, intType) {
						return tc.instantiateArrayType(elem)
					}
					return elem
				}
				continue
			}
			return res
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) magicSignatureForIndexExpr(containerExpr, indexExpr ast.ExprID, container, index types.TypeID) (sig *symbols.FunctionSignature, recvCand typeKeyCandidate, subst map[string]symbols.TypeKey, ambiguous bool, borrowInfo borrowMatchInfo) {
	if container == types.NoTypeID {
		return nil, typeKeyCandidate{}, nil, false, borrowMatchInfo{}
	}
	bestCost := -1
	var bestSig *symbols.FunctionSignature
	var bestRecv typeKeyCandidate
	var bestSubst map[string]symbols.TypeKey
	indexKey := tc.typeKeyForType(index)
	for _, recv := range tc.typeKeyCandidates(container) {
		if recv.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recv.key, "__index")
		for _, method := range methods {
			if method == nil || len(method.Params) < 2 {
				continue
			}
			if !tc.selfParamCompatible(container, method.Params[0], recv.key) {
				continue
			}
			methodSubst := tc.methodSubst(container, recv.key, method)
			expectedIndex := substituteTypeKeyParams(method.Params[1], methodSubst)
			if !tc.magicParamCompatible(expectedIndex, index, indexKey) {
				continue
			}
			costSelf, ok := tc.magicParamCost(substituteTypeKeyParams(method.Params[0], methodSubst), container, containerExpr, &borrowInfo)
			if !ok {
				continue
			}
			costIndex, ok := tc.magicParamCost(expectedIndex, index, indexExpr, &borrowInfo)
			if !ok {
				continue
			}
			cost := costSelf + costIndex
			if bestCost == -1 || cost < bestCost {
				bestCost = cost
				ambiguous = false
				bestSig = method
				bestRecv = recv
				bestSubst = methodSubst
			}
		}
	}
	if bestCost == -1 {
		return nil, typeKeyCandidate{}, nil, false, borrowInfo
	}
	return bestSig, bestRecv, bestSubst, ambiguous, borrowInfo
}

func (tc *typeChecker) magicIndexResultFromSig(sig *symbols.FunctionSignature, recv typeKeyCandidate, subst map[string]symbols.TypeKey, index types.TypeID) types.TypeID {
	if sig == nil {
		return types.NoTypeID
	}
	resultKey := substituteTypeKeyParams(sig.Result, subst)
	res := tc.typeFromKey(resultKey)
	if res != types.NoTypeID {
		return res
	}
	if elem, ok := tc.elementType(recv.base); ok && tc.types != nil {
		resultStr := strings.TrimSpace(string(resultKey))
		if strings.HasPrefix(resultStr, "&") {
			mut := strings.HasPrefix(resultStr, "&mut ")
			inner := strings.TrimSpace(strings.TrimPrefix(resultStr, "&mut "))
			if inner == resultStr {
				inner = strings.TrimSpace(strings.TrimPrefix(resultStr, "&"))
			}
			if inner == "T" || typeKeyEqual(symbols.TypeKey(inner), tc.typeKeyForType(elem)) {
				return tc.types.Intern(types.MakeReference(elem, mut))
			}
		}
		if payload, ok := tc.rangePayload(index); ok {
			intType := tc.types.Builtins().Int
			if intType != types.NoTypeID && tc.sameType(payload, intType) {
				return tc.instantiateArrayType(elem)
			}
		}
		return elem
	}
	return types.NoTypeID
}

func (tc *typeChecker) magicSignatureForIndexSet(container, index, value types.TypeID) *symbols.FunctionSignature {
	if container == types.NoTypeID || value == types.NoTypeID {
		return nil
	}
	indexKey := tc.typeKeyForType(index)
	valueKey := tc.typeKeyForType(value)
	for _, recv := range tc.typeKeyCandidates(container) {
		if recv.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recv.key, "__index_set")
		for _, sig := range methods {
			if sig == nil || len(sig.Params) < 3 {
				continue
			}
			if !tc.selfParamCompatible(container, sig.Params[0], recv.key) {
				continue
			}
			subst := tc.methodSubst(container, recv.key, sig)
			expectedIndex := substituteTypeKeyParams(sig.Params[1], subst)
			if !tc.magicParamCompatible(expectedIndex, index, indexKey) {
				continue
			}
			expectedValue := substituteTypeKeyParams(sig.Params[2], subst)
			if !tc.magicParamCompatible(expectedValue, value, valueKey) {
				continue
			}
			return sig
		}
	}
	return nil
}

func (tc *typeChecker) hasIndexSetter(container, index, value types.TypeID) bool {
	if container == types.NoTypeID || value == types.NoTypeID {
		return false
	}
	base := tc.valueType(container)
	if elem, ok := tc.arrayElemType(base); ok && tc.types != nil {
		intType := tc.types.Builtins().Int
		if index != types.NoTypeID && intType != types.NoTypeID && tc.sameType(index, intType) {
			if tt, ok := tc.types.Lookup(tc.resolveAlias(container)); ok && tt.Kind == types.KindReference && !tt.Mutable {
				return false
			}
			return tc.typesAssignable(elem, value, true)
		}
	}
	for _, recv := range tc.typeKeyCandidates(container) {
		if recv.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recv.key, "__index_set")
		for _, sig := range methods {
			if sig == nil || len(sig.Params) < 3 {
				continue
			}
			if !tc.selfParamCompatible(container, sig.Params[0], recv.key) {
				continue
			}
			subst := tc.methodSubst(container, recv.key, sig)
			expectedIndex := substituteTypeKeyParams(sig.Params[1], subst)
			if !tc.magicParamCompatible(expectedIndex, index, tc.typeKeyForType(index)) {
				continue
			}
			expectedValue := substituteTypeKeyParams(sig.Params[2], subst)
			if !tc.magicParamCompatible(expectedValue, value, tc.typeKeyForType(value)) {
				continue
			}
			return true
		}
	}
	return false
}
