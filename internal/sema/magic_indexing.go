package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
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
		// A tie is decided only between receiver candidates, where the earlier
		// (more specific) receiver wins. Two overloads of one receiver that
		// cost the same are ambiguous, as in ordinary overload resolution.
		recvCost := -1
		var recvSig *symbols.FunctionSignature
		var recvSubst map[string]symbols.TypeKey
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
			if _, widens := tc.magicIndexWidening(expectedIndex, index); widens {
				costIndex++
			}
			cost := costSelf + costIndex
			switch {
			case recvCost == -1 || cost < recvCost:
				recvCost, recvSig, recvSubst = cost, method, methodSubst
				if bestCost == -1 || cost < bestCost {
					bestCost = cost
					ambiguous = false
					bestSig = method
					bestRecv = recv
					bestSubst = methodSubst
				}
			case cost == recvCost && bestSig == recvSig && !sameMagicSignature(method, methodSubst, recvSig, recvSubst):
				ambiguous = true
			case cost == recvCost && bestSig == recvSig && magicGenericFormals(method, methodSubst) < magicGenericFormals(recvSig, recvSubst):
				// The same instantiated signature, written with fewer generic
				// formals: the concrete overload is the one a method call selects.
				recvSig, recvSubst = method, methodSubst
				bestSig, bestRecv, bestSubst = method, recv, methodSubst
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

// magicSignatureForIndexSet selects `__index_set` the way index resolution
// selects `__index`: a widened index costs one more than an exact one, the
// earlier (more specific) receiver wins a tie, and two overloads of one
// receiver at the same cost are ambiguous.
func (tc *typeChecker) magicSignatureForIndexSet(container, index, value types.TypeID) (sig *symbols.FunctionSignature, subst map[string]symbols.TypeKey, ambiguous bool) {
	if container == types.NoTypeID || value == types.NoTypeID {
		return nil, nil, false
	}
	indexKey := tc.typeKeyForType(index)
	valueKey := tc.typeKeyForType(value)
	bestCost := -1
	for _, recv := range tc.typeKeyCandidates(container) {
		if recv.key == "" {
			continue
		}
		recvCost := -1
		var recvSig *symbols.FunctionSignature
		var recvSubst map[string]symbols.TypeKey
		for _, method := range tc.lookupMagicMethods(recv.key, "__index_set") {
			if method == nil || len(method.Params) < 3 {
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
			expectedValue := substituteTypeKeyParams(method.Params[2], methodSubst)
			if !tc.magicParamCompatible(expectedValue, value, valueKey) {
				continue
			}
			cost := 0
			if _, widens := tc.magicIndexWidening(expectedIndex, index); widens {
				cost++
			}
			switch {
			case recvCost == -1 || cost < recvCost:
				recvCost, recvSig, recvSubst = cost, method, methodSubst
				if bestCost == -1 || cost < bestCost {
					bestCost, sig, subst, ambiguous = cost, method, methodSubst, false
				}
			case cost == recvCost && sig == recvSig && !sameMagicSignature(method, methodSubst, recvSig, recvSubst):
				ambiguous = true
			case cost == recvCost && sig == recvSig && magicGenericFormals(method, methodSubst) < magicGenericFormals(recvSig, recvSubst):
				recvSig, recvSubst = method, methodSubst
				sig, subst = method, methodSubst
			}
		}
	}
	return sig, subst, ambiguous
}

// selectIndexSetter is the `__index_set` an indexed assignment calls, with its
// index widening recorded on indexExpr; nil when none or when overloads tie.
func (tc *typeChecker) selectIndexSetter(container, index, value types.TypeID, indexExpr ast.ExprID) *symbols.FunctionSignature {
	sig, subst, ambiguous := tc.magicSignatureForIndexSet(container, index, value)
	if sig == nil || ambiguous || len(sig.Params) < 3 {
		return nil
	}
	if target, widens := tc.magicIndexWidening(substituteTypeKeyParams(sig.Params[1], subst), index); widens {
		tc.recordNumericWidening(indexExpr, index, target)
	}
	return sig
}

// hasIndexSetterReportingTies is hasIndexSetter that also reports two
// `__index_set` overloads of one receiver pricing the assignment the same.
func (tc *typeChecker) hasIndexSetterReportingTies(container, index, value types.TypeID, span source.Span) bool {
	if !tc.hasIndexSetter(container, index, value) {
		return false
	}
	if _, _, ambiguous := tc.magicSignatureForIndexSet(container, index, value); ambiguous {
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for index assignment")
	}
	return true
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

// magicIndexWidening reports whether an index argument reaches its by-value
// formal only through implicit numeric widening (a fixed-width integer to the
// dynamic `int`, say), and the formal type it widens to. Ordinary overload
// resolution prices that one above an exact match and records the conversion;
// index resolution does the same, so an exact overload wins and the chosen
// body receives a value of its own formal type.
func (tc *typeChecker) magicIndexWidening(expected symbols.TypeKey, index types.TypeID) (types.TypeID, bool) {
	formal := strings.TrimSpace(string(expected))
	if formal == "" || strings.HasPrefix(formal, "&") {
		return types.NoTypeID, false
	}
	formal = strings.TrimSpace(strings.TrimPrefix(formal, "own "))
	target := tc.typeFromKey(symbols.TypeKey(formal))
	if target == types.NoTypeID || !tc.needsNumericWidening(index, target) {
		return types.NoTypeID, false
	}
	return target, true
}

// sameMagicSignature reports whether two magic methods declare one signature
// once the receiver's type arguments are substituted: the same declaration
// reached twice (a mirrored or re-exported copy), or a generic formal that
// the receiver instantiates to the other overload's type, is one candidate,
// as in ordinary overload resolution, not a tie. Of two such candidates the
// one written with fewer generic formals is kept (magicGenericFormals), which
// is the one a method call of the same shape selects.
func sameMagicSignature(a *symbols.FunctionSignature, aSubst map[string]symbols.TypeKey, b *symbols.FunctionSignature, bSubst map[string]symbols.TypeKey) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil || len(a.Params) != len(b.Params) ||
		!typeKeyEqual(substituteTypeKeyParams(a.Result, aSubst), substituteTypeKeyParams(b.Result, bSubst)) {
		return false
	}
	for i := range a.Params {
		if !typeKeyEqual(substituteTypeKeyParams(a.Params[i], aSubst), substituteTypeKeyParams(b.Params[i], bSubst)) {
			return false
		}
	}
	return true
}

// magicGenericFormals counts the formals a receiver substitution rewrites: a
// formal written as the receiver's type parameter rather than a concrete type.
func magicGenericFormals(sig *symbols.FunctionSignature, subst map[string]symbols.TypeKey) int {
	n := 0
	for _, param := range sig.Params {
		if !typeKeyEqual(param, substituteTypeKeyParams(param, subst)) {
			n++
		}
	}
	return n
}
