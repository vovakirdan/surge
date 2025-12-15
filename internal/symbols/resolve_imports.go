package symbols

import (
	"fmt"
	"path/filepath"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/source"
)

// declareImport обрабатывает объявление импорта модуля.
// Поддерживает импорт отдельных символов, групп символов и импорт всех символов (import *).
func (fr *fileResolver) declareImport(itemID ast.ItemID, importItem *ast.ImportItem, itemSpan source.Span) {
	modulePath := fr.resolveImportModulePath(importItem.Module, itemSpan)
	hasItems := importItem.HasOne || len(importItem.Group) > 0 || importItem.ImportAll

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
	if importItem.ImportAll {
		fr.declareImportAll(itemID, importItem.Module, modulePath, itemSpan)
	}
}

// declareModuleAlias объявляет алиас модуля в текущей области видимости.
// Алиас позволяет обращаться к модулю по короткому имени вместо полного пути.
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

// declareImportName объявляет импортируемый символ с указанным именем.
// Поддерживает алиасы для импортируемых символов.
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

// declareImportAll импортирует все публичные символы из указанного модуля.
// Символы с атрибутом @hidden уже отфильтрованы в CollectExports.
func (fr *fileResolver) declareImportAll(itemID ast.ItemID, module []source.StringID, modulePath string, span source.Span) {
	if modulePath == "" {
		return
	}

	// Получаем экспорты модуля
	exports := fr.moduleExports[modulePath]
	if exports == nil {
		return
	}

	// Импортируем все публичные символы
	// @hidden символы уже отфильтрованы в CollectExports
	for name := range exports.Symbols {
		// Импортируем символ
		nameID := fr.builder.StringsInterner.Intern(name)
		fr.declareImportName(itemID, nameID, nameID, module, modulePath, span)
	}
}

// trackModuleImport отслеживает импорт модуля и проверяет на дубликаты.
// Возвращает false, если модуль уже был импортирован ранее.
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

// reportDuplicateModuleImport сообщает об ошибке дублирующегося импорта модуля.
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

// moduleAliasForImport определяет алиас модуля для импорта.
// Если алиас явно не указан и allowDefault=true, используется последний сегмент пути модуля.
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

// resolveImportModulePath разрешает путь импортируемого модуля.
// Применяет правила no_std и нормализует путь модуля.
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

// moduleSegmentsToStrings конвертирует сегменты модуля из StringID в строки.
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

// lookupString получает строку по её StringID из интернера строк.
func (fr *fileResolver) lookupString(id source.StringID) string {
	if id == source.NoStringID || fr.builder == nil || fr.builder.StringsInterner == nil {
		return ""
	}
	return fr.builder.StringsInterner.MustLookup(id)
}

// applyNoStdImportRules применяет правила импорта для модулей с флагом no_std.
// Заменяет импорты из stdlib на core, если модуль работает в режиме no_std.
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
