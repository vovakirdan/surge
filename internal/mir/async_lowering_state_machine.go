package mir

import (
	"fmt"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type awaitSite struct {
	pollBB     BlockID
	pollInstr  int
	readyBB    BlockID
	stateIndex int
	liveLocals localSet
	pendingBB  BlockID
}

type stateVariant struct {
	name     string
	tagSym   symbols.SymbolID
	locals   []LocalID
	resumeBB BlockID
}

type blockLiveness struct {
	use localSet
	def localSet
	in  localSet
	out localSet
}

// LowerAsyncStateMachine lowers async functions to a multi-suspend state machine.
func LowerAsyncStateMachine(m *Module, semaRes *sema.Result, symTable *symbols.Table) error {
	if m == nil {
		return nil
	}
	typesIn := (*types.Interner)(nil)
	if semaRes != nil {
		typesIn = semaRes.TypeInterner
	}
	if typesIn == nil {
		return fmt.Errorf("mir: async lowering requires type interner")
	}

	for _, f := range m.Funcs {
		if f == nil || !f.IsAsync {
			continue
		}
		if err := lowerAsyncStateMachineFunc(m, f, typesIn, semaRes, symTable); err != nil {
			return err
		}
	}
	return nil
}

func lowerAsyncStateMachineFunc(m *Module, f *Func, typesIn *types.Interner, semaRes *sema.Result, symTable *symbols.Table) error {
	if f == nil {
		return nil
	}
	payload := f.Result
	if payload == types.NoTypeID {
		payload = typesIn.Builtins().Nothing
	}

	taskType, err := taskTypeFor(typesIn, payload)
	if err != nil {
		return err
	}

	pollFnID, err := allocAsyncPollFunc(m)
	if err != nil {
		return err
	}

	origLocals := cloneLocals(f.Locals)
	origBlocks := f.Blocks
	origEntry := f.Entry

	pollFn := &Func{
		ID:      pollFnID,
		Sym:     symbols.NoSymbolID,
		Name:    f.Name + "$poll",
		Span:    f.Span,
		Result:  payload,
		IsAsync: false,
		Locals:  origLocals,
		Blocks:  origBlocks,
		Entry:   origEntry,
	}

	if m.Funcs == nil {
		m.Funcs = make(map[FuncID]*Func)
	}
	m.Funcs[pollFnID] = pollFn

	f.Locals = cloneLocals(origLocals)
	f.Blocks = nil
	f.Entry = NoBlockID

	awaitSites, err := splitAsyncAwaits(pollFn)
	if err != nil {
		return err
	}
	if err := rejectAwaitInLoops(pollFn, awaitSites); err != nil {
		return err
	}
	live := computeLiveness(pollFn)

	paramLocals := paramLocalSet(pollFn, symTable)
	variants := make([]stateVariant, 0, len(awaitSites)+1)
	variants = append(variants, stateVariant{
		name:     "S0",
		locals:   paramLocals.sorted(),
		resumeBB: origEntry,
	})

	for i := range awaitSites {
		awaitSites[i].stateIndex = i + 1
		awaitSites[i].liveLocals = cloneSet(live[awaitSites[i].pollBB].out)
		variants = append(variants, stateVariant{
			name:     fmt.Sprintf("S%d", awaitSites[i].stateIndex),
			locals:   awaitSites[i].liveLocals.sorted(),
			resumeBB: awaitSites[i].readyBB,
		})
	}

	stateType, err := buildAsyncStateUnion(m, typesIn, symTable, f, pollFn, variants)
	if err != nil {
		return err
	}

	stateLocal := addLocal(pollFn, "__state", stateType, localFlagsFor(typesIn, semaRes, stateType))
	entryBB := buildAsyncPollEntry(pollFn, stateLocal, variants)
	pollFn.Entry = entryBB

	if err := buildAsyncPendingBlocks(pollFn, stateLocal, awaitSites, variants); err != nil {
		return err
	}
	rewriteAsyncReturns(pollFn, stateLocal)

	if err := buildAsyncConstructorState(f, typesIn, semaRes, taskType, stateType, pollFnID, variants[0]); err != nil {
		return err
	}
	f.IsAsync = false
	f.Result = taskType
	return nil
}

func splitAsyncAwaits(f *Func) ([]awaitSite, error) {
	if f == nil {
		return nil, nil
	}
	var sites []awaitSite
	for {
		split := false
		for bi := range f.Blocks {
			bb := &f.Blocks[bi]
			for i := 0; i < len(bb.Instrs); i++ {
				ins := &bb.Instrs[i]
				if ins.Kind != InstrAwait {
					continue
				}
				awaitInstr := ins.Await
				prelude := append([]Instr(nil), bb.Instrs[:i]...)
				after := append([]Instr(nil), bb.Instrs[i+1:]...)
				origTerm := bb.Term

				afterBB := newBlock(f)
				f.Blocks[afterBB].Instrs = after
				f.Blocks[afterBB].Term = origTerm

				bb.Instrs = prelude
				pollInstr := Instr{Kind: InstrPoll, Poll: PollInstr{
					Dst:     awaitInstr.Dst,
					Task:    awaitInstr.Task,
					ReadyBB: afterBB,
					PendBB:  NoBlockID,
				}}
				bb.Instrs = append(bb.Instrs, pollInstr)
				bb.Term = Terminator{Kind: TermUnreachable}

				sites = append(sites, awaitSite{
					pollBB:    BlockID(bi), //nolint:gosec // bounded by block count
					pollInstr: len(bb.Instrs) - 1,
					readyBB:   afterBB,
				})
				split = true
				break
			}
			if split {
				break
			}
		}
		if !split {
			break
		}
	}

	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		for ii := range bb.Instrs {
			if bb.Instrs[ii].Kind == InstrAwait {
				return sites, fmt.Errorf("mir: async: await not normalized in %s", f.Name)
			}
		}
	}

	return sites, nil
}

