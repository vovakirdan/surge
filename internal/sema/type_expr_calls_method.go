package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) methodResultType(member *ast.ExprMemberData, recv types.TypeID, args []types.TypeID, span source.Span, staticReceiver bool) types.TypeID {
	if member == nil || tc.magic == nil {
		return types.NoTypeID
	}
	name := tc.lookupExportedName(member.Field)
	if name == "" {
		return types.NoTypeID
	}
	if recv != types.NoTypeID {
		if res := tc.boundMethodResult(recv, name, args); res != types.NoTypeID {
			return res
		}
	}
	// Get actual receiver type key once for compatibility checks
	actualRecvKey := tc.typeKeyForType(recv)
	if actualRecvKey == "" {
		tc.report(diag.SemaUnresolvedSymbol, span, "%s has no method %s", tc.typeLabel(recv), name)
		return types.NoTypeID
	}
	for _, recvCand := range tc.typeKeyCandidates(recv) {
		if recvCand.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recvCand.key, name)
		for _, sig := range methods {
			if sig == nil {
				continue
			}
			// Build type param substitution map for generic methods.
			subst := tc.methodSubst(recv, recvCand.key, sig)
			switch {
			case len(sig.Params) == 0:
				// static/associated method without explicit params
				if !staticReceiver || len(args) != 0 {
					continue
				}
			case tc.selfParamCompatible(recv, sig.Params[0], recvCand.key):
				// instance/associated method with compatible self (handles implicit borrow)
				if len(sig.Params)-1 != len(args) {
					continue
				}
				if !tc.methodParamsMatchWithSubst(sig.Params[1:], args, subst) {
					continue
				}
			case staticReceiver && tc.methodParamsMatchWithSubst(sig.Params, args, subst):
				// static method defined in extern block without self param
			case staticReceiver && name == "from_str" && tc.methodParamsMatchWithImplicitBorrow(sig.Params, args, subst):
				// allow implicit borrow for from_str arguments
			default:
				continue
			}
			// Substitute type params in result type key as well
			resultKey := substituteTypeKeyParams(sig.Result, subst)
			res := tc.typeFromKey(resultKey)
			return tc.adjustAliasUnaryResult(res, recvCand)
		}
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "%s has no method %s", tc.typeLabel(recv), name)
	return types.NoTypeID
}

func (tc *typeChecker) recordMethodCallSymbol(callID ast.ExprID, member *ast.ExprMemberData, recv types.TypeID, args []types.TypeID, staticReceiver bool) symbols.SymbolID {
	if callID == ast.NoExprID || member == nil || tc.symbols == nil {
		return symbols.NoSymbolID
	}
	if tc.symbols.ExprSymbols == nil {
		return symbols.NoSymbolID
	}
	symID := tc.resolveMethodCallSymbol(member, recv, args, staticReceiver)
	if symID.IsValid() {
		tc.symbols.ExprSymbols[callID] = symID
	}
	return symID
}

func (tc *typeChecker) recordMethodCallInstantiation(symID symbols.SymbolID, call *ast.ExprCallData, recv types.TypeID, span source.Span) {
	if call == nil || !symID.IsValid() {
		return
	}
	// Check for deprecated method usage
	tc.checkDeprecatedSymbol(symID, "function", span)
	sym := tc.symbolFromID(symID)
	if sym == nil || len(sym.TypeParams) == 0 {
		return
	}
	recvArgs := tc.receiverTypeArgs(recv)
	explicitArgs := tc.resolveCallTypeArgs(call.TypeArgs)
	typeArgs := make([]types.TypeID, 0, len(recvArgs)+len(explicitArgs))
	typeArgs = append(typeArgs, recvArgs...)
	typeArgs = append(typeArgs, explicitArgs...)
	if len(typeArgs) == 0 || len(typeArgs) != len(sym.TypeParams) {
		return
	}
	tc.rememberFunctionInstantiation(symID, typeArgs, span, "call")
}

