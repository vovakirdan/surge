package sema

import (
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

func (tc *typeChecker) markBindingMoved(symID symbols.SymbolID, span source.Span) {
	if !symID.IsValid() {
		return
	}
	if tc.movedBindings == nil {
		tc.movedBindings = make(map[symbols.SymbolID]source.Span)
	}
	if _, exists := tc.movedBindings[symID]; !exists {
		tc.movedBindings[symID] = span
	}
}

func (tc *typeChecker) clearBindingMoved(symID symbols.SymbolID) {
	if !symID.IsValid() || tc.movedBindings == nil {
		return
	}
	delete(tc.movedBindings, symID)
}

func (tc *typeChecker) checkUseAfterMove(symID symbols.SymbolID, span source.Span) {
	if !symID.IsValid() || tc.movedBindings == nil {
		return
	}
	if _, moved := tc.movedBindings[symID]; !moved {
		return
	}
	name := "_"
	if sym := tc.symbolFromID(symID); sym != nil {
		if symName := tc.lookupName(sym.Name); symName != "" {
			name = symName
		}
	}
	if tc.isTaskType(tc.bindingType(symID)) {
		tc.report(diag.SemaUseAfterMove, span, "use of moved task '%s'; call %s.clone() to keep a handle", name, name)
		return
	}
	tc.report(diag.SemaUseAfterMove, span, "use of moved value '%s'", name)
}

func (tc *typeChecker) snapshotMovedBindings() map[symbols.SymbolID]source.Span {
	out := make(map[symbols.SymbolID]source.Span, len(tc.movedBindings))
	for key, value := range tc.movedBindings {
		out[key] = value
	}
	return out
}

func (tc *typeChecker) restoreMovedBindings(snapshot map[symbols.SymbolID]source.Span) {
	tc.movedBindings = make(map[symbols.SymbolID]source.Span, len(snapshot))
	for key, value := range snapshot {
		tc.movedBindings[key] = value
	}
}

func mergeMovedBindings(a, b map[symbols.SymbolID]source.Span) map[symbols.SymbolID]source.Span {
	out := make(map[symbols.SymbolID]source.Span, len(a)+len(b))
	for key, value := range a {
		out[key] = value
	}
	for key, value := range b {
		if _, exists := out[key]; !exists {
			out[key] = value
		}
	}
	return out
}
