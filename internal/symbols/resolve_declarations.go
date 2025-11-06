package symbols

import (
	"surge/internal/ast"
	"surge/internal/source"
)

func (fr *fileResolver) declareLet(itemID ast.ItemID, letItem *ast.LetItem) {
	if letItem.Name == source.NoStringID {
		return
	}
	if letItem.Value.IsValid() {
		fr.walkExpr(letItem.Value)
	}
	flags := SymbolFlags(0)
	if letItem.Visibility == ast.VisPublic {
		flags |= SymbolFlagPublic
	}
	if letItem.IsMut {
		flags |= SymbolFlagMutable
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(letItem.NameSpan, letItem.Span)
	if symID, ok := fr.resolver.Declare(letItem.Name, span, SymbolLet, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareFn(itemID ast.ItemID, fnItem *ast.FnItem) {
	if fnItem.Name == source.NoStringID {
		return
	}
	flags := SymbolFlags(0)
	if fnItem.Flags&ast.FnModifierPublic != 0 {
		flags |= SymbolFlagPublic
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := fnNameSpan(fnItem)
	if symID, ok := fr.declareFunctionWithAttrs(itemID, fnItem, span, fnItem.FnKeywordSpan, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
	}
	fr.walkFn(itemID, fnItem)
}

func (fr *fileResolver) declareType(itemID ast.ItemID, typeItem *ast.TypeItem) {
	if typeItem.Name == source.NoStringID {
		return
	}
	flags := SymbolFlags(0)
	if typeItem.Visibility == ast.VisPublic {
		flags |= SymbolFlagPublic
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(typeItem.TypeKeywordSpan, typeItem.Span)
	if symID, ok := fr.resolver.Declare(typeItem.Name, span, SymbolType, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareTag(itemID ast.ItemID, tagItem *ast.TagItem) {
	if tagItem.Name == source.NoStringID {
		return
	}
	flags := SymbolFlags(0)
	if tagItem.Visibility == ast.VisPublic {
		flags |= SymbolFlagPublic
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(tagItem.TagKeywordSpan, tagItem.Span)
	if symID, ok := fr.resolver.Declare(tagItem.Name, span, SymbolTag, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareImport(itemID ast.ItemID, importItem *ast.ImportItem, itemSpan source.Span) {
	if importItem.ModuleAlias != source.NoStringID {
		fr.declareImportName(itemID, importItem.ModuleAlias, source.NoStringID, importItem.Module, itemSpan)
	}
	if importItem.HasOne {
		name := importItem.One.Alias
		if name == source.NoStringID {
			name = importItem.One.Name
		}
		fr.declareImportName(itemID, name, importItem.One.Name, importItem.Module, itemSpan)
	}
	for _, pair := range importItem.Group {
		name := pair.Alias
		if name == source.NoStringID {
			name = pair.Name
		}
		fr.declareImportName(itemID, name, pair.Name, importItem.Module, itemSpan)
	}
}

func (fr *fileResolver) declareImportName(itemID ast.ItemID, name, original source.StringID, module []source.StringID, span source.Span) {
	if name == source.NoStringID {
		return
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	if symID, ok := fr.resolver.Declare(name, span, SymbolImport, SymbolFlagImported, decl); ok {
		if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
			if len(module) > 0 {
				path := append([]source.StringID(nil), module...)
				sym.Aliases = append(sym.Aliases, path...)
			}
			if original != source.NoStringID && original != name {
				sym.Aliases = append(sym.Aliases, original)
			}
		}
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareExternFn(container ast.ItemID, member *ast.ExternMember, fnItem *ast.FnItem) {
	if fnItem.Name == source.NoStringID {
		return
	}
	flags := SymbolFlagImported
	if fnItem.Flags&ast.FnModifierPublic != 0 {
		flags |= SymbolFlagPublic
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       container,
	}
	span := fnNameSpan(fnItem)
	if symID, ok := fr.declareFunctionWithAttrs(container, fnItem, span, fnItem.FnKeywordSpan, flags, decl); ok {
		fr.appendItemSymbol(container, symID)
	}
}

func (fr *fileResolver) declareFunctionWithAttrs(itemID ast.ItemID, fnItem *ast.FnItem, span, keywordSpan source.Span, flags SymbolFlags, decl SymbolDecl) (SymbolID, bool) {
	attrs := fr.builder.Items.CollectAttrs(fnItem.AttrStart, fnItem.AttrCount)
	hasOverload := false
	hasOverride := false
	for _, attr := range attrs {
		name := fr.builder.StringsInterner.MustLookup(attr.Name)
		switch name {
		case "overload":
			hasOverload = true
		case "override":
			hasOverride = true
		}
	}

	scope := fr.resolver.CurrentScope()
	existing := fr.resolver.lookupInScope(scope, fnItem.Name, SymbolFunction.Mask())
	existingSymbols := make([]*Symbol, 0, len(existing))
	for _, id := range existing {
		existingSymbols = append(existingSymbols, fr.result.Table.Symbols.Get(id))
	}
	newSig := buildFunctionSignature(fr.builder, fnItem)

	if hasOverload && hasOverride {
		fr.reportInvalidOverride(fnItem.Name, span, "cannot combine @overload and @override", existing)
		return NoSymbolID, false
	}

	if hasOverride && len(existing) == 0 {
		fr.reportInvalidOverride(fnItem.Name, span, "@override requires an existing declaration", nil)
		return NoSymbolID, false
	}

	if len(existing) > 0 {
		switch {
		case hasOverload:
			if !signatureDiffersFromAll(newSig, existingSymbols) {
				fr.reportInvalidOverride(fnItem.Name, span, "@overload duplicates existing signature; use @override", existing)
				return NoSymbolID, false
			}
		case hasOverride:
			match := false
			for _, sym := range existingSymbols {
				if sym == nil {
					continue
				}
				if sym.Flags&SymbolFlagBuiltin != 0 {
					fr.reportInvalidOverride(fnItem.Name, span, "cannot override builtin function", existing)
					return NoSymbolID, false
				}
				if signaturesEqual(sym.Signature, newSig) {
					match = true
				}
			}
			if !match {
				fr.reportInvalidOverride(fnItem.Name, span, "@override requires matching signature", existing)
				return NoSymbolID, false
			}
		default:
			fr.reportMissingOverload(fnItem.Name, span, keywordSpan, existing, newSig)
			return NoSymbolID, false
		}
	}

	symID := fr.resolver.declareWithoutChecks(fnItem.Name, span, SymbolFunction, flags, decl, newSig)
	if !symID.IsValid() {
		return NoSymbolID, false
	}
	return symID, true
}
