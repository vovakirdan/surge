package mono

import (
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
}
