package mir

import "testing"

// The place walk's whole value is that it misses nothing: a pass that redirects
// a local and skips one instruction leaves that use reading storage nobody
// writes, with no crash and no diagnostic. These tests make the miss impossible
// to add rather than merely unlikely -- each walks EVERY kind by value against
// the enum's own sentinel, so a kind introduced later fails here before it can
// be forgotten in the walker.

func TestPlaceWalkCoversEveryInstrKind(t *testing.T) {
	for kind := InstrKind(0); kind < instrKindCount; kind++ {
		instr := Instr{Kind: kind}
		// A crossing carries one of every nested shape, so give it the ones a
		// zero value cannot: an optional return place and one of each list.
		if kind == InstrCrossing {
			ret := Place{Local: LocalID(7)}
			instr.Crossing.RemoteOps = []CrossingRemoteOp{{ReturnPlace: &ret}}
			instr.Crossing.Captures = []CrossingCapture{{}}
			instr.Crossing.State.Fields = []StructLitField{{}}
		}
		if kind == InstrSelect {
			instr.Select.Arms = []SelectArm{
				{Kind: SelectArmTask}, {Kind: SelectArmChanRecv},
				{Kind: SelectArmChanSend}, {Kind: SelectArmTimeout},
				{Kind: SelectArmDefault},
			}
		}
		if err := instrPlaces(&instr, func(*Place) {}); err != nil {
			t.Errorf("InstrKind %d (%s) is not covered by the place walk: %v", kind, kind.String(), err)
		}
	}
}

func TestPlaceWalkCoversEveryRValueKind(t *testing.T) {
	for kind := RValueKind(0); kind < rvalueKindCount; kind++ {
		rv := RValue{Kind: kind}
		if err := rvaluePlaces(&rv, func(*Place) {}); err != nil {
			t.Errorf("RValueKind %d is not covered by the place walk: %v", kind, err)
		}
	}
}

func TestPlaceWalkCoversEveryTermKind(t *testing.T) {
	for kind := TermKind(0); kind < termKindCount; kind++ {
		term := Terminator{Kind: kind}
		if err := termPlaces(&term, func(*Place) {}); err != nil {
			t.Errorf("TermKind %d is not covered by the place walk: %v", kind, err)
		}
	}
}

func TestPlaceWalkCoversEveryOperandKind(t *testing.T) {
	for kind := OperandKind(0); kind < operandKindCount; kind++ {
		op := Operand{Kind: kind}
		if err := operandPlace(&op, func(*Place) {}); err != nil {
			t.Errorf("OperandKind %d is not covered by the place walk: %v", kind, err)
		}
	}
}

// Coverage is not the same as REACHING, so this one asks the other question: is
// every place a function mentions actually handed to the visitor? It builds one
// function whose places sit in the positions most easily forgotten -- an
// assignment's destination, a call's destination, a terminator's operand -- and
// requires every one of them back.
func TestPlaceWalkReachesDestinationsAndTerminators(t *testing.T) {
	local := func(id uint32) Place { return Place{Local: LocalID(id)} }
	f := &Func{
		Blocks: []Block{{
			Instrs: []Instr{
				{Kind: InstrAssign, Assign: AssignInstr{
					Dst: local(1),
					Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandCopy, Place: local(2)}},
				}},
				{Kind: InstrCall, Call: CallInstr{
					HasDst: true,
					Dst:    local(3),
					Callee: Callee{Kind: CalleeSym},
					Args:   []Operand{{Kind: OperandMove, Place: local(4)}},
				}},
				{Kind: InstrDrop, Drop: DropInstr{Place: local(5)}},
			},
			Term: Terminator{Kind: TermReturn, Return: ReturnTerm{
				HasValue: true,
				Value:    Operand{Kind: OperandCopy, Place: local(6)},
			}},
		}},
	}

	seen := map[LocalID]bool{}
	if err := forEachPlace(f, func(p *Place) { seen[p.Local] = true }); err != nil {
		t.Fatalf("place walk: %v", err)
	}
	for id := LocalID(1); id <= 6; id++ {
		if !seen[id] {
			t.Errorf("place walk did not reach local %d", id)
		}
	}
}

// A visitor may REWRITE, and that is the whole reason the walk hands out
// pointers rather than values. Without this the walk could silently be visiting
// copies and every caller would be a no-op.
func TestPlaceWalkVisitsPlacesInPlace(t *testing.T) {
	f := &Func{Blocks: []Block{{
		Instrs: []Instr{{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: LocalID(1)},
			Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandCopy, Place: Place{Local: LocalID(1)}}},
		}}},
	}}}

	if err := forEachPlace(f, func(p *Place) {
		if p.Local == LocalID(1) {
			p.Local = LocalID(9)
		}
	}); err != nil {
		t.Fatalf("place walk: %v", err)
	}

	assign := f.Blocks[0].Instrs[0].Assign
	if assign.Dst.Local != LocalID(9) {
		t.Errorf("destination was not rewritten: got %d", assign.Dst.Local)
	}
	if assign.Src.Use.Place.Local != LocalID(9) {
		t.Errorf("operand was not rewritten: got %d", assign.Src.Use.Place.Local)
	}
}
