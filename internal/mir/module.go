package mir

import (
	"sort"

	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
	"surge/internal/valueops"
)

// ModuleMeta holds metadata for a MIR module.
type ModuleMeta struct {
	// UnionCases is the full canonical membership of every union the module
	// mentions, keyed by canonical type id, each list in UnionInfo.Members
	// order. It is what anything asking "what does this union contain" reads.
	UnionCases map[types.TypeID][]UnionCaseMeta

	// TagLayouts is the flattened, deduplicated tag view, for tag switches
	// only. It is deliberately NOT the membership; see UnionCaseMeta.
	TagLayouts map[types.TypeID][]TagCaseMeta
	TagNames   map[symbols.SymbolID]string
	TagAliases map[symbols.SymbolID]symbols.SymbolID

	// Layouts is the immutable, finalized source of truth for ABI layout
	// queries. It is populated only after async lowering and final CFG cleanup.
	Layouts *layout.Registry

	// Operations is the immutable, finalized source of truth for what may be
	// DONE with a value of a type: the frozen layout facts above paired with the
	// program's capability verdicts and the symbols that implement them.
	//
	// It is nil wherever no whole-program capability authority was reached, and
	// the registry's own accessors fail closed on a nil receiver, so a consumer
	// never has to tell "no plan" apart from "an empty plan".
	Operations *valueops.Registry

	// CallLayouts is the single authority for how a call is lowered: which
	// results travel through a hidden caller-owned destination, which arguments
	// travel by address, and which carry no payload at all.
	//
	// It is bound to the frozen layouts above and classifies on demand, so a
	// direct call and an indirect call to one callee reach one answer.
	//
	// It is nil wherever no finalized layout authority was reached, and its own
	// accessors fail closed on a nil receiver.
	CallLayouts *CallLayoutTable

	// FuncTypeArgs maps instantiated symbols to their concrete type arguments.
	// This is used by intrinsic implementations like size_of/align_of.
	FuncTypeArgs map[symbols.SymbolID][]types.TypeID
}

// UnionCaseKind is what a union case IS. It mirrors types.UnionMemberKind
// one-for-one, deliberately: the enumeration is the same enumeration.
type UnionCaseKind uint8

const (
	// UnionCaseBareType is a plain type alternative — the `E` in
	// `type Result<T, E> = ResOk(T) | E`.
	UnionCaseBareType UnionCaseKind = iota
	// UnionCaseNothing is the `nothing` alternative.
	UnionCaseNothing
	// UnionCaseTag is a tag alternative, with or without payload.
	UnionCaseTag
)

// UnionCaseMeta is one DIRECT member of one union, in the enumeration the
// physical layout uses.
//
// It is the FULL membership: every member of every kind gets an entry, in
// UnionInfo.Members order, with no filtering, no flattening and no dedupe.
//
// TagLayouts is a different thing and stays a different thing: a FLATTENED,
// DEDUPLICATED tag view for tag switches. It cannot serve as membership for two
// independent reasons — it can be LONGER than the direct members, because a
// nested union's tags are hoisted into it, and it is NOT INJECTIVE, because two
// distinct `nothing` alternatives collapse into one entry. Reading it as the
// membership is RV2-DEBT-233: a bare member that owned heap was invisible to
// the drop predicate and the drop body alike, and its contents leaked.
type UnionCaseMeta struct {
	// PhysicalCaseIndex is this case's position in types.UnionInfo.Members, and
	// therefore the index layout.PhysicalFacts.UnionCase expects: the layout
	// engine issues its case list from the same loop over the same slice.
	//
	// A flattened tag index is NOT this index whenever a union mixes kinds,
	// which is why the two must never meet at a call site.
	PhysicalCaseIndex int

	// Kind is what this case is.
	Kind UnionCaseKind

	// Name names the arm for all three kinds, spelled as the VM already spells
	// it: the tag's source name, "nothing", or "type#<id>" for a bare
	// alternative — a spelling no tag can collide with.
	Name string

	// TagSym is the module-canonical tag symbol, valid only for UnionCaseTag.
	TagSym symbols.SymbolID

	// PayloadTypes is what this case CARRIES: the tag's arguments for a tag,
	// none for `nothing`, and exactly the admitted type for a bare alternative.
	PayloadTypes []types.TypeID

	// BareType is the type a bare alternative admits, NoTypeID otherwise.
	BareType types.TypeID
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
