package symbols

import (
	"fmt"
	"unicode"

	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
)

// enforceFunctionNameStyle проверяет соответствие имени функции стилю именования.
// Функции должны начинаться с маленькой буквы.
func (fr *fileResolver) enforceFunctionNameStyle(name source.StringID, span source.Span) {
	fr.enforceNameStyle(name, span, diag.SemaFnNameStyle, unicode.ToLower, unicode.IsUpper, "lowercase function name")
}

// enforceTagNameStyle проверяет соответствие имени тега стилю именования.
// Теги должны начинаться с большой буквы.
func (fr *fileResolver) enforceTagNameStyle(name source.StringID, span source.Span) {
	fr.enforceNameStyle(name, span, diag.SemaTagNameStyle, unicode.ToUpper, unicode.IsLower, "capitalize tag name")
}

// enforceNameStyle проверяет соответствие имени указанному стилю именования.
// Выдает предупреждение и предлагает исправление, если имя не соответствует стилю.
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

// firstLetterIndex находит индекс первой буквы в массиве рун.
// Возвращает -1, если буква не найдена.
func firstLetterIndex(runes []rune) int {
	for i, r := range runes {
		if unicode.IsLetter(r) {
			return i
		}
	}
	return -1
}
