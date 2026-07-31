package mir

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/types"
)

// ownershipTestTypes is the smallest set of types that separates every
// type-dependent row of the classification: one that owns heap and is not a
// reference-counted scalar (string), one that owns heap and IS one (float),
// one that owns nothing (int), plus the reference and owning-pointer forms the
// deref rows turn on.
type ownershipTestTypes struct {
	in       *types.Interner
	sema     *sema.Result
	str      types.TypeID
	flt      types.TypeID
	plain    types.TypeID
	strRef   types.TypeID
	strOwn   types.TypeID
	strArray types.TypeID
}

func newOwnershipTestTypes(t *testing.T) ownershipTestTypes {
	t.Helper()
	in := types.NewInterner()
	b := in.Builtins()
	ot := ownershipTestTypes{
		in:       in,
		sema:     &sema.Result{TypeInterner: in},
		str:      b.String,
		flt:      b.Float,
		plain:    b.Int,
		strRef:   in.Intern(types.MakeReference(b.String, false)),
		strOwn:   in.Intern(types.MakeOwn(b.String)),
		strArray: in.Intern(types.MakeArray(b.String, 2)),
	}
	// The rows below are only meaningful if these premises hold; a change to
	// what owns heap must fail here rather than silently make cases redundant.
	if !ownsHeapFor(in, ot.sema, ot.str) || in.IsRefCountedScalar(ot.str) {
		t.Fatalf("string must own heap and not be a reference-counted scalar")
	}
	if !ownsHeapFor(in, ot.sema, ot.flt) || !in.IsRefCountedScalar(ot.flt) {
		t.Fatalf("float must own heap and be a reference-counted scalar")
	}
	if ownsHeapFor(in, ot.sema, ot.plain) {
		t.Fatalf("int must own no heap")
	}
	if ownsHeapFor(in, ot.sema, ot.strRef) {
		t.Fatalf("&string must own no heap")
	}
	if !ownsHeapFor(in, ot.sema, ot.strOwn) || !ownsHeapFor(in, ot.sema, ot.strArray) {
		t.Fatalf("own string and string[2] must own heap")
	}
	return ot
}

func (ot ownershipTestTypes) rvalue(rv RValue, resultTy types.TypeID) ownershipClass {
	return classifyRValue(&rv, resultTy, ot.in, ot.sema)
}

func (ot ownershipTestTypes) operand(op Operand) ownershipClass {
	return classifyOperand(&op, ot.in, ot.sema)
}

func copyOf(ty types.TypeID) Operand {
	return Operand{Kind: OperandCopy, Type: ty, Place: Place{Local: 0}}
}

