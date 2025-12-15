package mir_test

import (
	"context"
	"strings"
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

// TestValidate_ValidPrograms tests that valid programs pass validation.
func TestValidate_ValidPrograms(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "simple_function",
			src: `fn main() -> nothing {
				return;
			}`,
		},
		{
			name: "function_with_return",
			src: `fn add(a: int, b: int) -> int {
				return a + b;
			}`,
		},
		{
			name: "if_statement",
			src: `fn test(x: int) -> int {
				if x > 0 {
					return 1;
				} else {
					return 0;
				}
			}`,
		},
		{
			name: "while_loop",
			src: `fn test() -> nothing {
				let mut i = 0;
				while i < 10 {
					i = i + 1;
				}
				return;
			}`,
		},
		{
			name: "mutable_ref_drop",
			src: `fn main() -> nothing {
				let mut x: int = 1;
				let r: &mut int = &mut x;
				@drop r;
				x = 2;
				return;
			}`,
		},
		{
			name: "immutable_ref_drop",
			src: `fn main() -> nothing {
				let x: int = 1;
				let r: &int = &x;
				@drop r;
				return;
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mirMod, typeInterner, err := parseAndLowerMIR(t, tt.src)
			if err != nil {
				t.Fatalf("failed to lower: %v", err)
			}
			if mirMod == nil {
				t.Fatal("MIR module is nil")
			}

			err = mir.Validate(mirMod, typeInterner)
			if err != nil {
				t.Errorf("validation failed for valid program: %v", err)
			}
		})
	}
}

// TestValidate_UnterminatedBlock tests that unterminated blocks fail validation.
func TestValidate_UnterminatedBlock(t *testing.T) {
	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			0: {
				Name:   "test",
				Result: types.NoTypeID,
				Blocks: []mir.Block{
					{
						// No terminator - Term.Kind defaults to TermNone
					},
				},
			},
		},
	}

	err := mir.Validate(mod, nil)
	if err == nil {
		t.Error("expected validation error for unterminated block")
	} else if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("expected unterminated error, got: %v", err)
	}
}

// TestValidate_InvalidBlockTarget tests that invalid block targets fail validation.
func TestValidate_InvalidBlockTarget(t *testing.T) {
	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			0: {
				Name:   "test",
				Result: types.NoTypeID,
				Blocks: []mir.Block{
					{
						Term: mir.Terminator{
							Kind: mir.TermGoto,
							Goto: mir.GotoTerm{Target: 999}, // Invalid target
						},
					},
				},
			},
		},
	}

	err := mir.Validate(mod, nil)
	if err == nil {
		t.Error("expected validation error for invalid block target")
	} else if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected 'does not exist' error, got: %v", err)
	}
}

// TestValidate_InvalidLocalID tests that invalid local IDs fail validation.
func TestValidate_InvalidLocalID(t *testing.T) {
	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			0: {
				Name:   "test",
				Result: types.NoTypeID,
				Locals: []mir.Local{}, // No locals
				Blocks: []mir.Block{
					{
						Instrs: []mir.Instr{
							{
								Kind: mir.InstrAssign,
								Assign: mir.AssignInstr{
									Dst: mir.Place{Local: 999}, // Invalid local
									Src: mir.RValue{
										Kind: mir.RValueUse,
										Use: mir.Operand{
											Kind: mir.OperandConst,
											Const: mir.Const{
												Kind:     mir.ConstInt,
												IntValue: 1,
											},
										},
									},
								},
							},
						},
						Term: mir.Terminator{
							Kind:   mir.TermReturn,
							Return: mir.ReturnTerm{HasValue: false},
						},
					},
				},
			},
		},
	}

	err := mir.Validate(mod, nil)
	if err == nil {
		t.Error("expected validation error for invalid local ID")
	} else if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected 'does not exist' error, got: %v", err)
	}
}

// TestValidate_UnknownType tests that unknown types (NoTypeID) fail validation.
func TestValidate_UnknownType(t *testing.T) {
	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			0: {
				Name:   "test",
				Result: types.NoTypeID,
				Locals: []mir.Local{
					{Name: "x", Type: types.NoTypeID}, // Unknown type
				},
				Blocks: []mir.Block{
					{
						Term: mir.Terminator{
							Kind:   mir.TermReturn,
							Return: mir.ReturnTerm{HasValue: false},
						},
					},
				},
			},
		},
	}

	err := mir.Validate(mod, nil)
	if err == nil {
		t.Error("expected validation error for unknown type")
	} else if !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("expected 'unknown type' in error, got: %v", err)
	}
}

// TestValidate_ReturnMismatch_ValueInNothing tests returning value in nothing function.
func TestValidate_ReturnMismatch_ValueInNothing(t *testing.T) {
	typeInterner := types.NewInterner()
	nothingType := typeInterner.Builtins().Nothing
	intType := typeInterner.Builtins().Int

	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			0: {
				Name:   "test",
				Result: nothingType, // nothing
				Locals: []mir.Local{
					{Name: "x", Type: intType},
				},
				Blocks: []mir.Block{
					{
						Term: mir.Terminator{
							Kind: mir.TermReturn,
							Return: mir.ReturnTerm{
								HasValue: true, // Should not have value in nothing func
								Value: mir.Operand{
									Kind:  mir.OperandCopy,
									Place: mir.Place{Local: 0},
								},
							},
						},
					},
				},
			},
		},
	}

	err := mir.Validate(mod, typeInterner)
	if err == nil {
		t.Error("expected validation error for return with value in nothing function")
	} else if !strings.Contains(err.Error(), "return with value") {
		t.Errorf("expected 'return with value' error, got: %v", err)
	}
}

// TestValidate_ReturnMismatch_NoValueInNonNothing tests returning without value in non-nothing function.
func TestValidate_ReturnMismatch_NoValueInNonNothing(t *testing.T) {
	typeInterner := types.NewInterner()
	intType := typeInterner.Builtins().Int

	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			0: {
				Name:   "test",
				Result: intType, // returns int
				Blocks: []mir.Block{
					{
						Term: mir.Terminator{
							Kind: mir.TermReturn,
							Return: mir.ReturnTerm{
								HasValue: false, // Should have value
							},
						},
					},
				},
			},
		},
	}

	err := mir.Validate(mod, typeInterner)
	if err == nil {
		t.Error("expected validation error for return without value in non-nothing function")
	} else if !strings.Contains(err.Error(), "return without value") {
		t.Errorf("expected 'return without value' error, got: %v", err)
	}
}

// TestValidate_EndBorrowOnNonRef tests EndBorrow on non-reference local.
func TestValidate_EndBorrowOnNonRef(t *testing.T) {
	typeInterner := types.NewInterner()
	intType := typeInterner.Builtins().Int

	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			0: {
				Name:   "test",
				Result: types.NoTypeID,
				Locals: []mir.Local{
					{Name: "x", Type: intType, Flags: mir.LocalFlagCopy}, // Not a ref
				},
				Blocks: []mir.Block{
					{
						Instrs: []mir.Instr{
							{
								Kind: mir.InstrEndBorrow,
								EndBorrow: mir.EndBorrowInstr{
									Place: mir.Place{Local: 0}, // Trying to end_borrow on non-ref
								},
							},
						},
						Term: mir.Terminator{
							Kind:   mir.TermReturn,
							Return: mir.ReturnTerm{HasValue: false},
						},
					},
				},
			},
		},
	}

	err := mir.Validate(mod, typeInterner)
	if err == nil {
		t.Error("expected validation error for end_borrow on non-reference")
	} else if !strings.Contains(err.Error(), "non-reference") {
		t.Errorf("expected 'non-reference' error, got: %v", err)
	}
}

// TestValidate_DropOnCopy tests Drop on copy local.
func TestValidate_DropOnCopy(t *testing.T) {
	typeInterner := types.NewInterner()
	intType := typeInterner.Builtins().Int

	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			0: {
				Name:   "test",
				Result: types.NoTypeID,
				Locals: []mir.Local{
					{Name: "x", Type: intType, Flags: mir.LocalFlagCopy}, // Copy type
				},
				Blocks: []mir.Block{
					{
						Instrs: []mir.Instr{
							{
								Kind: mir.InstrDrop,
								Drop: mir.DropInstr{
									Place: mir.Place{Local: 0}, // Trying to drop copy type
								},
							},
						},
						Term: mir.Terminator{
							Kind:   mir.TermReturn,
							Return: mir.ReturnTerm{HasValue: false},
						},
					},
				},
			},
		},
	}

	err := mir.Validate(mod, typeInterner)
	if err == nil {
		t.Error("expected validation error for drop on copy local")
	} else if !strings.Contains(err.Error(), "drop on copy") {
		t.Errorf("expected 'drop on copy' error, got: %v", err)
	}
}

// TestValidate_DropOnRef tests Drop on reference local (should use end_borrow).
func TestValidate_DropOnRef(t *testing.T) {
	typeInterner := types.NewInterner()
	intType := typeInterner.Builtins().Int

	mod := &mir.Module{
		Funcs: map[mir.FuncID]*mir.Func{
			0: {
				Name:   "test",
				Result: types.NoTypeID,
				Locals: []mir.Local{
					{Name: "r", Type: intType, Flags: mir.LocalFlagRef}, // Reference type
				},
				Blocks: []mir.Block{
					{
						Instrs: []mir.Instr{
							{
								Kind: mir.InstrDrop,
								Drop: mir.DropInstr{
									Place: mir.Place{Local: 0}, // Trying to drop ref (should use end_borrow)
								},
							},
						},
						Term: mir.Terminator{
							Kind:   mir.TermReturn,
							Return: mir.ReturnTerm{HasValue: false},
						},
					},
				},
			},
		},
	}

	err := mir.Validate(mod, typeInterner)
	if err == nil {
		t.Error("expected validation error for drop on reference local")
	} else if !strings.Contains(err.Error(), "use end_borrow") {
		t.Errorf("expected 'use end_borrow' error, got: %v", err)
	}
}

// TestValidate_NilModule tests that nil module doesn't panic.
func TestValidate_NilModule(t *testing.T) {
	err := mir.Validate(nil, nil)
	if err != nil {
		t.Errorf("expected nil error for nil module, got: %v", err)
	}
}

// parseAndLowerMIR parses source code and lowers to MIR for testing.
func parseAndLowerMIR(t *testing.T, src string) (*mir.Module, *types.Interner, error) {
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

	return mirMod, typeInterner, nil
}
