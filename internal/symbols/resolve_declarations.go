package symbols

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/project"
	"surge/internal/source"
)

func (fr *fileResolver) findExistingSymbol(name source.StringID, kind SymbolKind, decl SymbolDecl) SymbolID {
	if !fr.reuseDecls || name == source.NoStringID || fr.result == nil || fr.result.Table == nil {
		return NoSymbolID
	}
	scopeID := fr.resolver.CurrentScope()
	scope := fr.result.Table.Scopes.Get(scopeID)
	if scope == nil {
		return NoSymbolID
	}
	candidates := scope.NameIndex[name]
	for _, id := range candidates {
		sym := fr.result.Table.Symbols.Get(id)
		if sym == nil || sym.Kind != kind {
			continue
		}
		if sym.Decl.ASTFile == decl.ASTFile && sym.Decl.Item == decl.Item && sym.Decl.Stmt == decl.Stmt && sym.Decl.Expr == decl.Expr {
			return id
		}
	}
	return NoSymbolID
}

func (fr *fileResolver) hasHiddenAttr(start ast.AttrID, count uint32) (bool, source.Span) {
	if count == 0 || !start.IsValid() {
		return false, source.Span{}
	}
	attrs := fr.builder.Items.CollectAttrs(start, count)
	for _, attr := range attrs {
		name, ok := fr.builder.StringsInterner.Lookup(attr.Name)
		if !ok {
			continue
		}
		if strings.EqualFold(name, "hidden") {
			return true, attr.Span
		}
	}
	return false, source.Span{}
}

func (fr *fileResolver) applyVisibilityFlags(base SymbolFlags, isPublic, hidden bool, hiddenSpan, itemSpan source.Span) SymbolFlags {
	flags := base
	if isPublic {
		flags |= SymbolFlagPublic
	}
	if hidden {
		flags &^= SymbolFlagPublic
		flags |= SymbolFlagFilePrivate
		if isPublic && fr.resolver.reporter != nil {
			msg := "@hidden makes the declaration file-private; remove 'pub' or '@hidden'"
			diagSpan := itemSpan
			if hiddenSpan != (source.Span{}) {
				if hiddenSpan.File == itemSpan.File {
					diagSpan = hiddenSpan.Cover(itemSpan)
				} else {
					diagSpan = hiddenSpan
				}
			}
			builder := diag.ReportWarning(fr.resolver.reporter, diag.SemaHiddenPublic, diagSpan, msg)
			if builder != nil {
				if hiddenSpan != (source.Span{}) {
					builder.WithFixSuggestion(fix.ReplaceSpan(
						"remove @hidden",
						hiddenSpan,
						"",
						"",
					))
				}
				builder.Emit()
			}
		}
	}
	return flags
}

func (fr *fileResolver) declareLet(itemID ast.ItemID, letItem *ast.LetItem) {
	if letItem.Name == source.NoStringID {
		return
	}
	if fr.isWildcard(letItem.Name) {
		if letItem.IsMut {
			fr.reportWildcardMut(preferSpan(letItem.MutSpan, letItem.Span))
		}
		return
	}
	isPublic := letItem.Visibility == ast.VisPublic
	hidden, hiddenSpan := fr.hasHiddenAttr(letItem.AttrStart, letItem.AttrCount)
	flags := fr.applyVisibilityFlags(0, isPublic, hidden, hiddenSpan, letItem.Span)
	if letItem.IsMut {
		flags |= SymbolFlagMutable
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(letItem.NameSpan, letItem.Span)
	if reused := fr.findExistingSymbol(letItem.Name, SymbolLet, decl); reused.IsValid() {
		fr.appendItemSymbol(itemID, reused)
	} else if symID, ok := fr.resolver.Declare(letItem.Name, span, SymbolLet, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
	}
	if fr.declareOnly {
		return
	}
	if letItem.Value.IsValid() {
		fr.walkExpr(letItem.Value)
	}
}

func (fr *fileResolver) declareConstItem(itemID ast.ItemID, constItem *ast.ConstItem) {
	if constItem == nil || constItem.Name == source.NoStringID {
		return
	}
	isPublic := constItem.Visibility == ast.VisPublic
	hidden, hiddenSpan := fr.hasHiddenAttr(constItem.AttrStart, constItem.AttrCount)
	flags := fr.applyVisibilityFlags(0, isPublic, hidden, hiddenSpan, constItem.Span)
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(constItem.NameSpan, constItem.Span)
	if reused := fr.findExistingSymbol(constItem.Name, SymbolConst, decl); reused.IsValid() {
		fr.appendItemSymbol(itemID, reused)
		return
	}
	if symID, ok := fr.resolver.Declare(constItem.Name, span, SymbolConst, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
		return
	}
}

func (fr *fileResolver) declareFn(itemID ast.ItemID, fnItem *ast.FnItem) {
	if fnItem.Name == source.NoStringID {
		return
	}
	isPublic := fnItem.Flags&ast.FnModifierPublic != 0
	hidden, hiddenSpan := fr.hasHiddenAttr(fnItem.AttrStart, fnItem.AttrCount)
	flags := fr.applyVisibilityFlags(0, isPublic, hidden, hiddenSpan, fnItem.Span)
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	nameSpan := fnNameSpan(fnItem)
	fr.enforceFunctionNameStyle(fnItem.Name, nameSpan)
	if reused := fr.findExistingSymbol(fnItem.Name, SymbolFunction, decl); reused.IsValid() {
		fr.appendItemSymbol(itemID, reused)
	} else if symID, ok := fr.declareFunctionWithAttrs(fnItem, nameSpan, fnItem.FnKeywordSpan, flags, decl, ""); ok {
		if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
			sym.TypeParams = append([]source.StringID(nil), fnItem.Generics...)
			sym.TypeParamSpan = fnItem.GenericsSpan
		}
		fr.appendItemSymbol(itemID, symID)
	}
	if fr.declareOnly {
		return
	}
	fr.walkFn(ScopeOwner{
		Kind:       ScopeOwnerItem,
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}, fnItem)
}