// TestClassifyRValueCoversEveryKind is the exhaustiveness gate for Table A. It
// names every RValueKind explicitly and cross-checks the list against the
// enum's own bound, so a kind added to instr.go without a row here fails rather
// than silently classifying as whatever the fallthrough answers.
func TestClassifyRValueCoversEveryKind(t *testing.T) {
	ot := newOwnershipTestTypes(t)

	// Written as one case per DISTINCT ANSWER, not one per kind: several rows
	// are type-dependent, and a bare walk over the enum would never reach the
	// branch that matters.
	cases := []struct {
		name     string
		kind     RValueKind
		rv       RValue
		resultTy types.TypeID
		want     ownershipClass
	}{
		{
			name: "use_of_an_aliasing_copy", kind: RValueUse,
			rv: RValue{Kind: RValueUse, Use: copyOf(ot.str)}, resultTy: ot.str,
			want: ownershipAliases,
		},
		{
			name: "use_of_a_retain", kind: RValueUse,
			rv:       RValue{Kind: RValueUse, Use: Operand{Kind: OperandRetain, Type: ot.flt}},
			resultTy: ot.flt, want: ownershipMints,
		},
		{
			// A pure pass-through asks its OWN operand, so a destination whose
			// type never got filled in must not turn a retain into "no
			// ownership here" — the retain still has to be seen as the bridge
			// it is.
			name: "use_answers_from_the_operand", kind: RValueUse,
			rv:       RValue{Kind: RValueUse, Use: Operand{Kind: OperandRetain, Type: ot.flt}},
			resultTy: types.NoTypeID, want: ownershipMints,
		},
		{
			name: "deref_through_a_reference", kind: RValueUnaryOp,
			rv: RValue{Kind: RValueUnaryOp, Unary: UnaryOp{
				Op: ast.ExprUnaryDeref, Operand: copyOf(ot.strRef),
			}}, resultTy: ot.str, want: ownershipAliases,
		},
		{
			name: "deref_through_an_owning_pointer", kind: RValueUnaryOp,
			rv: RValue{Kind: RValueUnaryOp, Unary: UnaryOp{
				Op: ast.ExprUnaryDeref, Operand: copyOf(ot.strOwn),
			}}, resultTy: ot.str, want: ownershipTransfers,
		},
		{
			name: "negation_of_an_owning_type", kind: RValueUnaryOp,
			rv: RValue{Kind: RValueUnaryOp, Unary: UnaryOp{
				Op: ast.ExprUnaryMinus, Operand: copyOf(ot.flt),
			}}, resultTy: ot.flt, want: ownershipMints,
		},
		{
			name: "negation_of_a_machine_word", kind: RValueUnaryOp,
			rv: RValue{Kind: RValueUnaryOp, Unary: UnaryOp{
				Op: ast.ExprUnaryMinus, Operand: copyOf(ot.plain),
			}}, resultTy: ot.plain, want: ownershipNotApplicable,
		},
		{
			// Both backends hand the operand straight back, so calling either
			// of these MINTS would let `own s` launder an alias into a value
			// something is licensed to release.
			name: "unary_plus_passes_through", kind: RValueUnaryOp,
			rv: RValue{Kind: RValueUnaryOp, Unary: UnaryOp{
				Op: ast.ExprUnaryPlus, Operand: copyOf(ot.flt),
			}}, resultTy: ot.flt, want: ownershipTransfers,
		},
		{
			name: "own_passes_through", kind: RValueUnaryOp,
			rv: RValue{Kind: RValueUnaryOp, Unary: UnaryOp{
				Op: ast.ExprUnaryOwn, Operand: copyOf(ot.str),
			}}, resultTy: ot.str, want: ownershipTransfers,
		},
		{
			name: "magic_binary_operator", kind: RValueBinaryOp,
			rv: RValue{Kind: RValueBinaryOp, Binary: BinaryOp{
				Op: ast.ExprBinaryAdd, Left: copyOf(ot.str), Right: copyOf(ot.str),
			}}, resultTy: ot.str, want: ownershipMints,
		},
		{
			name: "primitive_binary_operator", kind: RValueBinaryOp,
			rv: RValue{Kind: RValueBinaryOp, Binary: BinaryOp{
				Op: ast.ExprBinaryAdd, Left: copyOf(ot.plain), Right: copyOf(ot.plain),
			}}, resultTy: ot.plain, want: ownershipNotApplicable,
		},
		{
			name: "identity_cast", kind: RValueCast,
			rv: RValue{Kind: RValueCast, Cast: CastOp{
				Value: copyOf(ot.str), TargetTy: ot.str,
			}}, resultTy: ot.str, want: ownershipAliases,
		},
		{
			name: "representation_changing_cast", kind: RValueCast,
			rv: RValue{Kind: RValueCast, Cast: CastOp{
				Value: copyOf(ot.plain), TargetTy: ot.flt,
			}}, resultTy: ot.flt, want: ownershipMints,
		},
		{
			name: "cast_between_non_owning_types", kind: RValueCast,
			rv: RValue{Kind: RValueCast, Cast: CastOp{
				Value: copyOf(ot.plain), TargetTy: ot.plain,
			}}, resultTy: ot.plain, want: ownershipNotApplicable,
		},
		{
			name: "struct_literal", kind: RValueStructLit,
			rv: RValue{Kind: RValueStructLit, StructLit: StructLit{
				TypeID: ot.strArray,
				Fields: []StructLitField{{Name: "a", Value: copyOf(ot.str)}},
			}}, resultTy: ot.strArray, want: ownershipMints,
		},
		{
			name: "array_literal", kind: RValueArrayLit,
			rv: RValue{Kind: RValueArrayLit, ArrayLit: ArrayLit{
				Elems: []Operand{copyOf(ot.str)},
			}}, resultTy: ot.strArray, want: ownershipMints,
		},
		{
			name: "tuple_literal", kind: RValueTupleLit,
			rv: RValue{Kind: RValueTupleLit, TupleLit: TupleLit{
				Elems: []Operand{copyOf(ot.str)},
			}}, resultTy: ot.strArray, want: ownershipMints,
		},
		{
			name: "field_read", kind: RValueField,
			rv: RValue{Kind: RValueField, Field: FieldAccess{
				Object: copyOf(ot.strArray), FieldName: "a",
			}}, resultTy: ot.str, want: ownershipAliases,
		},
		{
			name: "field_moved_out", kind: RValueField,
			rv: RValue{Kind: RValueField, Field: FieldAccess{
				Object: copyOf(ot.strArray), FieldName: "a", MoveOut: true,
			}}, resultTy: ot.str, want: ownershipTransfers,
		},
		{
			name: "field_of_a_non_owning_type", kind: RValueField,
			rv: RValue{Kind: RValueField, Field: FieldAccess{
				Object: copyOf(ot.strArray), FieldName: "n",
			}}, resultTy: ot.plain, want: ownershipNotApplicable,
		},
		{
			name: "element_read", kind: RValueIndex,
			rv: RValue{Kind: RValueIndex, Index: IndexAccess{
				Object: copyOf(ot.strArray), Index: copyOf(ot.plain),
			}}, resultTy: ot.str, want: ownershipAliases,
		},
		{
			name: "element_read_of_a_non_owning_type", kind: RValueIndex,
			rv: RValue{Kind: RValueIndex, Index: IndexAccess{
				Object: copyOf(ot.strArray), Index: copyOf(ot.plain),
			}}, resultTy: ot.plain, want: ownershipNotApplicable,
		},
		{
			name: "tag_test", kind: RValueTagTest,
			rv: RValue{Kind: RValueTagTest, TagTest: TagTest{
				Value: copyOf(ot.str), TagName: "Some",
			}}, resultTy: ot.in.Builtins().Bool, want: ownershipNotApplicable,
		},
		{
			name: "borrowed_tag_payload", kind: RValueTagPayload,
			rv: RValue{Kind: RValueTagPayload, TagPayload: TagPayload{
				Value: copyOf(ot.str), TagName: "Some",
			}}, resultTy: ot.str, want: ownershipAliases,
		},
		{
			name: "moved_out_tag_payload", kind: RValueTagPayload,
			rv: RValue{Kind: RValueTagPayload, TagPayload: TagPayload{
				Value: copyOf(ot.str), TagName: "Some", MoveOut: true,
			}}, resultTy: ot.str, want: ownershipTransfers,
		},
		{
			name: "iter_init", kind: RValueIterInit,
			rv: RValue{Kind: RValueIterInit, IterInit: IterInit{
				Iterable: copyOf(ot.strArray),
			}}, resultTy: ot.plain, want: ownershipMints,
		},
		{
			name: "iter_next", kind: RValueIterNext,
			rv: RValue{Kind: RValueIterNext, IterNext: IterNext{
				Iter: copyOf(ot.plain),
			}}, resultTy: ot.plain, want: ownershipMints,
		},
		{
			name: "type_test", kind: RValueTypeTest,
			rv: RValue{Kind: RValueTypeTest, TypeTest: TypeTest{
				Value: copyOf(ot.str), TargetTy: ot.str,
			}}, resultTy: ot.in.Builtins().Bool, want: ownershipNotApplicable,
		},
		{
			name: "heir_test", kind: RValueHeirTest,
			rv: RValue{Kind: RValueHeirTest, HeirTest: HeirTest{
				Value: copyOf(ot.str), TargetTy: ot.str,
			}}, resultTy: ot.in.Builtins().Bool, want: ownershipNotApplicable,
		},
	}

	covered := make(map[RValueKind]bool, rvalueKindCount)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ot.rvalue(tc.rv, tc.resultTy); got != tc.want {
				t.Errorf("classifyRValue = %s, want %s", got, tc.want)
			}
		})
		covered[tc.kind] = true
	}
	for k := RValueKind(0); k < rvalueKindCount; k++ {
		if !covered[k] {
			t.Errorf("RValueKind %d has no classification case", k)
		}
	}
}

