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
		if _, ok := tc.matchArgument(expectedType, arg.ty, arg.isLiteral); !ok {
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
	for _, p := range sym.Signature.Params {
		b.WriteString(string(p))
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
		cost, ok := tc.matchArgument(expectedType, arg.ty, arg.isLiteral)
		if !ok {
			return 0, types.NoTypeID, nil, false
		}
		totalCost += cost
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

func (tc *typeChecker) instantiateTypeKeyWithInference(key symbols.TypeKey, actual types.TypeID, bindings map[string]types.TypeID, paramNames map[string]struct{}) types.TypeID {
	if key == "" || tc.types == nil {
		return types.NoTypeID
	}
	s := strings.TrimSpace(string(key))
	if s == "" {
		return types.NoTypeID
	}
	if _, ok := paramNames[s]; ok {
		if bound := bindings[s]; bound != types.NoTypeID {
			return bound
		}
		bound := tc.valueType(actual)
		if bound == types.NoTypeID {
			return types.NoTypeID
		}
		bindings[s] = bound
		return bound
	}
	if innerKey, lengthKey, length, hasLen, ok := parseArrayKey(s); ok {
		elemActual := tc.valueType(actual)
		if elem, ok := tc.arrayElemType(actual); ok {
			elemActual = elem
		}
		if hasLen && lengthKey != "" && length == 0 {
			if _, fixedLen, ok := tc.arrayFixedInfo(actual); ok && fixedLen > 0 {
				length = uint64(fixedLen)
			}
		}
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(innerKey), elemActual, bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		if hasLen {
			lenType := types.NoTypeID
			if _, actualLen, okLen := tc.arrayFixedInfo(actual); okLen && actualLen > 0 {
				lenType = tc.types.Intern(types.MakeConstUint(actualLen))
			}
			if lengthKey != "" {
				if bound := bindings[lengthKey]; bound != types.NoTypeID {
					lenType = bound
				} else if lenType != types.NoTypeID {
					bindings[lengthKey] = lenType
				}
			}
			if lenType == types.NoTypeID && length <= uint64(^uint32(0)) {
				lenType = tc.types.Intern(types.MakeConstUint(uint32(length)))
			}
			if lenType == types.NoTypeID {
				return types.NoTypeID
			}
			return tc.instantiateArrayFixedWithArg(inner, lenType)
		}
		return tc.instantiateArrayType(inner)
	}
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "("), ")"))
		if inner == "" {
			return tc.types.Builtins().Unit
		}
		expectedElems := splitTopLevel(inner)
		tupleType := tc.valueType(actual)
		info, ok := tc.types.TupleInfo(tupleType)
		if !ok || info == nil {
			return types.NoTypeID
		}
		if len(expectedElems) != len(info.Elems) {
			return types.NoTypeID
		}
		elems := make([]types.TypeID, 0, len(expectedElems))
		for i, part := range expectedElems {
			elem := tc.instantiateTypeKeyWithInference(symbols.TypeKey(part), info.Elems[i], bindings, paramNames)
			if elem == types.NoTypeID {
				return types.NoTypeID
			}
			elems = append(elems, elem)
		}
		if len(elems) == 0 {
			return tc.types.Builtins().Unit
		}
		return tc.types.RegisterTuple(elems)
	}
	switch {
	case strings.HasPrefix(s, "&mut "):
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&mut "))), tc.peelReference(actual), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeReference(inner, true))
	case strings.HasPrefix(s, "&"):
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&"))), tc.peelReference(actual), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeReference(inner, false))
	case strings.HasPrefix(s, "own "):
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "own "))), tc.valueType(actual), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeOwn(inner))
	case strings.HasPrefix(s, "*"):
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "*"))), tc.valueType(actual), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakePointer(inner))
	case strings.HasPrefix(s, "Option<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Option<"), ">")
		actualPayload := actual
		if payload, ok := tc.optionPayload(actual); ok {
			actualPayload = payload
		}
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(content), actualPayload, bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.resolveOptionType(inner, source.Span{}, tc.scopeOrFile(tc.currentScope()))
	case strings.HasPrefix(s, "Result<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Result<"), ">")
		parts := splitTopLevel(content)
		if len(parts) != 2 {
			return types.NoTypeID
		}
		okActual := actual
		errActual := actual
		if okType, errType, ok := tc.resultPayload(actual); ok {
			okActual = okType
			errActual = errType
		}
		okType := tc.instantiateTypeKeyWithInference(symbols.TypeKey(parts[0]), okActual, bindings, paramNames)
		errType := tc.instantiateTypeKeyWithInference(symbols.TypeKey(parts[1]), errActual, bindings, paramNames)
		if okType == types.NoTypeID || errType == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.resolveResultType(okType, errType, source.Span{}, tc.scopeOrFile(tc.currentScope()))
	default:
		return tc.typeFromKey(symbols.TypeKey(s))
	}
}

