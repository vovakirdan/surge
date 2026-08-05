package sema

import (
	"fmt"
	"slices"

	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) recordFunctionCall(callee symbols.SymbolID) {
	if tc == nil || tc.result == nil || !callee.IsValid() {
		return
	}
	if sym := tc.symbolFromID(callee); sym == nil || (sym.Kind != symbols.SymbolFunction && sym.Kind != symbols.SymbolTag) {
		return
	}
	caller := tc.currentFnSym()
	if !caller.IsValid() {
		return
	}
	if tc.result.FunctionCallEdges == nil {
		tc.result.FunctionCallEdges = make(map[symbols.SymbolID]map[symbols.SymbolID]struct{})
	}
	callees := tc.result.FunctionCallEdges[caller]
	if callees == nil {
		callees = make(map[symbols.SymbolID]struct{})
		tc.result.FunctionCallEdges[caller] = callees
	}
	callees[callee] = struct{}{}
}

// MergeFunctionCallEdges remaps and merges the sema-resolved ordinary call
// graph without changing either result's shared type metadata.
func MergeFunctionCallEdges(dst, src *Result, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if dst == nil || src == nil {
		return
	}
	if dst.FunctionCallEdges == nil {
		dst.FunctionCallEdges = make(map[symbols.SymbolID]map[symbols.SymbolID]struct{})
	}
	for caller, sourceCallees := range src.FunctionCallEdges {
		mappedCaller := remapInstantiationSymbol(caller, mapping)
		if !mappedCaller.IsValid() {
			continue
		}
		callees := dst.FunctionCallEdges[mappedCaller]
		if callees == nil {
			callees = make(map[symbols.SymbolID]struct{}, len(sourceCallees))
			dst.FunctionCallEdges[mappedCaller] = callees
		}
		for callee := range sourceCallees {
			mappedCallee := remapInstantiationSymbol(callee, mapping)
			if mappedCallee.IsValid() {
				callees[mappedCallee] = struct{}{}
			}
		}
	}
}

// MergeInstantiationTemplateParams preserves exact TypeID descriptors while
// moving only their template symbol into the post-merge namespace.
func MergeInstantiationTemplateParams(dst, src *Result, mapping map[symbols.SymbolID]symbols.SymbolID) error {
	if dst == nil || src == nil {
		return nil
	}
	if dst.InstantiationTemplateParams == nil {
		dst.InstantiationTemplateParams = make(map[symbols.SymbolID][]types.TypeID)
	}
	for template, params := range src.InstantiationTemplateParams {
		mapped := remapInstantiationSymbol(template, mapping)
		if !mapped.IsValid() {
			continue
		}
		if existing := dst.InstantiationTemplateParams[mapped]; len(existing) > 0 && !slices.Equal(existing, params) {
			return fmt.Errorf("generic template %d has conflicting exact parameter descriptors after merge", mapped)
		}
		dst.InstantiationTemplateParams[mapped] = slices.Clone(params)
	}
	return nil
}

// AddInstantiationCallableSeeds installs root-policy ordinary callable seeds.
func AddInstantiationCallableSeeds(result *Result, seeds []symbols.SymbolID) {
	if result == nil {
		return
	}
	if result.InstantiationCallableSeeds == nil {
		result.InstantiationCallableSeeds = make(map[symbols.SymbolID]struct{}, len(seeds))
	}
	for _, seed := range seeds {
		if seed.IsValid() {
			result.InstantiationCallableSeeds[seed] = struct{}{}
		}
	}
}
