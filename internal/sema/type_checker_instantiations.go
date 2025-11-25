package sema

import (
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) rememberFunctionInstantiation(symID symbols.SymbolID, args []types.TypeID) {
	if !symID.IsValid() || len(args) == 0 || tc.result == nil {
		return
	}
	if tc.fnInstantiationSeen == nil {
		tc.fnInstantiationSeen = make(map[string]struct{})
	}
	key := tc.instantiationKey(symID, args)
	if key == "" {
		return
	}
	if _, exists := tc.fnInstantiationSeen[key]; exists {
		return
	}
	tc.fnInstantiationSeen[key] = struct{}{}
	if tc.result.FunctionInstantiations == nil {
		tc.result.FunctionInstantiations = make(map[symbols.SymbolID][][]types.TypeID)
	}
	tc.result.FunctionInstantiations[symID] = append(tc.result.FunctionInstantiations[symID], append([]types.TypeID(nil), args...))
}
