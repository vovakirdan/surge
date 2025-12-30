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
			if !tc.methodParamMatches(sig.Params[1], index) {
				continue
			}
			res := tc.typeFromKey(sig.Result)
			if res == types.NoTypeID {
				if elem, ok := tc.elementType(recv.base); ok && tc.types != nil {
					resultStr := strings.TrimSpace(string(sig.Result))
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

func (tc *typeChecker) magicSignatureForIndexExpr(containerExpr, indexExpr ast.ExprID, container, index types.TypeID) (sig *symbols.FunctionSignature, recvCand typeKeyCandidate, ambiguous bool, borrowInfo borrowMatchInfo) {
	if container == types.NoTypeID {
		return nil, typeKeyCandidate{}, false, borrowMatchInfo{}
	}
	bestCost := -1
	var bestSig *symbols.FunctionSignature
	var bestRecv typeKeyCandidate
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
			if !tc.methodParamMatches(method.Params[1], index) {
				continue
			}
			costSelf, ok := tc.magicParamCost(method.Params[0], container, containerExpr, &borrowInfo)
			if !ok {
				continue
			}
			costIndex, ok := tc.magicParamCost(method.Params[1], index, indexExpr, &borrowInfo)
			if !ok {
				continue
			}
			cost := costSelf + costIndex
			if bestCost == -1 || cost < bestCost {
				bestCost = cost
				ambiguous = false
				bestSig = method
				bestRecv = recv
			}
		}
	}
	if bestCost == -1 {
		return nil, typeKeyCandidate{}, false, borrowInfo
	}
	return bestSig, bestRecv, ambiguous, borrowInfo
}

func (tc *typeChecker) magicIndexResultFromSig(sig *symbols.FunctionSignature, recv typeKeyCandidate, index types.TypeID) types.TypeID {
	if sig == nil {
		return types.NoTypeID
	}
	res := tc.typeFromKey(sig.Result)
	if res != types.NoTypeID {
		return res
	}
	if elem, ok := tc.elementType(recv.base); ok && tc.types != nil {
		resultStr := strings.TrimSpace(string(sig.Result))
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
			if !tc.methodParamMatches(sig.Params[1], index) {
				continue
			}
			if !tc.methodParamMatches(sig.Params[2], value) {
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
			if !tc.methodParamMatches(sig.Params[1], index) {
				continue
			}
			if !tc.methodParamMatches(sig.Params[2], value) {
				continue
			}
			return true
		}
	}
	return false
}
