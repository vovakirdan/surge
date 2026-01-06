package mir

import (
	"fmt"
	"sort"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
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

// LowerAsyncSingleSuspend lowers async functions to a single-suspend state machine.
func LowerAsyncSingleSuspend(m *Module, semaRes *sema.Result, symTable *symbols.Table) error {
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
		if err := lowerAsyncSingleSuspendFunc(m, f, typesIn, semaRes, symTable); err != nil {
			return err
		}
	}
	return nil
}

func lowerAsyncSingleSuspendFunc(m *Module, f *Func, typesIn *types.Interner, semaRes *sema.Result, symTable *symbols.Table) error {
	awaitBlock, awaitIndex, awaitInstr, awaitCount := findAwait(f)
	if awaitCount != 1 {
		return fmt.Errorf("mir: async: J1 supports only single await in async body (%s)", f.Name)
	}
	if awaitBlock == NoBlockID || awaitIndex < 0 || awaitInstr == nil {
		return fmt.Errorf("mir: async: await not found in %s", f.Name)
	}

	payload := f.Result
	if payload == types.NoTypeID {
		payload = typesIn.Builtins().Nothing
	}

	taskType, err := taskTypeFor(typesIn, payload)
	if err != nil {
		return err
	}
	optionType, err := optionTypeFor(typesIn, payload)
	if err != nil {
		return err
	}

	tagSymByName := buildTagSymByName(m, symTable)
	if layoutErr := ensureTagLayout(m, typesIn, tagSymByName, optionType); layoutErr != nil {
		return layoutErr
	}
	someTagSym, err := optionSomeTagSym(m, optionType)
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
		Result:  optionType,
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

	awaitTaskLocal := addLocal(pollFn, "__await_task", awaitInstr.Await.Task.Type, localFlagsFor(typesIn, semaRes, awaitInstr.Await.Task.Type))
	retLocal := addLocal(pollFn, "__poll_ret", optionType, localFlagsFor(typesIn, semaRes, optionType))

	pendingBB := newBlock(pollFn)
	pollBB, afterBB, err := splitAwaitBlock(pollFn, awaitBlock, awaitIndex, awaitInstr, awaitTaskLocal, pendingBB)
	if err != nil {
		return err
	}

	paramLocals := paramLocalSet(pollFn, symTable)
	assignedBefore := localsAssignedInBlock(pollFn, awaitBlock)
	usedAfter := localsUsedFrom(pollFn, afterBB)

	if awaitInstr.Await.Dst.IsValid() {
		usedAfter.delete(awaitInstr.Await.Dst.Local)
	}

	definiteBefore := localSet{}
	for id := range paramLocals {
		definiteBefore.add(id)
	}
	for id := range assignedBefore {
		definiteBefore.add(id)
	}

	for id := range usedAfter {
		if !definiteBefore.has(id) {
			return fmt.Errorf("mir: async: local %s must be initialized before await in %s", pollFn.Locals[id].Name, f.Name)
		}
	}

	savedLocals := localSet{}
	for id := range usedAfter {
		savedLocals.add(id)
	}
	for id := range paramLocals {
		savedLocals.add(id)
	}
	savedLocals.add(awaitTaskLocal)

	stateType, fieldNameByLocal, err := buildAsyncStateType(typesIn, pollFn, awaitTaskLocal, awaitInstr.Await.Task.Type, savedLocals)
	if err != nil {
		return err
	}
	stateLocal := addLocal(pollFn, "__state", stateType, localFlagsFor(typesIn, semaRes, stateType))

	entryBB := newBlock(pollFn)
	startBB := newBlock(pollFn)
	resumeBB := newBlock(pollFn)
	pollFn.Entry = entryBB

	appendInstr(pollFn, entryBB, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: stateLocal},
		Callee: Callee{Kind: CalleeValue, Name: "__task_state"},
	}})

	stateTagLocal := addLocal(pollFn, "__state_tag", typesIn.Builtins().Int, LocalFlagCopy)
	condLocal := addLocal(pollFn, "__state_is_start", typesIn.Builtins().Bool, LocalFlagCopy)

	appendInstr(pollFn, entryBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: stateTagLocal},
		Src: RValue{Kind: RValueField, Field: FieldAccess{Object: Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}}, FieldName: "__state_tag"}},
	}})
	appendInstr(pollFn, entryBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: condLocal},
		Src: RValue{Kind: RValueBinaryOp, Binary: BinaryOp{Op: ast.ExprBinaryEq, Left: Operand{Kind: OperandCopy, Place: Place{Local: stateTagLocal}}, Right: Operand{Kind: OperandConst, Type: typesIn.Builtins().Int, Const: Const{Kind: ConstInt, Type: typesIn.Builtins().Int, IntValue: 0}}}},
	}})
	setBlockTerm(pollFn, entryBB, Terminator{Kind: TermIf, If: IfTerm{Cond: Operand{Kind: OperandCopy, Place: Place{Local: condLocal}}, Then: startBB, Else: resumeBB}})

	for id := range paramLocals {
		fieldName, ok := fieldNameByLocal[id]
		if !ok {
			continue
		}
		appendInstr(pollFn, startBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: id},
			Src: RValue{Kind: RValueField, Field: FieldAccess{Object: Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}}, FieldName: fieldName}},
		}})
	}
	setBlockTerm(pollFn, startBB, Terminator{Kind: TermGoto, Goto: GotoTerm{Target: origEntry}})

	for id := range savedLocals {
		fieldName, ok := fieldNameByLocal[id]
		if !ok {
			continue
		}
		appendInstr(pollFn, resumeBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: id},
			Src: RValue{Kind: RValueField, Field: FieldAccess{Object: Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}}, FieldName: fieldName}},
		}})
	}
	setBlockTerm(pollFn, resumeBB, Terminator{Kind: TermGoto, Goto: GotoTerm{Target: pollBB}})

	for id := range savedLocals {
		fieldName, ok := fieldNameByLocal[id]
		if !ok {
			continue
		}
		appendInstr(pollFn, pendingBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: stateLocal, Proj: []PlaceProj{{Kind: PlaceProjField, FieldName: fieldName}}},
			Src: RValue{Kind: RValueUse, Use: operandForLocal(pollFn, id)},
		}})
	}
	appendInstr(pollFn, pendingBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: stateLocal, Proj: []PlaceProj{{Kind: PlaceProjField, FieldName: "__state_tag"}}},
		Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandConst, Type: typesIn.Builtins().Int, Const: Const{Kind: ConstInt, Type: typesIn.Builtins().Int, IntValue: 1}}},
	}})
	setBlockTerm(pollFn, pendingBB, Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: Operand{Kind: OperandConst, Type: optionType, Const: Const{Kind: ConstNothing, Type: optionType}}}})

	wrapPollReturns(pollFn, retLocal, pendingBB, someTagSym, payload)

	if err := buildAsyncConstructor(m, f, typesIn, semaRes, taskType, stateType, pollFnID, fieldNameByLocal, paramLocals); err != nil {
		return err
	}

	f.IsAsync = false
	f.Result = taskType
	return nil
}

