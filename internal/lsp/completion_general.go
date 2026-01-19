package lsp

import (
	"surge/internal/ast"
	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/types"
)

func typeCompletions(af *diagnose.AnalysisFile, file *source.File, offset uint32) []completionItem {
	entries := visibleSymbols(af, file, offset)
	items := make([]completionItem, 0, len(entries))
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if entry.sym == nil {
			continue
		}
		kind := completionKindForSymbol(af, entry.sym)
		if !isTypeCompletionKind(entry.sym, kind) {
			continue
		}
		label := lookupName(af, entry.sym.Name)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		items = append(items, completionItem{
			Label:    label,
			Kind:     kind,
			Detail:   symbolDetail(af, entry.sym),
			SortText: entry.sortKey,
		})
	}
	return items
}

func generalCompletions(af *diagnose.AnalysisFile, file *source.File, offset uint32) []completionItem {
	entries := visibleSymbols(af, file, offset)
	items := make([]completionItem, 0, len(entries))
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if entry.sym == nil {
			continue
		}
		label := lookupName(af, entry.sym.Name)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		items = append(items, completionItem{
			Label:    label,
			Kind:     completionKindForSymbol(af, entry.sym),
			Detail:   symbolDetail(af, entry.sym),
			SortText: entry.sortKey,
		})
	}
	if enumType := expectedEnumType(af, file, offset); enumType != types.NoTypeID {
		enumItems := enumVariantCompletions(af, enumType)
		items = append(items, enumItems...)
	}
	return items
}

func expectedEnumType(af *diagnose.AnalysisFile, file *source.File, offset uint32) types.TypeID {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return types.NoTypeID
	}
	exprID, _ := findExprAtOffset(af.Builder, file.ID, offset, false)
	if exprID == ast.NoExprID || af.Sema.ExprTypes == nil {
		return types.NoTypeID
	}
	ty := af.Sema.ExprTypes[exprID]
	if ty == types.NoTypeID {
		return types.NoTypeID
	}
	tt, ok := af.Sema.TypeInterner.Lookup(ty)
	if !ok || tt.Kind != types.KindEnum {
		return types.NoTypeID
	}
	return ty
}

func mergeCompletionItems(primary, secondary []completionItem) []completionItem {
	if len(primary) == 0 {
		return secondary
	}
	if len(secondary) == 0 {
		return primary
	}
	seen := make(map[string]struct{}, len(primary))
	out := make([]completionItem, 0, len(primary)+len(secondary))
	for _, item := range primary {
		out = append(out, item)
		seen[item.Label] = struct{}{}
	}
	for _, item := range secondary {
		if _, ok := seen[item.Label]; ok {
			continue
		}
		out = append(out, item)
	}
	return out
}
