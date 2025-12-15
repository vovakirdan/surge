package symbols

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

// findExistingSymbol ищет существующий символ в текущей области видимости.
// Используется для переиспользования символов при повторной обработке файла.
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

// declareLet объявляет let-переменную в текущей области видимости.
// Обрабатывает мутабельные и иммутабельные переменные, а также wildcard-имена.
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

// declareConstItem объявляет константу в текущей области видимости.
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

// declareFn объявляет функцию в текущей области видимости.
// Проверяет стиль именования и обрабатывает атрибуты функции.
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

// declareType объявляет тип в текущей области видимости.
// Проверяет атрибут @intrinsic для типов и валидирует структуру intrinsic-типов.
func (fr *fileResolver) declareType(itemID ast.ItemID, typeItem *ast.TypeItem) {
	if typeItem.Name == source.NoStringID {
		return
	}
	isPublic := typeItem.Visibility == ast.VisPublic
	hidden, hiddenSpan := fr.hasHiddenAttr(typeItem.AttrStart, typeItem.AttrCount)
	flags := fr.applyVisibilityFlags(0, isPublic, hidden, hiddenSpan, typeItem.Span)

	// Check for @intrinsic attribute on types
	if hasIntrinsic := fr.hasIntrinsicAttr(typeItem.AttrStart, typeItem.AttrCount); hasIntrinsic {
		if !fr.moduleAllowsIntrinsic() {
			span := preferSpan(typeItem.TypeKeywordSpan, typeItem.Span)
			fr.reportIntrinsicError(typeItem.Name, span, diag.SemaIntrinsicBadContext, "@intrinsic types must be declared in core or stdlib module")
			return
		}
		// Validate: @intrinsic type must be empty struct or have only __opaque field
		if !fr.isValidIntrinsicType(typeItem) {
			span := preferSpan(typeItem.TypeKeywordSpan, typeItem.Span)
			fr.reportIntrinsicError(typeItem.Name, span, diag.SemaIntrinsicHasBody, "@intrinsic type must be empty or have only '__opaque' field")
			return
		}
		flags |= SymbolFlagBuiltin
	}

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

// hasIntrinsicAttr проверяет, содержит ли список атрибутов @intrinsic.
func (fr *fileResolver) hasIntrinsicAttr(start ast.AttrID, count uint32) bool {
	if count == 0 || !start.IsValid() {
		return false
	}
	attrs := fr.builder.Items.CollectAttrs(start, count)
	for _, attr := range attrs {
		name, ok := fr.builder.StringsInterner.Lookup(attr.Name)
		if !ok {
			continue
		}
		if name == "intrinsic" {
			return true
		}
	}
	return false
}

// isValidIntrinsicType проверяет, является ли тип валидным для @intrinsic.
// Валидными являются: пустая структура или структура с единственным полем __opaque.
func (fr *fileResolver) isValidIntrinsicType(typeItem *ast.TypeItem) bool {
	if typeItem == nil {
		return false
	}
	// Only struct types can be @intrinsic
	if typeItem.Kind != ast.TypeDeclStruct {
		return false
	}
	structDecl := fr.builder.Items.TypeStruct(typeItem)
	if structDecl == nil {
		return false
	}
	// Empty struct is valid
	if structDecl.FieldsCount == 0 {
		return true
	}
	// Single field must be named __opaque
	if structDecl.FieldsCount == 1 {
		field := fr.builder.Items.StructField(structDecl.FieldsStart)
		if field != nil {
			fieldName, ok := fr.builder.StringsInterner.Lookup(field.Name)
			if ok && fieldName == "__opaque" {
				return true
			}
		}
	}
	return false
}

// declareContract объявляет контракт в текущей области видимости.
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

// declareTag объявляет тег в текущей области видимости.
// Проверяет стиль именования тегов (должны начинаться с большой буквы).
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

// declareExternFn объявляет внешнюю функцию из extern-блока.
// Обрабатывает методы с получателями и обычные функции.
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

// declareFunctionWithAttrs объявляет функцию с обработкой атрибутов.
// Поддерживает атрибуты @overload, @override, @intrinsic и @entrypoint.
// Выполняет проверку сигнатур и валидацию совместимости атрибутов.
func (fr *fileResolver) declareFunctionWithAttrs(fnItem *ast.FnItem, span, keywordSpan source.Span, flags SymbolFlags, decl SymbolDecl, receiverKey TypeKey) (SymbolID, bool) {
	attrs := fr.builder.Items.CollectAttrs(fnItem.AttrStart, fnItem.AttrCount)
	hasOverload := false
	hasOverride := false
	hasIntrinsic := false
	hasEntrypoint := false
	entrypointMode := EntrypointModeNone
	var entrypointAttr *ast.Attr
	for i := range attrs {
		attr := &attrs[i]
		name := fr.builder.StringsInterner.MustLookup(attr.Name)
		switch name {
		case "overload":
			hasOverload = true
		case "override":
			hasOverride = true
		case "intrinsic":
			hasIntrinsic = true
		case "entrypoint":
			hasEntrypoint = true
			entrypointAttr = attr
			entrypointMode = fr.parseEntrypointMode(attr, span)
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
			fr.reportIntrinsicError(fnItem.Name, span, diag.SemaIntrinsicBadContext, "@intrinsic functions must be declared in module core")
			return NoSymbolID, false
		}
		if fnItem.Body.IsValid() {
			fr.reportIntrinsicError(fnItem.Name, span, diag.SemaIntrinsicHasBody, "@intrinsic declarations cannot have a body")
			return NoSymbolID, false
		}
		flags |= SymbolFlagBuiltin
		existing = nil
		existingSymbols = nil
	}
	if hasEntrypoint && hasIntrinsic {
		msg := "@entrypoint cannot be combined with @intrinsic"
		if b := diag.ReportError(fr.resolver.reporter, diag.SemaEntrypointInvalidAttr, span, msg); b != nil {
			b.Emit()
		}
	}
	if hasEntrypoint && !fnItem.Body.IsValid() {
		msg := "@entrypoint function must have a body"
		if b := diag.ReportError(fr.resolver.reporter, diag.SemaEntrypointNoBody, span, msg); b != nil {
			b.Emit()
		}
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

	if hasEntrypoint {
		flags |= SymbolFlagEntrypoint
	}

	symID := fr.resolver.declareWithoutChecks(fnItem.Name, span, SymbolFunction, flags, decl, newSig)
	if !symID.IsValid() {
		return NoSymbolID, false
	}

	// Store entrypoint mode on the symbol
	if hasEntrypoint && entrypointAttr != nil {
		if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
			sym.EntrypointMode = entrypointMode
		}
	}

	return symID, true
}

// parseEntrypointMode извлекает режим из атрибута @entrypoint.
// Возвращает EntrypointModeNone, если аргумент отсутствует, или распарсенный режим.
// Сообщает об ошибках для невалидных/неизвестных режимов.
func (fr *fileResolver) parseEntrypointMode(attr *ast.Attr, _ source.Span) EntrypointMode {
	if attr == nil || len(attr.Args) == 0 {
		return EntrypointModeNone
	}
	argID := attr.Args[0]
	lit, ok := fr.builder.Exprs.Literal(argID)
	if !ok || lit == nil || lit.Kind != ast.ExprLitString {
		// Attribute argument must be a string literal
		if b := diag.ReportError(fr.resolver.reporter, diag.SemaEntrypointModeInvalid, attr.Span,
			"@entrypoint mode must be a string literal"); b != nil {
			b.Emit()
		}
		return EntrypointModeNone
	}
	modeStr := fr.builder.StringsInterner.MustLookup(lit.Value)
	// Remove surrounding quotes if present (literal includes quotes in some representations)
	modeStr = strings.Trim(modeStr, "\"")
	switch modeStr {
	case "argv":
		return EntrypointModeArgv
	case "stdin":
		return EntrypointModeStdin
	case "env":
		// Report future/unsupported error
		if b := diag.ReportError(fr.resolver.reporter, diag.FutEntrypointModeEnv, attr.Span,
			"@entrypoint(\"env\") mode is reserved for future use; use \"argv\" or \"stdin\""); b != nil {
			b.Emit()
		}
		return EntrypointModeEnv
	case "config":
		// Report future/unsupported error
		if b := diag.ReportError(fr.resolver.reporter, diag.FutEntrypointModeConfig, attr.Span,
			"@entrypoint(\"config\") mode is reserved for future use; use \"argv\" or \"stdin\""); b != nil {
			b.Emit()
		}
		return EntrypointModeConfig
	default:
		if b := diag.ReportError(fr.resolver.reporter, diag.SemaEntrypointModeInvalid, attr.Span,
			fmt.Sprintf("unknown @entrypoint mode %q; valid modes are 'argv', 'stdin'", modeStr)); b != nil {
			b.Emit()
		}
		return EntrypointModeNone
	}
}

// isProtectedSymbol проверяет, является ли символ защищенным.
// Защищенными считаются символы из защищенных модулей (core, stdlib) или встроенные импортированные символы.
func isProtectedSymbol(sym *Symbol) bool {
	if sym == nil {
		return false
	}
	if isProtectedModule(sym.ModulePath) {
		return true
	}
	return sym.Flags&SymbolFlagBuiltin != 0 && sym.Flags&SymbolFlagImported != 0
}
