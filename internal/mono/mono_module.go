package mono

import (
	"sort"

	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ArgsKey is a string representation of concrete type arguments.
type ArgsKey string

func argsKeyFromTypes(args []types.TypeID) ArgsKey {
	if len(args) == 0 {
		return ""
	}
	return ArgsKey(typeArgsKey(args))
}

// MonoKey uniquely identifies a monomorphized instance by original symbol and type arguments.
// Note: The name stutters with the package name, but is kept for consistency.
type MonoKey struct { //nolint:revive
	Sym     symbols.SymbolID
	ArgsKey ArgsKey
}

// MonoFunc represents a concrete function instance after monomorphization.
// Note: The name stutters with the package name, but is kept for consistency.
type MonoFunc struct { //nolint:revive
	Key         MonoKey
	InstanceSym symbols.SymbolID
	OrigSym     symbols.SymbolID
	TypeArgs    []types.TypeID

	Func *hir.Func
}

// MonoType represents a concrete type instance after monomorphization.
// Note: The name stutters with the package name, but is kept for consistency.
type MonoType struct { //nolint:revive
	Key      MonoKey
	OrigSym  symbols.SymbolID
	TypeArgs []types.TypeID
	TypeID   types.TypeID
}

// MonoModule contains the results of monomorphizing an entire HIR module.
// Note: The name stutters with the package name, but is kept for consistency.
type MonoModule struct { //nolint:revive
	Source *hir.Module

	Funcs     map[MonoKey]*MonoFunc
	FuncBySym map[symbols.SymbolID]*MonoFunc

	Types map[MonoKey]*MonoType

	Callables CallableMap
}

// SortedFuncKeys returns the module's function keys in a stable order.
//
// Funcs is a map, and Go randomises map iteration. Downstream passes turn
// this order into allocated identifiers (MIR function ids, and from those
// the async state types and drop-function ids), so ranging over it
// directly makes compiler output depend on iteration order and builds stop
// being reproducible. Every walk that lowers, emits, or otherwise affects
// output must go through this.
func (m *MonoModule) SortedFuncKeys() []MonoKey {
	if m == nil || len(m.Funcs) == 0 {
		return nil
	}
	keys := make([]MonoKey, 0, len(m.Funcs))
	for k := range m.Funcs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].Sym != keys[b].Sym {
			return keys[a].Sym < keys[b].Sym
		}
		return keys[a].ArgsKey < keys[b].ArgsKey
	})
	return keys
}

// SortedTypeKeys returns the module's type keys in a stable order, for the
// same reason SortedFuncKeys exists: Types is a map, and any walk whose
// order reaches output must not depend on Go's randomised map iteration.
func (m *MonoModule) SortedTypeKeys() []MonoKey {
	if m == nil || len(m.Types) == 0 {
		return nil
	}
	keys := make([]MonoKey, 0, len(m.Types))
	for k := range m.Types {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].Sym != keys[b].Sym {
			return keys[a].Sym < keys[b].Sym
		}
		return keys[a].ArgsKey < keys[b].ArgsKey
	})
	return keys
}