func findAwait(f *Func) (awaitBlock BlockID, awaitIndex int, awaitInstr *Instr, count int) {
	count = 0
	awaitBlock = NoBlockID
	awaitIndex = -1
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			if ins.Kind == InstrAwait {
				count++
				if count == 1 {
					awaitBlock = BlockID(bi) //nolint:gosec // bounded by block count
					awaitIndex = ii
					awaitInstr = ins
				}
			}
		}
	}
	return awaitBlock, awaitIndex, awaitInstr, count
}

func allocAsyncPollFunc(m *Module) (FuncID, error) {
	if m == nil {
		return NoFuncID, fmt.Errorf("mir: async: missing module")
	}
	maxID := FuncID(0)
	for id := range m.Funcs {
		if id > maxID {
			maxID = id
		}
	}
	return maxID + 1, nil
}

type stateField struct {
	name   string
	typeID types.TypeID
	local  LocalID
}

func buildAsyncStateType(typesIn *types.Interner, f *Func, awaitTaskLocal LocalID, awaitTaskType types.TypeID, savedLocals localSet) (types.TypeID, map[LocalID]string, error) {
	if typesIn == nil || typesIn.Strings == nil {
		return types.NoTypeID, nil, fmt.Errorf("mir: async: missing type interner strings")
	}
	name := fmt.Sprintf("__AsyncState$%s", f.Name)
	nameID := typesIn.Strings.Intern(name)
	stateID := typesIn.RegisterStruct(nameID, source.Span{})

	fields := []stateField{{name: "__state_tag", typeID: typesIn.Builtins().Int, local: NoLocalID}, {name: "__await_task", typeID: awaitTaskType, local: awaitTaskLocal}}
	for _, id := range savedLocals.sorted() {
		if id == awaitTaskLocal {
			continue
		}
		if int(id) < 0 || int(id) >= len(f.Locals) {
			continue
		}
		fields = append(fields, stateField{name: fmt.Sprintf("__l%d", id), typeID: f.Locals[id].Type, local: id})
	}

	structFields := make([]types.StructField, 0, len(fields))
	fieldNameByLocal := make(map[LocalID]string)
	for _, field := range fields {
		structFields = append(structFields, types.StructField{
			Name: typesIn.Strings.Intern(field.name),
			Type: field.typeID,
		})
		if field.local != NoLocalID {
			fieldNameByLocal[field.local] = field.name
		}
	}
	typesIn.SetStructFields(stateID, structFields)
	return stateID, fieldNameByLocal, nil
}

