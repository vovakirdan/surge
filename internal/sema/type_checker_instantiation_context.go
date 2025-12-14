package sema

import "surge/internal/symbols"

func (tc *typeChecker) pushFnSym(sym symbols.SymbolID) func() {
	tc.fnSymStack = append(tc.fnSymStack, sym)
	return func() {
		if len(tc.fnSymStack) > 0 {
			tc.fnSymStack = tc.fnSymStack[:len(tc.fnSymStack)-1]
		}
	}
}

func (tc *typeChecker) currentFnSym() symbols.SymbolID {
	if tc == nil || len(tc.fnSymStack) == 0 {
		return symbols.NoSymbolID
	}
	return tc.fnSymStack[len(tc.fnSymStack)-1]
}
