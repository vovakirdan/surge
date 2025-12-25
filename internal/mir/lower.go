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
			out:        out,
			mf:         mf,
			sema:       semaRes,
			types:      typesIn,
			symToLocal: make(map[symbols.SymbolID]LocalID),
			nextTemp:   1,
			consts:     consts,
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

	// Build __surge_start if there's an entrypoint
	surgeStart, err := BuildSurgeStart(mm, semaRes, typesIn, nextID)
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
		if tagLayouts := buildTagLayouts(out, mm.Source, typesIn); len(tagLayouts) != 0 {
			out.Meta.TagLayouts = tagLayouts
		}
	}

	return out, nil
}

func buildTagLayouts(m *Module, src *hir.Module, typesIn *types.Interner) map[types.TypeID][]TagCaseMeta {
	if m == nil || src == nil || typesIn == nil {
		return nil
	}
	if src.Symbols == nil || src.Symbols.Table == nil || src.Symbols.Table.Strings == nil || src.Symbols.Table.Symbols == nil {
		return nil
	}
	tagSymByName := make(map[source.StringID]symbols.SymbolID)
	maxSym, err := safecast.Conv[uint32](src.Symbols.Table.Symbols.Len())
	if err != nil {
		panic(fmt.Errorf("mir: symbol arena overflow: %w", err))
	}
	for id := symbols.SymbolID(1); id <= symbols.SymbolID(maxSym); id++ {
		sym := src.Symbols.Table.Symbols.Get(id)
		if sym == nil || sym.Kind != symbols.SymbolTag || sym.Name == source.NoStringID {
			continue
		}
		if existingID, exists := tagSymByName[sym.Name]; exists {
			existing := src.Symbols.Table.Symbols.Get(existingID)
			replace := false
			switch {
			case existing == nil:
				replace = true
			case sym.ModulePath == "core" && existing.ModulePath != "core":
				replace = true
			case sym.ModulePath != "" && existing.ModulePath == "":
				replace = true
			case sym.ModulePath == existing.ModulePath && id > existingID:
				replace = true
			}
			if replace {
				tagSymByName[sym.Name] = id
			}
			continue
		}
		tagSymByName[sym.Name] = id
	}

	typeIDs := make(map[types.TypeID]struct{})
	visitType := func(id types.TypeID) {
		if id == types.NoTypeID {
			return
		}
		id = canonicalType(typesIn, id)
		if id == types.NoTypeID {
			return
		}
		typeIDs[id] = struct{}{}
	}

	var visitOperand func(op *Operand)
	var visitRValue func(rv *RValue)
	visitOperand = func(op *Operand) {
		if op == nil {
			return
		}
		visitType(op.Type)
		if op.Kind == OperandConst {
			visitType(op.Const.Type)
		}
	}
	visitRValue = func(rv *RValue) {
		if rv == nil {
			return
		}
		switch rv.Kind {
		case RValueUse:
			visitOperand(&rv.Use)
		case RValueUnaryOp:
			visitOperand(&rv.Unary.Operand)
		case RValueBinaryOp:
			visitOperand(&rv.Binary.Left)
			visitOperand(&rv.Binary.Right)
		case RValueCast:
			visitOperand(&rv.Cast.Value)
			visitType(rv.Cast.TargetTy)
		case RValueStructLit:
			visitType(rv.StructLit.TypeID)
			for i := range rv.StructLit.Fields {
				visitOperand(&rv.StructLit.Fields[i].Value)
			}
		case RValueArrayLit:
			for i := range rv.ArrayLit.Elems {
				visitOperand(&rv.ArrayLit.Elems[i])
			}
		case RValueTupleLit:
			for i := range rv.TupleLit.Elems {
				visitOperand(&rv.TupleLit.Elems[i])
			}
		case RValueField:
			visitOperand(&rv.Field.Object)
		case RValueIndex:
			visitOperand(&rv.Index.Object)
			visitOperand(&rv.Index.Index)
		case RValueTagTest:
			visitOperand(&rv.TagTest.Value)
		case RValueTagPayload:
			visitOperand(&rv.TagPayload.Value)
		case RValueIterInit:
			visitOperand(&rv.IterInit.Iterable)
		case RValueIterNext:
			visitOperand(&rv.IterNext.Iter)
		default:
		}
	}

	for _, fn := range m.Funcs {
		if fn == nil {
			continue
		}
		visitType(fn.Result)
		for i := range fn.Locals {
			visitType(fn.Locals[i].Type)
		}
		for bi := range fn.Blocks {
			bb := &fn.Blocks[bi]
			for ii := range bb.Instrs {
				ins := &bb.Instrs[ii]
				switch ins.Kind {
				case InstrAssign:
					visitRValue(&ins.Assign.Src)
				case InstrCall:
					for ai := range ins.Call.Args {
						visitOperand(&ins.Call.Args[ai])
					}
				case InstrDrop:
					// place type is already on locals
				case InstrEndBorrow:
					// place type is already on locals
				case InstrAwait:
					visitOperand(&ins.Await.Task)
				case InstrSpawn:
					visitOperand(&ins.Spawn.Value)
				default:
				}
			}
			switch bb.Term.Kind {
			case TermReturn:
				if bb.Term.Return.HasValue {
					visitOperand(&bb.Term.Return.Value)
				}
			case TermIf:
				visitOperand(&bb.Term.If.Cond)
			case TermSwitchTag:
				visitOperand(&bb.Term.SwitchTag.Value)
			default:
			}
		}
	}

	layouts := make(map[types.TypeID][]TagCaseMeta)
	strs := src.Symbols.Table.Strings
	for typeID := range typeIDs {
		tt, ok := typesIn.Lookup(typeID)
		if !ok || tt.Kind != types.KindUnion {
			continue
		}
		info, ok := typesIn.UnionInfo(typeID)
		if !ok || info == nil || len(info.Members) == 0 {
			continue
		}
		cases := make([]TagCaseMeta, 0, len(info.Members))
		for _, member := range info.Members {
			switch member.Kind {
			case types.UnionMemberTag:
				tagName := strs.MustLookup(member.TagName)
				payload := make([]types.TypeID, len(member.TagArgs))
				for i := range member.TagArgs {
					payload[i] = canonicalType(typesIn, member.TagArgs[i])
				}
				cases = append(cases, TagCaseMeta{
					TagName:      tagName,
					TagSym:       tagSymByName[member.TagName],
					PayloadTypes: payload,
				})
			case types.UnionMemberNothing:
				cases = append(cases, TagCaseMeta{TagName: "nothing"})
			case types.UnionMemberType:
				continue
			default:
				cases = nil
			}
		}
		if len(cases) == 0 {
			continue
		}
		layouts[typeID] = cases
	}

	return layouts
}

func buildConstMap(src *hir.Module) map[symbols.SymbolID]*hir.ConstDecl {
	if src == nil || len(src.Consts) == 0 {
		return nil
	}
	out := make(map[symbols.SymbolID]*hir.ConstDecl, len(src.Consts))
	for i := range src.Consts {
		decl := &src.Consts[i]
		if !decl.SymbolID.IsValid() {
			continue
		}
		out[decl.SymbolID] = decl
	}
	return out
}

func canonicalType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if id == types.NoTypeID || typesIn == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		seen++
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID || target == id {
				return id
			}
			id = target
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
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

	consts     map[symbols.SymbolID]*hir.ConstDecl
	constStack map[symbols.SymbolID]bool
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
