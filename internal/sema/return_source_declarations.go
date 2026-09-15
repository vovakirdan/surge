package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) registerDeclaredFnType(fn *ast.FnItem, params []types.TypeID, result types.TypeID, scope symbols.ScopeID, owner symbols.SymbolID) types.TypeID {
	syntax := symbols.FunctionReturnSourceSyntax(tc.builder, fn)
	if !tc.checkReturnSourceDeclaration(syntax, params, result, scope, owner, ast.NoTypeID) {
		return types.NoTypeID
	}
	return tc.types.RegisterFnWithReturnSources(params, result, syntax.Sources())
}

func (tc *typeChecker) resolveFunctionTypeExpr(id ast.TypeID, scope symbols.ScopeID) types.TypeID {
	fn, ok := tc.builder.Types.Fn(id)
	if !ok || fn == nil {
		return types.NoTypeID
	}
	params := make([]types.TypeID, 0, len(fn.Params))
	allValid := true
	for _, param := range fn.Params {
		tc.validateAttrs(param.AttrStart, param.AttrCount, ast.AttrTargetFnTypeParam, diag.SemaError)
		resolved := tc.resolveTypeExprWithScope(param.Type, scope)
		if resolved == types.NoTypeID {
			allValid = false
		}
		if param.Variadic && resolved != types.NoTypeID {
			resolved = tc.instantiateArrayType(resolved)
		}
		params = append(params, resolved)
	}
	originalResult := tc.resolveTypeExprWithScope(fn.Return, scope)
	result := originalResult
	if result == types.NoTypeID {
		// Keep the existing function-type recovery after a result type error.
		result = tc.types.Builtins().Unit
	}
	syntax := symbols.FunctionTypeReturnSourceSyntax(tc.builder, id)
	if !tc.checkReturnSourceDeclaration(syntax, params, originalResult, scope, symbols.NoSymbolID, id) || !allValid {
		return types.NoTypeID
	}
	return tc.types.RegisterFnWithReturnSources(params, result, syntax.Sources())
}

func (tc *typeChecker) checkReturnSourceDeclaration(syntax symbols.ReturnSourceSyntax, params []types.TypeID, result types.TypeID, scope symbols.ScopeID, owner symbols.SymbolID, typeExpr ast.TypeID) bool {
	if syntax.Sources().IsAllInputs() {
		return true
	}
	if original := tc.returnSourceDeclaration(typeExpr, scope); original != nil {
		if slices.Equal(params, original.params) && result == original.result {
			return ValidateDeclaredReturnSources(tc.types, *original).Status != ReturnSourcesInvalid
		}
		for _, use := range tc.result.ReturnSourceInstantiations {
			if use.Declaration.TypeExpr == typeExpr && use.Scope == scope && slices.Equal(use.params, params) && use.result == result {
				return ValidateInstantiatedReturnSources(tc.types, use.Declaration, params, result).Status != ReturnSourcesInvalid
			}
		}
		use := ReturnSourceInstantiationRequest{Declaration: *original, Scope: scope, TypeParamEnv: tc.currentTypeParamEnv(), params: slices.Clone(params), result: result}
		tc.result.ReturnSourceInstantiations = append(tc.result.ReturnSourceInstantiations, use)
		// An invalid original already has its declaration diagnostic.
		if ValidateDeclaredReturnSources(tc.types, *original).Status == ReturnSourcesInvalid {
			return false
		}
		validation := ValidateInstantiatedReturnSources(tc.types, *original, params, result)
		if validation.Status == ReturnSourcesInvalid {
			tc.reportInvalidReturnSource(validation)
			return false
		}
		return true
	}
	request := NewReturnSourceDeclarationRequest(syntax, params, result)
	request.Owner = owner
	request.TypeExpr = typeExpr
	request.Scope = scope
	request.TypeParamEnv = tc.currentTypeParamEnv()
	// Record while the original type environment still exists. In particular,
	// an interned concrete FnInfo cannot say whether an owned specialization
	// came from a permitted conditional generic promise.
	if tc.result != nil {
		tc.result.ReturnSourceDeclarations = append(tc.result.ReturnSourceDeclarations, request)
	}
	validation := ValidateDeclaredReturnSources(tc.types, request)
	if validation.Status == ReturnSourcesInvalid {
		tc.reportInvalidReturnSource(validation)
		return false
	}
	return true
}

