package symbols

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
)

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
