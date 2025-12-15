package mono

import (
	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

type ArgsKey string

func argsKeyFromTypes(args []types.TypeID) ArgsKey {
	if len(args) == 0 {
		return ""
	}
	return ArgsKey(typeArgsKey(args))
}

type MonoKey struct {
	Sym     symbols.SymbolID
	ArgsKey ArgsKey
}

type MonoFunc struct {
	Key         MonoKey
	InstanceSym symbols.SymbolID
	OrigSym     symbols.SymbolID
	TypeArgs    []types.TypeID

	Func *hir.Func
}

type MonoType struct {
	Key      MonoKey
	OrigSym  symbols.SymbolID
	TypeArgs []types.TypeID
	TypeID   types.TypeID
}

type MonoModule struct {
	Source *hir.Module

	Funcs     map[MonoKey]*MonoFunc
	FuncBySym map[symbols.SymbolID]*MonoFunc

	Types map[MonoKey]*MonoType
}
