package mir_test

import (
	"testing"

	"surge/internal/mir"
)

// compareScrutineeOwnership has THREE outcomes and TagPayload.MoveOut is the
// one field carrying the answer into MIR, so all three have to be pinned here.
// Two of them agree on the flag and differ on everything downstream, which is
// the whole reason the flag exists: HIR's SubjectBorrowed collapses borrowed
// and duplicated together, and a verifier deriving ownership from the subject's
// own provenance cannot tell them apart either — both mint a subject, only one
// gets the narrowed release that makes an unretained extraction safe.
//
// The scrutinee temp's release is what separates the two: a BORROWED subject
// gets none (the union belongs to whoever the reference points at), while a
// DUPLICATED one is a genuine clone this compare reclaims.
const tagPayloadMoveOutSource = `
tag Payload(string);
tag Empty();
type Slot = Payload(string) | Empty;

@copy type Cell = { a: int, b: int };
tag Held(Cell);
tag Absent();
@copy type Holder = Held(Cell) | Absent;

fn peek(x: &string) -> int { return 1; }

// scrutineeBorrowed: reading a move-only union THROUGH a reference. The deref
// strips the reference, so the subject's type is a bare union and only the
// expression shape says this compare owns nothing.
fn reads_borrowed(slot: &Slot) -> int {
    return compare *slot {
        Payload(s) => peek(&s);
        Empty() => 0;
    };
}

// scrutineeDuplicated: a @copy value-composite union read through the SAME
// deref. The read clones, so the compare owns its envelope — and still
// deep-drops it, payload included, which is why the payload must not transfer.
fn reads_duplicated(h: &Holder) -> int {
    return compare *h {
        Held(c) => c.a;
        _ => 0 - 1;
    };
}

// scrutineeMoved: an owned subject handed to the compare outright. Its envelope
// gets the narrowed release, so the payload really does leave it.
fn owns_its_subject() -> int {
    let slot: Slot = Payload("v");
    return compare slot {
        Payload(s) => peek(&s);
        Empty() => 0;
    };
}

fn main() -> int { return 0; }
`

// scrutineeShape is what one compare left behind in MIR.
type scrutineeShape struct {
	payloads []bool // MoveOut, one per tag-payload read
	released bool   // the scrutinee temp is dropped or envelope-released
}

func compareShapeIn(t *testing.T, mod *mir.Module, fnName string) scrutineeShape {
	t.Helper()
	var shape scrutineeShape
	subjects := make(map[mir.LocalID]bool)
	found := false
	for _, id := range mod.SortedFuncIDs() {
		f := mod.Funcs[id]
		if f == nil || f.Name != fnName {
			continue
		}
		found = true
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				switch ins.Kind {
				case mir.InstrAssign:
					if ins.Assign.Src.Kind != mir.RValueTagPayload {
						continue
					}
					shape.payloads = append(shape.payloads, ins.Assign.Src.TagPayload.MoveOut)
					subjects[ins.Assign.Src.TagPayload.Value.Place.Local] = true
				case mir.InstrDrop:
					if subjects[ins.Drop.Place.Local] {
						shape.released = true
					}
				case mir.InstrEnvelopeRelease:
					if subjects[ins.EnvelopeRelease.Place.Local] {
						shape.released = true
					}
				}
			}
		}
	}
	if !found {
		t.Fatalf("no function named %q in module", fnName)
	}
	return shape
}

func TestTagPayloadMoveOutMatchesScrutineeOwnership(t *testing.T) {
	compiled := compileCrossingMIR(t, tagPayloadMoveOutSource, nil)

	cases := []struct {
		fn           string
		outcome      string
		wantMoveOut  bool
		wantReleased bool
	}{
		{"reads_borrowed", "scrutineeBorrowed", false, false},
		{"reads_duplicated", "scrutineeDuplicated", false, true},
		{"owns_its_subject", "scrutineeMoved", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.outcome, func(t *testing.T) {
			shape := compareShapeIn(t, compiled.mod, tc.fn)
			if len(shape.payloads) == 0 {
				t.Fatalf("%s: no tag-payload read reached MIR", tc.fn)
			}
			for i, got := range shape.payloads {
				if got != tc.wantMoveOut {
					t.Errorf("%s: payload read %d MoveOut = %v, want %v",
						tc.fn, i, got, tc.wantMoveOut)
				}
			}
			if shape.released != tc.wantReleased {
				t.Errorf("%s: scrutinee released = %v, want %v",
					tc.fn, shape.released, tc.wantReleased)
			}
		})
	}

	// The pairwise assertion the table above only implies: no two outcomes may
	// collapse into one MIR shape, or the flag has stopped carrying the third
	// answer and the borrowed/duplicated counterexample is back.
	shapes := map[string]scrutineeShape{}
	for _, tc := range cases {
		shapes[tc.outcome] = compareShapeIn(t, compiled.mod, tc.fn)
	}
	borrowed, duplicated := shapes["scrutineeBorrowed"], shapes["scrutineeDuplicated"]
	if borrowed.released == duplicated.released {
		t.Errorf("borrowed and duplicated scrutinees are indistinguishable in MIR")
	}
	if duplicated.payloads[0] == shapes["scrutineeMoved"].payloads[0] {
		t.Errorf("duplicated and moved payload reads carry the same MoveOut")
	}
}
