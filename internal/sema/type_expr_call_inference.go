package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) expectedTypeArgCount(candidates []symbols.SymbolID) int {
	for _, id := range candidates {
		if sym := tc.symbolFromID(id); sym != nil && len(sym.TypeParams) > 0 {
			return len(sym.TypeParams)
		}
	}
	return 0
}

func (tc *typeChecker) missingTypeParams(candidates []symbols.SymbolID, args []callArg) []string {
	for _, id := range candidates {
		if sym := tc.symbolFromID(id); sym != nil {
			if missing, ok := tc.inferMissingTypeParams(sym, args); ok {
				return missing
			}
		}
	}
	return nil
}

func (tc *typeChecker) inferMissingTypeParams(sym *symbols.Symbol, args []callArg) ([]string, bool) {
	if sym == nil || sym.Signature == nil || len(sym.TypeParams) == 0 {
		return nil, false
	}
	sig := sym.Signature
	variadicIndex := -1
	for i, v := range sig.Variadic {
		if v {
			variadicIndex = i
			break
		}
	}
	paramCount := len(sig.Params)
	if variadicIndex >= 0 {
		if len(args) < paramCount-1 {
			return nil, false
		}
	} else if len(args) != paramCount {
		return nil, false
	}

	paramNames, paramSet := tc.typeParamNameSet(sym)
	bindings := make(map[string]types.TypeID)
	for i, arg := range args {
		paramIndex := i
		if variadicIndex >= 0 && i >= variadicIndex {
			paramIndex = variadicIndex
		}
		expectedKey := sig.Params[paramIndex]
		expectedType := tc.instantiateTypeKeyWithInference(expectedKey, arg.ty, bindings, paramSet)
		if expectedType == types.NoTypeID {
			return nil, false
		}
		allowImplicitTo := tc.callAllowsImplicitTo(sym, paramIndex)
		if _, ok := tc.matchArgument(expectedType, arg.ty, arg.isLiteral, allowImplicitTo); !ok {
			return nil, false
		}
	}

	var missing []string
	for _, name := range paramNames {
		if bindings[name] == types.NoTypeID {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil, false
	}
	return missing, true
}

func (tc *typeChecker) candidateKey(sym *symbols.Symbol) string {
	if sym == nil || sym.Signature == nil {
		return ""
	}
	var b strings.Builder
	for i, p := range sym.Signature.Params {
		b.WriteString(string(p))
		if i < len(sym.Signature.Variadic) && sym.Signature.Variadic[i] {
			b.WriteString("...")
		}
		b.WriteByte('|')
	}
	b.WriteString("->")
	b.WriteString(string(sym.Signature.Result))
	return b.String()
}

func (tc *typeChecker) resolveCallTypeArgs(typeArgs []ast.TypeID) []types.TypeID {
	if len(typeArgs) == 0 {
		return nil
	}
	scope := tc.scopeOrFile(tc.currentScope())
	resolved := make([]types.TypeID, len(typeArgs))
	for i, arg := range typeArgs {
		if arg.IsValid() {
			resolved[i] = tc.resolveTypeExprWithScope(arg, scope)
		}
	}
	return resolved
}

func (tc *typeChecker) typeParamNameSet(sym *symbols.Symbol) (names []string, set map[string]struct{}) {
	if sym == nil || len(sym.TypeParams) == 0 {
		return nil, nil
	}
	names = make([]string, 0, len(sym.TypeParams))
	set = make(map[string]struct{}, len(sym.TypeParams))
	for _, id := range sym.TypeParams {
		if name := tc.lookupName(id); name != "" {
			names = append(names, name)
			set[name] = struct{}{}
		}
	}
	return names, set
}

func (tc *typeChecker) evaluateFunctionCandidate(sym *symbols.Symbol, args []callArg, typeArgs []types.TypeID) (cost int, result types.TypeID, concrete []types.TypeID, ok bool) {
	if sym == nil || sym.Signature == nil {
		return 0, types.NoTypeID, nil, false
	}
	sig := sym.Signature

	// Reorder args if any are named
	hasNamed := false
	for _, arg := range args {
		if arg.name != source.NoStringID {
			hasNamed = true
			break
		}
	}
	if hasNamed {
		reordered, success := tc.reorderArgsForSignature(sig, args)
		if !success {
			return 0, types.NoTypeID, nil, false
		}
		args = reordered
	}

	variadicIndex := -1
	for i, v := range sig.Variadic {
		if v {
			variadicIndex = i
			break
		}
	}
	paramCount := len(sig.Params)

	// Count required params (those without defaults)
	requiredParams := 0
	if len(sig.Defaults) == paramCount {
		for i, hasDefault := range sig.Defaults {
			if !hasDefault && (variadicIndex < 0 || i != variadicIndex) {
				requiredParams++
			}
		}
	} else {
		// Old behavior: no defaults info, all params are required
		requiredParams = paramCount
	}

	// Arity check with default parameters support
	if variadicIndex >= 0 {
		if len(args) < paramCount-1 {
			return 0, types.NoTypeID, nil, false
		}
	} else {
		// Check: args >= requiredParams && args <= paramCount
		if len(args) < requiredParams || len(args) > paramCount {
			return 0, types.NoTypeID, nil, false
		}
	}

	paramNames, paramSet := tc.typeParamNameSet(sym)
	bindings := make(map[string]types.TypeID)
	if len(typeArgs) > 0 {
		if len(typeArgs) != len(paramNames) {
			return 0, types.NoTypeID, nil, false
		}
		for i, name := range paramNames {
			if name == "" || typeArgs[i] == types.NoTypeID {
				return 0, types.NoTypeID, nil, false
			}
			bindings[name] = typeArgs[i]
		}
	}

	totalCost := 0
	for i, arg := range args {
		paramIndex := i
		if variadicIndex >= 0 && i >= variadicIndex {
			paramIndex = variadicIndex
		}
		expectedKey := sig.Params[paramIndex]
		expectedType := tc.instantiateTypeKeyWithInference(expectedKey, arg.ty, bindings, paramSet)
		if expectedType == types.NoTypeID {
			return 0, types.NoTypeID, nil, false
		}
		allowImplicitTo := tc.callAllowsImplicitTo(sym, paramIndex)
		cost, ok := tc.matchArgument(expectedType, arg.ty, arg.isLiteral, allowImplicitTo)
		if !ok {
			return 0, types.NoTypeID, nil, false
		}
		totalCost += cost
	}
	if variadicIndex >= 0 {
		// Penalize variadic candidates so exact-arity overloads win.
		totalCost += 1 + 2*len(args)
	}

	// Check that all type params were inferred from arguments
	for _, name := range paramNames {
		if bindings[name] == types.NoTypeID {
			// Type param not inferred from arguments - candidate is invalid
			return 0, types.NoTypeID, nil, false
		}
	}

	resultType := tc.instantiateResultType(sig.Result, bindings, paramSet)
	if len(paramNames) == 0 {
		return totalCost, resultType, nil, true
	}
	concreteArgs := make([]types.TypeID, len(paramNames))
	for i, name := range paramNames {
		concreteArgs[i] = bindings[name]
	}
	return totalCost, resultType, concreteArgs, true
}

func (tc *typeChecker) selectBestCandidate(
	candidates []symbols.SymbolID,
	args []callArg,
	typeArgs []types.TypeID,
	wantGeneric bool,
) (bestSym symbols.SymbolID, bestType types.TypeID, bestArgs []types.TypeID, ambiguous, ok bool) {
	bestCost := -1
	for _, symID := range candidates {
		sym := tc.symbolFromID(symID)
		if sym == nil || (sym.Kind != symbols.SymbolFunction && sym.Kind != symbols.SymbolTag) || sym.Signature == nil {
			continue
		}
		if tc.isGenericCandidate(sym, typeArgs) != wantGeneric {
			continue
		}
		cost, resType, concreteArgs, ok := tc.evaluateFunctionCandidate(sym, args, typeArgs)
		if !ok {
			continue
		}
		if bestCost == -1 || cost < bestCost {
			bestCost = cost
			bestType = resType
			bestSym = symID
			bestArgs = concreteArgs
			ambiguous = false
		} else if cost == bestCost {
			ambiguous = true
		}
	}
	if bestCost == -1 {
		return symbols.NoSymbolID, types.NoTypeID, nil, false, false
	}
	return bestSym, bestType, bestArgs, ambiguous, true
}

func (tc *typeChecker) isGenericCandidate(sym *symbols.Symbol, typeArgs []types.TypeID) bool {
	if sym == nil || len(sym.TypeParams) == 0 {
		return false
	}
	if len(typeArgs) != len(sym.TypeParams) {
		return true
	}
	for _, arg := range typeArgs {
		if arg == types.NoTypeID {
			return true
		}
	}
	return false
}

func (tc *typeChecker) matchArgument(expected, actual types.TypeID, isLiteral, allowImplicitTo bool) (int, bool) {
	if expected == types.NoTypeID || actual == types.NoTypeID || tc.types == nil {
		return 0, false
	}
	expected = tc.resolveAlias(expected)
	actual = tc.resolveAlias(actual)
	if expInfo, ok := tc.types.Lookup(expected); ok && expInfo.Kind == types.KindReference {
		if actInfo, okAct := tc.types.Lookup(actual); okAct && actInfo.Kind == types.KindReference {
			if expInfo.Mutable && !actInfo.Mutable {
				return 0, false
			}
			return tc.conversionCost(actInfo.Elem, expInfo.Elem, isLiteral, allowImplicitTo)
		}
		if actInfo, okAct := tc.types.Lookup(actual); okAct && actInfo.Kind == types.KindOwn {
			return tc.conversionCost(actInfo.Elem, expInfo.Elem, isLiteral, allowImplicitTo)
		}
		return tc.conversionCost(actual, expInfo.Elem, isLiteral, allowImplicitTo)
	}
	return tc.conversionCost(actual, expected, isLiteral, allowImplicitTo)
}

func (tc *typeChecker) conversionCost(actual, expected types.TypeID, isLiteral, allowImplicitTo bool) (int, bool) {
	if actual == types.NoTypeID || expected == types.NoTypeID || tc.types == nil {
		return 0, false
	}
	actual = tc.resolveAlias(actual)
	expected = tc.resolveAlias(expected)
	if actual == expected {
		return 0, true
	}
	if tc.types != nil {
		expInfo, okExp := tc.types.Lookup(expected)
		actInfo, okAct := tc.types.Lookup(actual)
		if okExp && okAct {
			if expInfo.Kind == types.KindOwn && actual == expInfo.Elem && tc.isCopyType(actual) {
				return 1, true
			}
			if actInfo.Kind == types.KindOwn && expected == actInfo.Elem && tc.isCopyType(expected) {
				return 1, true
			}
			if actInfo.Kind == types.KindReference {
				elem := tc.resolveAlias(actInfo.Elem)
				if expInfo.Kind == types.KindOwn {
					if elem == tc.resolveAlias(expInfo.Elem) && tc.isCopyType(elem) {
						return 1, true
					}
				} else if expInfo.Kind != types.KindReference && expInfo.Kind != types.KindPointer {
					if elem == expected && tc.isCopyType(elem) {
						return 1, true
					}
				}
			}
		}
	}
	if actInfo, okA := tc.types.FnInfo(actual); okA {
		if expInfo, okE := tc.types.FnInfo(expected); okE {
			if len(actInfo.Params) == len(expInfo.Params) && actInfo.Result == expInfo.Result {
				match := true
				for i := range actInfo.Params {
					if actInfo.Params[i] != expInfo.Params[i] {
						match = false
						break
					}
				}
				if match {
					return 0, true
				}
			}
		}
	}
	if info, ok := tc.types.UnionInfo(expected); ok && info != nil {
		best := -1
		for _, member := range info.Members {
			if member.Kind != types.UnionMemberType {
				continue
			}
			if cost, ok := tc.conversionCost(actual, member.Type, isLiteral, allowImplicitTo); ok {
				if best == -1 || cost < best {
					best = cost
				}
			}
		}
		if best >= 0 {
			return best, true
		}
	}
	if isLiteral && tc.literalCoercible(expected, actual) {
		return 1, true
	}
	if aInfo, okA := tc.numericInfo(actual); okA {
		if eInfo, okE := tc.numericInfo(expected); okE && aInfo.kind == eInfo.kind {
			if aInfo.width != types.WidthAny && eInfo.width == types.WidthAny {
				return 1, true
			}
			if aInfo.width < eInfo.width {
				return 1, true
			}
		}
	}
	// Try implicit conversion (cost 2, lower priority than other conversions)
	if allowImplicitTo {
		if _, found, _ := tc.tryImplicitConversion(actual, expected); found {
			return 2, true
		}
	}
	return 0, false
}

func (tc *typeChecker) collectArgTypes(args []callArg) []types.TypeID {
	if len(args) == 0 {
		return nil
	}
	out := make([]types.TypeID, 0, len(args))
	for _, arg := range args {
		out = append(out, arg.ty)
	}
	return out
}

// reorderArgsForSignature reorders arguments based on parameter names in the signature.
// Returns false if there are errors (unknown names, duplicates, missing required params).
func (tc *typeChecker) reorderArgsForSignature(sig *symbols.FunctionSignature, args []callArg) ([]callArg, bool) {
	if sig == nil || len(sig.ParamNames) != len(sig.Params) {
		// Can't reorder without param names
		return nil, false
	}

	// Build map from parameter name to position
	paramPos := make(map[source.StringID]int)
	for i, name := range sig.ParamNames {
		if name != source.NoStringID {
			paramPos[name] = i
		}
	}

	// Create result array
	result := make([]callArg, len(sig.Params))
	filled := make([]bool, len(sig.Params))

	// Process args
	for i, arg := range args {
		if arg.name == source.NoStringID {
			// Positional argument - must come before named args
			if i < len(result) {
				result[i] = arg
				filled[i] = true
			}
		} else {
			// Named argument
			pos, ok := paramPos[arg.name]
			if !ok {
				// Unknown parameter name - skip this candidate
				return nil, false
			}
			if filled[pos] {
				// Duplicate parameter - skip this candidate
				return nil, false
			}
			result[pos] = arg
			filled[pos] = true
		}
	}

	// Check for missing required parameters (those without defaults)
	if len(sig.Defaults) == len(sig.Params) {
		for i, isFilled := range filled {
			if !isFilled && (i >= len(sig.Defaults) || !sig.Defaults[i]) {
				// Missing required parameter - skip this candidate
				return nil, false
			}
		}
	}

	// Trim to actual filled count (for defaults)
	actualCount := 0
	for _, isFilled := range filled {
		if isFilled {
			actualCount++
		}
	}
	return result[:actualCount], true
}
