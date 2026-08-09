package vm_test

import (
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
	"surge/internal/vm"
)

// frozenLayouts freezes the physical layouts a hand-built module needs.
//
// A module the compiler produces carries these already. One assembled here does
// not, and a union has to be asked for its physical shape now that a value is
// its own storage rather than a pointer to a box -- so a fixture without them
// fails at the first tag read rather than at construction, which reads like a
// VM defect and is not one.
func frozenLayouts(intTy, unionType types.TypeID, typesIn *types.Interner) *layout.Registry {
	engine := layout.New(layout.X86_64LinuxGNU(), typesIn)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{intTy, unionType})
	if err != nil {
		panic("tag fixture layouts must freeze: " + err.Error())
	}
	return registry
}

func buildTagMatchModule(tagName string, tagSym symbols.SymbolID, makeTag bool) (*mir.Module, *types.Interner) {
	typesIn := types.NewInterner()
	intTy := typesIn.Builtins().Int

	// The arm name is interned rather than spelled as a bare id: a union is
	// asked for its arm names by name now, so an id with no table behind it
	// fails at the first tag read instead of here.
	typesIn.Strings = source.NewInterner()
	unionType := typesIn.RegisterUnion(typesIn.Strings.Intern("TagUnion"), source.Span{})
	typesIn.SetUnionMembers(unionType, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: typesIn.Strings.Intern(tagName), TagArgs: []types.TypeID{intTy}},
		{Kind: types.UnionMemberNothing, Type: typesIn.Builtins().Nothing},
	})

	mainSym := symbols.SymbolID(10)
	mainID := mir.FuncID(1)
	startID := mir.FuncID(2)

	mainFn := &mir.Func{
		ID:     mainID,
		Sym:    mainSym,
		Name:   "main",
		Result: intTy,
		Locals: []mir.Local{
			{Type: unionType, Name: "x"},
			{Type: intTy, Flags: mir.LocalFlagCopy, Name: "result"},
			{Type: unionType, Name: "__cmp"},
		},
		Entry: 0,
		Blocks: []mir.Block{
			{
				ID: 0,
				Instrs: []mir.Instr{
					func() mir.Instr {
						if makeTag {
							return mir.Instr{
								Kind: mir.InstrCall,
								Call: mir.CallInstr{
									HasDst: true,
									Dst:    mir.Place{Local: 0},
									Callee: mir.Callee{Kind: mir.CalleeSym, Sym: tagSym, Name: tagName},
									Args: []mir.Operand{
										{Kind: mir.OperandConst, Type: intTy, Const: mir.Const{Kind: mir.ConstInt, Type: intTy, IntValue: 1}},
									},
								},
							}
						}
						return mir.Instr{
							Kind: mir.InstrAssign,
							Assign: mir.AssignInstr{
								Dst: mir.Place{Local: 0},
								Src: mir.RValue{Kind: mir.RValueUse, Use: mir.Operand{Kind: mir.OperandConst, Type: unionType, Const: mir.Const{Kind: mir.ConstNothing, Type: unionType}}},
							},
						}
					}(),
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 2},
							Src: mir.RValue{Kind: mir.RValueUse, Use: mir.Operand{Kind: mir.OperandMove, Type: unionType, Place: mir.Place{Local: 0}}},
						},
					},
				},
				Term: mir.Terminator{
					Kind: mir.TermSwitchTag,
					SwitchTag: mir.SwitchTagTerm{
						Value:   mir.Operand{Kind: mir.OperandCopy, Type: unionType, Place: mir.Place{Local: 2}},
						Cases:   []mir.SwitchTagCase{{TagName: tagName, Target: 1}, {TagName: "nothing", Target: 2}},
						Default: 3,
					},
				},
			},
			{
				ID: 1,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 1},
							Src: mir.RValue{
								Kind:       mir.RValueTagPayload,
								TagPayload: mir.TagPayload{Value: mir.Operand{Kind: mir.OperandCopy, Type: unionType, Place: mir.Place{Local: 2}}, TagName: tagName, Index: 0},
							},
						},
					},
				},
				Term: mir.Terminator{Kind: mir.TermReturn, Return: mir.ReturnTerm{HasValue: true, Value: mir.Operand{Kind: mir.OperandCopy, Type: intTy, Place: mir.Place{Local: 1}}}},
			},
			{
				ID: 2,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 1},
							Src: mir.RValue{Kind: mir.RValueUse, Use: mir.Operand{Kind: mir.OperandConst, Type: intTy, Const: mir.Const{Kind: mir.ConstInt, Type: intTy, IntValue: 0}}},
						},
					},
				},
				Term: mir.Terminator{Kind: mir.TermReturn, Return: mir.ReturnTerm{HasValue: true, Value: mir.Operand{Kind: mir.OperandCopy, Type: intTy, Place: mir.Place{Local: 1}}}},
			},
			{ID: 3, Term: mir.Terminator{Kind: mir.TermUnreachable}},
		},
	}

	startFn := &mir.Func{
		ID:     startID,
		Sym:    symbols.NoSymbolID,
		Name:   "__surge_start",
		Result: types.NoTypeID,
		Locals: []mir.Local{
			{Type: intTy, Flags: mir.LocalFlagCopy, Name: "ret"},
			{Type: intTy, Flags: mir.LocalFlagCopy, Name: "code"},
		},
		Entry: 0,
		Blocks: []mir.Block{
			{
				ID: 0,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrCall,
						Call: mir.CallInstr{
							HasDst: true,
							Dst:    mir.Place{Local: 0},
							Callee: mir.Callee{Kind: mir.CalleeSym, Sym: mainSym, Name: "main"},
						},
					},
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 1},
							Src: mir.RValue{Kind: mir.RValueUse, Use: mir.Operand{Kind: mir.OperandCopy, Type: intTy, Place: mir.Place{Local: 0}}},
						},
					},
					{
						Kind: mir.InstrCall,
						Call: mir.CallInstr{
							HasDst: false,
							Callee: mir.Callee{Kind: mir.CalleeSym, Sym: symbols.NoSymbolID, Name: "rt_exit"},
							Args:   []mir.Operand{{Kind: mir.OperandCopy, Type: intTy, Place: mir.Place{Local: 1}}},
						},
					},
				},
				Term: mir.Terminator{Kind: mir.TermReturn},
			},
		},
	}

	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			mainID:  mainFn,
			startID: startFn,
		},
		FuncBySym: map[symbols.SymbolID]mir.FuncID{
			mainSym: mainID,
		},
		Meta: &mir.ModuleMeta{
			Layouts: frozenLayouts(intTy, unionType, typesIn),
			TagLayouts: map[types.TypeID][]mir.TagCaseMeta{
				unionType: {
					{TagName: tagName, TagSym: tagSym, PayloadTypes: []types.TypeID{intTy}},
					{TagName: "nothing"},
				},
			},
		},
	}

	return mod, typesIn
}