func rejectAwaitInLoops(f *Func, sites []awaitSite) error {
	if f == nil || len(sites) == 0 {
		return nil
	}
	awaitBlocks := make(map[BlockID]struct{}, len(sites))
	for _, site := range sites {
		awaitBlocks[site.pollBB] = struct{}{}
	}
	for bbID := range awaitBlocks {
		if hasCycleFrom(f, bbID) {
			return fmt.Errorf("mir: async: await inside loop is not supported in %s", f.Name)
		}
	}
	return nil
}

func hasCycleFrom(f *Func, start BlockID) bool {
	if f == nil || start == NoBlockID {
		return false
	}
	seen := make(map[BlockID]struct{})
	var stack []BlockID
	stack = append(stack, start)
	seen[start] = struct{}{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, succ := range succBlocks(f, id, false) {
			if succ == start {
				return true
			}
			if _, ok := seen[succ]; ok {
				continue
			}
			seen[succ] = struct{}{}
			stack = append(stack, succ)
		}
	}
	return false
}

func succBlocks(f *Func, bbID BlockID, includePollPending bool) []BlockID {
	if f == nil || bbID == NoBlockID || int(bbID) >= len(f.Blocks) {
		return nil
	}
	bb := &f.Blocks[bbID]
	if len(bb.Instrs) > 0 {
		last := &bb.Instrs[len(bb.Instrs)-1]
		if last.Kind == InstrPoll {
			out := []BlockID{}
			if last.Poll.ReadyBB != NoBlockID {
				out = append(out, last.Poll.ReadyBB)
			}
			if includePollPending && last.Poll.PendBB != NoBlockID {
				out = append(out, last.Poll.PendBB)
			}
			return out
		}
	}
	switch bb.Term.Kind {
	case TermGoto:
		return []BlockID{bb.Term.Goto.Target}
	case TermIf:
		return []BlockID{bb.Term.If.Then, bb.Term.If.Else}
	case TermSwitchTag:
		out := make([]BlockID, 0, len(bb.Term.SwitchTag.Cases)+1)
		for _, c := range bb.Term.SwitchTag.Cases {
			out = append(out, c.Target)
		}
		out = append(out, bb.Term.SwitchTag.Default)
		return out
	default:
		return nil
	}
}