func (tc *typeChecker) instantiateResultType(key symbols.TypeKey, bindings map[string]types.TypeID, paramNames map[string]struct{}) types.TypeID {
	if key == "" || tc.types == nil {
		return types.NoTypeID
	}
	s := strings.TrimSpace(string(key))
	if s == "" {
		return types.NoTypeID
	}
	if bound := bindings[s]; bound != types.NoTypeID {
		return bound
	}
	if _, ok := paramNames[s]; ok {
		return types.NoTypeID
	}
	if innerKey, lengthKey, length, hasLen, ok := parseArrayKey(s); ok {
		inner := tc.instantiateResultType(symbols.TypeKey(innerKey), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		if hasLen {
			lenType := types.NoTypeID
			if lengthKey != "" && length == 0 {
				if _, fixedLen, ok := tc.arrayFixedInfo(inner); ok && fixedLen > 0 {
					length = uint64(fixedLen)
				}
			}
			if lengthKey != "" {
				lenType = tc.instantiateResultType(symbols.TypeKey(lengthKey), bindings, paramNames)
			}
			if lenType == types.NoTypeID && length <= uint64(^uint32(0)) {
				lenType = tc.types.Intern(types.MakeConstUint(uint32(length)))
			}
			if lenType == types.NoTypeID {
				return types.NoTypeID
			}
			return tc.instantiateArrayFixedWithArg(inner, lenType)
		}
		return tc.instantiateArrayType(inner)
	}
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "("), ")"))
		if inner == "" {
			return tc.types.Builtins().Unit
		}
		parts := splitTopLevel(inner)
		elems := make([]types.TypeID, 0, len(parts))
		for _, part := range parts {
			elem := tc.instantiateResultType(symbols.TypeKey(part), bindings, paramNames)
			if elem == types.NoTypeID {
				return types.NoTypeID
			}
			elems = append(elems, elem)
		}
		if len(elems) == 0 {
			return tc.types.Builtins().Unit
		}
		return tc.types.RegisterTuple(elems)
	}
	switch {
	case strings.HasPrefix(s, "&mut "):
		inner := tc.instantiateResultType(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&mut "))), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeReference(inner, true))
	case strings.HasPrefix(s, "&"):
		inner := tc.instantiateResultType(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&"))), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeReference(inner, false))
	case strings.HasPrefix(s, "own "):
		inner := tc.instantiateResultType(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "own "))), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeOwn(inner))
	case strings.HasPrefix(s, "*"):
		inner := tc.instantiateResultType(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "*"))), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakePointer(inner))
	case strings.HasPrefix(s, "Option<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Option<"), ">")
		inner := tc.instantiateResultType(symbols.TypeKey(content), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.resolveOptionType(inner, source.Span{}, tc.scopeOrFile(tc.currentScope()))
	case strings.HasPrefix(s, "Result<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Result<"), ">")
		parts := splitTopLevel(content)
		if len(parts) != 2 {
			return types.NoTypeID
		}
		okType := tc.instantiateResultType(symbols.TypeKey(parts[0]), bindings, paramNames)
		errType := tc.instantiateResultType(symbols.TypeKey(parts[1]), bindings, paramNames)
		if okType == types.NoTypeID || errType == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.resolveResultType(okType, errType, source.Span{}, tc.scopeOrFile(tc.currentScope()))
	default:
		// Try to parse as generic type like "TypeName<A, B>"
		if baseName, typeArgKeys, ok := parseGenericTypeKey(s); ok {
			concreteArgs := make([]types.TypeID, len(typeArgKeys))
			for i, argKey := range typeArgKeys {
				arg := tc.instantiateResultType(symbols.TypeKey(argKey), bindings, paramNames)
				if arg == types.NoTypeID {
					return types.NoTypeID
				}
				concreteArgs[i] = arg
			}
			return tc.instantiateNamedGenericType(baseName, concreteArgs)
		}
		return tc.typeFromKey(symbols.TypeKey(s))
	}
}

