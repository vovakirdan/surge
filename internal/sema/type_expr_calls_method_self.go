package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) selfParamAddressable(selfKey symbols.TypeKey, recv types.TypeID, recvExpr ast.ExprID, info *borrowMatchInfo) bool {
	selfStr := strings.TrimSpace(string(selfKey))
	switch {
	case strings.HasPrefix(selfStr, "&mut "):
		if tc.isReferenceType(recv) {
			return true
		}
		if !recvExpr.IsValid() {
			return true
		}
		if !tc.isAddressableExpr(recvExpr) {
			if info != nil {
				info.record(recvExpr, true, borrowFailureNotAddressable)
			}
			return false
		}
		if !tc.isMutablePlaceExpr(recvExpr) {
			if info != nil {
				info.record(recvExpr, true, borrowFailureImmutable)
			}
			return false
		}
		return true
	case strings.HasPrefix(selfStr, "&"):
		if tc.isReferenceType(recv) {
			return true
		}
		if !recvExpr.IsValid() {
			return true
		}
		expectedType := tc.typeFromKey(selfKey)
		if tc.isBorrowableStringLiteral(recvExpr, expectedType) {
			return true
		}
		if tc.canMaterializeForRefString(recvExpr, expectedType) {
			return true
		}
		if tc.isAddressableExpr(recvExpr) {
			return true
		}
		if info != nil {
			info.record(recvExpr, false, borrowFailureNotAddressable)
		}
		return false
	default:
		return true
	}
}

// selfParamCompatible checks if receiver type can call method with given self parameter.
// candidateKey is the type key of the candidate we're checking (may be generic like "Option<T>").
func (tc *typeChecker) selfParamCompatible(recv types.TypeID, selfKey, candidateKey symbols.TypeKey) bool {
	actualRecvKey := tc.typeKeyForType(recv)
	if typeKeyMatchesWithGenerics(selfKey, actualRecvKey) {
		return true
	}

	selfStr := string(selfKey)
	recvStr := string(actualRecvKey)
	recvTT, ok := tc.types.Lookup(tc.resolveAlias(recv))
	if !ok {
		return false
	}

	if recvTT.Kind == types.KindOwn {
		if typeKeyMatchesWithGenerics(selfKey, candidateKey) {
			return tc.isCopyType(recvTT.Elem)
		}
	} else if recvTT.Kind != types.KindReference && recvTT.Kind != types.KindPointer {
		if typeKeyMatchesWithGenerics(selfKey, candidateKey) {
			return true
		}
	}

	if strings.HasPrefix(selfStr, "own ") {
		innerSelf := strings.TrimSpace(strings.TrimPrefix(selfStr, "own "))
		if recvTT.Kind == types.KindOwn {
			innerRecv := tc.typeKeyForType(recvTT.Elem)
			return typeKeyMatchesWithGenerics(symbols.TypeKey(innerSelf), innerRecv)
		}
		if recvTT.Kind != types.KindReference && recvTT.Kind != types.KindPointer {
			return typeKeyMatchesWithGenerics(candidateKey, symbols.TypeKey(innerSelf)) || typeKeyMatchesWithGenerics(actualRecvKey, symbols.TypeKey(innerSelf))
		}
	}

	if recvTT.Kind != types.KindReference && recvTT.Kind != types.KindPointer && strings.HasPrefix(selfStr, "&") {
		innerSelf := strings.TrimPrefix(selfStr, "&mut ")
		if innerSelf == selfStr {
			innerSelf = strings.TrimPrefix(selfStr, "&")
		}
		innerSelf = strings.TrimSpace(innerSelf)
		return typeKeyMatchesWithGenerics(candidateKey, symbols.TypeKey(innerSelf)) || typeKeyMatchesWithGenerics(actualRecvKey, symbols.TypeKey(innerSelf))
	}

	if recvTT.Kind == types.KindReference && strings.HasPrefix(selfStr, "&") {
		innerSelf := strings.TrimPrefix(selfStr, "&mut ")
		if innerSelf == selfStr {
			innerSelf = strings.TrimPrefix(selfStr, "&")
		}
		innerSelf = strings.TrimSpace(innerSelf)
		innerRecv := strings.TrimPrefix(recvStr, "&mut ")
		if innerRecv == recvStr {
			innerRecv = strings.TrimPrefix(recvStr, "&")
		}
		innerRecv = strings.TrimSpace(innerRecv)
		if typeKeyMatchesWithGenerics(symbols.TypeKey(innerSelf), symbols.TypeKey(innerRecv)) {
			return true
		}
		return typeKeyMatchesWithGenerics(symbols.TypeKey(innerSelf), candidateKey)
	}

	if recvTT.Kind == types.KindReference && recvTT.Mutable {
		if strings.HasPrefix(selfStr, "&") && !strings.HasPrefix(selfStr, "&mut ") {
			innerSelf := strings.TrimSpace(strings.TrimPrefix(selfStr, "&"))
			innerRecv := strings.TrimSpace(strings.TrimPrefix(recvStr, "&mut "))
			return typeKeyMatchesWithGenerics(symbols.TypeKey(innerSelf), symbols.TypeKey(innerRecv))
		}
	}

	if recvTT.Kind == types.KindOwn {
		innerRecv := tc.typeKeyForType(recvTT.Elem)
		if typeKeyMatchesWithGenerics(selfKey, innerRecv) {
			return tc.isCopyType(recvTT.Elem)
		}
		if strings.HasPrefix(selfStr, "&") {
			innerSelf := strings.TrimPrefix(selfStr, "&mut ")
			if innerSelf == selfStr {
				innerSelf = strings.TrimPrefix(selfStr, "&")
			}
			return typeKeyMatchesWithGenerics(symbols.TypeKey(strings.TrimSpace(innerSelf)), innerRecv)
		}
	}

	return false
}