func computeLiveness(f *Func) []blockLiveness {
	if f == nil {
		return nil
	}
	info := make([]blockLiveness, len(f.Blocks))
	for i := range f.Blocks {
		use, def := computeBlockUseDef(&f.Blocks[i])
		info[i].use = use
		info[i].def = def
	}

	changed := true
	for changed {
		changed = false
		for i := len(f.Blocks) - 1; i >= 0; i-- {
			out := localSet{}
			for _, succ := range succBlocks(f, BlockID(i), false) { //nolint:gosec // bounded by block count
				out = unionSet(out, info[succ].in)
			}
			in := unionSet(cloneSet(info[i].use), subtractSet(out, info[i].def))

			if !setEqual(out, info[i].out) || !setEqual(in, info[i].in) {
				info[i].out = out
				info[i].in = in
				changed = true
			}
		}
	}
	return info
}

func computeBlockUseDef(bb *Block) (use, def localSet) {
	use = localSet{}
	def = localSet{}
	if bb == nil {
		return use, def
	}
	addUse := func(id LocalID) {
		if id == NoLocalID {
			return
		}
		if def.has(id) {
			return
		}
		use.add(id)
	}
	addDef := func(id LocalID) {
		if id == NoLocalID {
			return
		}
		def.add(id)
	}

	for i := range bb.Instrs {
		ins := &bb.Instrs[i]
		switch ins.Kind {
		case InstrAssign:
			addUsesFromRValue(ins.Assign.Src, addUse)
			addUsesFromPlaceWrite(ins.Assign.Dst, addUse)
			addDefFromPlace(ins.Assign.Dst, addDef)
		case InstrCall:
			if ins.Call.Callee.Kind == CalleeValue {
				addUsesFromOperand(ins.Call.Callee.Value, addUse)
			}
			for i := range ins.Call.Args {
				addUsesFromOperand(ins.Call.Args[i], addUse)
			}
			if ins.Call.HasDst {
				addUsesFromPlaceWrite(ins.Call.Dst, addUse)
				addDefFromPlace(ins.Call.Dst, addDef)
			}
		case InstrDrop:
			addUsesFromPlace(ins.Drop.Place, addUse, true)
			addDefFromPlace(ins.Drop.Place, addDef)
		case InstrEndBorrow:
			addUsesFromPlace(ins.EndBorrow.Place, addUse, true)
			addDefFromPlace(ins.EndBorrow.Place, addDef)
		case InstrAwait:
			addUsesFromOperand(ins.Await.Task, addUse)
			addUsesFromPlaceWrite(ins.Await.Dst, addUse)
			addDefFromPlace(ins.Await.Dst, addDef)
		case InstrSpawn:
			addUsesFromOperand(ins.Spawn.Value, addUse)
			addUsesFromPlaceWrite(ins.Spawn.Dst, addUse)
			addDefFromPlace(ins.Spawn.Dst, addDef)
		case InstrPoll:
			addUsesFromOperand(ins.Poll.Task, addUse)
			addUsesFromPlaceWrite(ins.Poll.Dst, addUse)
			addDefFromPlace(ins.Poll.Dst, addDef)
		}
	}

	switch bb.Term.Kind {
	case TermReturn:
		if bb.Term.Return.HasValue {
			addUsesFromOperand(bb.Term.Return.Value, addUse)
		}
	case TermAsyncYield:
		addUsesFromOperand(bb.Term.AsyncYield.State, addUse)
	case TermAsyncReturn:
		addUsesFromOperand(bb.Term.AsyncReturn.State, addUse)
		if bb.Term.AsyncReturn.HasValue {
			addUsesFromOperand(bb.Term.AsyncReturn.Value, addUse)
		}
	case TermIf:
		addUsesFromOperand(bb.Term.If.Cond, addUse)
	case TermSwitchTag:
		addUsesFromOperand(bb.Term.SwitchTag.Value, addUse)
	}

	return use, def
}

