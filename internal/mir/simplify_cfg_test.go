package mir_test

import (
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/lexer"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// TestSimplifyCFG_TrivialGoto tests that trivial goto blocks are removed.
func TestSimplifyCFG_TrivialGoto(t *testing.T) {
	// Create a function with a trivial goto block in the middle:
	// bb0 (with instruction) -> bb1 (trivial goto) -> bb2 (return)
	typeInterner := types.NewInterner()
	intType := typeInterner.Builtins().Int

	f := &mir.Func{
		Name:  "test",
		Entry: 0,
		Locals: []mir.Local{
			{Name: "x", Type: intType, Flags: mir.LocalFlagCopy},
		},
		Blocks: []mir.Block{
			{
				ID: 0,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 0},
							Src: mir.RValue{
								Kind: mir.RValueUse,
								Use: mir.Operand{
									Kind:  mir.OperandConst,
									Const: mir.Const{Kind: mir.ConstInt, IntValue: 1},
								},
							},
						},
					},
				},
				Term: mir.Terminator{
					Kind: mir.TermGoto,
					Goto: mir.GotoTerm{Target: 1},
				},
			},
			{
				ID: 1,
				// No instructions - trivial goto
				Term: mir.Terminator{
					Kind: mir.TermGoto,
					Goto: mir.GotoTerm{Target: 2},
				},
			},
			{
				ID: 2,
				Term: mir.Terminator{
					Kind:   mir.TermReturn,
					Return: mir.ReturnTerm{HasValue: false},
				},
			},
		},
	}

	mir.SimplifyCFG(f)

	// Should have 2 blocks now (bb1 removed)
	if len(f.Blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(f.Blocks))
	}

	// bb0 should now go directly to bb1 (the old bb2)
	if f.Blocks[0].Term.Kind != mir.TermGoto {
		t.Errorf("expected TermGoto for bb0, got %v", f.Blocks[0].Term.Kind)
	}
	if f.Blocks[0].Term.Goto.Target != 1 {
		t.Errorf("expected bb0 to target bb1, got bb%d", f.Blocks[0].Term.Goto.Target)
	}

	// bb1 should be the return block
	if f.Blocks[1].Term.Kind != mir.TermReturn {
		t.Errorf("expected TermReturn for bb1, got %v", f.Blocks[1].Term.Kind)
	}
}

// TestSimplifyCFG_GotoChain tests that chains of goto blocks are collapsed.
func TestSimplifyCFG_GotoChain(t *testing.T) {
	// Create a chain: bb0 (with instr) -> bb1 -> bb2 -> bb3 (all trivial gotos except bb0 and bb3)
	typeInterner := types.NewInterner()
	intType := typeInterner.Builtins().Int

	f := &mir.Func{
		Name:  "test",
		Entry: 0,
		Locals: []mir.Local{
			{Name: "x", Type: intType, Flags: mir.LocalFlagCopy},
		},
		Blocks: []mir.Block{
			{
				ID: 0,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 0},
							Src: mir.RValue{
								Kind: mir.RValueUse,
								Use: mir.Operand{
									Kind:  mir.OperandConst,
									Const: mir.Const{Kind: mir.ConstInt, IntValue: 1},
								},
							},
						},
					},
				},
				Term: mir.Terminator{
					Kind: mir.TermGoto,
					Goto: mir.GotoTerm{Target: 1},
				},
			},
			{
				ID: 1,
				Term: mir.Terminator{
					Kind: mir.TermGoto,
					Goto: mir.GotoTerm{Target: 2},
				},
			},
			{
				ID: 2,
				Term: mir.Terminator{
					Kind: mir.TermGoto,
					Goto: mir.GotoTerm{Target: 3},
				},
			},
			{
				ID: 3,
				Term: mir.Terminator{
					Kind:   mir.TermReturn,
					Return: mir.ReturnTerm{HasValue: false},
				},
			},
		},
	}

	mir.SimplifyCFG(f)

	// Should have 2 blocks (bb0 -> bb3, rest removed)
	if len(f.Blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(f.Blocks))
	}

	// bb0 should go directly to the return block
	if f.Blocks[0].Term.Goto.Target != 1 {
		t.Errorf("expected bb0 to target bb1, got bb%d", f.Blocks[0].Term.Goto.Target)
	}
}

// TestSimplifyCFG_UnreachableBlocks tests that unreachable blocks are removed.
func TestSimplifyCFG_UnreachableBlocks(t *testing.T) {
	// Create a function with an unreachable block
	f := &mir.Func{
		Name:  "test",
		Entry: 0,
		Blocks: []mir.Block{
			{
				ID: 0,
				Term: mir.Terminator{
					Kind:   mir.TermReturn,
					Return: mir.ReturnTerm{HasValue: false},
				},
			},
			{
				ID: 1,
				// Unreachable block
				Term: mir.Terminator{
					Kind:        mir.TermUnreachable,
					Unreachable: struct{}{},
				},
			},
		},
	}

	mir.SimplifyCFG(f)

	// Should have 1 block (unreachable removed)
	if len(f.Blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(f.Blocks))
	}
}

