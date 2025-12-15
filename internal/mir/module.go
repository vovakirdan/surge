package mir

import "surge/internal/symbols"

type Module struct {
	Funcs     map[FuncID]*Func
	FuncBySym map[symbols.SymbolID]FuncID
}