func buildAsyncConstructor(_ *Module, f *Func, typesIn *types.Interner, semaRes *sema.Result, taskType, stateType types.TypeID, pollFnID FuncID, fieldNameByLocal map[LocalID]string, paramLocals localSet) error {
	if f == nil {
		return nil
	}

	entry := newBlock(f)
	f.Entry = entry

	stateTmp := addLocal(f, "__state_init", stateType, localFlagsFor(typesIn, semaRes, stateType))
	taskTmp := addLocal(f, "__task", taskType, localFlagsFor(typesIn, semaRes, taskType))

	litFields := make([]StructLitField, 0, len(paramLocals)+1)
	litFields = append(litFields, StructLitField{
		Name:  "__state_tag",
		Value: Operand{Kind: OperandConst, Type: typesIn.Builtins().Int, Const: Const{Kind: ConstInt, Type: typesIn.Builtins().Int, IntValue: 0}},
	})
	for id := range paramLocals {
		fieldName, ok := fieldNameByLocal[id]
		if !ok {
			continue
		}
		litFields = append(litFields, StructLitField{Name: fieldName, Value: operandForLocal(f, id)})
	}

	appendInstr(f, entry, Instr{Kind: InstrAssign, Assign: AssignInstr{Dst: Place{Local: stateTmp}, Src: RValue{Kind: RValueStructLit, StructLit: StructLit{TypeID: stateType, Fields: litFields}}}})
	appendInstr(f, entry, Instr{Kind: InstrCall, Call: CallInstr{HasDst: true, Dst: Place{Local: taskTmp}, Callee: Callee{Kind: CalleeValue, Name: "__task_create"}, Args: []Operand{{Kind: OperandConst, Type: typesIn.Builtins().Int64, Const: Const{Kind: ConstInt, Type: typesIn.Builtins().Int64, IntValue: int64(pollFnID)}}, {Kind: OperandMove, Place: Place{Local: stateTmp}}}}})
	setBlockTerm(f, entry, Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: Operand{Kind: OperandMove, Place: Place{Local: taskTmp}}}})
	return nil
}

