package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/trace"
	"surge/internal/types"
)

type callArg struct {
	name      source.StringID // parameter name if named argument
	ty        types.TypeID
	isLiteral bool
	expr      ast.ExprID
}

func (tc *typeChecker) callResultType(callID ast.ExprID, call *ast.ExprCallData, span source.Span) types.TypeID {
	// Трассировка вызова функции
	var traceSpan *trace.Span
	if tc.tracer != nil && tc.tracer.Level() >= trace.LevelDebug {
		traceSpan = trace.Begin(tc.tracer, trace.ScopeNode, "call_result_type", 0)
		traceSpan.WithExtra("args", fmt.Sprintf("%d", len(call.Args)))
	}
	defer func() {
		if traceSpan != nil {
			traceSpan.End("")
		}
	}()

	if call == nil {
		return types.NoTypeID
	}
	tc.typeExpr(call.Target)
	args := make([]callArg, 0, len(call.Args))
	for _, arg := range call.Args {
		argTy := tc.typeExpr(arg.Value)
		args = append(args, callArg{
			name:      arg.Name,
			ty:        argTy,
			isLiteral: tc.isLiteralExpr(arg.Value),
			expr:      arg.Value,
		})
		tc.observeMove(arg.Value, tc.exprSpan(arg.Value))
		tc.trackTaskPassedAsArg(arg.Value) // Track Task ownership transfer to callee
	}
	if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
		if module := tc.moduleSymbolForExpr(member.Target); module != nil {
			typeArgs := tc.resolveCallTypeArgs(call.TypeArgs)
			return tc.moduleFunctionResult(module, member.Field, args, typeArgs, span)
		}
	}
	ident, ok := tc.builder.Exprs.Ident(call.Target)
	if !ok || ident == nil {
		return types.NoTypeID
	}
	name := tc.lookupName(ident.Name)
	if name == "default" {
		symID := tc.symbolForExpr(call.Target)
		tc.recordCallSymbol(callID, symID)
		return tc.handleDefaultLikeCall(name, symID, call, span)
	}
	if name == "clone" {
		if result := tc.handleCloneCall(args, span); result != types.NoTypeID {
			tc.recordCallSymbol(callID, tc.symbolForExpr(call.Target))
			return result
		}
		// If handleCloneCall returns NoTypeID, fall through to normal resolution
		// which will report "no matching overload" or similar error
	}
	candidates := tc.functionCandidates(ident.Name)
	if traceSpan != nil {
		traceSpan.WithExtra("candidates", fmt.Sprintf("%d", len(candidates)))
	}
	displayName := name
	if displayName == "" {
		displayName = "_"
	}
	if len(candidates) == 0 {
		if symID := tc.symbolForExpr(call.Target); symID.IsValid() {
			if sym := tc.symbolFromID(symID); sym != nil {
				switch sym.Kind {
				case symbols.SymbolFunction:
					candidates = append(candidates, symID)
				case symbols.SymbolLet, symbols.SymbolParam:
					varType := tc.bindingType(symID)
					if fnInfo, found := tc.types.FnInfo(varType); found {
						return tc.callFunctionVariable(fnInfo, args, span)
					}
				}
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

	bestSym, bestType, bestArgs, ambiguous, ok := tc.selectBestCandidate(candidates, args, typeArgs, false)
	if ambiguous {
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for %s", displayName)
		return types.NoTypeID
	}
	if ok {
		if sym := tc.symbolFromID(bestSym); sym != nil {
			tc.validateFunctionCall(sym, call, tc.collectArgTypes(args))
			tc.recordImplicitConversionsForCall(sym, args)
		}
		// Check for deprecated function usage
		tc.checkDeprecatedSymbol(bestSym, "function", span)
		note := "call"
		if sym := tc.symbolFromID(bestSym); sym != nil && sym.Kind == symbols.SymbolTag {
			note = "tag"
		}
		tc.rememberFunctionInstantiation(bestSym, bestArgs, span, note)
		tc.recordCallSymbol(callID, bestSym)
		tc.checkArrayViewResizeCall(name, args, span)
		return bestType
	}

	bestSym, bestType, bestArgs, ambiguous, ok = tc.selectBestCandidate(candidates, args, typeArgs, true)
	if ambiguous {
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for %s", displayName)
		return types.NoTypeID
	}
	if ok {
		if sym := tc.symbolFromID(bestSym); sym != nil {
			tc.validateFunctionCall(sym, call, tc.collectArgTypes(args))
			tc.recordImplicitConversionsForCall(sym, args)
		}
		// Check for deprecated function usage
		tc.checkDeprecatedSymbol(bestSym, "function", span)
		note := "call"
		if sym := tc.symbolFromID(bestSym); sym != nil && sym.Kind == symbols.SymbolTag {
			note = "tag"
		}
		tc.rememberFunctionInstantiation(bestSym, bestArgs, span, note)
		tc.recordCallSymbol(callID, bestSym)
		tc.checkArrayViewResizeCall(name, args, span)
		return bestType
	}

	if len(call.TypeArgs) == 0 {
		if missing := tc.missingTypeParams(candidates, args); len(missing) > 0 {
			tc.reportCannotInferTypeParams(displayName, missing, span, call)
			return types.NoTypeID
		}
	} else {
		if expected := tc.expectedTypeArgCount(candidates); expected > 0 && expected != len(typeArgs) {
			tc.report(diag.SemaNoOverload, span, "%s expects %d type argument(s)", displayName, expected)
			return types.NoTypeID
		}
	}

	if tc.reportSingleCandidateCallMismatch(candidates, args, typeArgs) {
		return types.NoTypeID
	}

	tc.report(diag.SemaNoOverload, span, "no matching overload for %s", displayName)
	return types.NoTypeID
}

func (tc *typeChecker) reportSingleCandidateCallMismatch(candidates []symbols.SymbolID, args []callArg, typeArgs []types.TypeID) bool {
	if len(candidates) != 1 {
		return false
	}
	sym := tc.symbolFromID(candidates[0])
	if sym == nil || sym.Signature == nil || (sym.Kind != symbols.SymbolFunction && sym.Kind != symbols.SymbolTag) {
		return false
	}
	return tc.reportCallArgumentMismatch(sym, args, typeArgs)
}

func (tc *typeChecker) reportCallArgumentMismatch(sym *symbols.Symbol, args []callArg, typeArgs []types.TypeID) bool {
	if sym == nil || sym.Signature == nil {
		return false
	}
	sig := sym.Signature

	hasNamed := false
	for _, arg := range args {
		if arg.name != source.NoStringID {
			hasNamed = true
			break
		}
	}
	if hasNamed {
		reordered, ok := tc.reorderArgsForSignature(sig, args)
		if !ok {
			return false
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

	requiredParams := 0
	if len(sig.Defaults) == paramCount {
		for i, hasDefault := range sig.Defaults {
			if !hasDefault && (variadicIndex < 0 || i != variadicIndex) {
				requiredParams++
			}
		}
	} else {
		requiredParams = paramCount
	}

	if variadicIndex >= 0 {
		if len(args) < paramCount-1 {
			return false
		}
	} else if len(args) < requiredParams || len(args) > paramCount {
		return false
	}

	paramNames, paramSet := tc.typeParamNameSet(sym)
	bindings := make(map[string]types.TypeID)
	if len(typeArgs) > 0 {
		if len(typeArgs) != len(paramNames) {
			return false
		}
		for i, name := range paramNames {
			if name == "" || typeArgs[i] == types.NoTypeID {
				return false
			}
			bindings[name] = typeArgs[i]
		}
	}

	for i, arg := range args {
		paramIndex := i
		if variadicIndex >= 0 && i >= variadicIndex {
			paramIndex = variadicIndex
		}
		expectedKey := sig.Params[paramIndex]
		expectedType := tc.instantiateTypeKeyWithInference(expectedKey, arg.ty, bindings, paramSet)
		if expectedType == types.NoTypeID {
			return false
		}
		allowImplicitTo := tc.callAllowsImplicitTo(sym, paramIndex)
		if _, ok := tc.matchArgument(expectedType, arg.ty, arg.isLiteral, allowImplicitTo); !ok {
			tc.reportCallArgumentTypeMismatch(expectedType, arg.ty, arg.expr, allowImplicitTo)
			return true
		}
	}

	for _, name := range paramNames {
		if bindings[name] == types.NoTypeID {
			return false
		}
	}
	return false
}

func (tc *typeChecker) reportCallArgumentTypeMismatch(expected, actual types.TypeID, expr ast.ExprID, allowImplicitTo bool) {
	span := tc.exprSpan(expr)
	expectedLabel := tc.typeLabel(expected)
	actualLabel := tc.typeLabel(actual)
	if !allowImplicitTo {
		tc.report(diag.SemaTypeMismatch, span, "expected %s, got %s", expectedLabel, actualLabel)
		return
	}

	if _, _, ambiguous := tc.tryImplicitConversion(actual, expected); ambiguous {
		tc.report(diag.SemaAmbiguousConversion, span,
			"ambiguous conversion from %s to %s: multiple __to methods found",
			actualLabel, expectedLabel)
		return
	}

	tc.report(diag.SemaTypeMismatch, span,
		"expected %s, got %s; no implicit __to(%s, %s) -> %s",
		expectedLabel, actualLabel, actualLabel, expectedLabel, expectedLabel)
}

func (tc *typeChecker) recordCallSymbol(callID ast.ExprID, symID symbols.SymbolID) {
	if callID == ast.NoExprID || !symID.IsValid() || tc.symbols == nil || tc.symbols.ExprSymbols == nil {
		return
	}
	if sym := tc.symbolFromID(symID); sym != nil {
		if sym.Kind != symbols.SymbolFunction && sym.Kind != symbols.SymbolTag {
			return
		}
	}
	tc.symbols.ExprSymbols[callID] = symID
}

// callFunctionVariable validates and resolves a call to a function-typed variable.
// Returns the result type or NoTypeID if the call is invalid.
func (tc *typeChecker) callFunctionVariable(fnInfo *types.FnInfo, args []callArg, span source.Span) types.TypeID {
	// Check argument count
	if len(args) != len(fnInfo.Params) {
		tc.report(diag.SemaNoOverload, span,
			"function expects %d argument(s), got %d",
			len(fnInfo.Params), len(args))
		return types.NoTypeID
	}

	// Check each argument type
	for i, arg := range args {
		expectedType := fnInfo.Params[i]
		if !tc.typesAssignable(expectedType, arg.ty, true) {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(arg.expr),
				"expected %s, got %s",
				tc.typeLabel(expectedType), tc.typeLabel(arg.ty))
			return types.NoTypeID
		}
	}

	return fnInfo.Result
}

// recordImplicitConversionsForCall records implicit conversions for function arguments
// after the best overload has been selected. This must be called AFTER overload resolution.
func (tc *typeChecker) recordImplicitConversionsForCall(sym *symbols.Symbol, args []callArg) {
	if sym == nil || sym.Signature == nil {
		return
	}
	sig := sym.Signature

	// Handle variadic functions
	variadicIndex := -1
	for i, v := range sig.Variadic {
		if v {
			variadicIndex = i
			break
		}
	}

	for i, arg := range args {
		paramIndex := i
		if variadicIndex >= 0 && i >= variadicIndex {
			paramIndex = variadicIndex
		}
		if paramIndex >= len(sig.Params) {
			continue
		}

		expectedKey := sig.Params[paramIndex]
		expectedType := tc.typeFromKey(expectedKey)
		if expectedType == types.NoTypeID {
			continue
		}

		// Record implicit conversion if needed
		if !tc.typesAssignable(expectedType, arg.ty, true) && tc.callAllowsImplicitTo(sym, paramIndex) {
			if convType, found, _ := tc.tryImplicitConversion(arg.ty, expectedType); found {
				tc.recordImplicitConversion(arg.expr, arg.ty, convType)
			}
		}
	}
}

func (tc *typeChecker) callAllowsImplicitTo(sym *symbols.Symbol, _ int) bool {
	if sym == nil {
		return false
	}
	return sym.Flags&symbols.SymbolFlagAllowTo != 0
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
			for i := len(ids) - 1; i >= 0; i-- {
				id := ids[i]
				sym := tc.symbolFromID(id)
				if sym != nil && (sym.Kind == symbols.SymbolFunction || sym.Kind == symbols.SymbolTag) {
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

func (tc *typeChecker) handleDefaultLikeCall(name string, symID symbols.SymbolID, call *ast.ExprCallData, span source.Span) types.TypeID {
	if call == nil {
		return types.NoTypeID
	}
	if len(call.TypeArgs) == 0 {
		tc.reportCannotInferTypeParams(name, []string{"T"}, span, call)
		return types.NoTypeID
	}
	if len(call.TypeArgs) != 1 {
		tc.report(diag.SemaNoOverload, span, "%s expects 1 type argument", name)
		return types.NoTypeID
	}
	if len(call.Args) != 0 {
		tc.report(diag.SemaNoOverload, span, "%s does not take arguments", name)
		return types.NoTypeID
	}
	scope := tc.scopeOrFile(tc.currentScope())
	targetType := tc.resolveTypeExprWithScope(call.TypeArgs[0], scope)
	if targetType == types.NoTypeID {
		return types.NoTypeID
	}
	if name == "default" && !tc.defaultable(targetType) {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(call.Target), "default is not defined for %s", tc.typeLabel(targetType))
		return types.NoTypeID
	}
	if symID.IsValid() {
		if sym := tc.symbolFromID(symID); sym == nil || (sym.Kind != symbols.SymbolFunction && sym.Kind != symbols.SymbolTag) {
			symID = symbols.NoSymbolID
		}
	}
	if !symID.IsValid() && tc.builder != nil {
		if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
			if candidates := tc.functionCandidates(ident.Name); len(candidates) > 0 {
				symID = candidates[0]
			}
		}
	}
	if symID.IsValid() {
		// Check for deprecated function usage
		tc.checkDeprecatedSymbol(symID, "function", span)
		tc.rememberFunctionInstantiation(symID, []types.TypeID{targetType}, span, "call")
	}
	return targetType
}

// handleCloneCall handles special semantics for clone<T>(&value) -> T.
// For Copy types, this is a simple bitwise copy (no __clone lookup).
// For non-Copy types, this looks up the __clone magic method.
func (tc *typeChecker) handleCloneCall(args []callArg, span source.Span) types.TypeID {
	if len(args) != 1 {
		// Let normal overload resolution handle the error
		return types.NoTypeID
	}

	argType := args[0].ty
	// Get the inner type (strip reference if present)
	innerType := tc.valueType(argType)
	if innerType == types.NoTypeID {
		innerType = argType
	}

	// For Copy types, just return the type (simple bitwise copy)
	if tc.isCopyType(innerType) {
		return innerType
	}

	// For non-Copy types, look up __clone magic method
	typeKey := tc.typeKeyForType(innerType)
	methods := tc.lookupMagicMethods(typeKey, "__clone")

	if len(methods) == 0 {
		tc.report(diag.SemaTypeNotClonable, span,
			"type %s is not clonable (no __clone method defined)", tc.typeLabel(innerType))
		return types.NoTypeID
	}

	// Validate that __clone returns the same type
	// Signature should be: fn __clone(self: &T) -> T
	for _, sig := range methods {
		if sig == nil {
			continue
		}
		if sig.Result != "" && typeKeyEqual(sig.Result, typeKey) {
			// Found a valid __clone method with correct return type
			return innerType
		}
	}

	// Method found but signature invalid
	tc.report(diag.SemaTypeNotClonable, span,
		"type %s has __clone but with invalid signature", tc.typeLabel(innerType))
	return types.NoTypeID
}

func (tc *typeChecker) reportCannotInferTypeParams(name string, missing []string, span source.Span, call *ast.ExprCallData) {
	if tc.reporter == nil || len(missing) == 0 {
		return
	}
	displayName := name
	if displayName == "" {
		displayName = "_"
	}
	missingLabel := strings.Join(missing, ", ")
	msg := fmt.Sprintf("cannot infer type parameter %s for %s; use %s::<%s>(...)", missingLabel, displayName, displayName, missingLabel)
	b := diag.ReportError(tc.reporter, diag.SemaNoOverload, span, msg)
	if b == nil {
		return
	}
	if call != nil {
		if targetSpan := tc.exprSpan(call.Target); targetSpan != (source.Span{}) {
			insert := targetSpan.ZeroideToEnd()
			title := fmt.Sprintf("insert %s::<%s>", displayName, missingLabel)
			b.WithFixSuggestion(fix.InsertText(title, insert, "::<"+missingLabel+">", "", fix.Preferred()))
		}
	}
	b.Emit()
}

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
					continue
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

	// For "own T" params, we accept both "own T" and "T" (value types can be moved)
	innerExpected := substituted
	if after, found := strings.CutPrefix(substitutedStr, "own "); found {
		innerExpected = symbols.TypeKey(strings.TrimSpace(after))
	}

	for _, cand := range tc.typeKeyCandidates(arg) {
		if typeKeyEqual(cand.key, substituted) {
			return true
		}
		// Also check inner type for "own" params
		if innerExpected != substituted && typeKeyEqual(cand.key, innerExpected) {
			return true
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
	if recvTT.Kind != types.KindReference && recvTT.Kind != types.KindPointer {
		if typeKeyEqual(selfKey, candidateKey) {
			return true
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
			return true // self: T, receiver: own T -> move
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
