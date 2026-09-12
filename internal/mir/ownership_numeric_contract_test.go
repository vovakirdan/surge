package mir

import (
	"bytes"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/types"
)

type numericOwnershipRow struct {
	name    string
	typ     types.TypeID
	counted bool
	value   Const
}

// Both counted literals are outside their inline range. The expected classes
// below describe MIR ownership; physical allocations are observed in VM tests.
func numericOwnershipRows(in *types.Interner) []numericOwnershipRow {
	b := in.Builtins()
	intValue := func(typ types.TypeID) Const {
		return Const{Kind: ConstInt, Type: typ, Text: "4611686018427387907", IntValue: 4611686018427387907}
	}
	uintValue := func(typ types.TypeID) Const {
		return Const{Kind: ConstUint, Type: typ, Text: "9223372036854775811", UintValue: 9223372036854775811}
	}
	return []numericOwnershipRow{
		{"int64", b.Int64, false, intValue(b.Int64)},
		{"uint64", b.Uint64, false, uintValue(b.Uint64)},
		{"int", b.Int, true, intValue(b.Int)},
		{"uint", b.Uint, true, uintValue(b.Uint)},
	}
}

func TestClassifyNumericOwnership(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	for _, row := range numericOwnershipRows(ot.in) {
		t.Run(row.name, func(t *testing.T) {
			if got := ownsHeapFor(ot.in, ot.sema, row.typ); got != row.counted {
				t.Fatalf("ownsHeap=%v, want %v", got, row.counted)
			}
			if got := ot.in.IsRefCountedScalar(row.typ); got != row.counted {
				t.Fatalf("IsRefCountedScalar=%v, want %v", got, row.counted)
			}
			if row.counted && ConstFoldsToFixnum(ot.in, &row.value) {
				t.Fatal("counted literal witness unexpectedly fits inline")
			}
			alias, transfer, mint, captured := ownershipNotApplicable, ownershipNotApplicable, ownershipNotApplicable, ownershipNotApplicable
			if row.counted {
				alias, transfer, mint, captured = ownershipAliases, ownershipTransfers, ownershipMints, ownershipOwnedAtEntry
			}
			check := func(subject string, got, want ownershipClass) {
				t.Helper()
				if got != want {
					t.Errorf("%s=%s, want %s", subject, got, want)
				}
			}
			check("parameter", classifyParamAtEntry(row.typ, ot.in, ot.sema, false), alias)
			check("frame capture", classifyParamAtEntry(row.typ, ot.in, ot.sema, true), captured)
			if got := byValueArgContract(ot.in, ot.sema, row.typ, false); got != ArgContractBorrow {
				t.Errorf("by-value contract=%s, want borrow", got)
			}
			check("constant", ot.operand(Operand{Kind: OperandConst, Type: row.typ, Const: row.value}), mint)
			check("copy", ot.operand(copyOf(row.typ)), alias)
			check("move", ot.operand(Operand{Kind: OperandMove, Type: row.typ}), transfer)
			if row.counted {
				check("retained counted value", ot.operand(Operand{Kind: OperandRetain, Type: row.typ}), ownershipMints)
			}
			check("identity cast", ot.rvalue(RValue{Kind: RValueCast, Cast: CastOp{Value: copyOf(row.typ), TargetTy: row.typ}}, row.typ), alias)
			check("arithmetic", ot.rvalue(RValue{Kind: RValueBinaryOp, Binary: BinaryOp{Op: ast.ExprBinaryAdd, Left: copyOf(row.typ), Right: copyOf(row.typ)}}, row.typ), mint)
			arrayTy := ot.in.Intern(types.MakeArray(row.typ, 2))
			check("field borrow", ot.rvalue(RValue{Kind: RValueField, Field: FieldAccess{Object: copyOf(arrayTy), FieldIdx: 0}}, row.typ), alias)
			check("field transfer", ot.rvalue(RValue{Kind: RValueField, Field: FieldAccess{Object: copyOf(arrayTy), FieldIdx: 0, MoveOut: true}}, row.typ), transfer)
			check("index borrow", ot.rvalue(RValue{Kind: RValueIndex, Index: IndexAccess{Object: copyOf(arrayTy), Index: copyOf(ot.plain)}}, row.typ), alias)
		})
	}
}

