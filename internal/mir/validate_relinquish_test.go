package mir

import (
	"bytes"
	"strings"
	"testing"

	"surge/internal/sema"
)

// The rows below build MIR by hand because no program that reaches these
// sinks with a counted operand compiles through sema today: the capture
// stop-gap refuses site 1 outright and buildpipeline refuses a counted
// far-channel element and a counted crossing result. The rule has to be red
// on its own terms, so the shapes are spelled out directly.

func relinquishLocal(id LocalID) Place { return Place{Kind: PlaceLocal, Local: id} }

// relinquishFixture is one function with a float local (L0), an int64 local
// (L1) and a float temp (L2); the caller supplies the instructions of bb0 and
// its terminator.
func relinquishFixture(ot ownershipTestTypes, instrs []Instr, term Terminator, resultCrosses bool) *Func {
	return &Func{
		ID:                   0,
		Name:                 "relinquish",
		Result:               ot.flt,
		ResultCrossesThreads: resultCrosses,
		Locals: []Local{
			{Type: ot.flt, Flags: LocalFlagCopy | LocalFlagOwnsHeap, Name: "shared"},
			{Type: ot.plain, Flags: LocalFlagCopy, Name: "plain"},
			{Type: ot.flt, Flags: LocalFlagCopy | LocalFlagOwnsHeap, Name: "tmp"},
		},
		Blocks: []Block{{ID: 0, Instrs: instrs, Term: term}},
	}
}

func relinquishCrossing(fields ...StructLitField) Instr {
	return Instr{Kind: InstrCrossing, Crossing: CrossingInstr{
		Kind:  sema.CrossingLoweringSpawnOn,
		State: StructLit{Fields: fields},
	}}
}

func relinquishSelect(payload Operand) Instr {
	return Instr{Kind: InstrCrossing, Crossing: CrossingInstr{
		Kind: sema.CrossingLoweringChannelSelect,
		RemoteOps: []CrossingRemoteOp{
			{Method: "send", Value: payload},
			{Method: "recv"},
		},
	}}
}

func unshareOf(id LocalID) Instr {
	return Instr{Kind: InstrUnshare, Unshare: UnshareInstr{Place: relinquishLocal(id)}}
}