func TestVMTagsSwitchTagSome(t *testing.T) {
	requireVMBackend(t)
	tagSym := symbols.SymbolID(100)
	m, typesIn := buildTagMatchModule("Some", tagSym, true)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(m, rt, nil, typesIn, nil)
	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr)
	}
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestVMTagsSwitchTagNothing(t *testing.T) {
	requireVMBackend(t)
	tagSym := symbols.SymbolID(100)
	m, typesIn := buildTagMatchModule("Some", tagSym, false)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(m, rt, nil, typesIn, nil)
	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMTagsTagPayloadMismatchPanics(t *testing.T) {
	requireVMBackend(t)
	typesIn := types.NewInterner()
	intTy := typesIn.Builtins().Int
	tagName := "Some"
	// The arm name is interned rather than spelled as a bare id: a union is
	// asked for its arm names by name now, so an id with no table behind it
	// fails at the first tag read instead of here.
	typesIn.Strings = source.NewInterner()
	unionType := typesIn.RegisterUnion(typesIn.Strings.Intern("TagUnion"), source.Span{})
	typesIn.SetUnionMembers(unionType, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: typesIn.Strings.Intern(tagName), TagArgs: []types.TypeID{intTy}},
		{Kind: types.UnionMemberNothing, Type: typesIn.Builtins().Nothing},
	})

	tagSym := symbols.SymbolID(100)
	mainSym := symbols.SymbolID(10)
	mainID := mir.FuncID(1)
	startID := mir.FuncID(2)

	mainFn := &mir.Func{
		ID:     mainID,
		Sym:    mainSym,
		Name:   "main",
		Result: intTy,
		Locals: []mir.Local{
			{Type: unionType, Name: "x"},
			{Type: intTy, Flags: mir.LocalFlagCopy, Name: "out"},
		},
		Entry: 0,
		Blocks: []mir.Block{
			{
				ID: 0,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrCall,
						Call: mir.CallInstr{
							HasDst: true,
							Dst:    mir.Place{Local: 0},
							Callee: mir.Callee{Kind: mir.CalleeSym, Sym: tagSym, Name: tagName},
							Args:   []mir.Operand{{Kind: mir.OperandConst, Type: intTy, Const: mir.Const{Kind: mir.ConstInt, Type: intTy, IntValue: 1}}},
						},
					},
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 1},
							Src: mir.RValue{
								Kind:       mir.RValueTagPayload,
								TagPayload: mir.TagPayload{Value: mir.Operand{Kind: mir.OperandCopy, Type: unionType, Place: mir.Place{Local: 0}}, TagName: "nothing", Index: 0},
							},
						},
					},
				},
				Term: mir.Terminator{Kind: mir.TermReturn, Return: mir.ReturnTerm{HasValue: true, Value: mir.Operand{Kind: mir.OperandCopy, Type: intTy, Place: mir.Place{Local: 1}}}},
			},
		},
	}

	startFn := &mir.Func{
		ID:     startID,
		Sym:    symbols.NoSymbolID,
		Name:   "__surge_start",
		Result: types.NoTypeID,
		Locals: []mir.Local{{Type: intTy, Flags: mir.LocalFlagCopy, Name: "ret"}},
		Entry:  0,
		Blocks: []mir.Block{{ID: 0, Instrs: []mir.Instr{{Kind: mir.InstrCall, Call: mir.CallInstr{HasDst: true, Dst: mir.Place{Local: 0}, Callee: mir.Callee{Kind: mir.CalleeSym, Sym: mainSym, Name: "main"}}}, {Kind: mir.InstrCall, Call: mir.CallInstr{HasDst: false, Callee: mir.Callee{Kind: mir.CalleeSym, Sym: symbols.NoSymbolID, Name: "rt_exit"}, Args: []mir.Operand{{Kind: mir.OperandConst, Type: intTy, Const: mir.Const{Kind: mir.ConstInt, Type: intTy, IntValue: 0}}}}}}, Term: mir.Terminator{Kind: mir.TermReturn}}},
	}

	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{mainID: mainFn, startID: startFn},
		FuncBySym: map[symbols.SymbolID]mir.FuncID{
			mainSym: mainID,
		},
		Meta: &mir.ModuleMeta{
			Layouts: frozenLayouts(intTy, unionType, typesIn),
			TagLayouts: map[types.TypeID][]mir.TagCaseMeta{
				unionType: {
					{TagName: tagName, TagSym: tagSym, PayloadTypes: []types.TypeID{intTy}},
					{TagName: "nothing"},
				},
			},
		},
	}

	rt := vm.NewTestRuntime(nil, "")
	_, vmErr := runVM(mod, rt, nil, typesIn, nil)
	if vmErr == nil {
		t.Fatal("expected panic, got nil")
	}
	if vmErr.Code != vm.PanicTagPayloadTagMismatch {
		t.Fatalf("expected %v, got %v", vm.PanicTagPayloadTagMismatch, vmErr.Code)
	}
}

