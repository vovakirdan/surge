package mir

import "surge/internal/types"

func (b *surgeStartBuilder) erringType(elemType types.TypeID) types.TypeID {
	if b.typesIn == nil || b.mm == nil || b.mm.Source == nil || b.mm.Source.Symbols == nil || b.mm.Source.Symbols.Table == nil {
		return types.NoTypeID
	}
	stringTable := b.mm.Source.Symbols.Table.Strings
	if stringTable == nil {
		return types.NoTypeID
	}
	errType := b.errorType()
	if errType == types.NoTypeID {
		return types.NoTypeID
	}
	erringName := stringTable.Intern("Erring")
	if id, ok := b.typesIn.FindUnionInstance(erringName, []types.TypeID{elemType, errType}); ok {
		return id
	}
	return types.NoTypeID
}

func (b *surgeStartBuilder) errorType() types.TypeID {
	if b.typesIn == nil || b.mm == nil || b.mm.Source == nil || b.mm.Source.Symbols == nil || b.mm.Source.Symbols.Table == nil {
		return types.NoTypeID
	}
	stringTable := b.mm.Source.Symbols.Table.Strings
	if stringTable == nil {
		return types.NoTypeID
	}
	errorName := stringTable.Intern("Error")
	if id, ok := b.typesIn.FindStructInstance(errorName, nil); ok {
		return id
	}
	return types.NoTypeID
}

func (b *surgeStartBuilder) resolveAlias(id types.TypeID) types.TypeID {
	if b.typesIn == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		seen++
		tt, ok := b.typesIn.Lookup(id)
		if !ok {
			return id
		}
		if tt.Kind != types.KindAlias {
			return id
		}
		target, ok := b.typesIn.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
	}
	return id
}

func (b *surgeStartBuilder) localFlags(ty types.TypeID) LocalFlags {
	var out LocalFlags
	if b.isCopyType(ty) {
		out |= LocalFlagCopy
	}
	// The entrypoint builder has no sema result, so it uses the same fallback
	// `funcLowerer.ownsHeap` uses without one: the interner's Copy bit, which
	// is the answer the drop sites read before the ownership axis was named.
	if ty != types.NoTypeID && !b.isCopyType(ty) {
		out |= LocalFlagOwnsHeap
	}
	return out
}

func (b *surgeStartBuilder) isCopyType(ty types.TypeID) bool {
	if b.typesIn == nil || ty == types.NoTypeID {
		return false
	}
	return b.typesIn.IsCopy(ty)
}

func (b *surgeStartBuilder) isNothingType(ty types.TypeID) bool {
	if b.typesIn == nil || ty == types.NoTypeID {
		return false
	}
	tt, ok := b.typesIn.Lookup(ty)
	return ok && tt.Kind == types.KindNothing
}

func (b *surgeStartBuilder) isIntType(ty types.TypeID) bool {
	if b.typesIn == nil || ty == types.NoTypeID {
		return false
	}
	builtins := b.typesIn.Builtins()
	return ty == builtins.Int
}

func (b *surgeStartBuilder) intType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	return b.typesIn.Builtins().Int
}

func (b *surgeStartBuilder) uintType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	return b.typesIn.Builtins().Uint
}

func (b *surgeStartBuilder) boolType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	return b.typesIn.Builtins().Bool
}

func (b *surgeStartBuilder) stringType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	return b.typesIn.Builtins().String
}

func (b *surgeStartBuilder) refType(elem types.TypeID, mutable bool) types.TypeID {
	if b.typesIn == nil || elem == types.NoTypeID {
		return types.NoTypeID
	}
	return b.typesIn.Intern(types.MakeReference(elem, mutable))
}

func (b *surgeStartBuilder) stringArrayType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	// Dynamic array of strings (ArrayDynamicLength for slice/dynamic array)
	return b.typesIn.Intern(types.MakeArray(b.stringType(), types.ArrayDynamicLength))
}
