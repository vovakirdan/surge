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
	candidates := tc.publicModuleValueExports(exported)
	if len(candidates) == 0 {
		tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public", nameStr, sym.ModulePath)
		return sym
	}
	tc.applyExportToSymbol(sym, candidates[0], sym.ModulePath)
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

func (tc *typeChecker) moduleTypeMember(module *symbols.Symbol, field source.StringID, span source.Span, report bool) types.TypeID {
	if module == nil || module.ModulePath == "" || tc.exports == nil {
		return types.NoTypeID
	}
	exports := tc.exports[module.ModulePath]
	if exports == nil {
		if report {
			tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no exports", module.ModulePath)
		}
		return types.NoTypeID
	}
	nameStr := tc.lookupName(field)
	if nameStr == "" {
		nameStr = "_"
	}
	exported := exports.Lookup(nameStr)
	if len(exported) == 0 {
		if report {
			tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no member %q", module.ModulePath, nameStr)
		}
		return types.NoTypeID
	}
	for i := range exported {
		exp := &exported[i]
		if exp.Kind != symbols.SymbolType {
			continue
		}
		if exp.Flags&symbols.SymbolFlagPublic == 0 {
			if report {
				tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public", nameStr, module.ModulePath)
			}
			return types.NoTypeID
		}
		return exp.Type
	}
	if report {
		tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not a type", nameStr, module.ModulePath)
	}
	return types.NoTypeID
}