// TestClassifyOperandCoversEveryKind is the same gate for Table B.
func TestClassifyOperandCoversEveryKind(t *testing.T) {
	ot := newOwnershipTestTypes(t)

	cases := []struct {
		name string
		kind OperandKind
		op   Operand
		want ownershipClass
	}{
		{
			name: "owning_constant", kind: OperandConst,
			op: Operand{Kind: OperandConst, Type: ot.str, Const: Const{
				Kind: ConstString, Type: ot.str, StringValue: "held",
			}}, want: ownershipMints,
		},
		{
			name: "non_owning_constant", kind: OperandConst,
			op: Operand{Kind: OperandConst, Type: ot.plain, Const: Const{
				Kind: ConstInt, Type: ot.plain, IntValue: 1,
			}}, want: ownershipNotApplicable,
		},
		{
			name: "constant_typed_only_on_the_constant", kind: OperandConst,
			op: Operand{Kind: OperandConst, Const: Const{
				Kind: ConstString, Type: ot.str, StringValue: "held",
			}}, want: ownershipMints,
		},
		{
			name: "copy_of_an_owning_place", kind: OperandCopy,
			op: copyOf(ot.str), want: ownershipAliases,
		},
		{
			name: "copy_of_a_non_owning_place", kind: OperandCopy,
			op: copyOf(ot.plain), want: ownershipNotApplicable,
		},
		{
			name: "move_of_an_owning_place", kind: OperandMove,
			op:   Operand{Kind: OperandMove, Type: ot.str},
			want: ownershipTransfers,
		},
		{
			name: "move_of_a_non_owning_place", kind: OperandMove,
			op:   Operand{Kind: OperandMove, Type: ot.plain},
			want: ownershipNotApplicable,
		},
		{
			name: "address_of", kind: OperandAddrOf,
			op:   Operand{Kind: OperandAddrOf, Type: ot.strRef},
			want: ownershipNotApplicable,
		},
		{
			name: "mutable_address_of", kind: OperandAddrOfMut,
			op:   Operand{Kind: OperandAddrOfMut, Type: ot.strRef},
			want: ownershipNotApplicable,
		},
		{
			name: "retain", kind: OperandRetain,
			op:   Operand{Kind: OperandRetain, Type: ot.flt},
			want: ownershipMints,
		},
		{
			name: "value_composite_clone", kind: OperandCopyValue,
			op:   Operand{Kind: OperandCopyValue, Type: ot.strArray},
			want: ownershipMints,
		},
	}

	covered := make(map[OperandKind]bool, operandKindCount)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ot.operand(tc.op); got != tc.want {
				t.Errorf("classifyOperand = %s, want %s", got, tc.want)
			}
		})
		covered[tc.kind] = true
	}
	for k := OperandKind(0); k < operandKindCount; k++ {
		if !covered[k] {
			t.Errorf("OperandKind %d has no classification case", k)
		}
	}
}

