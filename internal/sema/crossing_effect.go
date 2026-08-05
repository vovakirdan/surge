package sema

import "surge/internal/symbols"

func (tc *typeChecker) markCurrentFunctionMayCross() {
	if tc == nil {
		return
	}
	fn := tc.currentFnSym()
	if !fn.IsValid() {
		return
	}
	if tc.directFunctionCrossing == nil {
		tc.directFunctionCrossing = make(map[symbols.SymbolID]struct{})
	}
	tc.directFunctionCrossing[fn] = struct{}{}
}

func (tc *typeChecker) recordFunctionCrossingCall(callee symbols.SymbolID) {
	if tc == nil || !callee.IsValid() {
		return
	}
	if sym := tc.symbolFromID(callee); sym == nil || sym.Kind != symbols.SymbolFunction {
		return
	}
	tc.recordFunctionCall(callee)
	caller := tc.currentFnSym()
	if !caller.IsValid() {
		return
	}
	if tc.functionCrossingEdges == nil {
		tc.functionCrossingEdges = make(map[symbols.SymbolID]map[symbols.SymbolID]struct{})
	}
	edges := tc.functionCrossingEdges[caller]
	if edges == nil {
		edges = make(map[symbols.SymbolID]struct{})
		tc.functionCrossingEdges[caller] = edges
	}
	edges[callee] = struct{}{}
}

func (tc *typeChecker) finalizeFunctionEffects() {
	if tc == nil || tc.result == nil {
		return
	}
	if tc.result.FunctionEffects == nil {
		tc.result.FunctionEffects = make(map[symbols.SymbolID]FunctionEffect)
	}
	for fn := range tc.directFunctionCrossing {
		tc.result.FunctionEffects[fn] = FunctionEffect{MayCross: true}
	}

	changed := true
	for changed {
		changed = false
		for caller, callees := range tc.functionCrossingEdges {
			if tc.result.FunctionEffects[caller].MayCross {
				continue
			}
			for callee := range callees {
				if tc.result.FunctionEffects[callee].MayCross {
					tc.result.FunctionEffects[caller] = FunctionEffect{MayCross: true}
					changed = true
					break
				}
			}
		}
	}
}