func buildTagSymByName(m *Module, symTable *symbols.Table) map[string]symbols.SymbolID {
	if symTable != nil && symTable.Symbols != nil && symTable.Strings != nil {
		out := make(map[string]symbols.SymbolID)
		maxSym := symTable.Symbols.Len()
		for id := symbols.SymbolID(1); id <= symbols.SymbolID(maxSym); id++ { //nolint:gosec // bounded by symbol table
			sym := symTable.Symbols.Get(id)
			if sym == nil || sym.Kind != symbols.SymbolTag || sym.Name == source.NoStringID {
				continue
			}
			name := symTable.Strings.MustLookup(sym.Name)
			if name != "" {
				out[name] = id
			}
		}
		return out
	}
	if m != nil && m.Meta != nil && len(m.Meta.TagNames) != 0 {
		out := make(map[string]symbols.SymbolID, len(m.Meta.TagNames))
		for sym, name := range m.Meta.TagNames {
			if name != "" {
				out[name] = sym
			}
		}
		return out
	}
	return nil
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

func optionSomeTagSym(m *Module, optionType types.TypeID) (symbols.SymbolID, error) {
	if m == nil || m.Meta == nil {
		return symbols.NoSymbolID, fmt.Errorf("mir: async: missing tag layouts")
	}
	cases := m.Meta.TagLayouts[optionType]
	for _, c := range cases {
		if c.TagName == "Some" {
			return c.TagSym, nil
		}
	}
	return symbols.NoSymbolID, fmt.Errorf("mir: async: missing Option::Some tag")
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

func optionTypeFor(typesIn *types.Interner, payload types.TypeID) (types.TypeID, error) {
	if typesIn == nil || typesIn.Strings == nil {
		return types.NoTypeID, fmt.Errorf("mir: async: missing type interner")
	}
	nameID := typesIn.Strings.Intern("Option")
	if id, ok := typesIn.FindUnionInstance(nameID, []types.TypeID{payload}); ok {
		return id, nil
	}
	if id, ok := typesIn.FindAliasInstance(nameID, []types.TypeID{payload}); ok {
		return id, nil
	}
	return types.NoTypeID, fmt.Errorf("mir: async: Option type not found for payload")
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

func splitAwaitBlock(f *Func, awaitBlock BlockID, awaitIndex int, awaitInstr *Instr, awaitTaskLocal LocalID, pendingBB BlockID) (pollBB, afterBB BlockID, err error) {
	if f == nil || awaitInstr == nil {
		return NoBlockID, NoBlockID, fmt.Errorf("mir: async: invalid await split")
	}
	if awaitBlock < 0 || int(awaitBlock) >= len(f.Blocks) {
		return NoBlockID, NoBlockID, fmt.Errorf("mir: async: await block out of range")
	}
	bb := &f.Blocks[awaitBlock]
	if awaitIndex < 0 || awaitIndex >= len(bb.Instrs) {
		return NoBlockID, NoBlockID, fmt.Errorf("mir: async: await index out of range")
	}

	prelude := append([]Instr(nil), bb.Instrs[:awaitIndex]...)
	after := append([]Instr(nil), bb.Instrs[awaitIndex+1:]...)
	origTerm := bb.Term

	pollBB = newBlock(f)
	afterBB = newBlock(f)

	bb.Instrs = prelude
	bb.Term = Terminator{Kind: TermGoto, Goto: GotoTerm{Target: pollBB}}

	appendInstr(f, pollBB, Instr{Kind: InstrAssign, Assign: AssignInstr{Dst: Place{Local: awaitTaskLocal}, Src: RValue{Kind: RValueUse, Use: awaitInstr.Await.Task}}})
	appendInstr(f, pollBB, Instr{Kind: InstrPoll, Poll: PollInstr{Dst: awaitInstr.Await.Dst, Task: Operand{Kind: OperandCopy, Place: Place{Local: awaitTaskLocal}}, ReadyBB: afterBB, PendBB: pendingBB}})
	setBlockTerm(f, pollBB, Terminator{Kind: TermNone})

	f.Blocks[afterBB].Instrs = after
	f.Blocks[afterBB].Term = origTerm

	return pollBB, afterBB, nil
}

func wrapPollReturns(f *Func, retLocal LocalID, pendingBB BlockID, someTagSym symbols.SymbolID, payload types.TypeID) {
	if f == nil {
		return
	}
	for bi := range f.Blocks {
		if BlockID(bi) == pendingBB { //nolint:gosec // bounded by block count
			continue
		}
		bb := &f.Blocks[bi]
		if bb.Term.Kind != TermReturn {
			continue
		}
		var arg Operand
		if bb.Term.Return.HasValue {
			arg = bb.Term.Return.Value
		} else {
			arg = Operand{Kind: OperandConst, Type: payload, Const: Const{Kind: ConstNothing, Type: payload}}
		}
		bb.Instrs = append(bb.Instrs, Instr{Kind: InstrCall, Call: CallInstr{HasDst: true, Dst: Place{Local: retLocal}, Callee: Callee{Kind: CalleeSym, Sym: someTagSym, Name: "Some"}, Args: []Operand{arg}}})
		bb.Term = Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: Operand{Kind: OperandCopy, Place: Place{Local: retLocal}}}}
	}
}
