package mir

import (
	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type ModuleMeta struct {
	TagLayouts map[types.TypeID][]TagCaseMeta
	TagNames   map[symbols.SymbolID]string
	TagAliases map[symbols.SymbolID]symbols.SymbolID

	// Layout is the single source of truth for ABI layout queries.
	Layout *layout.LayoutEngine

	// FuncTypeArgs maps instantiated symbols to their concrete type arguments.
	// This is used by intrinsic implementations like size_of/align_of.
	FuncTypeArgs map[symbols.SymbolID][]types.TypeID
}

type TagCaseMeta struct {
	TagName      string
	TagSym       symbols.SymbolID
	PayloadTypes []types.TypeID
}

type Global struct {
	Sym   symbols.SymbolID
	Type  types.TypeID
	Name  string
	IsMut bool
	Span  source.Span
}

type Module struct {
	Funcs     map[FuncID]*Func
	FuncBySym map[symbols.SymbolID]FuncID
	Globals   []Global
	Meta      *ModuleMeta
}
