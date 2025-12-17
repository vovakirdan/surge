package mir

import (
	"surge/internal/symbols"
	"surge/internal/types"
)

type ModuleMeta struct {
	TagLayouts map[types.TypeID][]TagCaseMeta
}

type TagCaseMeta struct {
	TagName      string
	TagSym       symbols.SymbolID
	PayloadTypes []types.TypeID
}

type Module struct {
	Funcs     map[FuncID]*Func
	FuncBySym map[symbols.SymbolID]FuncID
	Meta      *ModuleMeta
}
