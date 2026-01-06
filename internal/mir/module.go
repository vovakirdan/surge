package mir

import (
	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ModuleMeta holds metadata for a MIR module.
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

// TagCaseMeta holds metadata for a tag case.
type TagCaseMeta struct {
	TagName      string
	TagSym       symbols.SymbolID
	PayloadTypes []types.TypeID
}

// Global represents a global variable in MIR.
type Global struct {
	Sym   symbols.SymbolID
	Type  types.TypeID
	Name  string
	IsMut bool
	Span  source.Span
}

// Module represents a MIR module.
type Module struct {
	Funcs     map[FuncID]*Func
	FuncBySym map[symbols.SymbolID]FuncID
	Globals   []Global
	Meta      *ModuleMeta
}