func TestRelinquishedOperandsMustBeUnshared(t *testing.T) {
	ret := Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true}}
	rows := []struct {
		name    string
		instrs  func(ot ownershipTestTypes) []Instr
		term    func(ot ownershipTestTypes) Terminator
		crosses bool
		wantErr string
	}{
		{
			name: "a moved float with no un-share is refused",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{relinquishCrossing(StructLitField{Name: "__cap0",
					Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}})}
			},
			term:    func(ownershipTestTypes) Terminator { return ret },
			wantErr: "bb0 instr 0: state field __cap0 (L0, float) reaches the boundary without an un-share in its block",
		},
		{
			name: "an un-share immediately before the sink is the act",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{unshareOf(0), relinquishCrossing(StructLitField{Name: "__cap0",
					Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}})}
			},
			term: func(ownershipTestTypes) Terminator { return ret },
		},
		{
			name: "a retain read of the local after the un-share shares it again",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{
					unshareOf(0),
					{Kind: InstrAssign, Assign: AssignInstr{Dst: relinquishLocal(2), Src: RValue{Kind: RValueUse,
						Use: Operand{Kind: OperandRetain, Type: ot.flt, Place: relinquishLocal(0)}}}},
					relinquishCrossing(StructLitField{Name: "__cap0",
						Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}}),
				}
			},
			term:    func(ownershipTestTypes) Terminator { return ret },
			wantErr: "without an un-share in its block",
		},
		{
			name: "a write into the local after the un-share undoes it",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{
					unshareOf(0),
					{Kind: InstrAssign, Assign: AssignInstr{Dst: relinquishLocal(0), Src: RValue{Kind: RValueUse,
						Use: Operand{Kind: OperandRetain, Type: ot.flt, Place: relinquishLocal(2)}}}},
					relinquishCrossing(StructLitField{Name: "__cap0",
						Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}}),
				}
			},
			term:    func(ownershipTestTypes) Terminator { return ret },
			wantErr: "without an un-share in its block",
		},
		{
			name: "an un-share of another local does not count",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{unshareOf(2), relinquishCrossing(StructLitField{Name: "__cap0",
					Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}})}
			},
			term:    func(ownershipTestTypes) Terminator { return ret },
			wantErr: "without an un-share in its block",
		},
		{
			name: "a bare copy at the sink is the aliasing bug by name",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{unshareOf(0), relinquishSelect(
					Operand{Kind: OperandCopy, Type: ot.flt, Place: relinquishLocal(0)})}
			},
			term:    func(ownershipTestTypes) Terminator { return ret },
			wantErr: "bb0 instr 1: remote op 0 send payload reaches the boundary as Copy L0",
		},
		{
			name: "a retain at the sink is the retain trap by name",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{unshareOf(0), relinquishSelect(
					Operand{Kind: OperandRetain, Type: ot.flt, Place: relinquishLocal(0)})}
			},
			term:    func(ownershipTestTypes) Terminator { return ret },
			wantErr: "reaches the boundary as Retain L0",
		},
		{
			name: "a moved send payload with its un-share passes",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{unshareOf(2), relinquishSelect(
					Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(2)})}
			},
			term: func(ownershipTestTypes) Terminator { return ret },
		},
		{
			name: "a constant is minted at the site",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{relinquishSelect(Operand{Kind: OperandConst, Type: ot.flt,
					Const: Const{Kind: ConstFloat, Type: ot.flt, Text: "1.5"}})}
			},
			term: func(ownershipTestTypes) Terminator { return ret },
		},
		{
			name: "an int64 field shares nothing and is not asked",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{relinquishCrossing(StructLitField{Name: "__cap0",
					Value: Operand{Kind: OperandCopy, Type: ot.plain, Place: relinquishLocal(1)}})}
			},
			term: func(ownershipTestTypes) Terminator { return ret },
		},
		{
			name: "a blocking state field is a sink too",
			instrs: func(ot ownershipTestTypes) []Instr {
				return []Instr{{Kind: InstrBlocking, Blocking: BlockingInstr{State: StructLit{Fields: []StructLitField{
					{Name: "__cap0", Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}},
				}}}}}
			},
			term:    func(ownershipTestTypes) Terminator { return ret },
			wantErr: "bb0 instr 0: blocking state field __cap0 (L0, float) reaches the boundary without an un-share",
		},
		{
			name:   "a crossing body's async return without the act is refused",
			instrs: func(ownershipTestTypes) []Instr { return nil },
			term: func(ot ownershipTestTypes) Terminator {
				return Terminator{Kind: TermAsyncReturn, AsyncReturn: AsyncReturnTerm{
					State: Operand{Kind: OperandCopy, Place: relinquishLocal(1)}, HasValue: true,
					Value: Operand{Kind: OperandMove, Place: relinquishLocal(0)}}}
			},
			crosses: true,
			wantErr: "bb0 terminator: async return value (L0, float) reaches the boundary without an un-share",
		},
		{
			name:   "a crossing body's async return after the act passes",
			instrs: func(ownershipTestTypes) []Instr { return []Instr{unshareOf(0)} },
			term: func(ot ownershipTestTypes) Terminator {
				return Terminator{Kind: TermAsyncReturn, AsyncReturn: AsyncReturnTerm{
					State: Operand{Kind: OperandCopy, Place: relinquishLocal(1)}, HasValue: true,
					Value: Operand{Kind: OperandMove, Place: relinquishLocal(0)}}}
			},
			crosses: true,
		},
		{
			name:   "a local async body's return is not a boundary",
			instrs: func(ownershipTestTypes) []Instr { return nil },
			term: func(ot ownershipTestTypes) Terminator {
				return Terminator{Kind: TermAsyncReturn, AsyncReturn: AsyncReturnTerm{
					State: Operand{Kind: OperandCopy, Place: relinquishLocal(1)}, HasValue: true,
					Value: Operand{Kind: OperandMove, Place: relinquishLocal(0)}}}
			},
		},
		{
			name:   "a blocking body's return without the act is refused",
			instrs: func(ownershipTestTypes) []Instr { return nil },
			term: func(ot ownershipTestTypes) Terminator {
				return Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true,
					Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}}}
			},
			crosses: true,
			wantErr: "bb0 terminator: return value (L0, float) reaches the boundary without an un-share",
		},
		{
			name:   "a blocking body's return after the act passes",
			instrs: func(ownershipTestTypes) []Instr { return []Instr{unshareOf(0)} },
			term: func(ot ownershipTestTypes) Terminator {
				return Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true,
					Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}}}
			},
			crosses: true,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			ot := newOwnershipTestTypes(t)
			f := relinquishFixture(ot, row.instrs(ot), row.term(ot), row.crosses)
			err := validateRelinquishedOperandsArePrivate(f, ot.in)
			switch {
			case row.wantErr == "" && err != nil:
				t.Fatalf("unexpected refusal: %v", err)
			case row.wantErr != "" && err == nil:
				t.Fatalf("the shape passed; want %q", row.wantErr)
			case row.wantErr != "" && !strings.Contains(err.Error(), row.wantErr):
				t.Fatalf("refusal = %q, want it to contain %q", err, row.wantErr)
			}
		})
	}
}

