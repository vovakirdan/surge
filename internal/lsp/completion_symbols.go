package lsp

import (
	"fmt"
	"sort"
	"strings"

	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type symbolEntry struct {
	sym     *symbols.Symbol
	sortKey string
}

func visibleSymbols(af *diagnose.AnalysisFile, file *source.File, offset uint32) []symbolEntry {
	if af == nil || af.Symbols == nil || af.Symbols.Table == nil {
		return nil
	}
	scope := scopeAtOffset(af.Symbols.Table.Scopes, af.Symbols.FileScope, file.ID, offset)
	if !scope.IsValid() {
		scope = af.Symbols.FileScope
	}
	entries := make([]symbolEntry, 0)
	seen := make(map[string]symbolEntry)
	for scope.IsValid() {
		scopeData := af.Symbols.Table.Scopes.Get(scope)
		if scopeData == nil {
			break
		}
		for _, symID := range scopeData.Symbols {
			sym := af.Symbols.Table.Symbols.Get(symID)
			if sym == nil {
				continue
			}
			name := lookupName(af, sym.Name)
			if name == "" {
				continue
			}
			priority := symbolPriority(scopeData.Kind, sym)
			sortKey := sortKeyForCompletion(priority, name)
			if existing, ok := seen[name]; ok {
				if existing.sortKey <= sortKey {
					continue
				}
			}
			entry := symbolEntry{sym: sym, sortKey: sortKey}
			seen[name] = entry
		}
		scope = scopeData.Parent
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entries = append(entries, seen[name])
	}
	return entries
}

func symbolPriority(scopeKind symbols.ScopeKind, sym *symbols.Symbol) int {
	if sym == nil {
		return 4
	}
	if sym.Flags&symbols.SymbolFlagBuiltin != 0 {
		return 4
	}
	if sym.Flags&symbols.SymbolFlagImported != 0 {
		return 3
	}
	if scopeKind == symbols.ScopeFunction || scopeKind == symbols.ScopeBlock {
		return 1
	}
	return 2
}

func sortKeyForCompletion(priority int, name string) string {
	if priority < 1 {
		priority = 1
	}
	if priority > 9 {
		priority = 9
	}
	return fmt.Sprintf("%d_%s", priority, name)
}

func completionKindForSymbol(af *diagnose.AnalysisFile, sym *symbols.Symbol) int {
	if sym == nil {
		return completionItemKindText
	}
	switch sym.Kind {
	case symbols.SymbolFunction:
		if sym.ReceiverKey != "" {
			return completionItemKindMethod
		}
		return completionItemKindFunction
	case symbols.SymbolTag:
		return completionItemKindConstructor
	case symbols.SymbolLet, symbols.SymbolParam:
		return completionItemKindVariable
	case symbols.SymbolConst:
		return completionItemKindConstant
	case symbols.SymbolModule:
		return completionItemKindModule
	case symbols.SymbolContract:
		return completionItemKindInterface
	case symbols.SymbolType:
		return completionKindForType(af, sym.Type)
	default:
		return completionItemKindText
	}
}

func completionKindForExport(af *diagnose.AnalysisFile, sym *symbols.ExportedSymbol) int {
	if sym == nil {
		return completionItemKindText
	}
	switch sym.Kind {
	case symbols.SymbolFunction:
		if sym.ReceiverKey != "" {
			return completionItemKindMethod
		}
		return completionItemKindFunction
	case symbols.SymbolTag:
		return completionItemKindConstructor
	case symbols.SymbolLet:
		return completionItemKindVariable
	case symbols.SymbolConst:
		return completionItemKindConstant
	case symbols.SymbolType:
		return completionKindForType(af, sym.Type)
	case symbols.SymbolContract:
		return completionItemKindInterface
	default:
		return completionItemKindText
	}
}

func completionKindForType(af *diagnose.AnalysisFile, typeID types.TypeID) int {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil || typeID == types.NoTypeID {
		return completionItemKindClass
	}
	tt, ok := af.Sema.TypeInterner.Lookup(typeID)
	if !ok {
		return completionItemKindClass
	}
	switch tt.Kind {
	case types.KindEnum:
		return completionItemKindEnum
	case types.KindStruct:
		return completionItemKindStruct
	case types.KindUnion:
		return completionItemKindClass
	default:
		return completionItemKindClass
	}
}

func isTypeCompletionKind(sym *symbols.Symbol, kind int) bool {
	if sym == nil {
		return false
	}
	switch sym.Kind {
	case symbols.SymbolType, symbols.SymbolContract, symbols.SymbolTag:
		return true
	default:
		return kind == completionItemKindTypeParam
	}
}

func symbolDetail(af *diagnose.AnalysisFile, sym *symbols.Symbol) string {
	if sym == nil {
		return ""
	}
	if sym.Kind == symbols.SymbolFunction || sym.Kind == symbols.SymbolTag {
		return formatFunctionSignature(af, sym, lookupName(af, sym.Name))
	}
	if sym.Kind == symbols.SymbolType || sym.Kind == symbols.SymbolContract {
		return sym.Kind.String()
	}
	if sym.Type != types.NoTypeID && af.Sema != nil && af.Sema.TypeInterner != nil {
		return types.Label(af.Sema.TypeInterner, sym.Type)
	}
	return ""
}

func formatSignatureLabel(name string, sig *symbols.FunctionSignature, lookup func(source.StringID) string) string {
	if sig == nil {
		return ""
	}
	params := make([]string, 0, len(sig.Params))
	for i, param := range sig.Params {
		paramLabel := string(param)
		if lookup != nil && i < len(sig.ParamNames) {
			if pname := lookup(sig.ParamNames[i]); pname != "" {
				paramLabel = pname + ": " + paramLabel
			}
		}
		if i < len(sig.Variadic) && sig.Variadic[i] {
			paramLabel = "[" + paramLabel + "]"
		}
		params = append(params, paramLabel)
	}
	out := "fn " + name + "(" + strings.Join(params, ", ") + ")"
	if res := string(sig.Result); res != "" {
		out += " -> " + res
	}
	return out
}