// TestInstrMintsDestCoversEveryKind is the third gate: every InstrKind must
// land in exactly one bucket — carries a destination this rule MINTS, or
// carries none this rule answers for. The switch here is deliberately a second,
// independent statement of the answer rather than a call into the code under
// test, so a kind added to only one of the two shows up as a disagreement.
func TestInstrMintsDestCoversEveryKind(t *testing.T) {
	// A destination place distinguishable from the zero value, so "reported a
	// destination" cannot be confused with "reported nothing".
	dst := Place{Local: 7}

	cases := []struct {
		kind    InstrKind
		instr   Instr
		wantDst bool
	}{
		{kind: InstrAssign, instr: Instr{Kind: InstrAssign, Assign: AssignInstr{Dst: dst}}, wantDst: false},
		{kind: InstrCall, instr: Instr{Kind: InstrCall, Call: CallInstr{HasDst: true, Dst: dst}}, wantDst: true},
		{kind: InstrDrop, instr: Instr{Kind: InstrDrop, Drop: DropInstr{Place: dst}}, wantDst: false},
		{kind: InstrEndBorrow, instr: Instr{Kind: InstrEndBorrow, EndBorrow: EndBorrowInstr{Place: dst}}, wantDst: false},
		{kind: InstrAwait, instr: Instr{Kind: InstrAwait, Await: AwaitInstr{Dst: dst}}, wantDst: true},
		{kind: InstrSpawn, instr: Instr{Kind: InstrSpawn, Spawn: SpawnInstr{Dst: dst}}, wantDst: true},
		{kind: InstrCrossing, instr: Instr{Kind: InstrCrossing, Crossing: CrossingInstr{Dst: dst}}, wantDst: true},
		{kind: InstrBlocking, instr: Instr{Kind: InstrBlocking, Blocking: BlockingInstr{Dst: dst}}, wantDst: true},
		{kind: InstrPoll, instr: Instr{Kind: InstrPoll, Poll: PollInstr{Dst: dst}}, wantDst: true},
		{kind: InstrJoinAll, instr: Instr{Kind: InstrJoinAll, JoinAll: JoinAllInstr{Dst: dst}}, wantDst: true},
		{kind: InstrChanSend, instr: Instr{Kind: InstrChanSend}, wantDst: false},
		{kind: InstrChanRecv, instr: Instr{Kind: InstrChanRecv, ChanRecv: ChanRecvInstr{Dst: dst}}, wantDst: true},
		{kind: InstrNetWait, instr: Instr{Kind: InstrNetWait}, wantDst: false},
		{kind: InstrTimeout, instr: Instr{Kind: InstrTimeout, Timeout: TimeoutInstr{Dst: dst}}, wantDst: true},
		{kind: InstrSelect, instr: Instr{Kind: InstrSelect, Select: SelectInstr{Dst: dst}}, wantDst: true},
		{kind: InstrNop, instr: Instr{Kind: InstrNop}, wantDst: false},
		{kind: InstrEnvelopeRelease, instr: Instr{Kind: InstrEnvelopeRelease, EnvelopeRelease: EnvelopeReleaseInstr{Place: dst}}, wantDst: false},
	}

	if len(cases) != int(instrKindCount) {
		t.Fatalf("%d InstrKind cases for %d kinds", len(cases), instrKindCount)
	}
	covered := make(map[InstrKind]bool, instrKindCount)
	for _, tc := range cases {
		if covered[tc.kind] {
			t.Fatalf("InstrKind %s listed twice", tc.kind)
		}
		covered[tc.kind] = true
		gotDst, gotOK := instrMintsDest(&tc.instr)
		if gotOK != tc.wantDst {
			t.Errorf("instrMintsDest(%s) reported a destination = %v, want %v", tc.kind, gotOK, tc.wantDst)
			continue
		}
		if gotOK && (gotDst.Kind != dst.Kind || gotDst.Local != dst.Local) {
			t.Errorf("instrMintsDest(%s) = %+v, want %+v", tc.kind, gotDst, dst)
		}
	}
	for k := InstrKind(0); k < instrKindCount; k++ {
		if !covered[k] {
			t.Errorf("InstrKind %d has no bucket", k)
		}
	}

	// A call with no destination defines nothing, which is a different answer
	// from the kind carrying no destination field at all.
	noDst := Instr{Kind: InstrCall, Call: CallInstr{Dst: dst}}
	if _, ok := instrMintsDest(&noDst); ok {
		t.Errorf("instrMintsDest reported a destination for a call with HasDst false")
	}
}