// reportInvalidReturnSource keeps the broken rule as the message and says, at
// the marker, what the promise means and how to repair the declaration.
func (tc *typeChecker) reportInvalidReturnSource(validation ReturnSourceValidation) {
	code := validation.Code
	if code == 0 {
		code = diag.SemaError
	}
	b := diag.ReportError(tc.reporter, code, validation.Span, validation.Reason)
	if b == nil {
		return
	}
	switch code {
	case diag.SemaReturnSourceArgument:
		b.WithNote(validation.Span, "`@return_source` marks the parameter it is written on; it has no argument form")
		b.WithHelp(validation.Span, "write `@return_source` without parentheses on each parameter the result may borrow from")
	case diag.SemaReturnSourceMissingParam:
		b.WithNote(validation.Span, "the marker names a parameter position this signature does not have")
		b.WithHelp(validation.Span, "put `@return_source` directly on one of the function's parameters")
	case diag.SemaReturnSourceOwnedParam:
		b.WithNote(validation.Span, "a borrowed result can only point into a parameter that itself carries a reference")
		b.WithHelp(validation.Span, "take this parameter by reference (`&T`), or remove `@return_source` from it")
	case diag.SemaReturnSourceOwnedResult:
		b.WithNote(validation.Span, "a result that owns its value borrows from no parameter, so there is no source to declare")
		b.WithHelp(validation.Span, "return a reference, or remove `@return_source`")
	}
	b.Emit()
}

func (tc *typeChecker) returnSourceDeclaration(typeExpr ast.TypeID, scope symbols.ScopeID) *ReturnSourceDeclarationRequest {
	if !typeExpr.IsValid() || tc.result == nil {
		return nil
	}
	for i := range tc.result.ReturnSourceDeclarations {
		request := &tc.result.ReturnSourceDeclarations[i]
		if request.TypeExpr == typeExpr && request.Scope == scope {
			return request
		}
	}
	return nil
}

// A forward alias can be instantiated before its declaration is populated.
// Resolve only targets that own marked callback syntax in the original generic
// environment, so concrete resolution cannot invent an owned declaration.
func (tc *typeChecker) ensureReturnSourceAliasTemplate(target ast.TypeID, scope symbols.ScopeID, owner symbols.SymbolID, params []genericParamSpec) {
	if tc.result == nil || len(params) == 0 {
		return
	}
	var callbacks []ast.TypeID
	tc.collectReturnSourceFunctionTypes(target, &callbacks)
	missing := false
	for _, id := range callbacks {
		missing = missing || tc.returnSourceDeclaration(id, scope) == nil
	}
	if !missing {
		return
	}
	key := "return-source-template/" + tc.instantiationKey(owner, nil)
	if _, active := tc.typeInstantiationInProgress[key]; active {
		return
	}
	if tc.typeInstantiationInProgress == nil {
		tc.typeInstantiationInProgress = make(map[string]struct{})
	}
	tc.typeInstantiationInProgress[key] = struct{}{}
	defer delete(tc.typeInstantiationInProgress, key)
	if pushed := tc.pushTypeParams(owner, params, nil); pushed {
		defer tc.popTypeParams()
	}
	tc.resolveTypeExprWithScope(target, scope)
}

// This walk follows syntax-owned children only, never named alias declarations.
func (tc *typeChecker) collectReturnSourceFunctionTypes(id ast.TypeID, out *[]ast.TypeID) {
	if !id.IsValid() {
		return
	}
	node := tc.builder.Types.Get(id)
	if node == nil {
		return
	}
	visit := func(child ast.TypeID) { tc.collectReturnSourceFunctionTypes(child, out) }
	switch node.Kind {
	case ast.TypeExprFn:
		fn, _ := tc.builder.Types.Fn(id)
		if !symbols.FunctionTypeReturnSourceSyntax(tc.builder, id).Sources().IsAllInputs() {
			*out = append(*out, id)
		}
		for _, param := range fn.Params {
			visit(param.Type)
		}
		visit(fn.Return)
	case ast.TypeExprPath:
		path, _ := tc.builder.Types.Path(id)
		for _, segment := range path.Segments {
			for _, arg := range segment.Generics {
				visit(arg)
			}
		}
	case ast.TypeExprUnary:
		unary, _ := tc.builder.Types.UnaryType(id)
		visit(unary.Inner)
	case ast.TypeExprArray:
		array, _ := tc.builder.Types.Array(id)
		visit(array.Elem)
	case ast.TypeExprTuple:
		tuple, _ := tc.builder.Types.Tuple(id)
		for _, elem := range tuple.Elems {
			visit(elem)
		}
	case ast.TypeExprOptional:
		optional, _ := tc.builder.Types.Optional(id)
		visit(optional.Inner)
	case ast.TypeExprErrorable:
		errorable, _ := tc.builder.Types.Errorable(id)
		visit(errorable.Inner)
		visit(errorable.Error)
	}
}