func addUsesFromOperand(op Operand, addUse func(LocalID)) {
	switch op.Kind {
	case OperandCopy, OperandMove, OperandAddrOf, OperandAddrOfMut:
		addUsesFromPlace(op.Place, addUse, true)
	}
}

func addUsesFromRValue(rv RValue, addUse func(LocalID)) {
	switch rv.Kind {
	case RValueUse:
		addUsesFromOperand(rv.Use, addUse)
	case RValueUnaryOp:
		addUsesFromOperand(rv.Unary.Operand, addUse)
	case RValueBinaryOp:
		addUsesFromOperand(rv.Binary.Left, addUse)
		addUsesFromOperand(rv.Binary.Right, addUse)
	case RValueCast:
		addUsesFromOperand(rv.Cast.Value, addUse)
	case RValueStructLit:
		for i := range rv.StructLit.Fields {
			addUsesFromOperand(rv.StructLit.Fields[i].Value, addUse)
		}
	case RValueArrayLit:
		for i := range rv.ArrayLit.Elems {
			addUsesFromOperand(rv.ArrayLit.Elems[i], addUse)
		}
	case RValueTupleLit:
		for i := range rv.TupleLit.Elems {
			addUsesFromOperand(rv.TupleLit.Elems[i], addUse)
		}
	case RValueField:
		addUsesFromOperand(rv.Field.Object, addUse)
	case RValueIndex:
		addUsesFromOperand(rv.Index.Object, addUse)
		addUsesFromOperand(rv.Index.Index, addUse)
	case RValueTagTest:
		addUsesFromOperand(rv.TagTest.Value, addUse)
	case RValueTagPayload:
		addUsesFromOperand(rv.TagPayload.Value, addUse)
	case RValueIterInit:
		addUsesFromOperand(rv.IterInit.Iterable, addUse)
	case RValueIterNext:
		addUsesFromOperand(rv.IterNext.Iter, addUse)
	case RValueTypeTest:
		addUsesFromOperand(rv.TypeTest.Value, addUse)
	case RValueHeirTest:
		addUsesFromOperand(rv.HeirTest.Value, addUse)
	}
}

func addUsesFromPlace(p Place, addUse func(LocalID), includeBase bool) {
	if includeBase && p.Kind == PlaceLocal {
		addUse(p.Local)
	}
	for _, proj := range p.Proj {
		if proj.Kind == PlaceProjIndex && proj.IndexLocal != NoLocalID {
			addUse(proj.IndexLocal)
		}
	}
}

func addUsesFromPlaceWrite(p Place, addUse func(LocalID)) {
	if len(p.Proj) == 0 {
		return
	}
	addUsesFromPlace(p, addUse, true)
}

func addDefFromPlace(p Place, addDef func(LocalID)) {
	if p.Kind == PlaceLocal && len(p.Proj) == 0 {
		addDef(p.Local)
	}
}