// TestKindCountSentinelsStayLast pins the one way the three walks above can go
// blind: a kind appended AFTER a sentinel is never reached by an iteration
// bounded by it. String() names every real kind and nothing else, so the kind
// below each sentinel must name itself and the sentinel must not.
func TestKindCountSentinelsStayLast(t *testing.T) {
	if got := InstrKind(instrKindCount).String(); got != "Unknown" {
		t.Errorf("a kind exists at instrKindCount (%s); the sentinel is no longer last", got)
	}
	if got := (instrKindCount - 1).String(); got == "Unknown" {
		t.Errorf("instrKindCount overshoots the last real InstrKind")
	}
	if got := OperandKind(operandKindCount).String(); got != "Unknown" {
		t.Errorf("a kind exists at operandKindCount (%s); the sentinel is no longer last", got)
	}
	if got := (operandKindCount - 1).String(); got == "Unknown" {
		t.Errorf("operandKindCount overshoots the last real OperandKind")
	}
	// RValueKind has no String(); classifyRValue is the equivalent oracle. A
	// value past the end matches no row and falls through to unclassified.
	ot := newOwnershipTestTypes(t)
	past := RValue{Kind: rvalueKindCount}
	if got := classifyRValue(&past, ot.str, ot.in, ot.sema); got != ownershipUnclassified {
		t.Errorf("a kind exists at rvalueKindCount (classified %s); the sentinel is no longer last", got)
	}
	last := RValue{Kind: rvalueKindCount - 1, HeirTest: HeirTest{Value: copyOf(ot.str)}}
	if got := classifyRValue(&last, ot.in.Builtins().Bool, ot.in, ot.sema); got == ownershipUnclassified {
		t.Errorf("rvalueKindCount overshoots the last real RValueKind")
	}
}

