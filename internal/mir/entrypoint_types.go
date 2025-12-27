package mir

import (
	"fortio.org/safecast"

	"surge/internal/symbols"
	"surge/internal/types"
)

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

func (b *surgeStartBuilder) isBuiltinFromStrType(typeID types.TypeID) bool {
	if b.typesIn == nil {
		return false
	}
	tt, ok := b.typesIn.Lookup(b.resolveAlias(typeID))
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindInt, types.KindUint, types.KindFloat, types.KindBool, types.KindString:
		return true
	default:
		return false
	}
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

func (b *surgeStartBuilder) findFromStrMethod(typeID types.TypeID) symbols.SymbolID {
	if b.mm == nil || b.mm.Source == nil || b.mm.Source.Symbols == nil || b.mm.Source.Symbols.Table == nil {
		return symbols.NoSymbolID
	}
	typeKey := b.typeKeyForType(typeID)
	if typeKey == "" {
		return symbols.NoSymbolID
	}
	table := b.mm.Source.Symbols.Table
	for i := range table.Symbols.Len() {
		raw, err := safecast.Conv[uint32](i + 1)
		if err != nil {
			continue
		}
		symID := symbols.SymbolID(raw)
		sym := table.Symbols.Get(symID)
		if sym == nil || sym.Kind != symbols.SymbolFunction {
			continue
		}
		name, ok := table.Strings.Lookup(sym.Name)
		if !ok || name != "from_str" {
			continue
		}
		if sym.ReceiverKey != typeKey {
			continue
		}
		return symID
	}
	return symbols.NoSymbolID
}

// findToMethod looks up a __to method that converts srcType to targetType.
// Returns NoSymbolID if not found.
func (b *surgeStartBuilder) findToMethod(srcType, targetType types.TypeID) symbols.SymbolID {
	if b.mm == nil || b.mm.Source == nil || b.mm.Source.Symbols == nil {
		return symbols.NoSymbolID
	}

	table := b.mm.Source.Symbols.Table
	if table == nil || table.Symbols == nil {
		return symbols.NoSymbolID
	}

	// Get source type key for matching receiver
	srcTypeKey := b.typeKeyForType(srcType)
	if srcTypeKey == "" {
		return symbols.NoSymbolID
	}

	// Search for __to method with matching signature
	for i := range table.Symbols.Len() {
		raw, err := safecast.Conv[uint32](i + 1) // +1 because SymbolID 0 is NoSymbolID
		if err != nil {
			continue
		}
		symID := symbols.SymbolID(raw)
		sym := table.Symbols.Get(symID)
		if sym == nil || sym.Kind != symbols.SymbolFunction {
			continue
		}

		// Check name is "__to"
		name, ok := table.Strings.Lookup(sym.Name)
		if !ok || name != "__to" {
			continue
		}

		// Check receiver matches source type
		if sym.ReceiverKey != srcTypeKey {
			continue
		}

		// Check signature: (self, target) -> target
		sig := sym.Signature
		if sig == nil || len(sig.Params) != 2 {
			continue
		}

		// Params[1] should be the target type, Result should equal target
		targetTypeKey := b.typeKeyForType(targetType)
		if sig.Params[1] == targetTypeKey && sig.Result == targetTypeKey {
			return symID
		}
	}

	return symbols.NoSymbolID
}

func (b *surgeStartBuilder) localFlags(ty types.TypeID) LocalFlags {
	var out LocalFlags
	if b.isCopyType(ty) {
		out |= LocalFlagCopy
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
