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
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	if _, ok := seen[id]; ok {
		return false
	}
	seen[id] = struct{}{}

	tt, ok := typesIn.Lookup(id)
	if !ok {
		return false
	}

	switch tt.Kind {
	case types.KindGenericParam:
		return true

	case types.KindPointer, types.KindReference, types.KindOwn, types.KindArray:
		return typeContainsGenericParam(typesIn, tt.Elem, seen)

	case types.KindTuple:
		info, ok := typesIn.TupleInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, el := range info.Elems {
			if typeContainsGenericParam(typesIn, el, seen) {
				return true
			}
		}
		return false

	case types.KindFn:
		info, ok := typesIn.FnInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, p := range info.Params {
			if typeContainsGenericParam(typesIn, p, seen) {
				return true
			}
		}
		return typeContainsGenericParam(typesIn, info.Result, seen)

	case types.KindStruct:
		info, ok := typesIn.StructInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, a := range info.TypeArgs {
			if typeContainsGenericParam(typesIn, a, seen) {
				return true
			}
		}
		for _, f := range typesIn.StructFields(id) {
			if typeContainsGenericParam(typesIn, f.Type, seen) {
				return true
			}
		}
		return false

	case types.KindUnion:
		info, ok := typesIn.UnionInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, a := range info.TypeArgs {
			if typeContainsGenericParam(typesIn, a, seen) {
				return true
			}
		}
		for _, m := range info.Members {
			if typeContainsGenericParam(typesIn, m.Type, seen) {
				return true
			}
			for _, a := range m.TagArgs {
				if typeContainsGenericParam(typesIn, a, seen) {
					return true
				}
			}
		}
		return false

	case types.KindAlias:
		info, ok := typesIn.AliasInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, a := range info.TypeArgs {
			if typeContainsGenericParam(typesIn, a, seen) {
				return true
			}
		}
		return typeContainsGenericParam(typesIn, info.Target, seen)

	default:
		return false
	}
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

	for _, mf := range mm.Funcs {
		if mf == nil {
			continue
		}
		if !typeArgsAreConcrete(typesIn, mf.TypeArgs) {
			return fmt.Errorf("mono: non-concrete type args for func sym=%d", mf.OrigSym)
		}
		if mf.Func == nil {
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
		if !typeArgsAreConcrete(typesIn, mt.TypeArgs) || typeContainsGenericParam(typesIn, mt.TypeID, make(map[types.TypeID]struct{})) {
			return fmt.Errorf("mono: non-concrete type instantiation sym=%d type#%d", mt.OrigSym, mt.TypeID)
		}
	}

	return nil
}
