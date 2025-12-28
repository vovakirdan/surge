package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
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
		if result := tc.handleCloneCall(callID, args, span); result != types.NoTypeID {
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

	selMono := tc.selectBestCandidate(candidates, args, typeArgs, false)
	if selMono.ambiguous {
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for %s", displayName)
		return types.NoTypeID
	}
	if selMono.ok {
		if sym := tc.symbolFromID(selMono.sym); sym != nil {
			tc.materializeCallArguments(sym, args, selMono.typeArgs)
			tc.validateFunctionCall(sym, call, tc.collectArgTypes(args))
			tc.recordImplicitConversionsForCall(sym, args)
			tc.applyCallOwnership(sym, args)
			tc.dropImplicitBorrowsForCall(sym, args, selMono.result)
		}
		// Check for deprecated function usage
		tc.checkDeprecatedSymbol(selMono.sym, "function", span)
		note := "call"
		if sym := tc.symbolFromID(selMono.sym); sym != nil && sym.Kind == symbols.SymbolTag {
			note = "tag"
		}
		tc.rememberFunctionInstantiation(selMono.sym, selMono.typeArgs, span, note)
		tc.recordCallSymbol(callID, selMono.sym)
		tc.checkArrayViewResizeCall(name, args, span)
		return selMono.result
	}

	selGeneric := tc.selectBestCandidate(candidates, args, typeArgs, true)
	if selGeneric.ambiguous {
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for %s", displayName)
		return types.NoTypeID
	}
	if selGeneric.ok {
		if sym := tc.symbolFromID(selGeneric.sym); sym != nil {
			tc.materializeCallArguments(sym, args, selGeneric.typeArgs)
			tc.validateFunctionCall(sym, call, tc.collectArgTypes(args))
			tc.recordImplicitConversionsForCall(sym, args)
			tc.dropImplicitBorrowsForCall(sym, args, selGeneric.result)
		}
		// Check for deprecated function usage
		tc.checkDeprecatedSymbol(selGeneric.sym, "function", span)
		note := "call"
		if sym := tc.symbolFromID(selGeneric.sym); sym != nil && sym.Kind == symbols.SymbolTag {
			note = "tag"
		}
		tc.rememberFunctionInstantiation(selGeneric.sym, selGeneric.typeArgs, span, note)
		tc.recordCallSymbol(callID, selGeneric.sym)
		tc.checkArrayViewResizeCall(name, args, span)
		return selGeneric.result
	}

	if selMono.matchInfo != nil && selMono.matchInfo.expr.IsValid() {
		tc.reportBorrowFailure(selMono.matchInfo)
		return types.NoTypeID
	}
	if selGeneric.matchInfo != nil && selGeneric.matchInfo.expr.IsValid() {
		tc.reportBorrowFailure(selGeneric.matchInfo)
		return types.NoTypeID
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
		var borrowInfo borrowMatchInfo
		if _, ok := tc.matchArgument(expectedType, arg.ty, arg.isLiteral, allowImplicitTo, arg.expr, &borrowInfo); !ok {
			if borrowInfo.expr.IsValid() {
				tc.reportBorrowFailure(&borrowInfo)
				return true
			}
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
