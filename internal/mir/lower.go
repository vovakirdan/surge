package mir

import (
	"fmt"
	"slices"

	"fortio.org/safecast"

	"surge/internal/hir"
	"surge/internal/layout"
	"surge/internal/mono"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// LowerModule converts a monomorphized module to MIR.
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

	funcTypeArgs := make(map[symbols.SymbolID][]types.TypeID, len(mm.Funcs))
	for _, mf := range mm.Funcs {
		if mf == nil || !mf.InstanceSym.IsValid() || len(mf.TypeArgs) == 0 {
			continue
		}
		funcTypeArgs[mf.InstanceSym] = slices.Clone(mf.TypeArgs)
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

	globals, symToGlobal := buildGlobalMap(mm.Source)
	out.Globals = globals
	staticStringGlobals := make(map[string]GlobalID)
	staticStringInits := make(map[GlobalID]string)

	consts := buildConstMap(mm.Source)

	nextID := FuncID(1)
	for _, mf := range monoFuncs {
		if mf == nil || mf.Func == nil {
			continue
		}
		if mf.Func.IsIntrinsic() {
			continue
		}
		id := nextID
		nextID++
		fl := &funcLowerer{
			out:                 out,
			mf:                  mf,
			mono:                mm,
			sema:                semaRes,
			types:               typesIn,
			symbols:             nil,
			symToLocal:          make(map[symbols.SymbolID]LocalID),
			symToGlobal:         symToGlobal,
			nextTemp:            1,
			scopeLocal:          NoLocalID,
			consts:              consts,
			staticStringGlobals: staticStringGlobals,
			staticStringInits:   staticStringInits,
			nextFuncID:          &nextID,
		}
		if mm.Source != nil {
			fl.symbols = mm.Source.Symbols
		}
		f, err := fl.lowerFunc(id, mf.Func)
		if err != nil {
			return nil, err
		}
		out.Funcs[id] = f
		if f != nil && f.Sym.IsValid() {
			out.FuncBySym[f.Sym] = id
			if mf.OrigSym.IsValid() && len(mf.TypeArgs) == 0 {
				if _, exists := out.FuncBySym[mf.OrigSym]; !exists {
					out.FuncBySym[mf.OrigSym] = id
				}
			}
		}
	}

	// Build __surge_start if there's an entrypoint
	surgeStart, err := BuildSurgeStart(mm, semaRes, typesIn, nextID, out.Globals, symToGlobal, staticStringGlobals, staticStringInits)
	if err != nil {
		return nil, fmt.Errorf("building __surge_start: %w", err)
	}
	if surgeStart != nil {
		out.Funcs[surgeStart.ID] = surgeStart
		// __surge_start has no symbol, so don't add to FuncBySym
	}

	out.Meta = &ModuleMeta{
		Layout:       layout.New(layout.X86_64LinuxGNU(), typesIn),
		FuncTypeArgs: funcTypeArgs,
	}

	if mm.Source != nil {
		tagLayouts, tagNames := buildTagLayouts(out, mm.Source, typesIn)
		tagAliases := buildTagAliases(mm)
		if len(tagLayouts) != 0 || len(tagNames) != 0 || len(tagAliases) != 0 {
			out.Meta.TagLayouts = tagLayouts
			if len(tagNames) != 0 {
				out.Meta.TagNames = tagNames
			}
			if len(tagAliases) != 0 {
				out.Meta.TagAliases = tagAliases
			}
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
	out     *Module
	mf      *mono.MonoFunc
	mono    *mono.MonoModule
	sema    *sema.Result
	types   *types.Interner
	symbols *symbols.Result

	f   *Func
	cur BlockID

	symToLocal  map[symbols.SymbolID]LocalID
	symToGlobal map[symbols.SymbolID]GlobalID
	nextTemp    uint32
	scopeLocal  LocalID

	loopStack   []loopCtx
	returnStack []returnCtx

	consts     map[symbols.SymbolID]*hir.ConstDecl
	constStack map[symbols.SymbolID]bool

	staticStringGlobals map[string]GlobalID
	staticStringInits   map[GlobalID]string
	nextFuncID          *FuncID
}

func (l *funcLowerer) lowerFunc(id FuncID, fn *hir.Func) (*Func, error) {
	if l == nil || fn == nil {
		return nil, nil
	}
	if !fn.SymbolID.IsValid() {
		return nil, fmt.Errorf("mir: function %q has no symbol id", fn.Name)
	}

	l.f = &Func{
		ID:       id,
		Sym:      fn.SymbolID,
		Name:     fn.Name,
		Span:     fn.Span,
		Result:   fn.Result,
		IsAsync:  fn.IsAsync(),
		Failfast: fn.Flags.HasFlag(hir.FuncFailfast),
	}

	// Locals: function parameters.
	l.f.ParamCount = len(fn.Params)
	for _, p := range fn.Params {
		if p.SymbolID.IsValid() {
			l.ensureLocal(p.SymbolID, p.Name, p.Type, p.Span)
			continue
		}
		name := p.Name
		if name == "" {
			name = "_"
		}
		addLocal(l.f, name, p.Type, l.localFlags(p.Type))
	}
	if l.f.IsAsync && l.types != nil {
		scopeType := l.types.Builtins().Uint
		l.scopeLocal = addLocal(l.f, "__scope", scopeType, localFlagsFor(l.types, l.sema, scopeType))
		l.f.ScopeLocal = l.scopeLocal
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

func (l *funcLowerer) lowerSyntheticFunc(id FuncID, name string, body *hir.Block, result types.TypeID, span source.Span, isAsync, failfast bool) (*Func, error) {
	if l == nil {
		return nil, nil
	}

	l.f = &Func{
		ID:       id,
		Sym:      symbols.NoSymbolID,
		Name:     name,
		Span:     span,
		Result:   result,
		IsAsync:  isAsync,
		Failfast: failfast,
	}

	entry := l.newBlock()
	l.f.Entry = entry
	l.cur = entry

	if l.f.IsAsync && l.types != nil {
		scopeType := l.types.Builtins().Uint
		l.scopeLocal = addLocal(l.f, "__scope", scopeType, localFlagsFor(l.types, l.sema, scopeType))
		l.f.ScopeLocal = l.scopeLocal
	}

	if body != nil {
		if err := l.lowerBlock(body); err != nil {
			return nil, err
		}
	}

	if !l.curBlock().Terminated() {
		if result == types.NoTypeID || l.isNothingType(result) {
			l.setTerm(&Terminator{Kind: TermReturn})
		} else {
			l.setTerm(&Terminator{Kind: TermUnreachable})
		}
	}

	for i := range l.f.Blocks {
		if l.f.Blocks[i].Term.Kind == TermNone {
			l.f.Blocks[i].Term.Kind = TermUnreachable
		}
	}

	return l.f, nil
}

func (l *funcLowerer) forkLowerer() *funcLowerer {
	if l == nil {
		return nil
	}
	return &funcLowerer{
		out:                 l.out,
		mf:                  l.mf,
		sema:                l.sema,
		types:               l.types,
		symToLocal:          make(map[symbols.SymbolID]LocalID),
		symToGlobal:         l.symToGlobal,
		nextTemp:            1,
		scopeLocal:          NoLocalID,
		consts:              l.consts,
		staticStringGlobals: l.staticStringGlobals,
		staticStringInits:   l.staticStringInits,
		nextFuncID:          l.nextFuncID,
	}
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

func (l *funcLowerer) allocFuncID() FuncID {
	if l == nil || l.nextFuncID == nil {
		return NoFuncID
	}
	id := *l.nextFuncID
	*l.nextFuncID++
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

func (l *funcLowerer) isTaskType(ty types.TypeID) bool {
	if l == nil || l.types == nil || ty == types.NoTypeID {
		return false
	}
	resolved := resolveAlias(l.types, ty)
	if info, ok := l.types.StructInfo(resolved); ok && info != nil {
		return l.typeNameMatches(info.Name, "Task")
	}
	if info, ok := l.types.AliasInfo(resolved); ok && info != nil {
		return l.typeNameMatches(info.Name, "Task")
	}
	return false
}

func (l *funcLowerer) isChannelType(ty types.TypeID) bool {
	if l == nil || l.types == nil || ty == types.NoTypeID {
		return false
	}
	base := l.unwrapContainerType(ty)
	resolved := resolveAlias(l.types, base)
	if info, ok := l.types.StructInfo(resolved); ok && info != nil {
		return l.typeNameMatches(info.Name, "Channel")
	}
	if info, ok := l.types.AliasInfo(resolved); ok && info != nil {
		return l.typeNameMatches(info.Name, "Channel")
	}
	return false
}

func (l *funcLowerer) unwrapContainerType(id types.TypeID) types.TypeID {
	if l == nil || l.types == nil || id == types.NoTypeID {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 16 {
		seen++
		tt, ok := l.types.Lookup(resolveAlias(l.types, id))
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindReference, types.KindOwn, types.KindPointer:
			if tt.Elem == types.NoTypeID || tt.Elem == id {
				return id
			}
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func (l *funcLowerer) taskPayloadType(task types.TypeID) (types.TypeID, bool) {
	if l == nil || l.types == nil || task == types.NoTypeID {
		return types.NoTypeID, false
	}
	resolved := resolveAlias(l.types, task)
	if info, ok := l.types.StructInfo(resolved); ok && info != nil {
		if !l.typeNameMatches(info.Name, "Task") {
			return types.NoTypeID, false
		}
		if args := l.types.StructArgs(resolved); len(args) == 1 {
			return args[0], true
		}
		return types.NoTypeID, false
	}
	if info, ok := l.types.AliasInfo(resolved); ok && info != nil {
		if !l.typeNameMatches(info.Name, "Task") {
			return types.NoTypeID, false
		}
		if args := l.types.AliasArgs(resolved); len(args) == 1 {
			return args[0], true
		}
	}
	return types.NoTypeID, false
}

func (l *funcLowerer) typeNameMatches(nameID source.StringID, name string) bool { //nolint:unparam // name may vary in future
	if l == nil || l.types == nil || l.types.Strings == nil || nameID == source.NoStringID {
		return false
	}
	got, ok := l.types.Strings.Lookup(nameID)
	return ok && got == name
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
