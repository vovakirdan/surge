package mir

import (
	"fmt"
	"slices"

	"fortio.org/safecast"

	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func LowerModule(mm *mono.MonoModule, semaRes *sema.Result) (*Module, error) {
	out := &Module{
		Funcs:     make(map[FuncID]*Func),
		FuncBySym: make(map[symbols.SymbolID]FuncID),
	}
	if mm == nil {
		return out, nil
	}

	typesIn := (*types.Interner)(nil)
	if semaRes != nil {
		typesIn = semaRes.TypeInterner
	}
	if typesIn == nil && mm.Source != nil {
		typesIn = mm.Source.TypeInterner
	}

	monoFuncs := make([]*mono.MonoFunc, 0, len(mm.Funcs))
	for _, mf := range mm.Funcs {
		if mf != nil && mf.Func != nil {
			monoFuncs = append(monoFuncs, mf)
		}
	}
	slices.SortStableFunc(monoFuncs, func(a, b *mono.MonoFunc) int {
		nameA := ""
		if a.Func != nil {
			nameA = a.Func.Name
		}
		nameB := ""
		if b.Func != nil {
			nameB = b.Func.Name
		}
		if nameA != nameB {
			if nameA < nameB {
				return -1
			}
			return 1
		}
		if a.InstanceSym != b.InstanceSym {
			if a.InstanceSym < b.InstanceSym {
				return -1
			}
			return 1
		}
		return 0
	})

	nextID := FuncID(1)
	for _, mf := range monoFuncs {
		if mf == nil || mf.Func == nil {
			continue
		}
		id := nextID
		nextID++
		fl := &funcLowerer{
			out:        out,
			mf:         mf,
			sema:       semaRes,
			types:      typesIn,
			symToLocal: make(map[symbols.SymbolID]LocalID),
			nextTemp:   1,
		}
		f, err := fl.lowerFunc(id, mf.Func)
		if err != nil {
			return nil, err
		}
		out.Funcs[id] = f
		if f != nil && f.Sym.IsValid() {
			out.FuncBySym[f.Sym] = id
		}
	}

	return out, nil
}

type loopCtx struct {
	breakTarget    BlockID
	continueTarget BlockID
}

type returnCtx struct {
	exit      BlockID
	hasResult bool
	result    Place
}

type funcLowerer struct {
	out   *Module
	mf    *mono.MonoFunc
	sema  *sema.Result
	types *types.Interner

	f   *Func
	cur BlockID

	symToLocal map[symbols.SymbolID]LocalID
	nextTemp   uint32

	loopStack   []loopCtx
	returnStack []returnCtx
}

func (l *funcLowerer) lowerFunc(id FuncID, fn *hir.Func) (*Func, error) {
	if l == nil || fn == nil {
		return nil, nil
	}
	if !fn.SymbolID.IsValid() {
		return nil, fmt.Errorf("mir: function %q has no symbol id", fn.Name)
	}

	l.f = &Func{
		ID:     id,
		Sym:    fn.SymbolID,
		Name:   fn.Name,
		Span:   fn.Span,
		Result: fn.Result,
	}

	// Locals: function parameters.
	for _, p := range fn.Params {
		if !p.SymbolID.IsValid() {
			continue
		}
		l.ensureLocal(p.SymbolID, p.Name, p.Type, p.Span)
	}

	// Entry block.
	entry := l.newBlock()
	l.f.Entry = entry
	l.cur = entry

	// Body.
	if fn.Body != nil {
		if err := l.lowerBlock(fn.Body); err != nil {
			return nil, err
		}
	}

	// Implicit fallthrough.
	if !l.curBlock().Terminated() {
		if fn.Result == types.NoTypeID || l.isNothingType(fn.Result) {
			l.setTerm(&Terminator{Kind: TermReturn})
		} else {
			l.setTerm(&Terminator{Kind: TermUnreachable})
		}
	}

	// Ensure every block has a terminator.
	for i := range l.f.Blocks {
		if l.f.Blocks[i].Term.Kind == TermNone {
			l.f.Blocks[i].Term.Kind = TermUnreachable
		}
	}

	return l.f, nil
}

func (l *funcLowerer) curBlock() *Block {
	if l == nil || l.f == nil {
		return nil
	}
	idx := int(l.cur)
	if idx < 0 || idx >= len(l.f.Blocks) {
		return nil
	}
	return &l.f.Blocks[idx]
}

func (l *funcLowerer) newBlock() BlockID {
	if l == nil || l.f == nil {
		return NoBlockID
	}
	raw, err := safecast.Conv[int32](len(l.f.Blocks))
	if err != nil {
		panic(fmt.Errorf("mir: block id overflow: %w", err))
	}
	id := BlockID(raw)
	l.f.Blocks = append(l.f.Blocks, Block{ID: id, Term: Terminator{Kind: TermNone}})
	return id
}

func (l *funcLowerer) startBlock(id BlockID) {
	if l == nil {
		return
	}
	l.cur = id
}

func (l *funcLowerer) setTerm(t *Terminator) {
	b := l.curBlock()
	if b == nil || b.Terminated() || t == nil {
		return
	}
	b.Term = *t
}

func (l *funcLowerer) emit(ins *Instr) {
	b := l.curBlock()
	if b == nil || b.Terminated() || ins == nil {
		return
	}
	b.Instrs = append(b.Instrs, *ins)
}

func (l *funcLowerer) ensureLocal(sym symbols.SymbolID, name string, ty types.TypeID, span source.Span) LocalID {
	if l == nil || l.f == nil || !sym.IsValid() {
		return NoLocalID
	}
	if existing, ok := l.symToLocal[sym]; ok {
		return existing
	}
	raw, err := safecast.Conv[int32](len(l.f.Locals))
	if err != nil {
		panic(fmt.Errorf("mir: local id overflow: %w", err))
	}
	id := LocalID(raw)
	l.symToLocal[sym] = id

	l.f.Locals = append(l.f.Locals, Local{
		Sym:   sym,
		Type:  ty,
		Flags: l.localFlags(ty),
		Name:  name,
		Span:  span,
	})

	return id
}

func (l *funcLowerer) newTemp(ty types.TypeID, hint string, span source.Span) LocalID {
	if l == nil || l.f == nil {
		return NoLocalID
	}
	raw, err := safecast.Conv[int32](len(l.f.Locals))
	if err != nil {
		panic(fmt.Errorf("mir: local id overflow: %w", err))
	}
	id := LocalID(raw)
	name := fmt.Sprintf("tmp_%s%d", hint, l.nextTemp)
	l.nextTemp++

	l.f.Locals = append(l.f.Locals, Local{
		Sym:   symbols.NoSymbolID,
		Type:  ty,
		Flags: l.localFlags(ty),
		Name:  name,
		Span:  span,
	})
	return id
}

func (l *funcLowerer) localFlags(ty types.TypeID) LocalFlags {
	var out LocalFlags
	if l.isCopyType(ty) {
		out |= LocalFlagCopy
	}
	if l.types == nil || ty == types.NoTypeID {
		return out
	}
	resolved := resolveAlias(l.types, ty)
	tt, ok := l.types.Lookup(resolved)
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
	default:
	}
	return out
}

func (l *funcLowerer) isCopyType(ty types.TypeID) bool {
	if l == nil || ty == types.NoTypeID {
		return false
	}
	if l.sema != nil {
		return l.sema.IsCopyType(ty)
	}
	if l.types == nil {
		return false
	}
	return l.types.IsCopy(resolveAlias(l.types, ty))
}

func (l *funcLowerer) isNothingType(ty types.TypeID) bool {
	if l == nil || l.types == nil || ty == types.NoTypeID {
		return false
	}
	tt, ok := l.types.Lookup(resolveAlias(l.types, ty))
	return ok && tt.Kind == types.KindNothing
}

func resolveAlias(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		tt, ok := typesIn.Lookup(id)
		if !ok || tt.Kind != types.KindAlias {
			return id
		}
		target, ok := typesIn.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
		seen++
	}
	return id
}
