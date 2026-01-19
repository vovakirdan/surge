package lsp

import (
	"sort"

	"surge/internal/ast"
	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type methodEntry struct {
	name   string
	detail string
}

func memberCompletions(snapshot *diagnose.AnalysisSnapshot, af *diagnose.AnalysisFile, file *source.File, targetOffset uint32) []completionItem {
	if af == nil || file == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return nil
	}
	exprID, _ := findExprAtOffset(af.Builder, file.ID, targetOffset, false)
	recvType := exprTypeForCompletion(af, exprID)
	if recvType == types.NoTypeID {
		return nil
	}
	fields := structFieldCompletions(af, recvType)
	methods := methodCompletions(snapshot, af, recvType, false)
	return mergeCompletionItems(fields, methods)
}

func staticCompletions(snapshot *diagnose.AnalysisSnapshot, af *diagnose.AnalysisFile, file *source.File, targetOffset uint32) []completionItem {
	if af == nil || file == nil {
		return nil
	}
	exprID, expr := findExprAtOffset(af.Builder, file.ID, targetOffset, false)
	if expr == nil {
		return nil
	}
	if moduleSym := moduleSymbolForExpr(af, exprID); moduleSym != nil {
		modulePath := moduleSym.ModulePath
		if modulePath == "" && moduleSym.ImportName != source.NoStringID {
			modulePath = lookupName(af, moduleSym.ImportName)
		}
		if modulePath == "" {
			return nil
		}
		exports := lookupModuleExports(snapshot, modulePath)
		if exports == nil {
			return nil
		}
		return exportCompletions(af, exports, "2_")
	}
	if enumType := enumTypeForExpr(af, exprID); enumType != types.NoTypeID {
		return enumVariantCompletions(af, enumType)
	}
	if af.Sema == nil || af.Sema.TypeInterner == nil {
		return nil
	}
	recvType := typeForStaticExpr(af, exprID)
	if recvType == types.NoTypeID {
		return nil
	}
	return methodCompletions(snapshot, af, recvType, true)
}

func structFieldCompletions(af *diagnose.AnalysisFile, recvType types.TypeID) []completionItem {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return nil
	}
	fields := collectStructFields(af.Sema.TypeInterner, recvType)
	items := make([]completionItem, 0, len(fields))
	for _, field := range fields {
		name := lookupName(af, field.Name)
		if name == "" {
			continue
		}
		detail := ""
		if ty := field.Type; ty != types.NoTypeID {
			detail = types.Label(af.Sema.TypeInterner, ty)
		}
		items = append(items, completionItem{
			Label:    name,
			Kind:     completionItemKindField,
			Detail:   detail,
			SortText: "1_" + name,
		})
	}
	return items
}

func methodCompletions(snapshot *diagnose.AnalysisSnapshot, af *diagnose.AnalysisFile, recvType types.TypeID, staticOnly bool) []completionItem {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return nil
	}
	methods := collectMethods(snapshot, af, recvType, staticOnly)
	items := make([]completionItem, 0, len(methods))
	for _, method := range methods {
		items = append(items, completionItem{
			Label:    method.name,
			Kind:     completionItemKindMethod,
			Detail:   method.detail,
			SortText: "1_" + method.name,
		})
	}
	return items
}

func enumVariantCompletions(af *diagnose.AnalysisFile, enumType types.TypeID) []completionItem {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return nil
	}
	info, ok := af.Sema.TypeInterner.EnumInfo(enumType)
	if !ok || info == nil || len(info.Variants) == 0 {
		return nil
	}
	items := make([]completionItem, 0, len(info.Variants))
	for _, variant := range info.Variants {
		name := lookupName(af, variant.Name)
		if name == "" {
			continue
		}
		items = append(items, completionItem{
			Label:    name,
			Kind:     completionItemKindEnumMember,
			SortText: "1_" + name,
		})
	}
	return items
}

