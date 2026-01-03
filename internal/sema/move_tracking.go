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
