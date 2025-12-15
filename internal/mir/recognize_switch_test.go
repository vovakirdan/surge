package mir_test

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// TestRecognizeSwitchTag_TwoCases tests that a chain of two tag_tests is converted to switch_tag.
func TestRecognizeSwitchTag_TwoCases(t *testing.T) {
	// Create a function with pattern:
	// bb0: L1 = tag_test copy L0 is Some; if copy L1 then bb2 else bb1
	// bb1: L2 = tag_test copy L0 is nothing; if copy L2 then bb3 else bb4
	// bb2: return some value
	// bb3: return nothing value
	// bb4: unreachable

	typeInterner := types.NewInterner()
	boolType := typeInterner.Builtins().Bool
	intType := typeInterner.Builtins().Int

	f := &mir.Func{
		Name:  "test",
		Entry: 0,
		Locals: []mir.Local{
			{Name: "x", Type: intType},                               // L0: the value being matched
			{Name: "tmp1", Type: boolType, Flags: mir.LocalFlagCopy}, // L1: tag_test result 1
			{Name: "tmp2", Type: boolType, Flags: mir.LocalFlagCopy}, // L2: tag_test result 2
		},
		Blocks: []mir.Block{
			{
				ID: 0,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 1},
							Src: mir.RValue{
								Kind: mir.RValueTagTest,
								TagTest: mir.TagTest{
									Value:   mir.Operand{Kind: mir.OperandCopy, Place: mir.Place{Local: 0}},
									TagName: "Some",
								},
							},
						},
					},
				},
				Term: mir.Terminator{
					Kind: mir.TermIf,
					If: mir.IfTerm{
						Cond: mir.Operand{Kind: mir.OperandCopy, Place: mir.Place{Local: 1}},
						Then: 2,
						Else: 1,
					},
				},
			},
			{
				ID: 1,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 2},
							Src: mir.RValue{
								Kind: mir.RValueTagTest,
								TagTest: mir.TagTest{
									Value:   mir.Operand{Kind: mir.OperandCopy, Place: mir.Place{Local: 0}},
									TagName: "nothing",
								},
							},
						},
					},
				},
				Term: mir.Terminator{
					Kind: mir.TermIf,
					If: mir.IfTerm{
						Cond: mir.Operand{Kind: mir.OperandCopy, Place: mir.Place{Local: 2}},
						Then: 3,
						Else: 4,
					},
				},
			},
			{
				ID: 2,
				Term: mir.Terminator{
					Kind:   mir.TermReturn,
					Return: mir.ReturnTerm{HasValue: false},
				},
			},
			{
				ID: 3,
				Term: mir.Terminator{
					Kind:   mir.TermReturn,
					Return: mir.ReturnTerm{HasValue: false},
				},
			},
			{
				ID: 4,
				Term: mir.Terminator{
					Kind:        mir.TermUnreachable,
					Unreachable: struct{}{},
				},
			},
		},
	}

	mir.RecognizeSwitchTag(f)

	// bb0 should now have switch_tag terminator
	if f.Blocks[0].Term.Kind != mir.TermSwitchTag {
		t.Errorf("expected TermSwitchTag for bb0, got %v", f.Blocks[0].Term.Kind)
	}

	switchTerm := f.Blocks[0].Term.SwitchTag

	// Should have 2 cases
	if len(switchTerm.Cases) != 2 {
		t.Errorf("expected 2 cases, got %d", len(switchTerm.Cases))
	}

	// Check cases
	if switchTerm.Cases[0].TagName != "Some" {
		t.Errorf("expected first case to be Some, got %s", switchTerm.Cases[0].TagName)
	}
	if switchTerm.Cases[0].Target != 2 {
		t.Errorf("expected Some case to target bb2, got bb%d", switchTerm.Cases[0].Target)
	}

	if switchTerm.Cases[1].TagName != "nothing" {
		t.Errorf("expected second case to be nothing, got %s", switchTerm.Cases[1].TagName)
	}
	if switchTerm.Cases[1].Target != 3 {
		t.Errorf("expected nothing case to target bb3, got bb%d", switchTerm.Cases[1].Target)
	}

	// Check default
	if switchTerm.Default != 4 {
		t.Errorf("expected default to be bb4, got bb%d", switchTerm.Default)
	}

	// bb0 should have no instructions (tag_test removed)
	if len(f.Blocks[0].Instrs) != 0 {
		t.Errorf("expected 0 instructions in bb0, got %d", len(f.Blocks[0].Instrs))
	}
}