func buildAsyncStateUnion(m *Module, typesIn *types.Interner, symTable *symbols.Table, f *Func, pollFn *Func, variants []stateVariant) (types.TypeID, error) {
	if typesIn == nil || typesIn.Strings == nil {
		return types.NoTypeID, fmt.Errorf("mir: async: missing type interner strings")
	}
	name := fmt.Sprintf("__AsyncState$%s", f.Name)
	nameID := typesIn.Strings.Intern(name)
	stateID := typesIn.RegisterUnion(nameID, source.Span{})

	members := make([]types.UnionMember, 0, len(variants))
	for _, v := range variants {
		tagNameID := typesIn.Strings.Intern(v.name)
		payload := make([]types.TypeID, 0, len(v.locals))
		for _, localID := range v.locals {
			if pollFn == nil || localID == NoLocalID || int(localID) >= len(pollFn.Locals) {
				return types.NoTypeID, fmt.Errorf("mir: async: invalid local in state payload for %s", v.name)
			}
			payload = append(payload, pollFn.Locals[localID].Type)
		}
		members = append(members, types.UnionMember{Kind: types.UnionMemberTag, TagName: tagNameID, TagArgs: payload})
	}
	typesIn.SetUnionMembers(stateID, members)

	if m.Meta == nil {
		m.Meta = &ModuleMeta{}
	}
	if m.Meta.TagNames == nil {
		m.Meta.TagNames = make(map[symbols.SymbolID]string)
	}

	tagSymByName := make(map[string]symbols.SymbolID, len(variants))
	nextSym := nextSyntheticTagSym(m, symTable)
	for i := range variants {
		name := variants[i].name
		var symID symbols.SymbolID
		if symTable != nil && symTable.Symbols != nil && symTable.Strings != nil {
			nameID := symTable.Strings.Intern(name)
			symID = symTable.Symbols.New(&symbols.Symbol{Name: nameID, Kind: symbols.SymbolTag})
		} else {
			symID = nextSym
			nextSym++
		}
		variants[i].tagSym = symID
		tagSymByName[name] = symID
		if symID.IsValid() {
			m.Meta.TagNames[symID] = name
		}
	}

	if err := ensureTagLayout(m, typesIn, tagSymByName, stateID); err != nil {
		return types.NoTypeID, err
	}
	return stateID, nil
}

func nextSyntheticTagSym(m *Module, symTable *symbols.Table) symbols.SymbolID {
	maxSym := symbols.SymbolID(0)
	if symTable != nil && symTable.Symbols != nil {
		maxSym = symbols.SymbolID(symTable.Symbols.Len())
	}
	if m != nil && m.Meta != nil {
		for sym := range m.Meta.TagNames {
			if sym > maxSym {
				maxSym = sym
			}
		}
		for _, cases := range m.Meta.TagLayouts {
			for _, c := range cases {
				if c.TagSym > maxSym {
					maxSym = c.TagSym
				}
			}
		}
	}
	return maxSym + 1
}

func buildAsyncPollEntry(f *Func, stateLocal LocalID, variants []stateVariant) BlockID {
	if f == nil {
		return NoBlockID
	}
	entryBB := newBlock(f)
	defaultBB := newBlock(f)
	setBlockTerm(f, defaultBB, Terminator{Kind: TermUnreachable})

	appendInstr(f, entryBB, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: stateLocal},
		Callee: Callee{Kind: CalleeValue, Name: "__task_state"},
	}})

	cases := make([]SwitchTagCase, 0, len(variants))
	for i := range variants {
		variantBB := newBlock(f)
		cases = append(cases, SwitchTagCase{TagName: variants[i].name, Target: variantBB})

		for idx, localID := range variants[i].locals {
			appendInstr(f, variantBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: localID},
				Src: RValue{Kind: RValueTagPayload, TagPayload: TagPayload{
					Value:   Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
					TagName: variants[i].name,
					Index:   idx,
				}},
			}})
		}
		setBlockTerm(f, variantBB, Terminator{Kind: TermGoto, Goto: GotoTerm{Target: variants[i].resumeBB}})
	}

	setBlockTerm(f, entryBB, Terminator{Kind: TermSwitchTag, SwitchTag: SwitchTagTerm{
		Value:   Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
		Cases:   cases,
		Default: defaultBB,
	}})

	return entryBB
}