func (fr *fileResolver) declareType(itemID ast.ItemID, typeItem *ast.TypeItem) {
	if typeItem.Name == source.NoStringID {
		return
	}
	isPublic := typeItem.Visibility == ast.VisPublic
	hidden, hiddenSpan := fr.hasHiddenAttr(typeItem.AttrStart, typeItem.AttrCount)
	flags := fr.applyVisibilityFlags(0, isPublic, hidden, hiddenSpan, typeItem.Span)
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(typeItem.TypeKeywordSpan, typeItem.Span)
	if reused := fr.findExistingSymbol(typeItem.Name, SymbolType, decl); reused.IsValid() {
		fr.appendItemSymbol(itemID, reused)
	} else if symID, ok := fr.resolver.Declare(typeItem.Name, span, SymbolType, flags, decl); ok {
		if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
			sym.TypeParams = append([]source.StringID(nil), typeItem.Generics...)
			sym.TypeParamSpan = typeItem.GenericsSpan
		}
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareContract(itemID ast.ItemID, contractItem *ast.ContractDecl) {
	if contractItem == nil || contractItem.Name == source.NoStringID {
		return
	}
	isPublic := contractItem.Visibility == ast.VisPublic
	hidden, hiddenSpan := fr.hasHiddenAttr(contractItem.AttrStart, contractItem.AttrCount)
	flags := fr.applyVisibilityFlags(0, isPublic, hidden, hiddenSpan, contractItem.Span)
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(contractItem.NameSpan, preferSpan(contractItem.ContractKeywordSpan, contractItem.Span))
	if reused := fr.findExistingSymbol(contractItem.Name, SymbolContract, decl); reused.IsValid() {
		fr.appendItemSymbol(itemID, reused)
	} else if symID, ok := fr.resolver.Declare(contractItem.Name, span, SymbolContract, flags, decl); ok {
		if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
			sym.TypeParams = append([]source.StringID(nil), contractItem.Generics...)
			sym.TypeParamSpan = contractItem.GenericsSpan
		}
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareTag(itemID ast.ItemID, tagItem *ast.TagItem) {
	if tagItem.Name == source.NoStringID {
		return
	}
	isPublic := tagItem.Visibility == ast.VisPublic
	hidden, hiddenSpan := fr.hasHiddenAttr(tagItem.AttrStart, tagItem.AttrCount)
	flags := fr.applyVisibilityFlags(0, isPublic, hidden, hiddenSpan, tagItem.Span)
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	nameSpan := preferSpan(tagItem.NameSpan, preferSpan(tagItem.TagKeywordSpan, tagItem.Span))
	fr.enforceTagNameStyle(tagItem.Name, nameSpan)
	if reused := fr.findExistingSymbol(tagItem.Name, SymbolTag, decl); reused.IsValid() {
		fr.appendItemSymbol(itemID, reused)
	} else if symID, ok := fr.resolver.Declare(tagItem.Name, nameSpan, SymbolTag, flags, decl); ok {
		if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
			sym.TypeParams = append([]source.StringID(nil), tagItem.Generics...)
			sym.TypeParamSpan = tagItem.GenericsSpan
		}
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareImport(itemID ast.ItemID, importItem *ast.ImportItem, itemSpan source.Span) {
	modulePath := fr.resolveImportModulePath(importItem.Module, itemSpan)
	hasItems := importItem.HasOne || len(importItem.Group) > 0

	if !hasItems {
		if modulePath != "" {
			if !fr.trackModuleImport(modulePath, itemSpan) {
				return
			}
		}
		if alias := fr.moduleAliasForImport(importItem, true); alias != source.NoStringID {
			fr.declareModuleAlias(itemID, alias, modulePath, itemSpan)
		}
	}

	if importItem.HasOne {
		name := importItem.One.Alias
		if name == source.NoStringID {
			name = importItem.One.Name
		}
		fr.declareImportName(itemID, name, importItem.One.Name, importItem.Module, modulePath, itemSpan)
	}
	for _, pair := range importItem.Group {
		name := pair.Alias
		if name == source.NoStringID {
			name = pair.Name
		}
		fr.declareImportName(itemID, name, pair.Name, importItem.Module, modulePath, itemSpan)
	}
}

func (fr *fileResolver) declareModuleAlias(itemID ast.ItemID, alias source.StringID, modulePath string, span source.Span) {
	if alias == source.NoStringID {
		return
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	if symID, ok := fr.resolver.Declare(alias, span, SymbolModule, SymbolFlagImported, decl); ok {
		if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
			sym.ModulePath = modulePath
		}
		if fr.aliasModulePaths != nil {
			fr.aliasModulePaths[alias] = modulePath
		}
		if exports := fr.moduleExports[modulePath]; exports != nil && fr.aliasExports != nil {
			fr.aliasExports[alias] = exports
		}
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareImportName(itemID ast.ItemID, name, original source.StringID, module []source.StringID, modulePath string, span source.Span) {
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
			sym.ModulePath = modulePath
			sym.ImportName = original
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

func (fr *fileResolver) declareExternFn(container ast.ItemID, member ast.ExternMemberID, receiverKey TypeKey, fnItem *ast.FnItem) {
	if fnItem.Name == source.NoStringID {
		return
	}
	isPublic := fnItem.Flags&ast.FnModifierPublic != 0
	hidden, hiddenSpan := fr.hasHiddenAttr(fnItem.AttrStart, fnItem.AttrCount)
	flags := fr.applyVisibilityFlags(SymbolFlagImported, isPublic, hidden, hiddenSpan, fnItem.Span)
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       container,
	}
	span := fnNameSpan(fnItem)
	if reused := fr.findExistingSymbol(fnItem.Name, SymbolFunction, decl); reused.IsValid() {
		if member.IsValid() {
			fr.appendExternSymbol(member, reused)
		}
		fr.appendItemSymbol(container, reused)
		return
	}
	if symID, ok := fr.declareFunctionWithAttrs(fnItem, span, fnItem.FnKeywordSpan, flags, decl, receiverKey); ok {
		if block, _ := fr.builder.Items.Extern(container); block != nil {
			if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
				sym.Receiver = block.Target
				sym.ReceiverKey = receiverKey
				sym.Flags |= SymbolFlagMethod
			}
		}
		if member.IsValid() {
			fr.appendExternSymbol(member, symID)
		}
		if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
			sym.TypeParams = append([]source.StringID(nil), fnItem.Generics...)
			sym.TypeParamSpan = fnItem.GenericsSpan
		}
		fr.appendItemSymbol(container, symID)
	}
}

func (fr *fileResolver) declareFunctionWithAttrs(fnItem *ast.FnItem, span, keywordSpan source.Span, flags SymbolFlags, decl SymbolDecl, receiverKey TypeKey) (SymbolID, bool) {
	attrs := fr.builder.Items.CollectAttrs(fnItem.AttrStart, fnItem.AttrCount)
	hasOverload := false
	hasOverride := false
	hasIntrinsic := false
	for _, attr := range attrs {
		name := fr.builder.StringsInterner.MustLookup(attr.Name)
		switch name {
		case "overload":
			hasOverload = true
		case "override":
			hasOverride = true
		case "intrinsic":
			hasIntrinsic = true
		}
	}

	scope := fr.resolver.CurrentScope()
	existing := fr.resolver.lookupInScope(scope, fnItem.Name, SymbolFunction.Mask())
	if len(existing) > 0 {
		filtered := make([]SymbolID, 0, len(existing))
		for _, id := range existing {
			sym := fr.result.Table.Symbols.Get(id)
			if sym == nil {
				continue
			}
			if sym.ModulePath == fr.modulePath && sym.Decl.ASTFile == 0 {
				continue
			}
			if receiverKey != "" {
				if sym.ReceiverKey != receiverKey {
					continue
				}
			} else if sym.ReceiverKey != "" || sym.Flags&SymbolFlagMethod != 0 {
				continue
			}
			filtered = append(filtered, id)
		}
		existing = filtered
	}
	existingSymbols := make([]*Symbol, 0, len(existing))
	for _, id := range existing {
		existingSymbols = append(existingSymbols, fr.result.Table.Symbols.Get(id))
	}
	newSig := buildFunctionSignature(fr.builder, fnItem)
	protectedMatch := false
	hasPublicAncestor := false
	for _, sym := range existingSymbols {
		if sym == nil {
			continue
		}
		if sym.Flags&SymbolFlagPublic != 0 {
			hasPublicAncestor = true
		}
		if isProtectedSymbol(sym) && signaturesEqual(sym.Signature, newSig) {
			protectedMatch = true
		}
	}

	if hasOverload && hasOverride {
		fr.reportInvalidOverride(fnItem.Name, span, "cannot combine @overload and @override", existing)
		return NoSymbolID, false
	}

	if hasOverride && len(existing) == 0 {
		fr.reportInvalidOverride(fnItem.Name, span, "@override requires an existing declaration", nil)
		return NoSymbolID, false
	}

	if hasIntrinsic {
		if hasOverride {
			fr.reportIntrinsicError(fnItem.Name, span, diag.SemaIntrinsicBadContext, "@intrinsic cannot be combined with @override")
			return NoSymbolID, false
		}
		if !fr.moduleAllowsIntrinsic() {
			fr.reportIntrinsicError(fnItem.Name, span, diag.SemaIntrinsicBadContext, "@intrinsic functions must be declared in module core/intrinsics")
			return NoSymbolID, false
		}
		if fnItem.Body.IsValid() {
			fr.reportIntrinsicError(fnItem.Name, span, diag.SemaIntrinsicHasBody, "@intrinsic declarations cannot have a body")
			return NoSymbolID, false
		}
		if !fr.intrinsicNameAllowed(fnItem.Name) {
			msg := fmt.Sprintf("unknown intrinsic; allowed names: %s", intrinsicAllowedNamesDisplay)
			fr.reportIntrinsicError(fnItem.Name, span, diag.SemaIntrinsicBadName, msg)
			return NoSymbolID, false
		}
		flags |= SymbolFlagBuiltin
		existing = nil
		existingSymbols = nil
	}

	if hasOverride && len(existing) > 0 && hasPublicAncestor && flags&SymbolFlagPublic == 0 {
		fr.reportInvalidOverride(fnItem.Name, span, "@override cannot reduce visibility; add 'pub'", existing)
		return NoSymbolID, false
	}

	if len(existing) > 0 {
		if protectedMatch {
			fr.reportInvalidOverride(fnItem.Name, span, "function already defined in core/stdlib (overrides are forbidden)", existing)
			return NoSymbolID, false
		}
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

func (fr *fileResolver) enforceFunctionNameStyle(name source.StringID, span source.Span) {
	fr.enforceNameStyle(name, span, diag.SemaFnNameStyle, unicode.ToLower, unicode.IsUpper, "lowercase function name")
}

func isProtectedSymbol(sym *Symbol) bool {
	if sym == nil {
		return false
	}
	if isProtectedModule(sym.ModulePath) {
		return true
	}
	return sym.Flags&SymbolFlagBuiltin != 0 && sym.Flags&SymbolFlagImported != 0
}

func (fr *fileResolver) enforceTagNameStyle(name source.StringID, span source.Span) {
	fr.enforceNameStyle(name, span, diag.SemaTagNameStyle, unicode.ToUpper, unicode.IsLower, "capitalize tag name")
}

func (fr *fileResolver) enforceNameStyle(name source.StringID, span source.Span, code diag.Code, convert func(rune) rune, trigger func(rune) bool, fixTitle string) {
	if name == source.NoStringID || fr.resolver == nil || fr.resolver.reporter == nil || fr.builder == nil {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	runes := []rune(nameStr)
	idx := firstLetterIndex(runes)
	if idx == -1 {
		return
	}
	r := runes[idx]
	if !trigger(r) {
		return
	}
	original := nameStr
	runes[idx] = convert(r)
	newName := string(runes)
	msg := fmt.Sprintf("consider renaming '%s' to '%s' to follow naming conventions", original, newName)
	builder := diag.ReportWarning(fr.resolver.reporter, code, span, msg)
	if builder == nil {
		return
	}
	fixID := fix.MakeFixID(code, span)
	builder.WithFixSuggestion(fix.ReplaceSpan(
		fixTitle,
		span,
		newName,
		original,
		fix.WithID(fixID),
		fix.WithKind(diag.FixKindRefactor),
		fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
	))
	builder.Emit()
}

func firstLetterIndex(runes []rune) int {
	for i, r := range runes {
		if unicode.IsLetter(r) {
			return i
		}
	}
	return -1
}

func (fr *fileResolver) trackModuleImport(modulePath string, span source.Span) bool {
	if modulePath == "" {
		return true
	}
	if prev, ok := fr.moduleImports[modulePath]; ok {
		fr.reportDuplicateModuleImport(modulePath, span, prev)
		return false
	}
	fr.moduleImports[modulePath] = span
	return true
}

func (fr *fileResolver) reportDuplicateModuleImport(modulePath string, span, prev source.Span) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	msg := fmt.Sprintf("module %q already imported", modulePath)
	builder := diag.ReportError(fr.resolver.reporter, diag.SemaDuplicateSymbol, span, msg)
	if builder == nil {
		return
	}
	if prev != (source.Span{}) {
		builder.WithNote(prev, "previous import here")
	}
	builder.Emit()
}

func (fr *fileResolver) moduleAliasForImport(importItem *ast.ImportItem, allowDefault bool) source.StringID {
	if importItem == nil {
		return source.NoStringID
	}
	if importItem.ModuleAlias != source.NoStringID {
		return importItem.ModuleAlias
	}
	if !allowDefault {
		return source.NoStringID
	}
	for i := len(importItem.Module) - 1; i >= 0; i-- {
		seg := importItem.Module[i]
		segStr := fr.lookupString(seg)
		if segStr == "" || segStr == "." || segStr == ".." {
			continue
		}
		return seg
	}
	return source.NoStringID
}

func (fr *fileResolver) resolveImportModulePath(module []source.StringID, span source.Span) string {
	segs := fr.moduleSegmentsToStrings(module)
	if len(segs) == 0 {
		return ""
	}
	segs = fr.applyNoStdImportRules(segs, span)
	base := fr.baseDir
	if base == "" && fr.filePath != "" {
		base = filepath.Dir(fr.filePath)
	}
	if norm, err := project.ResolveImportPath(fr.modulePath, base, segs); err == nil {
		return norm
	}
	joined := strings.Join(segs, "/")
	if norm, err := project.NormalizeModulePath(joined); err == nil {
		return norm
	}
	return joined
}

func (fr *fileResolver) moduleSegmentsToStrings(module []source.StringID) []string {
	if len(module) == 0 || fr.builder == nil || fr.builder.StringsInterner == nil {
		return nil
	}
	out := make([]string, 0, len(module))
	for _, seg := range module {
		out = append(out, fr.lookupString(seg))
	}
	return out
}

func (fr *fileResolver) lookupString(id source.StringID) string {
	if id == source.NoStringID || fr.builder == nil || fr.builder.StringsInterner == nil {
		return ""
	}
	return fr.builder.StringsInterner.MustLookup(id)
}

func (fr *fileResolver) applyNoStdImportRules(segs []string, span source.Span) []string {
	if !fr.noStd || len(segs) == 0 || segs[0] != "stdlib" {
		return segs
	}
	replacement := append([]string{"core"}, segs[1:]...)
	if fr.resolver != nil && fr.resolver.reporter != nil {
		corePath := strings.Join(replacement, "/")
		msg := fmt.Sprintf("stdlib is not available in no_std modules; import %q instead", corePath)
		if b := diag.ReportError(fr.resolver.reporter, diag.SemaNoStdlib, span, msg); b != nil {
			b.Emit()
		}
	}
	return replacement
}