func TestVMTagsSwitchOnNonTagPanics(t *testing.T) {
	requireVMBackend(t)
	typesIn := types.NewInterner()
	intTy := typesIn.Builtins().Int

	mainSym := symbols.SymbolID(10)
	mainID := mir.FuncID(1)
	startID := mir.FuncID(2)

	mainFn := &mir.Func{
		ID:     mainID,
		Sym:    mainSym,
		Name:   "main",
		Result: intTy,
		Locals: []mir.Local{
			{Type: intTy, Flags: mir.LocalFlagCopy, Name: "x"},
		},
		Entry: 0,
		Blocks: []mir.Block{
			{
				ID: 0,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 0},
							Src: mir.RValue{Kind: mir.RValueUse, Use: mir.Operand{Kind: mir.OperandConst, Type: intTy, Const: mir.Const{Kind: mir.ConstInt, Type: intTy, IntValue: 1}}},
						},
					},
				},
				Term: mir.Terminator{
					Kind: mir.TermSwitchTag,
					SwitchTag: mir.SwitchTagTerm{
						Value:   mir.Operand{Kind: mir.OperandCopy, Type: intTy, Place: mir.Place{Local: 0}},
						Cases:   []mir.SwitchTagCase{{TagName: "Some", Target: 1}},
						Default: 1,
					},
				},
			},
			{ID: 1, Term: mir.Terminator{Kind: mir.TermReturn, Return: mir.ReturnTerm{HasValue: true, Value: mir.Operand{Kind: mir.OperandConst, Type: intTy, Const: mir.Const{Kind: mir.ConstInt, Type: intTy, IntValue: 0}}}}},
		},
	}

	startFn := &mir.Func{
		ID:     startID,
		Sym:    symbols.NoSymbolID,
		Name:   "__surge_start",
		Result: types.NoTypeID,
		Locals: []mir.Local{{Type: intTy, Flags: mir.LocalFlagCopy, Name: "ret"}},
		Entry:  0,
		Blocks: []mir.Block{{ID: 0, Instrs: []mir.Instr{{Kind: mir.InstrCall, Call: mir.CallInstr{HasDst: true, Dst: mir.Place{Local: 0}, Callee: mir.Callee{Kind: mir.CalleeSym, Sym: mainSym, Name: "main"}}}, {Kind: mir.InstrCall, Call: mir.CallInstr{HasDst: false, Callee: mir.Callee{Kind: mir.CalleeSym, Sym: symbols.NoSymbolID, Name: "rt_exit"}, Args: []mir.Operand{{Kind: mir.OperandConst, Type: intTy, Const: mir.Const{Kind: mir.ConstInt, Type: intTy, IntValue: 0}}}}}}, Term: mir.Terminator{Kind: mir.TermReturn}}},
	}

	mod := &mir.Module{
		Funcs:     map[mir.FuncID]*mir.Func{mainID: mainFn, startID: startFn},
		FuncBySym: map[symbols.SymbolID]mir.FuncID{mainSym: mainID},
	}

	rt := vm.NewTestRuntime(nil, "")
	_, vmErr := runVM(mod, rt, nil, typesIn, nil)
	if vmErr == nil {
		t.Fatal("expected panic, got nil")
	}
	if vmErr.Code != vm.PanicSwitchTagOnNonTag {
		t.Fatalf("expected %v, got %v", vm.PanicSwitchTagOnNonTag, vmErr.Code)
	}
}

func TestVMTagsOptionMatchNothingFromSource(t *testing.T) {
	sourceCode := `fn unwrap_or_zero(x: int?) -> int {
    return compare x {
        Some(v) => v;
        nothing => 0;
    };
}

@entrypoint
fn main() -> int {
    let x: int? = nothing;
    return unwrap_or_zero(x);
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.exitCode)
	}
}