func (tc *typeChecker) moduleFunctionResult(callID ast.ExprID, module *symbols.Symbol, name source.StringID, args []callArg, typeArgs []types.TypeID, span source.Span) types.TypeID {
	exported := tc.moduleExportsByName(module, name, span)
	if len(exported) == 0 {
		return types.NoTypeID
	}
	type moduleCandidate struct {
		id  symbols.SymbolID
		sym *symbols.Symbol
	}
	candidates := make([]moduleCandidate, 0, len(exported))
	for _, exp := range tc.publicModuleValueExports(exported) {
		if exp == nil || (exp.Kind != symbols.SymbolFunction && exp.Kind != symbols.SymbolTag) {
			continue
		}
		if tc.receiverBoundModuleExport(exp) {
			continue
		}
		symID := tc.ensureImportedModuleExportSymbol(module.ModulePath, name, exp, span)
		sym := tc.symbolFromID(symID)
		if sym == nil {
			sym = tc.exportedSymbolToSymbol(exp, module.ModulePath)
		}
		candidates = append(candidates, moduleCandidate{id: symID, sym: sym})
	}
	if len(candidates) == 0 {
		tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public or not a function", tc.lookupName(name), module.ModulePath)
		return types.NoTypeID
	}
	bestCost := -1
	bestType := types.NoTypeID
	var bestSym *symbols.Symbol
	var bestSymID symbols.SymbolID
	var bestArgs []types.TypeID
	bestName := tc.lookupName(name)
	if bestName == "" {
		bestName = "_"
	}
	var borrowInfo borrowMatchInfo
	for _, cand := range candidates {
		cost, result, concreteArgs, ok := tc.evaluateFunctionCandidate(cand.sym, args, typeArgs, &borrowInfo)
		if !ok {
			continue
		}
		if bestCost == -1 || cost < bestCost {
			bestCost = cost
			bestType = result
			bestSym = cand.sym
			bestSymID = cand.id
			bestArgs = concreteArgs
		} else if cost == bestCost {
			tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for %s", bestName)
			return types.NoTypeID
		}
	}
	if bestType != types.NoTypeID {
		if bestSym != nil {
			tc.materializeCallArguments(bestSym, args, bestArgs)
			tc.recordImplicitConversionsForCall(bestSym, args)
			tc.applyCallOwnership(bestSym, args)
			tc.dropImplicitBorrowsForCall(bestSym, args, bestType)
			if bestName != "" && bestName != "_" {
				tc.checkArrayViewResizeCall(bestName, args, span)
			}
		}
		if bestSymID.IsValid() {
			tc.checkDeprecatedSymbol(bestSymID, "function", span)
			note := "call"
			if bestSym != nil && bestSym.Kind == symbols.SymbolTag {
				note = "tag"
			}
			tc.rememberFunctionInstantiation(bestSymID, bestArgs, span, note)
			tc.recordCallSymbol(callID, bestSymID)
		}
		return bestType
	}
	if borrowInfo.expr.IsValid() {
		tc.reportBorrowFailure(&borrowInfo)
		return types.NoTypeID
	}
	if len(candidates) == 1 && tc.reportCallArgumentMismatch(candidates[0].sym, args, typeArgs) {
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
	candidates := tc.publicModuleValueExports(exported)
	if len(candidates) > 0 {
		return candidates[0]
	}
	nameStr := tc.lookupName(field)
	if nameStr == "" {
		nameStr = "_"
	}
	tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public", nameStr, module.ModulePath)
	return nil
}

func (tc *typeChecker) receiverBoundModuleExport(exp *symbols.ExportedSymbol) bool {
	return exp != nil && exp.Kind == symbols.SymbolFunction && (exp.ReceiverKey != "" || exp.Flags&symbols.SymbolFlagMethod != 0)
}

func (tc *typeChecker) publicModuleValueExports(exported []symbols.ExportedSymbol) []*symbols.ExportedSymbol {
	values := make([]*symbols.ExportedSymbol, 0, len(exported))
	methods := make([]*symbols.ExportedSymbol, 0, len(exported))
	for i := range exported {
		if exported[i].Flags&symbols.SymbolFlagPublic == 0 {
			continue
		}
		if tc.receiverBoundModuleExport(&exported[i]) {
			methods = append(methods, &exported[i])
			continue
		}
		values = append(values, &exported[i])
	}
	if len(values) > 0 {
		return values
	}
	return methods
}

func (tc *typeChecker) ensureImportedModuleExportSymbol(modulePath string, name source.StringID, exp *symbols.ExportedSymbol, fallback source.Span) symbols.SymbolID {
	if exp == nil || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return symbols.NoSymbolID
	}
	nameID := exp.NameID
	if nameID == source.NoStringID {
		nameID = name
	}
	if nameID == source.NoStringID && exp.Name != "" && tc.builder != nil && tc.builder.StringsInterner != nil {
		nameID = tc.builder.StringsInterner.Intern(exp.Name)
	}
	if nameID == source.NoStringID {
		return symbols.NoSymbolID
	}

	scope := tc.fileScope()
	if scope.IsValid() && tc.symbols.Table.Scopes != nil {
		if scopeData := tc.symbols.Table.Scopes.Get(scope); scopeData != nil {
			for _, id := range scopeData.NameIndex[nameID] {
				sym := tc.symbolFromID(id)
				if tc.moduleExportMatchesSymbol(sym, modulePath, nameID, exp) {
					return id
				}
			}
		}
	}

	sym := tc.exportedSymbolToSymbol(exp, modulePath)
	if sym == nil {
		return symbols.NoSymbolID
	}
	sym.Name = nameID
	sym.ImportName = nameID
	sym.Scope = scope
	if sym.Span == (source.Span{}) {
		sym.Span = fallback
	}
	id := tc.symbols.Table.Symbols.New(sym)
	if scope.IsValid() && tc.symbols.Table.Scopes != nil {
		if scopeData := tc.symbols.Table.Scopes.Get(scope); scopeData != nil {
			scopeData.Symbols = append(scopeData.Symbols, id)
			if scopeData.NameIndex == nil {
				scopeData.NameIndex = make(map[source.StringID][]symbols.SymbolID)
			}
			scopeData.NameIndex[nameID] = append(scopeData.NameIndex[nameID], id)
		}
	}
	return id
}

func (tc *typeChecker) moduleExportMatchesSymbol(sym *symbols.Symbol, modulePath string, name source.StringID, exp *symbols.ExportedSymbol) bool {
	if sym == nil || exp == nil {
		return false
	}
	if sym.ModulePath != modulePath || sym.Name != name || sym.Kind != exp.Kind {
		return false
	}
	if !typeKeyEqual(sym.ReceiverKey, exp.ReceiverKey) {
		return false
	}
	return moduleFunctionSignaturesEqual(sym.Signature, exp.Signature)
}

func moduleFunctionSignaturesEqual(a, b *symbols.FunctionSignature) bool {
	if a == nil || b == nil {
		return a == b
	}
	if !typeKeyEqual(a.Result, b.Result) || len(a.Params) != len(b.Params) || a.HasSelf != b.HasSelf {
		return false
	}
	for i := range a.Params {
		if !typeKeyEqual(a.Params[i], b.Params[i]) ||
			boolAt(a.Variadic, i) != boolAt(b.Variadic, i) ||
			boolAt(a.Defaults, i) != boolAt(b.Defaults, i) ||
			boolAt(a.AllowTo, i) != boolAt(b.AllowTo, i) {
			return false
		}
	}
	return true
}

func boolAt(values []bool, index int) bool {
	return index >= 0 && index < len(values) && values[index]
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
