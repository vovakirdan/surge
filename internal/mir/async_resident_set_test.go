package mir

import "testing"

// The end-to-end tests next door prove a resident FIELD appears and that the
// body addresses it. They cannot see the other half of the rule -- that a
// resident is excluded from the payload union -- because a resident that was
// promoted AND still packed produces exactly the same field. It would simply
// carry a stale copy of a place the child is mutating, which is the two-storage
// problem the promotion exists to remove, arrived at from the other side. So the
// exclusion is tested directly.

func TestResidentSetWithoutRemovesOnlyResidents(t *testing.T) {
	f := &Func{Locals: []Local{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}}
	residents := newResidentSet(f, []LocalID{1, 3})

	live := []LocalID{0, 1, 2, 3}
	got := residents.without(live)

	want := []LocalID{0, 2}
	if len(got) != len(want) {
		t.Fatalf("kept %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kept %v, want %v", got, want)
		}
	}

	// The caller's slice is a liveness set that is read again afterwards, so
	// filtering must not write through it. A version that filtered in place would
	// pass every assertion above and silently truncate the live set of whatever
	// asked next.
	if len(live) != 4 || live[0] != 0 || live[1] != 1 || live[2] != 2 || live[3] != 3 {
		t.Fatalf("the caller's live set was mutated: %v", live)
	}
}

func TestResidentSetWithoutIsIdentityWhenNothingIsPromoted(t *testing.T) {
	f := &Func{Locals: []Local{{Name: "a"}, {Name: "b"}}}
	residents := newResidentSet(f, nil)
	live := []LocalID{0, 1}
	got := residents.without(live)
	if len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("an activation that promotes nothing must pack everything; got %v", got)
	}
}

// A promoted place may itself be a composite whose field is read. The rewrite has
// to PREFIX the frame field and keep what was already there, or `v.x` silently
// becomes the whole of `v` -- a read of the wrong size from the right address,
// which no type check downstream is positioned to catch.
func TestResidentRewriteKeepsAnExistingProjection(t *testing.T) {
	f := &Func{
		Locals: []Local{{Name: "v"}, {Name: "__state"}},
		Blocks: []Block{{
			Instrs: []Instr{{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: LocalID(0), Proj: []PlaceProj{{Kind: PlaceProjField, FieldName: "x", FieldIdx: -1}}},
				Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandCopy, Place: Place{Local: LocalID(0)}}},
			}}},
		}},
	}
	residents := newResidentSet(f, []LocalID{0})
	if err := residents.rewritePlaces(f, LocalID(1)); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	dst := f.Blocks[0].Instrs[0].Assign.Dst
	if dst.Local != LocalID(1) {
		t.Fatalf("the place was not redirected onto the frame: local %d", int64(dst.Local))
	}
	if len(dst.Proj) != 2 {
		t.Fatalf("expected the frame field followed by the original projection, got %d projections", len(dst.Proj))
	}
	if dst.Proj[0].FieldName != residents.fields[LocalID(0)] {
		t.Fatalf("the frame field is not first: %q", dst.Proj[0].FieldName)
	}
	if dst.Proj[1].FieldName != "x" {
		t.Fatalf("the original projection was lost: %q", dst.Proj[1].FieldName)
	}

	src := f.Blocks[0].Instrs[0].Assign.Src.Use.Place
	if src.Local != LocalID(1) || len(src.Proj) != 1 || src.Proj[0].FieldName != residents.fields[LocalID(0)] {
		t.Fatalf("the operand was not redirected: %+v", src)
	}
}

// A local nobody promoted is left exactly as it was. Without this, a rewrite that
// redirected everything would satisfy the test above and move every local in the
// activation into the frame.
func TestResidentRewriteLeavesUnpromotedLocalsAlone(t *testing.T) {
	f := &Func{
		Locals: []Local{{Name: "v"}, {Name: "w"}, {Name: "__state"}},
		Blocks: []Block{{
			Instrs: []Instr{{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: LocalID(1)},
				Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandCopy, Place: Place{Local: LocalID(1)}}},
			}}},
		}},
	}
	residents := newResidentSet(f, []LocalID{0})
	if err := residents.rewritePlaces(f, LocalID(2)); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	dst := f.Blocks[0].Instrs[0].Assign.Dst
	if dst.Local != LocalID(1) || len(dst.Proj) != 0 {
		t.Fatalf("an unpromoted local was moved into the frame: %+v", dst)
	}
}
