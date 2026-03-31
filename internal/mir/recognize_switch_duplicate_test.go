package mir_test

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

func TestRecognizeSwitchTag_DuplicateTagNotConverted(t *testing.T) {
	typeInterner := types.NewInterner()
	boolType := typeInterner.Builtins().Bool
	intType := typeInterner.Builtins().Int

	f := &mir.Func{
		Name:  "test",
		Entry: 0,
		Locals: []mir.Local{
			{Name: "x", Type: intType},
			{Name: "tmp1", Type: boolType, Flags: mir.LocalFlagCopy},
			{Name: "tmp2", Type: boolType, Flags: mir.LocalFlagCopy},
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
									TagName: "Success",
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
									TagName: "Success",
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
			{ID: 2, Term: mir.Terminator{Kind: mir.TermReturn, Return: mir.ReturnTerm{HasValue: false}}},
			{ID: 3, Term: mir.Terminator{Kind: mir.TermReturn, Return: mir.ReturnTerm{HasValue: false}}},
			{ID: 4, Term: mir.Terminator{Kind: mir.TermReturn, Return: mir.ReturnTerm{HasValue: false}}},
		},
	}

	mir.RecognizeSwitchTag(f)

	if f.Blocks[0].Term.Kind != mir.TermIf {
		t.Fatalf("expected duplicate-tag chain to stay as if, got %v", f.Blocks[0].Term.Kind)
	}
}
