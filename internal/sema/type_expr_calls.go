package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type callArg struct {
	ty        types.TypeID
	isLiteral bool
	expr      ast.ExprID
}

func (tc *typeChecker) callResultType(call *ast.ExprCallData, span source.Span) types.TypeID {
	if call == nil {
		return types.NoTypeID
	}
	tc.typeExpr(call.Target)
	args := make([]callArg, 0, len(call.Args))
	for _, arg := range call.Args {
		argTy := tc.typeExpr(arg)
		args = append(args, callArg{
			ty:        argTy,
			isLiteral: tc.isLiteralExpr(arg),
			expr:      arg,
		})
		tc.observeMove(arg, tc.exprSpan(arg))
	}
	ident, ok := tc.builder.Exprs.Ident(call.Target)
	if !ok || ident == nil {
		return types.NoTypeID
	}
	name := tc.lookupName(ident.Name)
	if name == "default" && len(call.TypeArgs) == 1 {
		scope := tc.scopeOrFile(tc.currentScope())
		targetType := tc.resolveTypeExprWithScope(call.TypeArgs[0], scope)
		if targetType == types.NoTypeID {
			return types.NoTypeID
		}
		if !tc.defaultable(targetType) {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(call.Target), "default is not defined for %s", tc.typeLabel(targetType))
			return types.NoTypeID
		}
		return targetType
	}
	candidates := tc.functionCandidates(ident.Name)
	if len(candidates) == 0 {
		if symID := tc.symbolForExpr(call.Target); symID.IsValid() {
			if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolFunction {
				candidates = append(candidates, symID)
			}
		}
	}
	if len(candidates) == 0 {
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaNoOverload, span, "no matching overload for %s", name)
		return types.NoTypeID
	}
	typeArgs := tc.resolveCallTypeArgs(call.TypeArgs)

	bestCost := -1
	bestType := types.NoTypeID
	bestSym := symbols.NoSymbolID
	ambiguous := false
	for _, symID := range candidates {
		sym := tc.symbolFromID(symID)
		if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
			continue
		}
		cost, resType, ok := tc.evaluateFunctionCandidate(sym, args, typeArgs)
		if !ok {
			continue
		}
		if bestCost == -1 || cost < bestCost {
			bestCost = cost
			bestType = resType
			bestSym = symID
			ambiguous = false
		} else if cost == bestCost {
			ambiguous = true
		}
	}
	if bestCost == -1 {
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaNoOverload, span, "no matching overload for %s", name)
		return types.NoTypeID
	}
	if ambiguous {
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for %s", name)
		return types.NoTypeID
	}
	if bestSym.IsValid() {
		if sym := tc.symbolFromID(bestSym); sym != nil {
			tc.validateFunctionCall(sym, call, tc.collectArgTypes(args))
		}
	}
	return bestType
}

func (tc *typeChecker) functionCandidates(name source.StringID) []symbols.SymbolID {
	if name == source.NoStringID || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil {
		return nil
	}
	seen := make(map[string]struct{})
	scope := tc.currentScope()
	if !scope.IsValid() {
		scope = tc.fileScope()
	}
	for scope.IsValid() {
		scopeData := tc.symbols.Table.Scopes.Get(scope)
		if scopeData == nil {
			break
		}
		if ids := scopeData.NameIndex[name]; len(ids) > 0 {
			out := make([]symbols.SymbolID, 0, len(ids))
			for _, id := range ids {
				sym := tc.symbolFromID(id)
				if sym != nil && sym.Kind == symbols.SymbolFunction {
					if key := tc.candidateKey(sym); key != "" {
						if _, dup := seen[key]; dup {
							continue
						}
						seen[key] = struct{}{}
					}
					out = append(out, id)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		scope = scopeData.Parent
	}
	return nil
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

func (tc *typeChecker) evaluateFunctionCandidate(sym *symbols.Symbol, args []callArg, typeArgs []types.TypeID) (int, types.TypeID, bool) {
	if sym == nil || sym.Signature == nil {
		return 0, types.NoTypeID, false
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
			return 0, types.NoTypeID, false
		}
	} else if len(args) != paramCount {
		return 0, types.NoTypeID, false
	}

	paramNames, paramSet := tc.typeParamNameSet(sym)
	bindings := make(map[string]types.TypeID)
	if len(typeArgs) > 0 {
		if len(typeArgs) != len(paramNames) {
			return 0, types.NoTypeID, false
		}
		for i, name := range paramNames {
			if name == "" || typeArgs[i] == types.NoTypeID {
				return 0, types.NoTypeID, false
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
			return 0, types.NoTypeID, false
		}
		cost, ok := tc.matchArgument(expectedType, arg.ty, arg.isLiteral)
		if !ok {
			return 0, types.NoTypeID, false
		}
		totalCost += cost
	}

	resultType := tc.instantiateResultType(sig.Result, bindings, paramSet)
	return totalCost, resultType, true
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
	case strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"):
		elemActual := tc.valueType(actual)
		if info, ok := tc.types.Lookup(tc.resolveAlias(actual)); ok && info.Kind == types.KindArray {
			elemActual = info.Elem
		}
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(strings.TrimSpace(s[1:len(s)-1])), elemActual, bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeArray(inner, types.ArrayDynamicLength))
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
	case strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"):
		inner := tc.instantiateResultType(symbols.TypeKey(strings.TrimSpace(s[1:len(s)-1])), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeArray(inner, types.ArrayDynamicLength))
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

func (tc *typeChecker) methodResultType(member *ast.ExprMemberData, recv types.TypeID, args []types.TypeID, span source.Span) types.TypeID {
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
	for _, recvCand := range tc.typeKeyCandidates(recv) {
		if recvCand.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recvCand.key, name)
		for _, sig := range methods {
			if sig == nil || len(sig.Params) == 0 || !typeKeyEqual(sig.Params[0], recvCand.key) {
				continue
			}
			if len(sig.Params)-1 != len(args) {
				continue
			}
			if !tc.methodParamsMatch(sig.Params[1:], args) {
				continue
			}
			res := tc.typeFromKey(sig.Result)
			return tc.adjustAliasUnaryResult(res, recvCand)
		}
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "%s has no method %s", tc.typeLabel(recv), name)
	return types.NoTypeID
}

func (tc *typeChecker) methodParamsMatch(expected []symbols.TypeKey, args []types.TypeID) bool {
	if len(expected) != len(args) {
		return false
	}
	for i, arg := range args {
		if !tc.methodParamMatches(expected[i], arg) {
			return false
		}
	}
	return true
}

func (tc *typeChecker) methodParamMatches(expected symbols.TypeKey, arg types.TypeID) bool {
	if expected == "" {
		return false
	}
	for _, cand := range tc.typeKeyCandidates(arg) {
		if typeKeyEqual(cand.key, expected) {
			return true
		}
	}
	return false
}