func collectMethods(snapshot *diagnose.AnalysisSnapshot, af *diagnose.AnalysisFile, recvType types.TypeID, staticOnly bool) []methodEntry {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return nil
	}
	recvKeys := typeKeyCandidates(af.Sema.TypeInterner, recvType)
	if len(recvKeys) == 0 {
		return nil
	}
	items := make([]methodEntry, 0)
	seen := make(map[string]struct{})

	addMethod := func(name string, sig *symbols.FunctionSignature) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		detail := ""
		if sig != nil {
			detail = formatSignatureLabel(name, sig, func(id source.StringID) string {
				return lookupName(af, id)
			})
		}
		items = append(items, methodEntry{name: name, detail: detail})
	}

	if af.Symbols != nil && af.Symbols.Table != nil {
		if data := af.Symbols.Table.Symbols.Data(); data != nil {
			for i := range data {
				sym := &data[i]
				if sym.Kind != symbols.SymbolFunction || sym.ReceiverKey == "" || sym.Signature == nil {
					continue
				}
				if staticOnly && sym.Signature.HasSelf {
					continue
				}
				if !staticOnly && !sym.Signature.HasSelf {
					continue
				}
				if !receiverKeyMatches(recvKeys, sym.ReceiverKey) {
					continue
				}
				name := lookupName(af, sym.Name)
				addMethod(name, sym.Signature)
			}
		}
	}
	if snapshot != nil && snapshot.ModuleExports != nil {
		for _, exp := range snapshot.ModuleExports {
			if exp == nil {
				continue
			}
			for name, list := range exp.Symbols {
				for i := range list {
					item := &list[i]
					if item.Kind != symbols.SymbolFunction || item.ReceiverKey == "" || item.Signature == nil {
						continue
					}
					if item.Flags&symbols.SymbolFlagPublic == 0 && item.Flags&symbols.SymbolFlagBuiltin == 0 {
						continue
					}
					if staticOnly && item.Signature.HasSelf {
						continue
					}
					if !staticOnly && !item.Signature.HasSelf {
						continue
					}
					if !receiverKeyMatches(recvKeys, item.ReceiverKey) {
						continue
					}
					addMethod(name, item.Signature)
				}
			}
		}
	}

	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	return items
}

func receiverKeyMatches(candidates []symbols.TypeKey, receiver symbols.TypeKey) bool {
	for _, cand := range candidates {
		if typeKeyMatchesWithGenerics(cand, receiver) {
			return true
		}
	}
	return false
}

func moduleSymbolForExpr(af *diagnose.AnalysisFile, exprID ast.ExprID) *symbols.Symbol {
	if af == nil || af.Builder == nil || af.Symbols == nil || af.Symbols.Table == nil {
		return nil
	}
	if af.Symbols.ExprSymbols != nil {
		if symID, ok := af.Symbols.ExprSymbols[exprID]; ok && symID.IsValid() {
			if sym := af.Symbols.Table.Symbols.Get(symID); sym != nil && sym.Kind == symbols.SymbolModule {
				return sym
			}
		}
	}
	ident, ok := af.Builder.Exprs.Ident(exprID)
	if !ok || ident == nil {
		return nil
	}
	symID := lookupSymbolInScopeChain(af.Symbols.Table, af.Symbols.FileScope, ident.Name)
	if symID.IsValid() {
		if sym := af.Symbols.Table.Symbols.Get(symID); sym != nil && sym.Kind == symbols.SymbolModule {
			return sym
		}
	}
	return nil
}

func enumTypeForExpr(af *diagnose.AnalysisFile, exprID ast.ExprID) types.TypeID {
	if af == nil || af.Builder == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return types.NoTypeID
	}
	ident, ok := af.Builder.Exprs.Ident(exprID)
	if !ok || ident == nil {
		return types.NoTypeID
	}
	if af.Symbols == nil || af.Symbols.Table == nil {
		return types.NoTypeID
	}
	symID := symbols.NoSymbolID
	if af.Symbols.ExprSymbols != nil {
		if resolved, ok := af.Symbols.ExprSymbols[exprID]; ok {
			symID = resolved
		}
	}
	if !symID.IsValid() {
		symID = lookupSymbolInScopeChain(af.Symbols.Table, af.Symbols.FileScope, ident.Name)
	}
	if !symID.IsValid() {
		return types.NoTypeID
	}
	sym := af.Symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.Kind != symbols.SymbolType || sym.Type == types.NoTypeID {
		return types.NoTypeID
	}
	tt, found := af.Sema.TypeInterner.Lookup(sym.Type)
	if !found || tt.Kind != types.KindEnum {
		return types.NoTypeID
	}
	return sym.Type
}