func (tc *typeChecker) peelReference(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return id
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return id
	}
	switch tt.Kind {
	case types.KindReference, types.KindOwn:
		return tt.Elem
	default:
		return id
	}
}

func (tc *typeChecker) matchArgument(expected, actual types.TypeID, isLiteral bool) (int, bool) {
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
			return tc.conversionCost(actInfo.Elem, expInfo.Elem, isLiteral)
		}
		return tc.conversionCost(actual, expInfo.Elem, isLiteral)
	}
	return tc.conversionCost(actual, expected, isLiteral)
}

func (tc *typeChecker) conversionCost(actual, expected types.TypeID, isLiteral bool) (int, bool) {
	if actual == types.NoTypeID || expected == types.NoTypeID || tc.types == nil {
		return 0, false
	}
	actual = tc.resolveAlias(actual)
	expected = tc.resolveAlias(expected)
	if actual == expected {
		return 0, true
	}
	if info, ok := tc.types.UnionInfo(expected); ok && info != nil {
		best := -1
		for _, member := range info.Members {
			if member.Kind != types.UnionMemberType {
				continue
			}
			if cost, ok := tc.conversionCost(actual, member.Type, isLiteral); ok {
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
	if _, found, _ := tc.tryImplicitConversion(actual, expected); found {
		return 2, true
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

// parseGenericTypeKey parses "TypeName<A, B>" -> ("TypeName", ["A", "B"], true)
// Returns false if the string is not a generic type pattern.
func parseGenericTypeKey(s string) (base string, args []string, ok bool) {
	openIdx := strings.Index(s, "<")
	if openIdx == -1 || !strings.HasSuffix(s, ">") {
		return "", nil, false
	}
	base = strings.TrimSpace(s[:openIdx])
	if base == "" {
		return "", nil, false
	}
	// Skip known built-in generic types that have special handling
	if base == "Option" || base == "Result" {
		return "", nil, false
	}
	content := s[openIdx+1 : len(s)-1]
	args = splitTopLevel(content)
	if len(args) == 0 {
		return "", nil, false
	}
	return base, args, true
}

// instantiateNamedGenericType creates Type<int, string> from base name and type args.
// For tag names (e.g., Success, Some), it creates a tag type (single-member union).
func (tc *typeChecker) instantiateNamedGenericType(base string, args []types.TypeID) types.TypeID {
	if tc.builder == nil || base == "" {
		return types.NoTypeID
	}
	name := tc.builder.StringsInterner.Intern(base)
	scope := tc.scopeOrFile(tc.currentScope())

	// First check if this is a tag name - create tag type if so
	if tagSymID := tc.lookupTagSymbol(name, scope); tagSymID.IsValid() {
		return tc.instantiateTagType(name, args)
	}

	// Otherwise resolve as regular named type
	if len(args) == 0 {
		return types.NoTypeID
	}
	return tc.resolveNamedType(name, args, nil, source.Span{}, scope)
}

// instantiateTagType creates a tag type (single-member union) for a tag like Success<int>.
// This is used when a tag constructor returns its own type rather than the full union type.
func (tc *typeChecker) instantiateTagType(tagName source.StringID, args []types.TypeID) types.TypeID {
	if tc.types == nil {
		return types.NoTypeID
	}

	// Create a union with just this tag as a member
	typeID := tc.types.RegisterUnionInstance(tagName, source.Span{}, args)
	members := []types.UnionMember{{
		Kind:    types.UnionMemberTag,
		TagName: tagName,
		TagArgs: args,
	}}
	tc.types.SetUnionMembers(typeID, members)
	return typeID
}
