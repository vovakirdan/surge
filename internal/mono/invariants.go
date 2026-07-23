package mono

import (
	"fmt"

	"surge/internal/types"
)

func typeArgsAreConcrete(typesIn *types.Interner, args []types.TypeID) bool {
	if len(args) == 0 {
		return true
	}
	if typesIn == nil {
		return false
	}
	for _, a := range args {
		if a == types.NoTypeID {
			return false
		}
		if typeContainsGenericParam(typesIn, a, make(map[types.TypeID]struct{})) {
			return false
		}
	}
	return true
}

func typeContainsGenericParam(typesIn *types.Interner, id types.TypeID, seen map[types.TypeID]struct{}) bool {
	_ = seen
	return types.ContainsGenericParam(typesIn, id)
}

func validateMonoModuleNoTypeParams(mm *MonoModule, typesIn *types.Interner) error {
	if mm == nil {
		return nil
	}
	if typesIn == nil {
		if len(mm.Funcs) > 0 || len(mm.Types) > 0 {
			return fmt.Errorf("mono: missing types interner")
		}
		return nil
	}

	for _, __k := range mm.SortedFuncKeys() {
		mf := mm.Funcs[__k]
		if mf == nil {
			continue
		}
		if !typeArgsAreConcrete(typesIn, mf.TypeArgs) {
			return fmt.Errorf("mono: non-concrete type args for func sym=%d", mf.OrigSym)
		}
		if mf.Func == nil {
			continue
		}
		if mm.Source != nil && mm.Source.Symbols != nil && mm.Source.Symbols.Table != nil && mm.Source.Symbols.Table.Symbols != nil {
			if sym := mm.Source.Symbols.Table.Symbols.Get(mf.OrigSym); sym != nil && len(sym.TypeParams) > 0 {
				continue
			}
		}
		if mf.Func.IsIntrinsic() || mf.Func.Body == nil {
			continue
		}
		if mf.Func.IsGeneric() {
			return fmt.Errorf("mono: function %s is still generic", mf.Func.Name)
		}

		var bad types.TypeID
		collectTypeFromFunc(mf.Func, func(id types.TypeID) {
			if bad != types.NoTypeID || id == types.NoTypeID {
				return
			}
			if typeContainsGenericParam(typesIn, id, make(map[types.TypeID]struct{})) {
				bad = id
			}
		})
		if bad != types.NoTypeID {
			return fmt.Errorf("mono: generic type parameter leaked into func sym=%d via type#%d", mf.OrigSym, bad)
		}
	}

	for _, mt := range mm.Types {
		if mt == nil {
			continue
		}
		if mm.Source != nil && mm.Source.Symbols != nil && mm.Source.Symbols.Table != nil && mm.Source.Symbols.Table.Symbols != nil {
			if sym := mm.Source.Symbols.Table.Symbols.Get(mt.OrigSym); sym != nil && len(sym.TypeParams) > 0 {
				continue
			}
		}
		if !typeArgsAreConcrete(typesIn, mt.TypeArgs) || typeContainsGenericParam(typesIn, mt.TypeID, make(map[types.TypeID]struct{})) {
			return fmt.Errorf("mono: non-concrete type instantiation sym=%d type#%d", mt.OrigSym, mt.TypeID)
		}
	}

	return nil
}
