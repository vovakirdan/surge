package symbols

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

func (fr *fileResolver) bindComparePattern(exprID ast.ExprID) {
	if !exprID.IsValid() || fr.builder == nil {
		return
	}
	node := fr.builder.Exprs.Get(exprID)
	if node == nil {
		return
	}
	switch node.Kind {
	case ast.ExprIdent:
		ident, _ := fr.builder.Exprs.Ident(exprID)
		if ident == nil || ident.Name == source.NoStringID {
			return
		}
		if fr.builder.StringsInterner.MustLookup(ident.Name) == "_" {
			return
		}
		decl := SymbolDecl{
			SourceFile: fr.sourceFile,
			ASTFile:    fr.fileID,
		}
		if symID, ok := fr.resolver.Declare(ident.Name, node.Span, SymbolLet, 0, decl); ok {
			fr.result.ExprSymbols[exprID] = symID
		}
	case ast.ExprCall:
		call, _ := fr.builder.Exprs.Call(exprID)
		if call == nil {
			return
		}
		fr.walkExpr(call.Target)
		for _, arg := range call.Args {
			fr.bindComparePattern(arg.Value)
		}
	case ast.ExprTuple:
		tuple, _ := fr.builder.Exprs.Tuple(exprID)
		if tuple == nil {
			return
		}
		for _, elem := range tuple.Elements {
			fr.bindComparePattern(elem)
		}
	default:
		fr.walkExpr(exprID)
	}
}

func (fr *fileResolver) resolveIdent(exprID ast.ExprID, span source.Span, name source.StringID) {
	if name == source.NoStringID || fr.resolver == nil {
		return
	}
	if fr.isWildcard(name) {
		fr.reportWildcardValue(span)
		return
	}
	if symID, ok := fr.resolver.Lookup(name); ok {
		if fr.tryResolveImportSymbol(exprID, span, symID) {
			return
		}
		fr.result.ExprSymbols[exprID] = symID
		return
	}
	if fr.hasTypeParam(name) {
		// Type parameters are handled by the type checker; treat as resolved.
		return
	}
	fr.reportUnresolved(name, span)
}

func (fr *fileResolver) reportUnresolved(name source.StringID, span source.Span) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	if nameStr == "_" {
		return
	}
	msg := fmt.Sprintf("cannot resolve '%s'", nameStr)
	if b := diag.ReportError(fr.resolver.reporter, diag.SemaUnresolvedSymbol, span, msg); b != nil {
		b.Emit()
	}
}

func (fr *fileResolver) reportWildcardValue(span source.Span) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	if span == (source.Span{}) {
		span = fr.fileSpan()
	}
	if b := diag.ReportError(fr.resolver.reporter, diag.SemaWildcardValue, span, "wildcard '_' cannot be used as a value"); b != nil {
		b.Emit()
	}
}

func (fr *fileResolver) reportWildcardMut(span source.Span) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	if span == (source.Span{}) {
		span = fr.fileSpan()
	}
	if b := diag.ReportError(fr.resolver.reporter, diag.SemaWildcardMut, span, "wildcard '_' cannot be mutable"); b != nil {
		b.Emit()
	}
}

func (fr *fileResolver) isWildcard(name source.StringID) bool {
	if name == source.NoStringID || fr.builder == nil || fr.builder.StringsInterner == nil {
		return false
	}
	return fr.lookupString(name) == "_"
}

func (fr *fileResolver) fileSpan() source.Span {
	if fr.builder == nil {
		return source.Span{}
	}
	if file := fr.builder.Files.Get(fr.fileID); file != nil {
		return file.Span
	}
	return source.Span{}
}

func (fr *fileResolver) checkAmbiguousCall(target ast.ExprID) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	targetExpr := fr.builder.Exprs.Get(target)
	if targetExpr == nil || targetExpr.Kind != ast.ExprIdent {
		return
	}
	data, _ := fr.builder.Exprs.Ident(target)
	if data == nil || data.Name == source.NoStringID {
		return
	}
	fnSyms := fr.collectFileScopeSymbols(data.Name, SymbolFunction)
	if len(fnSyms) == 0 {
		return
	}
	tagSyms := fr.collectFileScopeSymbols(data.Name, SymbolTag)
	if len(tagSyms) == 0 {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(data.Name)
	msg := fmt.Sprintf("identifier '%s' matches both a function and a tag constructor", nameStr)
	if b := diag.ReportError(fr.resolver.reporter, diag.SemaAmbiguousCtorOrFn, targetExpr.Span, msg); b != nil {
		combined := append(append([]SymbolID(nil), fnSyms...), tagSyms...)
		fr.attachPreviousNotes(b, combined)
		b.Emit()
	}
}

func (fr *fileResolver) collectFileScopeSymbols(name source.StringID, kinds ...SymbolKind) []SymbolID {
	if fr.result == nil || fr.result.Table == nil || name == source.NoStringID {
		return nil
	}
	scope := fr.result.Table.Scopes.Get(fr.result.FileScope)
	if scope == nil {
		return nil
	}
	ids := scope.NameIndex[name]
	if len(ids) == 0 || len(kinds) == 0 {
		return nil
	}
	want := make(map[SymbolKind]struct{}, len(kinds))
	for _, kind := range kinds {
		want[kind] = struct{}{}
	}
	out := make([]SymbolID, 0, len(ids))
	for _, id := range ids {
		sym := fr.result.Table.Symbols.Get(id)
		if sym == nil {
			continue
		}
		if _, ok := want[sym.Kind]; ok {
			out = append(out, id)
		}
	}
	return out
}

func (fr *fileResolver) tryResolveImportSymbol(exprID ast.ExprID, span source.Span, symID SymbolID) bool {
	sym := fr.result.Table.Symbols.Get(symID)
	if sym == nil || sym.Kind != SymbolImport {
		return false
	}
	modulePath := sym.ModulePath
	exports := fr.moduleExports[modulePath]
	name := sym.ImportName
	if name == source.NoStringID {
		name = sym.Name
	}
	nameStr := fr.lookupString(name)
	if nameStr == "" {
		nameStr = "_"
	}
	if exports == nil {
		fr.reportModuleMemberNotFound(modulePath, name, span)
		return true
	}
	exported := exports.Lookup(nameStr)
	if len(exported) == 0 {
		fr.reportModuleMemberNotFound(modulePath, name, span)
		return true
	}
	publics := make([]*ExportedSymbol, 0, len(exported))
	for i := range exported {
		if exported[i].Flags&SymbolFlagPublic != 0 {
			publics = append(publics, &exported[i])
		}
	}
	if len(publics) == 0 {
		refSpan := exported[0].Span
		fr.reportModuleMemberNotPublic(modulePath, name, span, refSpan)
		return true
	}
	var first SymbolID
	for _, cand := range publics {
		if cand == nil {
			continue
		}
		synth := fr.syntheticSymbolForExport(modulePath, nameStr, cand, span)
		if !synth.IsValid() {
			continue
		}
		if !first.IsValid() {
			first = synth
		}
	}
	if first.IsValid() {
		fr.result.ExprSymbols[exprID] = first
		return true
	}
	return false
}
