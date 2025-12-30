package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
		if tc.typesAssignable(expectedType, arg.ty, true) {
			tc.dropImplicitBorrow(arg.expr, expectedType, arg.ty, tc.exprSpan(arg.expr))
			tc.recordTagUnionUpcast(arg.expr, arg.ty, expectedType)
			tc.recordNumericWidening(arg.expr, arg.ty, expectedType)
			continue
		}
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(arg.expr),
			"expected %s, got %s",
			tc.typeLabel(expectedType), tc.typeLabel(arg.ty))
		return types.NoTypeID
	}
	for i, arg := range args {
		tc.applyParamOwnershipForType(fnInfo.Params[i], arg.expr, arg.ty, tc.exprSpan(arg.expr))
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
		if tc.recordTagUnionUpcast(arg.expr, arg.ty, expectedType) {
			continue
		}
		if tc.recordNumericWidening(arg.expr, arg.ty, expectedType) {
			continue
		}
		if !tc.typesAssignable(expectedType, arg.ty, true) && tc.callAllowsImplicitTo(sym, paramIndex) {
			if convType, found, _ := tc.tryImplicitConversion(arg.ty, expectedType); found {
				tc.recordImplicitConversion(arg.expr, arg.ty, convType)
			}
		}
	}
}

// materializeCallArguments applies expected types to literal arguments after overload resolution.
// This ensures numeric literals are range-checked and typed to the selected parameter types.
func (tc *typeChecker) materializeCallArguments(sym *symbols.Symbol, args []callArg, concreteArgs []types.TypeID) {
	if sym == nil || sym.Signature == nil || tc.builder == nil {
		return
	}
	sig := sym.Signature

	hasNamed := false
	for _, arg := range args {
		if arg.name != source.NoStringID {
			hasNamed = true
			break
		}
	}
	ordered := args
	if hasNamed {
		if reordered, ok := tc.reorderArgsForSignature(sig, args); ok {
			ordered = reordered
		} else {
			return
		}
	}

	paramNames, paramSet := tc.typeParamNameSet(sym)
	bindings := make(map[string]types.TypeID)
	if len(paramNames) > 0 && len(concreteArgs) == len(paramNames) {
		for i, name := range paramNames {
			if name != "" {
				bindings[name] = concreteArgs[i]
			}
		}
	}

	variadicIndex := -1
	for i, v := range sig.Variadic {
		if v {
			variadicIndex = i
			break
		}
	}

	for i, arg := range ordered {
		paramIndex := i
		if variadicIndex >= 0 && i >= variadicIndex {
			paramIndex = variadicIndex
		}
		if paramIndex >= len(sig.Params) {
			continue
		}
		expectedType := tc.instantiateResultType(sig.Params[paramIndex], bindings, paramSet)
		if expectedType == types.NoTypeID {
			continue
		}
		tc.materializeNumericLiteral(arg.expr, expectedType)
		tc.materializeArrayLiteral(arg.expr, expectedType)
	}

	for i := range args {
		if ty := tc.result.ExprTypes[args[i].expr]; ty != types.NoTypeID {
			args[i].ty = ty
		}
	}
}

func (tc *typeChecker) callAllowsImplicitTo(sym *symbols.Symbol, paramIndex int) bool {
	if sym == nil {
		return false
	}
	if sym.Flags&symbols.SymbolFlagAllowTo != 0 {
		return true
	}
	if sym.Signature == nil || paramIndex < 0 || paramIndex >= len(sym.Signature.AllowTo) {
		return false
	}
	return sym.Signature.AllowTo[paramIndex]
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
func (tc *typeChecker) handleCloneCall(callID ast.ExprID, args []callArg, span source.Span) types.TypeID {
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

	if tc.types != nil {
		if tt, ok := tc.types.Lookup(tc.resolveAlias(innerType)); ok && tt.Kind == types.KindGenericParam {
			// Defer clone validation for generic parameters to monomorphization.
			return innerType
		}
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
			if args[0].expr.IsValid() {
				tc.applyParamOwnership(symbols.TypeKey("&"), args[0].expr, args[0].ty, tc.exprSpan(args[0].expr))
			}
			tc.recordCloneSymbol(callID, innerType)
			return innerType
		}
	}

	// Method found but signature invalid
	tc.report(diag.SemaTypeNotClonable, span,
		"type %s has __clone but with invalid signature", tc.typeLabel(innerType))
	return types.NoTypeID
}

func (tc *typeChecker) recordCloneSymbol(expr ast.ExprID, recv types.TypeID) {
	if !expr.IsValid() || tc.result == nil || tc.builder == nil || tc.builder.StringsInterner == nil {
		return
	}
	if tc.result.CloneSymbols == nil {
		tc.result.CloneSymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	nameID := tc.builder.StringsInterner.Intern("__clone")
	member := &ast.ExprMemberData{Field: nameID}
	tc.result.CloneSymbols[expr] = tc.resolveMethodCallSymbol(member, recv, ast.NoExprID, nil, nil, false)
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
