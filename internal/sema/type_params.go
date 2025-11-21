package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// pushTypeParams installs generic parameters into the current environment and records their bounds.
func (tc *typeChecker) pushTypeParams(owner symbols.SymbolID, names []source.StringID, bindings []types.TypeID) bool {
	if len(names) == 0 || tc.types == nil {
		return false
	}
	if len(bindings) > 0 && len(bindings) != len(names) {
		return false
	}
	var ownerBounds map[source.StringID][]symbols.BoundInstance
	if owner.IsValid() {
		if sym := tc.symbolFromID(owner); sym != nil && len(sym.TypeParamSymbols) > 0 {
			ownerBounds = make(map[source.StringID][]symbols.BoundInstance, len(sym.TypeParamSymbols))
			for _, tp := range sym.TypeParamSymbols {
				ownerBounds[tp.Name] = tp.Bounds
			}
		}
	}
	scope := make(map[source.StringID]types.TypeID, len(names))
	tc.typeParamMarks = append(tc.typeParamMarks, len(tc.typeParamStack))
	for i, name := range names {
		var id types.TypeID
		if len(bindings) > 0 {
			id = bindings[i]
		} else {
			ui32, err := safecast.Conv[uint32](i)
			if err != nil {
				panic(fmt.Errorf("type param index overflow: %w", err))
			}
			id = tc.types.RegisterTypeParam(name, uint32(owner), ui32)
			tc.typeParamNames[id] = name
		}
		scope[name] = id
		tc.typeParamStack = append(tc.typeParamStack, id)
		if ownerBounds != nil {
			if bounds := ownerBounds[name]; len(bounds) > 0 {
				tc.typeParamBounds[id] = bounds
			}
		}
	}
	tc.typeParams = append(tc.typeParams, scope)
	tc.typeParamEnv = append(tc.typeParamEnv, tc.nextParamEnv)
	tc.nextParamEnv++
	return true
}

// applyTypeParamBounds applies already-resolved bounds from the owner symbol onto the current env ids.
func (tc *typeChecker) applyTypeParamBounds(owner symbols.SymbolID) {
	if !owner.IsValid() {
		return
	}
	sym := tc.symbolFromID(owner)
	if sym == nil || len(sym.TypeParamSymbols) == 0 {
		return
	}
	for _, tp := range sym.TypeParamSymbols {
		if id := tc.lookupTypeParam(tp.Name); id != types.NoTypeID {
			tc.typeParamBounds[id] = tp.Bounds
		}
	}
}

func (tc *typeChecker) popTypeParams() {
	if len(tc.typeParams) == 0 {
		return
	}
	tc.typeParams = tc.typeParams[:len(tc.typeParams)-1]
	if len(tc.typeParamEnv) > 0 {
		tc.typeParamEnv = tc.typeParamEnv[:len(tc.typeParamEnv)-1]
	}
	if len(tc.typeParamMarks) > 0 {
		start := tc.typeParamMarks[len(tc.typeParamMarks)-1]
		tc.typeParamMarks = tc.typeParamMarks[:len(tc.typeParamMarks)-1]
		if start >= 0 && start <= len(tc.typeParamStack) {
			for i := len(tc.typeParamStack) - 1; i >= start; i-- {
				id := tc.typeParamStack[i]
				delete(tc.typeParamBounds, id)
			}
			tc.typeParamStack = tc.typeParamStack[:start]
		}
	}
}

func (tc *typeChecker) currentTypeParamEnv() uint32 {
	if len(tc.typeParamEnv) == 0 {
		return 0
	}
	return tc.typeParamEnv[len(tc.typeParamEnv)-1]
}

func (tc *typeChecker) lookupTypeParam(name source.StringID) types.TypeID {
	if name == source.NoStringID {
		return types.NoTypeID
	}
	for i := len(tc.typeParams) - 1; i >= 0; i-- {
		scope := tc.typeParams[i]
		if id, ok := scope[name]; ok {
			return id
		}
	}
	return types.NoTypeID
}
