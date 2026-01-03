package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) moduleSymbolForExpr(id ast.ExprID) *symbols.Symbol {
	if !id.IsValid() {
		return nil
	}
	symID := tc.symbolForExpr(id)
	if !symID.IsValid() {
		if ident, ok := tc.builder.Exprs.Ident(id); ok && ident != nil {
			symID = tc.lookupSymbolAny(ident.Name, tc.currentScope())
		}
	}
	if !symID.IsValid() {
		return nil
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Kind != symbols.SymbolModule {
		return nil
	}
	return sym
}

func (tc *typeChecker) resolveImportedValueSymbol(sym *symbols.Symbol, name source.StringID, span source.Span) *symbols.Symbol {
	if sym == nil || sym.ModulePath == "" || tc.exports == nil {
		return sym
	}
	exports := tc.exports[sym.ModulePath]
	if exports == nil {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no exports", sym.ModulePath)
		return sym
	}
	nameStr := tc.lookupName(sym.ImportName)
	if nameStr == "" {
		nameStr = tc.lookupName(name)
	}
	if nameStr == "" {
		nameStr = "_"
	}
	exported := exports.Lookup(nameStr)
	if len(exported) == 0 {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no member %q", sym.ModulePath, nameStr)
		return sym
	}
	var candidate *symbols.ExportedSymbol
	for i := range exported {
		if exported[i].Flags&symbols.SymbolFlagPublic != 0 {
			candidate = &exported[i]
			break
		}
	}
	if candidate == nil {
		tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public", nameStr, sym.ModulePath)
		return sym
	}
	tc.applyExportToSymbol(sym, candidate, sym.ModulePath)
	return sym
}

func (tc *typeChecker) typeOfModuleMember(module *symbols.Symbol, field source.StringID, span source.Span) types.TypeID {
	exp := tc.lookupModuleExport(module, field, span)
	if exp == nil {
		return types.NoTypeID
	}
	switch exp.Kind {
	case symbols.SymbolConst, symbols.SymbolLet, symbols.SymbolFunction, symbols.SymbolTag, symbols.SymbolType:
		return exp.Type
	default:
		tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not a value", tc.lookupName(field), module.ModulePath)
		return types.NoTypeID
	}
}

func (tc *typeChecker) moduleFunctionResult(module *symbols.Symbol, name source.StringID, args []callArg, typeArgs []types.TypeID, span source.Span) types.TypeID {
	exported := tc.moduleExportsByName(module, name, span)
	if len(exported) == 0 {
		return types.NoTypeID
	}
	candidates := make([]*symbols.Symbol, 0, len(exported))
	for i := range exported {
		if exported[i].Kind != symbols.SymbolFunction || exported[i].Flags&symbols.SymbolFlagPublic == 0 {
			continue
		}
		candidates = append(candidates, tc.exportedSymbolToSymbol(&exported[i], module.ModulePath))
	}
	if len(candidates) == 0 {
		tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public or not a function", tc.lookupName(name), module.ModulePath)
		return types.NoTypeID
	}
	bestCost := -1
	bestType := types.NoTypeID
	bestName := tc.lookupName(name)
	if bestName == "" {
		bestName = "_"
	}
	var borrowInfo borrowMatchInfo
	for _, cand := range candidates {
		cost, result, _, ok := tc.evaluateFunctionCandidate(cand, args, typeArgs, &borrowInfo)
		if !ok {
			continue
		}
		if bestCost == -1 || cost < bestCost {
			bestCost = cost
			bestType = result
		} else if cost == bestCost {
			tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for %s", bestName)
			return types.NoTypeID
		}
	}
	if bestType != types.NoTypeID {
		return bestType
	}
	if borrowInfo.expr.IsValid() {
		tc.reportBorrowFailure(&borrowInfo)
		return types.NoTypeID
	}
	if len(candidates) == 1 && tc.reportCallArgumentMismatch(candidates[0], args, typeArgs) {
		return types.NoTypeID
	}
	tc.report(diag.SemaNoOverload, span, "no matching overload for %s", bestName)
	return types.NoTypeID
}

func (tc *typeChecker) moduleExportsByName(module *symbols.Symbol, field source.StringID, span source.Span) []symbols.ExportedSymbol {
	if module == nil || module.ModulePath == "" || tc.exports == nil {
		return nil
	}
	exports := tc.exports[module.ModulePath]
	if exports == nil {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no exports", module.ModulePath)
		return nil
	}
	nameStr := tc.lookupName(field)
	if nameStr == "" {
		nameStr = "_"
	}
	exported := exports.Lookup(nameStr)
	if len(exported) == 0 {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no member %q", module.ModulePath, nameStr)
		return nil
	}
	return exported
}

func (tc *typeChecker) lookupModuleExport(module *symbols.Symbol, field source.StringID, span source.Span) *symbols.ExportedSymbol {
	exported := tc.moduleExportsByName(module, field, span)
	if len(exported) == 0 {
		return nil
	}
	for i := range exported {
		if exported[i].Flags&symbols.SymbolFlagPublic != 0 {
			return &exported[i]
		}
	}
	nameStr := tc.lookupName(field)
	if nameStr == "" {
		nameStr = "_"
	}
	tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public", nameStr, module.ModulePath)
	return nil
}

func (tc *typeChecker) applyExportToSymbol(target *symbols.Symbol, exp *symbols.ExportedSymbol, modulePath string) {
	if target == nil || exp == nil {
		return
	}
	resolved := tc.exportedSymbolToSymbol(exp, modulePath)
	if resolved == nil {
		return
	}
	target.Kind = resolved.Kind
	target.Type = resolved.Type
	target.Signature = resolved.Signature
	target.TypeParams = resolved.TypeParams
	target.TypeParamSymbols = resolved.TypeParamSymbols
	target.TypeParamSpan = resolved.TypeParamSpan
	target.Flags |= symbols.SymbolFlagImported
}

func (tc *typeChecker) exportedSymbolToSymbol(exp *symbols.ExportedSymbol, modulePath string) *symbols.Symbol {
	if exp == nil {
		return nil
	}
	sym := &symbols.Symbol{
		Name:          exp.NameID,
		Kind:          exp.Kind,
		Flags:         exp.Flags | symbols.SymbolFlagImported,
		Span:          exp.Span,
		ModulePath:    modulePath,
		Type:          exp.Type,
		Signature:     exp.Signature,
		ReceiverKey:   exp.ReceiverKey,
		TypeParamSpan: exp.TypeParamSpan,
	}
	if tc.builder != nil && tc.builder.StringsInterner != nil && len(exp.TypeParamNames) > 0 {
		for _, n := range exp.TypeParamNames {
			sym.TypeParams = append(sym.TypeParams, tc.builder.StringsInterner.Intern(n))
		}
	} else if len(exp.TypeParams) > 0 {
		sym.TypeParams = append([]source.StringID(nil), exp.TypeParams...)
	}
	if len(exp.TypeParamSyms) > 0 {
		sym.TypeParamSymbols = symbols.CloneTypeParamSymbols(exp.TypeParamSyms)
		if tc.builder != nil && tc.builder.StringsInterner != nil && len(exp.TypeParamNames) > 0 {
			limit := len(sym.TypeParamSymbols)
			if len(exp.TypeParamNames) < limit {
				limit = len(exp.TypeParamNames)
			}
			for i := range limit {
				sym.TypeParamSymbols[i].Name = tc.builder.StringsInterner.Intern(exp.TypeParamNames[i])
			}
		}
	}
	if exp.Contract != nil {
		sym.Contract = symbols.CloneContractSpec(exp.Contract)
	}
	return sym
}
