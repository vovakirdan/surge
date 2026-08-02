package mir

import (
	"bytes"
	"strings"
	"testing"
)

func TestDumpModuleOwnershipAnnotationsAreOptInAndReadOnly(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	local := func(id LocalID) Place { return Place{Kind: PlaceLocal, Local: id} }
	mod := &Module{Funcs: map[FuncID]*Func{
		0: {
			ID:   0,
			Name: "ownership",
			Locals: []Local{
				{Type: ot.str, Flags: LocalFlagOwnsHeap, Name: "minted"},
				{Type: ot.str, Flags: LocalFlagOwnsHeap, Name: "alias"},
				{Type: ot.str, Flags: LocalFlagOwnsHeap, Name: "transfer"},
				{Type: ot.str, Flags: LocalFlagOwnsHeap, Name: "flag_only"},
				{Type: ot.plain, Flags: LocalFlagCopy, Name: "plain"},
				{Type: ot.str, Name: "call_result"},
				{Type: ot.str, Flags: LocalFlagOwnsHeap, Name: "move_source"},
			},
			Blocks: []Block{
				{
					ID: 0,
					Instrs: []Instr{
						{
							Kind: InstrAssign,
							Assign: AssignInstr{Dst: local(0), Src: RValue{
								Kind: RValueUse,
								Use: Operand{Kind: OperandConst, Type: ot.str, Const: Const{
									Kind: ConstString, Type: ot.str, StringValue: "fresh",
								}},
							}},
						},
						{
							Kind: InstrAssign,
							Assign: AssignInstr{Dst: local(1), Src: RValue{
								Kind: RValueCast,
								Cast: CastOp{
									Value:    Operand{Kind: OperandCopy, Place: local(0)},
									TargetTy: ot.str,
								},
							}},
						},
						{
							Kind: InstrAssign,
							Assign: AssignInstr{Dst: local(2), Src: RValue{
								Kind: RValueUse,
								Use:  Operand{Kind: OperandMove, Place: local(6)},
							}},
						},
						{
							Kind: InstrAssign,
							Assign: AssignInstr{Dst: local(4), Src: RValue{
								Kind: RValueUse,
								Use: Operand{Kind: OperandConst, Type: ot.plain, Const: Const{
									Kind: ConstInt, Type: ot.plain, IntValue: 7,
								}},
							}},
						},
						{
							Kind: InstrCall,
							Call: CallInstr{HasDst: true, Dst: local(5), Callee: Callee{
								Kind: CalleeSym, Name: "noop",
							}},
						},
						{Kind: InstrDrop, Drop: DropInstr{Place: local(0)}},
					},
					Term: Terminator{Kind: TermIf, If: IfTerm{
						Cond: Operand{Kind: OperandConst, Type: ot.in.Builtins().Bool, Const: Const{
							Kind: ConstBool, Type: ot.in.Builtins().Bool, BoolValue: true,
						}},
						Then: 1,
						Else: 2,
					}},
				},
				{
					ID: 1,
					Instrs: []Instr{
						{Kind: InstrDrop, Drop: DropInstr{
							Place: Place{Kind: PlaceLocal, Local: 1, Proj: []PlaceProj{{
								Kind: PlaceProjField, FieldIdx: 0,
							}}},
							Shallow: true,
						}},
						{Kind: InstrEnvelopeRelease, EnvelopeRelease: EnvelopeReleaseInstr{
							Place: local(2), Cursor: true,
						}},
					},
					Term: Terminator{Kind: TermReturn},
				},
				{ID: 2, Term: Terminator{Kind: TermReturn}},
			},
		},
	}}

	const wantDefault = "funcs=1\n" +
		"\n" +
		"fn ownership:\n" +
		"  locals:\n" +
		"    L0: string [owns_heap] name=minted\n" +
		"    L1: string [owns_heap] name=alias\n" +
		"    L2: string [owns_heap] name=transfer\n" +
		"    L3: string [owns_heap] name=flag_only\n" +
		"    L4: int [copy] name=plain\n" +
		"    L5: string name=call_result\n" +
		"    L6: string [owns_heap] name=move_source\n" +
		"  bb0:\n" +
		"    L0 = const \"fresh\"\n" +
		"    L1 = cast copy L0 to string\n" +
		"    L2 = move L6\n" +
		"    L4 = const 7\n" +
		"    L5 = call noop()\n" +
		"    drop L0\n" +
		"    if const true then bb1 else bb2\n" +
		"  bb1:\n" +
		"    drop_shallow L1.#0\n" +
		"    release_cursor L2\n" +
		"    return\n" +
		"  bb2:\n" +
		"    return\n"

	var before bytes.Buffer
	if err := DumpModule(&before, mod, ot.in, DumpOptions{}); err != nil {
		t.Fatalf("default dump: %v", err)
	}
	if got := before.String(); got != wantDefault {
		t.Fatalf("default dump changed:\n--- got ---\n%s--- want ---\n%s", got, wantDefault)
	}

	var disabled bytes.Buffer
	if err := DumpModule(&disabled, mod, ot.in, DumpOptions{Sema: ot.sema}); err != nil {
		t.Fatalf("disabled annotated dump: %v", err)
	}
	if got := disabled.String(); got != wantDefault {
		t.Fatalf("passing sema without opting in changed the dump:\n%s", got)
	}

	const wantAnnotated = "funcs=1\n" +
		"\n" +
		"fn ownership:\n" +
		"  locals:\n" +
		"    L0: string [owns_heap, owes_release] name=minted\n" +
		"    L1: string [owns_heap, owes_release] name=alias\n" +
		"    L2: string [owns_heap, owes_release] name=transfer\n" +
		"    L3: string [owns_heap] name=flag_only\n" +
		"    L4: int [copy] name=plain\n" +
		"    L5: string name=call_result\n" +
		"    L6: string [owns_heap] name=move_source\n" +
		"  bb0:\n" +
		"    L0 = const \"fresh\" [effect=mint]\n" +
		"    L1 = cast copy L0 to string [effect=alias]\n" +
		"    L2 = move L6 [effect=transfer]\n" +
		"    L4 = const 7\n" +
		"    L5 = call noop()\n" +
		"    drop L0\n" +
		"    if const true then bb1 else bb2\n" +
		"  bb1:\n" +
		"    drop_shallow L1.#0\n" +
		"    release_cursor L2\n" +
		"    return\n" +
		"  bb2:\n" +
		"    return\n"

	var annotated bytes.Buffer
	if err := DumpModule(&annotated, mod, ot.in, DumpOptions{
		AnnotateOwnership: true,
		Sema:              ot.sema,
	}); err != nil {
		t.Fatalf("annotated dump: %v", err)
	}
	if got := annotated.String(); got != wantAnnotated {
		t.Fatalf("annotated dump mismatch:\n--- got ---\n%s--- want ---\n%s", got, wantAnnotated)
	}

	var after bytes.Buffer
	if err := DumpModule(&after, mod, ot.in, DumpOptions{}); err != nil {
		t.Fatalf("default dump after annotation: %v", err)
	}
	if got := after.String(); got != before.String() {
		t.Fatalf("annotated dump mutated MIR:\n--- before ---\n%s--- after ---\n%s", before.String(), got)
	}
}

func TestDumpModuleOwnershipAnnotationsRequireSema(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	mod := &Module{Funcs: map[FuncID]*Func{}}
	var out bytes.Buffer

	err := DumpModule(&out, mod, ot.in, DumpOptions{AnnotateOwnership: true})
	if err == nil || !strings.Contains(err.Error(), "require semantic analysis") {
		t.Fatalf("expected fail-closed semantic-analysis error, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("fail-closed dump wrote partial output: %q", out.String())
	}
}