// TestSimplifyCFG_IfBranches tests that trivial goto in if branches are simplified.
func TestSimplifyCFG_IfBranches(t *testing.T) {
	// Create: bb0 (if) -> bb1 (trivial goto) -> bb3 (return)
	//                  -> bb2 (trivial goto) -> bb3
	typeInterner := types.NewInterner()
	boolType := typeInterner.Builtins().Bool

	f := &mir.Func{
		Name:  "test",
		Entry: 0,
		Locals: []mir.Local{
			{Name: "cond", Type: boolType, Flags: mir.LocalFlagCopy},
		},
		Blocks: []mir.Block{
			{
				ID: 0,
				Term: mir.Terminator{
					Kind: mir.TermIf,
					If: mir.IfTerm{
						Cond: mir.Operand{Kind: mir.OperandCopy, Place: mir.Place{Local: 0}},
						Then: 1,
						Else: 2,
					},
				},
			},
			{
				ID: 1,
				Term: mir.Terminator{
					Kind: mir.TermGoto,
					Goto: mir.GotoTerm{Target: 3},
				},
			},
			{
				ID: 2,
				Term: mir.Terminator{
					Kind: mir.TermGoto,
					Goto: mir.GotoTerm{Target: 3},
				},
			},
			{
				ID: 3,
				Term: mir.Terminator{
					Kind:   mir.TermReturn,
					Return: mir.ReturnTerm{HasValue: false},
				},
			},
		},
	}

	mir.SimplifyCFG(f)

	// Should have 2 blocks (bb0 and bb3)
	if len(f.Blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(f.Blocks))
	}

	// bb0's if should now target bb1 (old bb3) for both branches
	if f.Blocks[0].Term.Kind != mir.TermIf {
		t.Errorf("expected TermIf for bb0")
	}
	if f.Blocks[0].Term.If.Then != 1 {
		t.Errorf("expected then to be bb1, got bb%d", f.Blocks[0].Term.If.Then)
	}
	if f.Blocks[0].Term.If.Else != 1 {
		t.Errorf("expected else to be bb1, got bb%d", f.Blocks[0].Term.If.Else)
	}
}

// TestSimplifyCFG_PreservesSemantics tests that SimplifyCFG preserves semantics
// using a real program through the full pipeline.
func TestSimplifyCFG_PreservesSemantics(t *testing.T) {
	// Simple if-else that generates trivial goto blocks
	src := `
fn test(x: int) -> int {
    if x > 0 {
        return 1;
    } else {
        return 0;
    }
}
`
	mirMod, typeInterner, err := parseAndLowerMIRWithSimplify(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if mirMod == nil {
		t.Fatal("MIR module is nil")
	}

	// Validation should pass after simplification
	err = mir.Validate(mirMod, typeInterner)
	if err != nil {
		t.Errorf("validation failed after SimplifyCFG: %v", err)
	}

	// Check that there are no trivial goto blocks remaining
	for _, f := range mirMod.Funcs {
		for i := range f.Blocks {
			bb := &f.Blocks[i]
			if len(bb.Instrs) == 0 && bb.Term.Kind == mir.TermGoto {
				t.Errorf("function %s still has trivial goto block at bb%d", f.Name, i)
			}
		}
	}
}

// TestSimplifyCFG_EmptyFunction tests that empty functions don't panic.
func TestSimplifyCFG_EmptyFunction(t *testing.T) {
	f := &mir.Func{
		Name:   "test",
		Entry:  0,
		Blocks: []mir.Block{},
	}

	// Should not panic
	mir.SimplifyCFG(f)

	if len(f.Blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(f.Blocks))
	}
}

// TestSimplifyCFG_NilFunction tests that nil functions don't panic.
func TestSimplifyCFG_NilFunction(t *testing.T) {
	// Should not panic
	mir.SimplifyCFG(nil)
}

// parseAndLowerMIRWithSimplify parses source code, lowers to MIR, and runs SimplifyCFG.
func parseAndLowerMIRWithSimplify(t *testing.T, src string) (*mir.Module, *types.Interner, error) {
	t.Helper()

	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)

	sharedStrings := source.NewInterner()
	typeInterner := types.NewInterner()

	bag := diag.NewBag(100)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, sharedStrings)

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 100,
	}

	result := parser.ParseFile(context.Background(), fs, lx, builder, opts)
	if bag.HasErrors() {
		for _, d := range bag.Items() {
			t.Logf("parse error: %v", d)
		}
		return nil, nil, nil
	}

	// Run symbols resolution
	symbolsRes := symbols.ResolveFile(builder, result.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "test",
		FilePath:   "test.sg",
	})

	// Create instantiation map for sema
	instMap := mono.NewInstantiationMap()

	// Run sema
	semaOpts := sema.Options{
		Reporter:       &diag.BagReporter{Bag: bag},
		Symbols:        &symbolsRes,
		Types:          typeInterner,
		Instantiations: mono.NewInstantiationMapRecorder(instMap),
	}
	semaRes := sema.Check(context.Background(), builder, result.File, semaOpts)

	if bag.HasErrors() {
		for _, d := range bag.Items() {
			t.Logf("sema error: %v", d)
		}
		return nil, nil, nil
	}

	// Lower to HIR
	hirModule, err := hir.Lower(context.Background(), builder, result.File, &semaRes, &symbolsRes)
	if err != nil {
		return nil, nil, err
	}

	// Monomorphize
	monoMod, err := mono.MonomorphizeModule(hirModule, instMap, &semaRes, mono.Options{})
	if err != nil {
		return nil, nil, err
	}

	// Lower to MIR
	mirMod, err := mir.LowerModule(monoMod, &semaRes)
	if err != nil {
		return nil, nil, err
	}

	// Run SimplifyCFG on all functions
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
	}

	return mirMod, typeInterner, nil
}