func typeForStaticExpr(af *diagnose.AnalysisFile, exprID ast.ExprID) types.TypeID {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return types.NoTypeID
	}
	expr := af.Builder.Exprs.Get(exprID)
	if expr == nil {
		return types.NoTypeID
	}
	if af.Symbols == nil || af.Symbols.Table == nil {
		return types.NoTypeID
	}
	if af.Symbols.ExprSymbols != nil {
		if symID, ok := af.Symbols.ExprSymbols[exprID]; ok && symID.IsValid() {
			if sym := af.Symbols.Table.Symbols.Get(symID); sym != nil && sym.Kind == symbols.SymbolType && sym.Type != types.NoTypeID {
				return sym.Type
			}
		}
	}
	if expr.Kind == ast.ExprIdent && af.Symbols != nil && af.Symbols.Table != nil {
		if ident, ok := af.Builder.Exprs.Ident(exprID); ok && ident != nil {
			symID := lookupSymbolInScopeChain(af.Symbols.Table, af.Symbols.FileScope, ident.Name)
			if symID != symbols.NoSymbolID {
				if sym := af.Symbols.Table.Symbols.Get(symID); sym != nil && sym.Kind == symbols.SymbolType && sym.Type != types.NoTypeID {
					return sym.Type
				}
			}
		}
	}
	if af.Sema.ExprTypes != nil {
		if ty := af.Sema.ExprTypes[exprID]; ty != types.NoTypeID {
			return ty
		}
	}
	return types.NoTypeID
}

func exprTypeForCompletion(af *diagnose.AnalysisFile, exprID ast.ExprID) types.TypeID {
	if af == nil || af.Sema == nil {
		return types.NoTypeID
	}
	if af.Sema.ExprTypes != nil {
		if ty := af.Sema.ExprTypes[exprID]; ty != types.NoTypeID {
			return ty
		}
	}
	if af.Symbols == nil || af.Symbols.Table == nil || af.Symbols.ExprSymbols == nil {
		return types.NoTypeID
	}
	if symID, ok := af.Symbols.ExprSymbols[exprID]; ok && symID.IsValid() {
		sym := af.Symbols.Table.Symbols.Get(symID)
		if sym != nil && sym.Type != types.NoTypeID {
			return sym.Type
		}
	}
	return types.NoTypeID
}

func collectStructFields(interner *types.Interner, recvType types.TypeID) []types.StructField {
	if interner == nil || recvType == types.NoTypeID {
		return nil
	}
	base := valueType(interner, recvType)
	info, ok := interner.StructInfo(base)
	if !ok || info == nil {
		return nil
	}
	return interner.StructFields(base)
}

func valueType(interner *types.Interner, typeID types.TypeID) types.TypeID {
	if interner == nil || typeID == types.NoTypeID {
		return typeID
	}
	seen := make(map[types.TypeID]struct{}, 8)
	for typeID != types.NoTypeID {
		if _, ok := seen[typeID]; ok {
			return typeID
		}
		seen[typeID] = struct{}{}
		tt, ok := interner.Lookup(typeID)
		if !ok {
			return typeID
		}
		switch tt.Kind {
		case types.KindOwn, types.KindReference, types.KindPointer:
			typeID = tt.Elem
		case types.KindAlias:
			target, ok := interner.AliasTarget(typeID)
			if !ok || target == types.NoTypeID {
				return typeID
			}
			typeID = target
		default:
			return typeID
		}
	}
	return typeID
}
