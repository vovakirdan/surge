package mir

import (
	"sort"

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

	// Layouts is the immutable, finalized source of truth for ABI layout
	// queries. It is populated only after async lowering and final CFG cleanup.
	Layouts *layout.Registry

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

// SortedFuncIDs returns the module's function ids in ascending order.
//
// Funcs is a map, and Go randomises map iteration. Several passes walk it
// while ALLOCATING identifiers — async lowering interns state types, the
// LLVM backend assigns drop-function ids — so ranging over it directly
// makes those ids depend on iteration order and the compiler stops being
// reproducible: the same input yields different (still correct) output run
// to run, which defeats golden comparison, IR diffing, and bisecting a
// codegen regression. Every walk that allocates, emits, or otherwise
// affects output must go through this.
func (m *Module) SortedFuncIDs() []FuncID {
	if m == nil || len(m.Funcs) == 0 {
		return nil
	}
	ids := make([]FuncID, 0, len(m.Funcs))
	for id := range m.Funcs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	return ids
}
