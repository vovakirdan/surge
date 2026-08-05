package sema

import (
	"fmt"

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
	tc.callTargetDepth++
	tc.typeExpr(call.Target)
	tc.callTargetDepth--
	twoPhase := tc.beginTwoPhaseArgs(call.Args)
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
	// Every argument is evaluated: reserved `&mut` argument borrows become
	// exclusive here, and anything still borrowing their places is a real
	// conflict with the callee's access.
	tc.activateTwoPhaseArgs(twoPhase)
	if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
		if module := tc.moduleSymbolForExpr(member.Target); module != nil {
			typeArgs := tc.resolveCallTypeArgs(call.TypeArgs)
			return tc.moduleFunctionResult(callID, module, member.Field, args, typeArgs, span)
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
	if symID := tc.symbolForExpr(call.Target); symID.IsValid() {
		if sym := tc.symbolFromID(symID); sym != nil {
			switch sym.Kind {
			case symbols.SymbolLet, symbols.SymbolParam:
				varType := tc.resolveAlias(tc.bindingType(symID))
				if fnInfo, found := tc.types.FnInfo(varType); found {
					return tc.callFunctionVariable(fnInfo, args, span)
				}
			}
		}
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
					varType := tc.resolveAlias(tc.bindingType(symID))
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
			argTypes := tc.collectArgTypes(args)
			tc.validateFunctionCall(sym, call, argTypes)
			if !tc.validateSpecialCall(sym, call, argTypes, span) {
				return types.NoTypeID
			}
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
		tc.recordFunctionCrossingCall(selMono.sym)
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
			argTypes := tc.collectArgTypes(args)
			tc.validateFunctionCall(sym, call, argTypes)
			if !tc.validateSpecialCall(sym, call, argTypes, span) {
				return types.NoTypeID
			}
			tc.recordImplicitConversionsForCall(sym, args)
			// The monomorphic branch has always applied argument
			// ownership; this branch silently skipped it, so moves
			// through generic calls went untracked (a moved value
			// stayed "live" in the caller — the double-free source the
			// leaf-drop epic hit through push -> array_push).
			tc.applyCallOwnership(sym, args)
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
		tc.recordFunctionCrossingCall(selGeneric.sym)
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
	if tc.reportBorrowIntoOwnedParam(expected, actual, expr) {
		return
	}
	if tc.reportOwnedParamNeedsMarker(expected, actual, expr) {
		return
	}
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

// reportBorrowIntoOwnedParam explains the one mismatch a plain "expected X,
// got &X" line hides: the parameter takes OWNERSHIP, so binding a borrow to it
// would let callee and caller free the same value. Reports and returns true
// only for that shape (borrow of a non-Copy value against its owned type).
// reportOwnedParamNeedsMarker explains a parameter that demands OWNERSHIP being
// handed a plain value, and reports whether it did.
//
// "expected own Inner, got Inner" is true and tells a newcomer nothing: the two
// labels differ by a word they have not met, and nothing says the word is theirs
// to write. This is the direction the language keeps deliberately — a plain value
// must not silently satisfy a demand for ownership, or the giving-away happens
// with nothing at the use site saying so — so the message names the marker and
// the alternative to it.
//
// The fix is offered but NOT always-safe, and the difference from the
// partial-move marker is real. There, `own` was the only thing missing and the
// read was already a move. Here the author has a genuine choice: give the value
// away, or clone it and keep theirs. A compiler that swept `own` in
// automatically would decide that for them, so this one waits to be asked.
func (tc *typeChecker) reportOwnedParamNeedsMarker(expected, actual types.TypeID, expr ast.ExprID) bool {
	if tc.types == nil || tc.reporter == nil || expected == types.NoTypeID || actual == types.NoTypeID {
		return false
	}
	expInfo, ok := tc.types.Lookup(tc.resolveAlias(expected))
	if !ok || expInfo.Kind != types.KindOwn {
		return false
	}
	// Only when the marker alone would close the gap: the value already has the
	// type the parameter wants underneath its `own`.
	if !tc.typesAssignable(tc.resolveAlias(expInfo.Elem), actual, true) {
		return false
	}
	if tc.isCopyType(actual) {
		// A Copy value satisfies an owned parameter as it stands; if it reached
		// here the mismatch is something else and this is not the explanation.
		return false
	}
	span := tc.exprSpan(expr)
	label := tc.typeLabel(expInfo.Elem)
	b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, span,
		fmt.Sprintf("this parameter takes ownership of %s, so the value has to be given away: write `own`", label))
	if b == nil {
		return false
	}
	b.WithNote(span,
		"passing it by ownership ends your use of it: the callee frees it, and reading it here afterwards is an error")
	b.WithNote(span, fmt.Sprintf(
		"hint: to keep the %s you have, pass a clone instead", label))
	b.WithFixSuggestion(fix.InsertText(
		"insert `own` to give the value away",
		span.ZeroideToStart(), "own ", "",
		fix.WithApplicability(diag.FixApplicabilitySafeWithHeuristics)))
	b.Emit()
	return true
}

func (tc *typeChecker) reportBorrowIntoOwnedParam(expected, actual types.TypeID, expr ast.ExprID) bool {
	return tc.reportBorrowIntoOwned(expected, actual, expr, "this parameter", "the callee")
}

// reportBorrowIntoOwned is the shared engine: role names the owning
// destination ("this parameter", "field 'x'"), freer names who frees it.
func (tc *typeChecker) reportBorrowIntoOwned(expected, actual types.TypeID, expr ast.ExprID, role, freer string) bool {
	if tc.types == nil || expected == types.NoTypeID || actual == types.NoTypeID {
		return false
	}
	actInfo, ok := tc.types.Lookup(tc.resolveAlias(actual))
	if !ok || actInfo.Kind != types.KindReference {
		return false
	}
	elem := tc.resolveAlias(actInfo.Elem)
	if tc.isCopyType(elem) {
		return false
	}
	expectedResolved := tc.resolveAlias(expected)
	if expInfo, ok := tc.types.Lookup(expectedResolved); ok && expInfo.Kind == types.KindOwn {
		expectedResolved = tc.resolveAlias(expInfo.Elem)
	}
	if expectedResolved != elem {
		return false
	}
	span := tc.exprSpan(expr)
	name := ""
	inner := tc.unwrapGroupExpr(expr)
	if node := tc.builder.Exprs.Get(inner); node != nil && node.Kind == ast.ExprUnary {
		if unary := tc.builder.Exprs.Unaries.Get(uint32(node.Payload)); unary != nil {
			if ident, ok := tc.builder.Exprs.Ident(unary.Operand); ok && ident != nil {
				name = tc.lookupName(ident.Name)
			}
		}
	}
	elemLabel := tc.typeLabel(elem)
	if tc.reporter == nil {
		tc.report(diag.SemaBorrowIntoOwnedParam, span,
			"%s takes ownership of a %s, but the value provided is only a borrow (%s)",
			role, elemLabel, tc.typeLabel(actual))
		return true
	}
	b := diag.ReportError(tc.reporter, diag.SemaBorrowIntoOwnedParam, span,
		fmt.Sprintf("%s takes ownership of a %s, but the value provided is only a borrow (%s)",
			role, elemLabel, tc.typeLabel(actual)))
	if b == nil {
		return true
	}
	b.WithNote(span, fmt.Sprintf(
		"an owned value is freed by %s; binding a borrow to it would free the borrowed value twice", freer))
	if name != "" {
		b.WithNote(span, fmt.Sprintf(
			"hint: pass '%s' itself to give the value away, or keep yours and pass a copy: %s.__clone()",
			name, name))
	} else {
		b.WithNote(span, "hint: pass the value itself to give it away, or pass a copy via .__clone()")
	}
	b.Emit()
	return true
}

func (tc *typeChecker) recordCallSymbol(callID ast.ExprID, symID symbols.SymbolID) {
	if callID == ast.NoExprID || !symID.IsValid() {
		return
	}
	if sym := tc.symbolFromID(symID); sym != nil {
		if sym.Kind != symbols.SymbolFunction && sym.Kind != symbols.SymbolTag {
			return
		}
	}
	tc.recordFunctionCall(symID)
	if tc.symbols == nil || tc.symbols.ExprSymbols == nil {
		return
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