// The post-split half sees only the operand's shape: a MOVE with no un-share
// in sight passes it (the act was checked before the split), a COPY or a
// RETAIN never does.
func TestRelinquishedOperandShapesAfterTheSplit(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	move := Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}
	f := relinquishFixture(ot, []Instr{relinquishSelect(move)}, Terminator{Kind: TermUnreachable}, false)
	if err := validateRelinquishedOperandShapes(f, ot.in); err != nil {
		t.Fatalf("a MOVE out of a bare local is the shape the runtime consumes: %v", err)
	}
	if err := validateRelinquishedOperandsArePrivate(f, ot.in); err == nil {
		t.Fatal("the same shape must still fail the act check: nothing un-shared L0")
	}
	for _, kind := range []OperandKind{OperandCopy, OperandRetain, OperandCopyValue} {
		op := move
		op.Kind = kind
		shaped := relinquishFixture(ot, []Instr{relinquishSelect(op)}, Terminator{Kind: TermUnreachable}, false)
		err := validateRelinquishedOperandShapes(shaped, ot.in)
		if err == nil || !strings.Contains(err.Error(), "reaches the boundary as "+kind.String()) {
			t.Fatalf("%s at a boundary must be refused by shape, got %v", kind, err)
		}
	}
	// A bare local a live child borrows is rewritten by the split into a
	// RESIDENT field of the frame (`__state.__resident$p$N`); the act was
	// checked on the bare local before the split, and the field is the same
	// storage, so the post-split shape rule reads a MOVE out of it as a bare
	// local. Found by the refuter of 2026-09-06: a `spawn on` capture of an
	// `own P{ v: a }` whose address had entered a child task was refused with
	// this validator's text instead of building. Any other projection stays
	// refused.
	resident := Place{Local: 0, Proj: []PlaceProj{{Kind: PlaceProjField, FieldName: residentFieldName(3, "p"), FieldIdx: -1}}}
	f = relinquishFixture(ot, []Instr{relinquishSelect(Operand{Kind: OperandMove, Type: ot.flt, Place: resident})},
		Terminator{Kind: TermUnreachable}, false)
	if err := validateRelinquishedOperandShapes(f, ot.in); err != nil {
		t.Fatalf("a MOVE out of a resident field is the bare local under its post-split name: %v", err)
	}
	projected := Place{Local: 0, Proj: []PlaceProj{{Kind: PlaceProjField, FieldName: "v", FieldIdx: 0}}}
	f = relinquishFixture(ot, []Instr{relinquishSelect(Operand{Kind: OperandMove, Type: ot.flt, Place: projected})},
		Terminator{Kind: TermUnreachable}, false)
	if err := validateRelinquishedOperandShapes(f, ot.in); err == nil || !strings.Contains(err.Error(), "reaches the boundary as Move L0.#0") {
		t.Fatalf("a MOVE out of an ordinary field projection must stay refused by shape, got %v", err)
	}
	// The structural validator carries the shape rule, so a hand-built module
	// reaches it through the public entry point too.
	f = relinquishFixture(ot, []Instr{relinquishSelect(Operand{Kind: OperandCopy, Type: ot.flt, Place: relinquishLocal(0)})},
		Terminator{Kind: TermUnreachable}, false)
	mod := &Module{Funcs: map[FuncID]*Func{0: f}}
	err := ValidateStructureWithOptions(mod, ot.in, ValidateOptions{
		CrossingForms: map[sema.CrossingLoweringKind]bool{sema.CrossingLoweringChannelSelect: true},
	})
	if err == nil || !strings.Contains(err.Error(), "reaches the boundary as Copy L0") {
		t.Fatalf("ValidateStructure must carry the shape rule, got %v", err)
	}
}