// TestRecognizeSwitchTag_NoPattern tests that blocks without the pattern are not changed.
func TestRecognizeSwitchTag_NoPattern(t *testing.T) {
	typeInterner := types.NewInterner()
	boolType := typeInterner.Builtins().Bool

	// Block with if but no tag_test
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
					Kind:   mir.TermReturn,
					Return: mir.ReturnTerm{HasValue: false},
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

	mir.RecognizeSwitchTag(f)

	// Should still be if, not switch_tag
	if f.Blocks[0].Term.Kind != mir.TermIf {
		t.Errorf("expected TermIf for bb0 (no pattern), got %v", f.Blocks[0].Term.Kind)
	}
}

// TestRecognizeSwitchTag_SingleTagTest tests that a single tag_test is not converted.
func TestRecognizeSwitchTag_SingleTagTest(t *testing.T) {
	// Only one tag_test should not trigger conversion (need at least 2 cases)
	typeInterner := types.NewInterner()
	boolType := typeInterner.Builtins().Bool
	intType := typeInterner.Builtins().Int

	f := &mir.Func{
		Name:  "test",
		Entry: 0,
		Locals: []mir.Local{
			{Name: "x", Type: intType},
			{Name: "tmp1", Type: boolType, Flags: mir.LocalFlagCopy},
		},
		Blocks: []mir.Block{
			{
				ID: 0,
				Instrs: []mir.Instr{
					{
						Kind: mir.InstrAssign,
						Assign: mir.AssignInstr{
							Dst: mir.Place{Local: 1},
							Src: mir.RValue{
								Kind: mir.RValueTagTest,
								TagTest: mir.TagTest{
									Value:   mir.Operand{Kind: mir.OperandCopy, Place: mir.Place{Local: 0}},
									TagName: "Some",
								},
							},
						},
					},
				},
				Term: mir.Terminator{
					Kind: mir.TermIf,
					If: mir.IfTerm{
						Cond: mir.Operand{Kind: mir.OperandCopy, Place: mir.Place{Local: 1}},
						Then: 1,
						Else: 2,
					},
				},
			},
			{
				ID: 1,
				Term: mir.Terminator{
					Kind:   mir.TermReturn,
					Return: mir.ReturnTerm{HasValue: false},
				},
			},
			{
				ID: 2,
				// This block does NOT have a tag_test pattern
				Term: mir.Terminator{
					Kind:        mir.TermUnreachable,
					Unreachable: struct{}{},
				},
			},
		},
	}

	mir.RecognizeSwitchTag(f)

	// Should still be if because we need at least 2 cases
	if f.Blocks[0].Term.Kind != mir.TermIf {
		t.Errorf("expected TermIf for bb0 (single tag_test), got %v", f.Blocks[0].Term.Kind)
	}
}

// TestRecognizeSwitchTag_NilFunction tests that nil function doesn't panic.
func TestRecognizeSwitchTag_NilFunction(t *testing.T) {
	// Should not panic
	mir.RecognizeSwitchTag(nil)
}

// TestRecognizeSwitchTag_EmptyFunction tests that empty function doesn't panic.
func TestRecognizeSwitchTag_EmptyFunction(t *testing.T) {
	f := &mir.Func{
		Name:   "test",
		Entry:  0,
		Blocks: []mir.Block{},
	}

	// Should not panic
	mir.RecognizeSwitchTag(f)
}
