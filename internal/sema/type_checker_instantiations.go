package sema

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) rememberFunctionInstantiation(symID symbols.SymbolID, args []types.TypeID, site source.Span, note string) {
	if !symID.IsValid() || len(args) == 0 || tc.result == nil {
		return
	}

	if tc.insts != nil {
		caller := tc.currentFnSym()
		if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolTag {
			tc.insts.RecordTagInstantiation(symID, args, site, caller, note)
		} else {
			tc.insts.RecordFnInstantiation(symID, args, site, caller, note)
		}
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