func buildAsyncPendingBlocks(f *Func, stateLocal LocalID, sites []awaitSite, variants []stateVariant) error {
	if f == nil {
		return nil
	}
	if len(variants) == 0 {
		return nil
	}
	for i := range sites {
		variantIdx := sites[i].stateIndex
		if variantIdx <= 0 || variantIdx >= len(variants) {
			return fmt.Errorf("mir: async: state index out of range")
		}
		pendingBB := newBlock(f)
		sites[i].pendingBB = pendingBB

		args := make([]Operand, 0, len(variants[variantIdx].locals))
		for _, localID := range variants[variantIdx].locals {
			args = append(args, operandForLocal(f, localID))
		}
		appendInstr(f, pendingBB, Instr{Kind: InstrCall, Call: CallInstr{
			HasDst: true,
			Dst:    Place{Local: stateLocal},
			Callee: Callee{Kind: CalleeSym, Sym: variants[variantIdx].tagSym, Name: variants[variantIdx].name},
			Args:   args,
		}})
		setBlockTerm(f, pendingBB, Terminator{Kind: TermAsyncYield, AsyncYield: AsyncYieldTerm{
			State: operandForLocal(f, stateLocal),
		}})

		pollBB := sites[i].pollBB
		if pollBB < 0 || int(pollBB) >= len(f.Blocks) {
			return fmt.Errorf("mir: async: poll block out of range")
		}
		bb := &f.Blocks[pollBB]
		if sites[i].pollInstr < 0 || sites[i].pollInstr >= len(bb.Instrs) {
			return fmt.Errorf("mir: async: poll instruction out of range")
		}
		pollInstr := &bb.Instrs[sites[i].pollInstr]
		if pollInstr.Kind != InstrPoll {
			return fmt.Errorf("mir: async: expected poll instruction in %s", f.Name)
		}
		pollInstr.Poll.PendBB = pendingBB
	}
	return nil
}

func rewriteAsyncReturns(f *Func, stateLocal LocalID) {
	if f == nil {
		return
	}
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		if bb.Term.Kind != TermReturn {
			continue
		}
		newTerm := Terminator{Kind: TermAsyncReturn, AsyncReturn: AsyncReturnTerm{
			State: operandForLocal(f, stateLocal),
		}}
		if bb.Term.Return.HasValue {
			newTerm.AsyncReturn.HasValue = true
			newTerm.AsyncReturn.Value = bb.Term.Return.Value
		}
		bb.Term = newTerm
	}
}

func buildAsyncConstructorState(f *Func, typesIn *types.Interner, semaRes *sema.Result, taskType, stateType types.TypeID, pollFnID FuncID, startVariant stateVariant) error {
	if f == nil {
		return nil
	}
	entry := newBlock(f)
	f.Entry = entry

	stateTmp := addLocal(f, "__state_init", stateType, localFlagsFor(typesIn, semaRes, stateType))
	taskTmp := addLocal(f, "__task", taskType, localFlagsFor(typesIn, semaRes, taskType))

	args := make([]Operand, 0, len(startVariant.locals))
	for _, localID := range startVariant.locals {
		args = append(args, operandForLocal(f, localID))
	}

	appendInstr(f, entry, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: stateTmp},
		Callee: Callee{Kind: CalleeSym, Sym: startVariant.tagSym, Name: startVariant.name},
		Args:   args,
	}})
	appendInstr(f, entry, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: taskTmp},
		Callee: Callee{Kind: CalleeValue, Name: "__task_create"},
		Args: []Operand{{
			Kind:  OperandConst,
			Type:  typesIn.Builtins().Int,
			Const: Const{Kind: ConstInt, Type: typesIn.Builtins().Int, IntValue: int64(pollFnID)},
		}, {
			Kind:  OperandMove,
			Place: Place{Local: stateTmp},
		}},
	}})
	setBlockTerm(f, entry, Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: Operand{Kind: OperandMove, Place: Place{Local: taskTmp}}}})
	return nil
}

func cloneSet(s localSet) localSet {
	if len(s) == 0 {
		return nil
	}
	out := make(localSet, len(s))
	for id := range s {
		out.add(id)
	}
	return out
}

func unionSet(dst localSet, src localSet) localSet {
	if dst == nil {
		dst = localSet{}
	}
	for id := range src {
		dst.add(id)
	}
	return dst
}

func subtractSet(src localSet, sub localSet) localSet {
	if len(src) == 0 {
		return nil
	}
	out := localSet{}
	for id := range src {
		if sub.has(id) {
			continue
		}
		out.add(id)
	}
	return out
}

func setEqual(a, b localSet) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if !b.has(id) {
			return false
		}
	}
	return true
}