// TestClassifyParamAtEntry pins the entry axiom against the predicate it
// mirrors. A parameter is a terminal ROOT, so the one answer it must never give
// is TRANSFERS — there is no earlier definition to inherit from.
func TestClassifyParamAtEntry(t *testing.T) {
	ot := newOwnershipTestTypes(t)

	cases := []struct {
		name string
		ty   types.TypeID
		want ownershipClass
	}{
		{"droppable_by_value", ot.str, ownershipOwnedAtEntry},
		{"value_composite", ot.strArray, ownershipOwnedAtEntry},
		{"owning_pointer", ot.strOwn, ownershipOwnedAtEntry},
		// Copy at the surface, so the caller keeps its binding AND its
		// reference for the whole call: a borrow, not owned at entry.
		{"reference_counted_scalar", ot.flt, ownershipAliases},
		{"reference", ot.strRef, ownershipNotApplicable},
		{"non_owning", ot.plain, ownershipNotApplicable},
		{"unknown", types.NoTypeID, ownershipNotApplicable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyParamAtEntry(tc.ty, ot.in, ot.sema)
			if got != tc.want {
				t.Errorf("classifyParamAtEntry = %s, want %s", got, tc.want)
			}
			if got == ownershipTransfers {
				t.Errorf("a parameter classified TRANSFERS, which has nothing to inherit from")
			}
		})
	}

	// The caller's side of the same call has to agree about the same type, or
	// the callee thinks it owns what the caller never handed over.
	for _, ty := range []types.TypeID{ot.str, ot.strArray, ot.flt, ot.strRef, ot.plain} {
		entry := classifyParamAtEntry(ty, ot.in, ot.sema)
		contract := byValueArgContract(ot.in, ot.sema, ty, false)
		if (entry == ownershipOwnedAtEntry) != (contract == ArgContractTransferOwned) {
			t.Errorf("type %d: entry axiom %s disagrees with argument contract %s", ty, entry, contract)
		}
	}
}

// TestClassifyNilInputsAreAGap pins the fallthrough direction: an absent RValue
// or operand is a gap to report, never a permissive answer.
func TestClassifyNilInputsAreAGap(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	if got := classifyRValue(nil, ot.str, ot.in, ot.sema); got != ownershipUnclassified {
		t.Errorf("classifyRValue(nil) = %s, want unclassified", got)
	}
	if got := classifyOperand(nil, ot.in, ot.sema); got != ownershipUnclassified {
		t.Errorf("classifyOperand(nil) = %s, want unclassified", got)
	}
	if _, ok := instrMintsDest(nil); ok {
		t.Errorf("instrMintsDest(nil) reported a destination")
	}
}
