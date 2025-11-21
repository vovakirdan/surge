package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) resolveTypeParamBounds(paramIDs []ast.TypeParamID, scope symbols.ScopeID, markUsage func(ast.TypeID)) []symbols.TypeParamSymbol {
	if tc.builder == nil || len(paramIDs) == 0 {
		return nil
	}
	bounds := make([]symbols.TypeParamSymbol, 0, len(paramIDs))
	scope = tc.scopeOrFile(scope)
	for _, pid := range paramIDs {
		param := tc.builder.Items.TypeParam(pid)
		if param == nil {
			continue
		}
		spec := symbols.TypeParamSymbol{
			Name: param.Name,
			Span: param.Span,
		}
		spec.Bounds = tc.resolveBoundsForParam(param, scope, markUsage)
		if id := tc.lookupTypeParam(param.Name); id != types.NoTypeID && len(spec.Bounds) > 0 {
			if tc.typeParamBounds == nil {
				tc.typeParamBounds = make(map[types.TypeID][]symbols.BoundInstance)
			}
			tc.typeParamBounds[id] = spec.Bounds
		}
		bounds = append(bounds, spec)
	}
	return bounds
}

func (tc *typeChecker) resolveBoundsForParam(param *ast.TypeParam, scope symbols.ScopeID, markUsage func(ast.TypeID)) []symbols.BoundInstance {
	if param == nil || param.BoundsNum == 0 || !param.Bounds.IsValid() {
		return nil
	}
	result := make([]symbols.BoundInstance, 0, param.BoundsNum)
	seen := make(map[source.StringID]source.Span, param.BoundsNum)
	paramName := tc.lookupName(param.Name)
	if paramName == "" {
		paramName = "_"
	}
	end := param.Bounds + ast.TypeParamBoundID(param.BoundsNum)
	for boundID := param.Bounds; boundID < end; boundID++ {
		bound := tc.builder.Items.TypeParamBound(boundID)
		if bound == nil {
			continue
		}
		if markUsage != nil {
			for _, arg := range bound.TypeArgs {
				markUsage(arg)
			}
		}
		contractName := tc.lookupName(bound.Name)
		if contractName == "" {
			contractName = "_"
		}
		if prev, ok := seen[bound.Name]; ok {
			tc.reportDuplicateBound(contractName, paramName, bound.Span, prev)
			continue
		}
		seen[bound.Name] = bound.Span
		if inst, ok := tc.resolveBoundInstance(bound, scope, paramName, contractName); ok {
			result = append(result, inst)
		}
	}
	return result
}

func (tc *typeChecker) resolveBoundInstance(bound *ast.TypeParamBound, scope symbols.ScopeID, paramName, contractName string) (symbols.BoundInstance, bool) {
	var inst symbols.BoundInstance
	if bound == nil {
		return inst, false
	}
	scope = tc.scopeOrFile(scope)

	contractID := tc.lookupContractSymbol(bound.Name, scope)
	if !contractID.IsValid() {
		if alt := tc.lookupSymbolAny(bound.Name, scope); alt.IsValid() {
			span := bound.NameSpan
			if span == (source.Span{}) {
				span = bound.Span
			}
			tc.report(diag.SemaContractBoundNotContract, span, "'%s' is not a contract (in bounds of '%s')", contractName, paramName)
			return inst, false
		}
		tc.report(diag.SemaContractBoundNotFound, bound.Span, "unknown contract '%s' in bounds of '%s'", contractName, paramName)
		return inst, false
	}
	sym := tc.symbolFromID(contractID)
	if sym == nil || sym.Kind != symbols.SymbolContract {
		span := bound.NameSpan
		if span == (source.Span{}) {
			span = bound.Span
		}
		tc.report(diag.SemaContractBoundNotContract, span, "'%s' is not a contract (in bounds of '%s')", contractName, paramName)
		return inst, false
	}

	args, ok := tc.resolveBoundArgs(bound, scope)
	if !ok {
		return inst, false
	}
	inst.Contract = contractID
	inst.GenericArgs = args
	inst.Span = bound.Span
	return inst, true
}

func (tc *typeChecker) resolveBoundArgs(bound *ast.TypeParamBound, scope symbols.ScopeID) ([]types.TypeID, bool) {
	if bound == nil || len(bound.TypeArgs) == 0 {
		return nil, true
	}
	args := make([]types.TypeID, 0, len(bound.TypeArgs))
	allOK := true
	for _, argID := range bound.TypeArgs {
		argType := tc.resolveTypeExprWithScope(argID, scope)
		args = append(args, argType)
		if argType != types.NoTypeID {
			continue
		}
		allOK = false
		argSpan := bound.Span
		if expr := tc.builder.Types.Get(argID); expr != nil && expr.Span != (source.Span{}) {
			argSpan = expr.Span
		}
		name := tc.typeExprName(argID)
		if name == "" {
			name = "type"
		}
		tc.report(diag.SemaContractBoundTypeError, argSpan, "unknown type '%s' in contract arguments", name)
	}
	return args, allOK
}

func (tc *typeChecker) reportDuplicateBound(contractName, paramName string, span, prev source.Span) {
	if tc.reporter == nil {
		return
	}
	if contractName == "" {
		contractName = "_"
	}
	if paramName == "" {
		paramName = "_"
	}
	msg := fmt.Sprintf("duplicate contract '%s' in bounds of '%s'", contractName, paramName)
	builder := diag.ReportWarning(tc.reporter, diag.SemaContractBoundDuplicate, span, msg)
	if builder == nil {
		return
	}
	if prev != (source.Span{}) {
		builder.WithNote(prev, "previous bound is here")
	}
	builder.Emit()
}

func (tc *typeChecker) typeExprName(id ast.TypeID) string {
	if tc.builder == nil {
		return ""
	}
	expr := tc.builder.Types.Get(id)
	if expr == nil {
		return ""
	}
	if expr.Kind == ast.TypeExprPath {
		if path, ok := tc.builder.Types.Path(id); ok && path != nil && len(path.Segments) > 0 {
			name := tc.lookupName(path.Segments[0].Name)
			if name != "" {
				return name
			}
		}
	}
	return ""
}
