package mir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Func represents a function in MIR.
type Func struct {
	ID   FuncID
	Sym  symbols.SymbolID
	Name string
	Span source.Span

	Result         types.TypeID
	IsAsync        bool
	Failfast       bool
	AsyncLoweredV2 bool
	ParamCount     int

	Locals []Local
	Blocks []Block
	Entry  BlockID

	ScopeLocal LocalID
}
