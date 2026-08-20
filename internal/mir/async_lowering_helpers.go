package mir

// Shared building blocks for the MIR-level async and crossing lowerings: local
// sets, block and local construction, the ownership flags a synthesized local
// needs, and the tag-layout/type lookups the state machines ask for.
//
// This file used to also hold a SINGLE-SUSPEND state machine lowering, which no
// caller ever reached — it was removed rather than kept as a second, untested
// account of a protocol the live lowering implements differently (through the
// pc-dispatch state machine in async_codegen.go and the tag-payload channel).

import (
	"fmt"
	"sort"

	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

type localSet map[LocalID]struct{}

func (s localSet) add(id LocalID) {
	if s == nil || id == NoLocalID {
		return
	}
	s[id] = struct{}{}
}

func (s localSet) has(id LocalID) bool {
	if s == nil {
		return false
	}
	_, ok := s[id]
	return ok
}

func (s localSet) delete(id LocalID) {
	if s == nil {
		return
	}
	delete(s, id)
}

func (s localSet) sorted() []LocalID {
	if len(s) == 0 {
		return nil
	}
	ids := make([]LocalID, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func allocAsyncPollFunc(m *Module) (FuncID, error) {
	if m == nil {
		return NoFuncID, fmt.Errorf("mir: async: missing module")
	}
	maxID := FuncID(0)
	for _, id := range m.SortedFuncIDs() {
		if id > maxID {
			maxID = id
		}
	}
	return maxID + 1, nil
}

func ensureTagLayout(m *Module, typesIn *types.Interner, tagSymByName map[string]symbols.SymbolID, unionType types.TypeID) error {
	if m == nil || typesIn == nil {
		return nil
	}
	if m.Meta == nil {
		m.Meta = &ModuleMeta{}
	}
	if m.Meta.TagLayouts == nil {
		m.Meta.TagLayouts = make(map[types.TypeID][]TagCaseMeta)
	}
	if _, ok := m.Meta.TagLayouts[unionType]; ok {
		return nil
	}
	info, ok := typesIn.UnionInfo(unionType)
	if !ok || info == nil {
		return fmt.Errorf("mir: async: missing union info for type#%d", unionType)
	}
	cases := make([]TagCaseMeta, 0, len(info.Members))
	for _, member := range info.Members {
		switch member.Kind {
		case types.UnionMemberTag:
			if typesIn.Strings == nil {
				return fmt.Errorf("mir: async: missing strings for tag layout")
			}
			tagName := typesIn.Strings.MustLookup(member.TagName)
			tagSym, ok := tagSymByName[tagName]
			if !ok || !tagSym.IsValid() {
				return fmt.Errorf("mir: async: missing tag symbol for %s", tagName)
			}
			payload := make([]types.TypeID, len(member.TagArgs))
			copy(payload, member.TagArgs)
			cases = append(cases, TagCaseMeta{TagName: tagName, TagSym: tagSym, PayloadTypes: payload})
		case types.UnionMemberNothing:
			cases = append(cases, TagCaseMeta{TagName: "nothing"})
		case types.UnionMemberType:
			continue
		}
	}
	if len(cases) == 0 {
		return fmt.Errorf("mir: async: empty tag layout for type#%d", unionType)
	}
	m.Meta.TagLayouts[unionType] = cases
	return nil
}

func taskTypeFor(typesIn *types.Interner, payload types.TypeID) (types.TypeID, error) {
	if typesIn == nil || typesIn.Strings == nil {
		return types.NoTypeID, fmt.Errorf("mir: async: missing type interner")
	}
	nameID := typesIn.Strings.Intern("Task")
	if id, ok := typesIn.FindStructInstance(nameID, []types.TypeID{payload}); ok {
		return id, nil
	}
	if id, ok := typesIn.FindAliasInstance(nameID, []types.TypeID{payload}); ok {
		return id, nil
	}
	return types.NoTypeID, fmt.Errorf("mir: async: Task type not found for payload")
}

func cloneLocals(locals []Local) []Local {
	if len(locals) == 0 {
		return nil
	}
	clone := make([]Local, len(locals))
	copy(clone, locals)
	return clone
}

func addLocal(f *Func, name string, ty types.TypeID, flags LocalFlags) LocalID {
	if f == nil {
		return NoLocalID
	}
	id := LocalID(len(f.Locals)) //nolint:gosec // bounded by locals length
	f.Locals = append(f.Locals, Local{Name: name, Type: ty, Flags: flags})
	return id
}

// localFlagsFor is the async/crossing lowering's flag computation, for locals
// built outside a funcLowerer (a poll function's state and result slots). It
// must answer the same way `funcLowerer.localFlags` does — the two flag the same
// axes for the same types, and a local that disagrees with its own type is how a
// drop goes missing or lands twice.
func localFlagsFor(typesIn *types.Interner, semaRes *sema.Result, ty types.TypeID) LocalFlags {
	var out LocalFlags
	isCopy := false
	if semaRes != nil {
		isCopy = semaRes.IsCopyType(ty)
	} else if typesIn != nil {
		isCopy = typesIn.IsCopy(ty)
	}
	if isCopy {
		out |= LocalFlagCopy
	}
	if ownsHeapFor(typesIn, semaRes, ty) {
		out |= LocalFlagOwnsHeap
	}
	if typesIn == nil || ty == types.NoTypeID {
		return out
	}
	resolved := resolveAlias(typesIn, ty)
	tt, ok := typesIn.Lookup(resolved)
	if !ok {
		return out
	}
	switch tt.Kind {
	case types.KindOwn:
		out |= LocalFlagOwn
	case types.KindReference:
		if tt.Mutable {
			out |= LocalFlagRefMut
		} else {
			out |= LocalFlagRef
		}
	case types.KindPointer:
		out |= LocalFlagPtr
	}
	return out
}

func newBlock(f *Func) BlockID {
	if f == nil {
		return NoBlockID
	}
	id := BlockID(len(f.Blocks)) //nolint:gosec // bounded by block count
	f.Blocks = append(f.Blocks, Block{ID: id, Term: Terminator{Kind: TermNone}})
	return id
}

//nolint:gocritic // hugeParam: passing Instr by value is intentional here
func appendInstr(f *Func, bb BlockID, ins Instr) {
	if f == nil || bb == NoBlockID {
		return
	}
	if int(bb) < 0 || int(bb) >= len(f.Blocks) {
		return
	}
	f.Blocks[bb].Instrs = append(f.Blocks[bb].Instrs, ins)
}

//nolint:gocritic // hugeParam: passing Terminator by value is intentional here
func setBlockTerm(f *Func, bb BlockID, term Terminator) {
	if f == nil || bb == NoBlockID {
		return
	}
	if int(bb) < 0 || int(bb) >= len(f.Blocks) {
		return
	}
	f.Blocks[bb].Term = term
}

// ownsHeapFor is the sema-free leg of the OwnsHeap axis, matching
// `funcLowerer.ownsHeap` including its no-sema fallback: without a sema result
// the interner's Copy bit is the answer the drop sites read before the axis was
// named.
func ownsHeapFor(typesIn *types.Interner, semaRes *sema.Result, ty types.TypeID) bool {
	if ty == types.NoTypeID {
		return false
	}
	if semaRes != nil {
		return semaRes.OwnsHeap(ty)
	}
	if typesIn == nil {
		return false
	}
	return !typesIn.IsCopy(ty)
}