// A plain scalar bypasses both rules. A fixed-width array still needs the
// runtime view walk, while a counted scalar or array also needs privacy.
func TestNumericRelinquishOperandContracts(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	for _, row := range numericOwnershipRows(ot.in) {
		t.Run(row.name, func(t *testing.T) {
			for _, form := range []struct {
				name  string
				array bool
			}{{"scalar", false}, {"array", true}} {
				t.Run(form.name, func(t *testing.T) {
					typ := row.typ
					if form.array {
						typ = ot.in.Intern(types.MakeArray(typ, types.ArrayDynamicLength))
					}
					subject := row.counted || form.array
					if got := mayShareCountedBlockIn(ot.in, typ); got != row.counted {
						t.Fatalf("may share counted block=%v, want %v", got, row.counted)
					}
					if got := needsRelinquishWalkIn(ot.in, typ); got != subject {
						t.Fatalf("needs relinquish walk=%v, want %v", got, subject)
					}
					for _, action := range []struct {
						name string
						kind OperandKind
						walk bool
					}{
						{"moved_without_walk", OperandMove, false},
						{"moved_with_walk", OperandMove, true},
						{"copy_after_walk", OperandCopy, true},
						{"retain_after_walk", OperandRetain, true},
					} {
						t.Run(action.name, func(t *testing.T) {
							flags := LocalFlagCopy
							if subject {
								flags |= LocalFlagOwnsHeap
							}
							f := &Func{ID: 0, Name: "numeric_relinquish", Locals: []Local{{Type: typ, Flags: flags, Name: "value"}}, Blocks: []Block{{ID: 0, Term: Terminator{Kind: TermUnreachable}}}}
							if action.walk {
								f.Blocks[0].Instrs = append(f.Blocks[0].Instrs, unshareOf(0))
							}
							op := Operand{Kind: action.kind, Type: typ, Place: relinquishLocal(0)}
							f.Blocks[0].Instrs = append(f.Blocks[0].Instrs, relinquishCrossing(StructLitField{Name: "__cap0", Value: op}))
							shape := validateRelinquishedOperandShapes(f, ot.in)
							wantShape := subject && action.kind != OperandMove
							if (shape != nil) != wantShape {
								t.Fatalf("shape refusal=%v, want refusal=%v", shape, wantShape)
							}
							act := validateRelinquishedOperandsArePrivate(f, ot.in)
							wantAct := subject && (!action.walk || wantShape)
							if (act != nil) != wantAct {
								t.Fatalf("act refusal=%v, want refusal=%v", act, wantAct)
							}
							if wantShape && !strings.Contains(shape.Error(), "reaches the boundary as "+action.kind.String()+" L0") {
								t.Fatalf("wrong shape refusal: %v", shape)
							}
							if wantAct && !wantShape && !strings.Contains(act.Error(), "without an un-share in its block") {
								t.Fatalf("wrong act refusal: %v", act)
							}
						})
					}
				})
			}
		})
	}
}

func TestNumericUnshareAliasOwnership(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	for _, row := range numericOwnershipRows(ot.in) {
		t.Run(row.name, func(t *testing.T) {
			flags := LocalFlagCopy
			if row.counted {
				flags |= LocalFlagOwnsHeap
			}
			f := &Func{ID: 0, Name: "numeric_alias", Locals: []Local{
				{Type: row.typ, Flags: flags, Name: "owner"},
				{Type: row.typ, Flags: flags, Name: "alias"},
			}, Blocks: []Block{{ID: 0, Instrs: []Instr{
				{Kind: InstrAssign, Assign: AssignInstr{Dst: relinquishLocal(0), Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandConst, Type: row.typ, Const: row.value}}}},
				{Kind: InstrAssign, Assign: AssignInstr{Dst: relinquishLocal(1), Src: RValue{Kind: RValueUse, Use: copyOf(row.typ)}}},
				unshareOf(1), unshareOf(0),
			}, Term: Terminator{Kind: TermReturn}}}}
			got := VerifyOwnership(&Module{Funcs: map[FuncID]*Func{0: f}}, ot.in, ot.sema)
			if row.counted {
				if len(got) != 1 || got[0].ConsumingKind != OwnershipSinkUnshare || got[0].Local != 1 {
					t.Fatalf("want exactly one unshare finding on alias L1, got %+v", got)
				}
			} else if len(got) != 0 {
				t.Fatalf("fixed-width unshare should be irrelevant, got %+v", got)
			}
		})
	}
}

func TestDumpNumericOwnershipAnnotations(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	for _, row := range numericOwnershipRows(ot.in) {
		t.Run(row.name, func(t *testing.T) {
			mod := &Module{Funcs: map[FuncID]*Func{0: {
				ID: 0, Name: "numeric_dump", Locals: []Local{{Type: row.typ, Flags: LocalFlagCopy, Name: "value"}},
				Blocks: []Block{{ID: 0, Instrs: []Instr{{Kind: InstrAssign, Assign: AssignInstr{
					Dst: relinquishLocal(0), Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandConst, Type: row.typ, Const: row.value}},
				}}}, Term: Terminator{Kind: TermReturn}}},
			}}}
			var before, annotated, after bytes.Buffer
			if err := DumpModule(&before, mod, ot.in, DumpOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := DumpModule(&annotated, mod, ot.in, DumpOptions{AnnotateOwnership: true, Sema: ot.sema}); err != nil {
				t.Fatal(err)
			}
			line := "    L0 = const " + row.value.Text
			if row.value.Kind == ConstUint {
				line += ":uint"
			}
			if !strings.Contains(before.String(), line+"\n") || strings.Contains(before.String(), "[effect=") {
				t.Fatalf("default numeric dump changed:\n%s", &before)
			}
			if row.counted {
				line += " [effect=mint]"
			}
			if !strings.Contains(annotated.String(), line+"\n") {
				t.Fatalf("annotated dump missing %q:\n%s", line, &annotated)
			}
			if err := DumpModule(&after, mod, ot.in, DumpOptions{}); err != nil {
				t.Fatal(err)
			}
			if after.String() != before.String() {
				t.Fatal("ownership annotation mutated the numeric MIR")
			}
		})
	}
}