// The instruction prints as `unshare L`, and the dump is byte-stable for the
// release mnemonics it now shares its spelling helper with.
func TestUnshareInstructionPrints(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	f := relinquishFixture(ot, []Instr{
		unshareOf(0),
		{Kind: InstrEnvelopeRelease, EnvelopeRelease: EnvelopeReleaseInstr{Place: relinquishLocal(2), Cursor: true}},
		{Kind: InstrEnvelopeRelease, EnvelopeRelease: EnvelopeReleaseInstr{Place: relinquishLocal(2)}},
	}, Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true,
		Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}}}, true)
	var out bytes.Buffer
	if err := DumpModule(&out, &Module{Funcs: map[FuncID]*Func{0: f}}, ot.in, DumpOptions{}); err != nil {
		t.Fatalf("dump: %v", err)
	}
	const want = "  bb0:\n" +
		"    unshare L0\n" +
		"    release_cursor L2\n" +
		"    release_box L2\n" +
		"    return move L0\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("dump does not spell the instructions:\n--- got ---\n%s--- want fragment ---\n%s", out.String(), want)
	}
	if got := InstrUnshare.String(); got != "Unshare" {
		t.Fatalf("InstrUnshare.String() = %q", got)
	}
}

// The ownership verifier reads an un-share the way it reads a drop: the place
// must own the reference it gives up, so un-sharing an ALIAS is a finding of
// its own kind.
func TestUnshareOfAnAliasIsAnOwnershipFinding(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	f := relinquishFixture(ot, []Instr{
		{Kind: InstrAssign, Assign: AssignInstr{Dst: relinquishLocal(0), Src: RValue{Kind: RValueUse,
			Use: Operand{Kind: OperandConst, Type: ot.flt, Const: Const{Kind: ConstFloat, Type: ot.flt, Text: "1.5"}}}}},
		{Kind: InstrAssign, Assign: AssignInstr{Dst: relinquishLocal(2), Src: RValue{Kind: RValueUse,
			Use: Operand{Kind: OperandCopy, Type: ot.flt, Place: relinquishLocal(0)}}}},
		unshareOf(2),
		unshareOf(0),
	}, Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true,
		Value: Operand{Kind: OperandMove, Type: ot.flt, Place: relinquishLocal(0)}}}, true)
	findings := VerifyOwnership(&Module{Funcs: map[FuncID]*Func{0: f}}, ot.in, ot.sema)
	if len(findings) != 1 || findings[0].ConsumingKind != OwnershipSinkUnshare || findings[0].Local != 2 {
		t.Fatalf("want exactly one `unshare` finding on the alias L2, got %+v", findings)
	}
	if got := OwnershipSinkUnshare.String(); got != "unshare" {
		t.Fatalf("OwnershipSinkUnshare.String() = %q", got)
	}
}
