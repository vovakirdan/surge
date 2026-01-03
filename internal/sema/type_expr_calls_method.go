package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) methodResultType(member *ast.ExprMemberData, recv types.TypeID, recvExpr ast.ExprID, args []types.TypeID, argExprs []ast.ExprID, span source.Span, staticReceiver bool) types.TypeID {
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
	var borrowInfo borrowMatchInfo
	sawReceiverMatch := false
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
			case len(sig.Params) > 0 && tc.selfParamCompatible(recv, sig.Params[0], recvCand.key):
				// instance/associated method with compatible self (handles implicit borrow)
				if !tc.selfParamAddressable(sig.Params[0], recv, recvExpr, &borrowInfo) {
					continue
				}
				sawReceiverMatch = true
				if len(sig.Params)-1 != len(args) {
					continue
				}
				if !tc.methodParamsMatchWithSubst(sig.Params[1:], args, subst) {
					continue
				}
			case staticReceiver:
				sawReceiverMatch = true
				if len(sig.Params) != len(args) {
					continue
				}
				if name == "from_str" {
					if !tc.methodParamsMatchWithSubst(sig.Params, args, subst) {
						if !tc.methodParamsMatchWithImplicitBorrow(sig.Params, args, argExprs, subst, &borrowInfo) {
							continue
						}
					}
				} else if !tc.methodParamsMatchWithSubst(sig.Params, args, subst) {
					continue
				}
			default:
				continue
			}
			// Substitute type params in result type key as well
			resultKey := substituteTypeKeyParams(sig.Result, subst)
			res := tc.typeFromKey(resultKey)
			return tc.adjustAliasUnaryResult(res, recvCand)
		}
	}
	if borrowInfo.expr.IsValid() {
		tc.reportBorrowFailure(&borrowInfo)
		return types.NoTypeID
	}
	if sawReceiverMatch {
		tc.report(diag.SemaNoOverload, span, "no matching overload for %s.%s", tc.typeLabel(recv), name)
		return types.NoTypeID
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "%s has no method %s", tc.typeLabel(recv), name)
	return types.NoTypeID
}

func (tc *typeChecker) recordMethodCallSymbol(callID ast.ExprID, member *ast.ExprMemberData, recv types.TypeID, recvExpr ast.ExprID, args []types.TypeID, argExprs []ast.ExprID, staticReceiver bool) symbols.SymbolID {
	if callID == ast.NoExprID || member == nil || tc.symbols == nil {
		return symbols.NoSymbolID
	}
	if tc.symbols.ExprSymbols == nil {
		return symbols.NoSymbolID
	}
	symID := tc.resolveMethodCallSymbol(member, recv, recvExpr, args, argExprs, staticReceiver)
	if symID.IsValid() {
		tc.symbols.ExprSymbols[callID] = symID
	}
	return symID
}

func (tc *typeChecker) recordMethodCallInstantiation(symID symbols.SymbolID, recv types.TypeID, explicitArgs []types.TypeID, span source.Span) {
	if !symID.IsValid() {
		return
	}
	// Check for deprecated method usage
	tc.checkDeprecatedSymbol(symID, "function", span)
	sym := tc.symbolFromID(symID)
	if sym == nil || len(sym.TypeParams) == 0 {
		return
	}
	recvArgs := tc.receiverTypeArgs(recv)
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

func (tc *typeChecker) resolveMethodCallSymbol(member *ast.ExprMemberData, recv types.TypeID, recvExpr ast.ExprID, args []types.TypeID, argExprs []ast.ExprID, staticReceiver bool) symbols.SymbolID {
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
			if !typeKeyMatchesWithGenerics(sym.ReceiverKey, recvCand.key) {
				continue
			}
			sig := sym.Signature
			subst := tc.methodSubst(recv, recvCand.key, sig)
			switch {
			case sig.HasSelf:
				if !tc.selfParamCompatible(recv, sig.Params[0], recvCand.key) {
					continue
				}
				if !tc.selfParamAddressable(sig.Params[0], recv, recvExpr, nil) {
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
					if name != "from_str" || !tc.methodParamsMatchWithImplicitBorrow(sig.Params, args, argExprs, subst, nil) {
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
	if subst := tc.buildTypeParamSubst(recv, recvKey); len(subst) > 0 {
		return subst
	}
	return tc.receiverTypeParamSubst(recv)
}

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
// candidateKey is the type key of the candidate we're checking (may be generic like "Option<T>")
// Implements implicit borrow rules from LANGUAGE.md §8.
// Note: Mutability checks for implicit &mut borrow are deferred to borrow-checker.
func (tc *typeChecker) selfParamCompatible(recv types.TypeID, selfKey, candidateKey symbols.TypeKey) bool {
	// Get actual receiver key for compatibility checks
	actualRecvKey := tc.typeKeyForType(recv)

	// Exact match with actual receiver key
	if typeKeyMatchesWithGenerics(selfKey, actualRecvKey) {
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
		if typeKeyMatchesWithGenerics(selfKey, candidateKey) {
			return tc.isCopyType(recvTT.Elem)
		}
	} else if recvTT.Kind != types.KindReference && recvTT.Kind != types.KindPointer {
		if typeKeyMatchesWithGenerics(selfKey, candidateKey) {
			return true
		}
	}

	// Case: receiver is value or own T, self is own T (implicit move)
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
			return typeKeyMatchesWithGenerics(candidateKey, symbols.TypeKey(innerSelf)) || typeKeyMatchesWithGenerics(actualRecvKey, symbols.TypeKey(innerSelf))
		}
	}

	// Case: receiver is &T / &mut T, self is &T / &mut T (generic-aware)
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

	// Case: receiver is &mut T, self is &T (reborrow as shared)
	if recvTT.Kind == types.KindReference && recvTT.Mutable {
		if strings.HasPrefix(selfStr, "&") && !strings.HasPrefix(selfStr, "&mut ") {
			innerSelf := strings.TrimSpace(strings.TrimPrefix(selfStr, "&"))
			innerRecv := strings.TrimSpace(strings.TrimPrefix(recvStr, "&mut "))
			return typeKeyMatchesWithGenerics(symbols.TypeKey(innerSelf), symbols.TypeKey(innerRecv))
		}
	}

	// Case: receiver is own T, self is T, &T, or &mut T
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