func (tc *typeChecker) receiverTypeArgs(recv types.TypeID) []types.TypeID {
	if recv == types.NoTypeID || tc.types == nil {
		return nil
	}
	resolved := tc.resolveAlias(recv)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return nil
	}
	if tt.Kind == types.KindOwn || tt.Kind == types.KindReference || tt.Kind == types.KindPointer {
		if tt.Elem != types.NoTypeID {
			resolved = tc.resolveAlias(tt.Elem)
		}
	}
	return tc.typeArgsForType(resolved)
}

func (tc *typeChecker) resolveMethodCallSymbol(member *ast.ExprMemberData, recv types.TypeID, args []types.TypeID, staticReceiver bool) symbols.SymbolID {
	if member == nil || recv == types.NoTypeID {
		return symbols.NoSymbolID
	}
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return symbols.NoSymbolID
	}
	name := tc.lookupExportedName(member.Field)
	if name == "" {
		return symbols.NoSymbolID
	}
	data := tc.symbols.Table.Symbols.Data()
	if data == nil {
		return symbols.NoSymbolID
	}
	for _, recvCand := range tc.typeKeyCandidates(recv) {
		if recvCand.key == "" {
			continue
		}
		for i := len(data) - 1; i >= 0; i-- {
			sym := &data[i]
			if sym.Kind != symbols.SymbolFunction || sym.ReceiverKey == "" || sym.Signature == nil {
				continue
			}
			if tc.symbolName(sym.Name) != name {
				continue
			}
			if !typeKeyEqual(sym.ReceiverKey, recvCand.key) {
				continue
			}
			sig := sym.Signature
			subst := tc.methodSubst(recv, recvCand.key, sig)
			switch {
			case sig.HasSelf:
				if !tc.selfParamCompatible(recv, sig.Params[0], recvCand.key) {
					continue
				}
				if len(sig.Params)-1 != len(args) {
					continue
				}
				if !tc.methodParamsMatchWithSubst(sig.Params[1:], args, subst) {
					continue
				}
			case staticReceiver:
				if len(sig.Params) != len(args) {
					continue
				}
				if !tc.methodParamsMatchWithSubst(sig.Params, args, subst) {
					if name != "from_str" || !tc.methodParamsMatchWithImplicitBorrow(sig.Params, args, subst) {
						continue
					}
				}
			default:
				continue
			}
			// Symbol IDs are bounded by the arena size, which is always < MaxUint32.
			return symbols.SymbolID(i + 1) //nolint:gosec // Add 1 because Data() returns s.data[1:]
		}
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) methodSubst(recv types.TypeID, recvKey symbols.TypeKey, sig *symbols.FunctionSignature) map[string]symbols.TypeKey {
	if sig != nil && sig.HasSelf && len(sig.Params) > 0 {
		if subst := tc.buildTypeParamSubst(recv, sig.Params[0]); len(subst) > 0 {
			return subst
		}
	}
	return tc.buildTypeParamSubst(recv, recvKey)
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

func (tc *typeChecker) methodParamsMatchWithImplicitBorrow(expected []symbols.TypeKey, args []types.TypeID, subst map[string]symbols.TypeKey) bool {
	if len(expected) != len(args) {
		return false
	}
	for i, arg := range args {
		expectedKey := substituteTypeKeyParams(expected[i], subst)
		if !tc.magicParamCompatible(expectedKey, arg, tc.typeKeyForType(arg)) {
			return false
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
	// Apply type parameter substitution if available
	substituted := substituteTypeKeyParams(expected, subst)
	substitutedStr := string(substituted)

	argCopy := tc.isCopyType(arg)
	argOwnNonCopy := false
	if tc.types != nil {
		if tt, ok := tc.types.Lookup(tc.resolveAlias(arg)); ok && tt.Kind == types.KindOwn && !argCopy {
			argOwnNonCopy = true
		}
	}

	// For "own T" params, we accept both "own T" and "T" only for Copy types.
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
		// Also check inner type for "own" params
		if innerExpected != substituted && typeKeyEqual(cand.key, innerExpected) {
			if argCopy {
				return true
			}
		}
	}
	return false
}

// selfParamCompatible checks if receiver type can call method with given self parameter.
// candidateKey is the type key of the candidate we're checking (may be generic like "Option<T>")
// Implements implicit borrow rules from LANGUAGE.md §8.
// Note: Mutability checks for implicit &mut borrow are deferred to borrow-checker.
func (tc *typeChecker) selfParamCompatible(recv types.TypeID, selfKey, candidateKey symbols.TypeKey) bool {
	// Get actual receiver key for compatibility checks
	actualRecvKey := tc.typeKeyForType(recv)

	// Exact match with actual receiver key
	if typeKeyEqual(selfKey, actualRecvKey) {
		return true
	}

	selfStr := string(selfKey)
	recvStr := string(actualRecvKey)

	// Get receiver type info
	recvTT, ok := tc.types.Lookup(tc.resolveAlias(recv))
	if !ok {
		return false
	}

	// For non-reference/non-pointer types: if self matches candidate key, it's compatible
	// This handles generics (Option<int> calling self: Option<T> via candidate Option<T>)
	// and value types calling methods on their base candidate
	if recvTT.Kind == types.KindOwn {
		if typeKeyEqual(selfKey, candidateKey) {
			return tc.isCopyType(recvTT.Elem)
		}
	} else if recvTT.Kind != types.KindReference && recvTT.Kind != types.KindPointer {
		if typeKeyEqual(selfKey, candidateKey) {
			return true
		}
	}

	// Case: receiver is value or own T, self is own T (implicit move)
	if strings.HasPrefix(selfStr, "own ") {
		innerSelf := strings.TrimSpace(strings.TrimPrefix(selfStr, "own "))
		if recvTT.Kind == types.KindOwn {
			innerRecv := tc.typeKeyForType(recvTT.Elem)
			return typeKeyEqual(symbols.TypeKey(innerSelf), innerRecv)
		}
		if recvTT.Kind != types.KindReference && recvTT.Kind != types.KindPointer {
			return typeKeyEqual(candidateKey, symbols.TypeKey(innerSelf)) || typeKeyEqual(actualRecvKey, symbols.TypeKey(innerSelf))
		}
	}

	// Case: receiver is value T or own T, self is &T or &mut T (implicit borrow)
	// Borrow-checker will verify mut binding for &mut case
	if recvTT.Kind != types.KindReference && recvTT.Kind != types.KindPointer {
		if strings.HasPrefix(selfStr, "&") {
			innerSelf := strings.TrimPrefix(selfStr, "&mut ")
			if innerSelf == selfStr {
				innerSelf = strings.TrimPrefix(selfStr, "&")
			}
			innerSelf = strings.TrimSpace(innerSelf)
			// Check against both candidate key and actual recv key
			return typeKeyEqual(candidateKey, symbols.TypeKey(innerSelf)) || typeKeyEqual(actualRecvKey, symbols.TypeKey(innerSelf))
		}
	}

	// Case: receiver is &mut T, self is &T (reborrow as shared)
	if recvTT.Kind == types.KindReference && recvTT.Mutable {
		if strings.HasPrefix(selfStr, "&") && !strings.HasPrefix(selfStr, "&mut ") {
			innerSelf := strings.TrimSpace(strings.TrimPrefix(selfStr, "&"))
			innerRecv := strings.TrimSpace(strings.TrimPrefix(recvStr, "&mut "))
			return typeKeyEqual(symbols.TypeKey(innerSelf), symbols.TypeKey(innerRecv))
		}
	}

	// Case: receiver is own T, self is T, &T, or &mut T
	if recvTT.Kind == types.KindOwn {
		innerRecv := tc.typeKeyForType(recvTT.Elem)
		if typeKeyEqual(selfKey, innerRecv) {
			return tc.isCopyType(recvTT.Elem)
		}
		if strings.HasPrefix(selfStr, "&") {
			innerSelf := strings.TrimPrefix(selfStr, "&mut ")
			if innerSelf == selfStr {
				innerSelf = strings.TrimPrefix(selfStr, "&")
			}
			return typeKeyEqual(symbols.TypeKey(strings.TrimSpace(innerSelf)), innerRecv)
		}
	}

	return false
}
