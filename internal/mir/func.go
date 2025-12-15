package mir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type Func struct {
	ID   FuncID
	Sym  symbols.SymbolID
	Name string
	Span source.Span

	Result types.TypeID

	Locals []Local
	Blocks []Block
	Entry  BlockID
}
