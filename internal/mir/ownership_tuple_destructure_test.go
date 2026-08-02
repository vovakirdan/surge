package mir_test

import (
	"testing"

	"surge/internal/mir"
)

const ownershipTupleDestructureSource = `
fn produce() -> (int, string, bool) {
    return (1, "hello", true);
}

fn main() {
    let (a, b, c) = produce();
    let a_value = a;
    let b_value = b;
    let c_value = c;
}

fn discards_string() {
    let (number, _, flag) = produce();
    let _ = number;
    let _ = flag;
}

fn produce_nested() -> ((string, int), bool) {
    return (("nested", 7), true);
}

fn destructures_nested() {
    let ((text, number), flag) = produce_nested();
    let text_value = text;
    let _ = number;
    let _ = flag;
}
`

func TestOwnershipTupleDestructureTransfersFieldsAndReleasesEnvelope(t *testing.T) {
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipTupleDestructureSource)
	findings := mir.VerifyOwnership(mod, typesIn, semaRes)
	if got := findingsIn(findings, "main"); len(got) != 0 {
		t.Errorf("tuple destructure should be ownership-clean, got:\n%s", joinLines(got))
	}

	var fieldReads, movedFields, shallowReleases int
	for _, fn := range mod.Funcs {
		if fn == nil || fn.Name != "main" {
			continue
		}
		for bi := range fn.Blocks {
			for ii := range fn.Blocks[bi].Instrs {
				ins := &fn.Blocks[bi].Instrs[ii]
				if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueField {
					fieldReads++
					if ins.Assign.Src.Field.MoveOut {
						movedFields++
					}
				}
				if ins.Kind == mir.InstrDrop && ins.Drop.Shallow {
					shallowReleases++
				}
			}
		}
	}
	if fieldReads != 3 || movedFields != fieldReads {
		t.Errorf("tuple field transfers = %d/%d, want 3/3", movedFields, fieldReads)
	}
	if shallowReleases != 1 {
		t.Errorf("tuple shallow envelope releases = %d, want 1", shallowReleases)
	}
}

func TestOwnershipTupleDestructureReclaimsIgnoredFields(t *testing.T) {
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipTupleDestructureSource)
	findings := mir.VerifyOwnership(mod, typesIn, semaRes)
	if got := findingsIn(findings, "discards_string"); len(got) != 0 {
		t.Errorf("tuple destructure with ignored field should be ownership-clean, got:\n%s", joinLines(got))
	}

	var projectedDrops, shallowReleases int
	for _, fn := range mod.Funcs {
		if fn == nil || fn.Name != "discards_string" {
			continue
		}
		for bi := range fn.Blocks {
			for ii := range fn.Blocks[bi].Instrs {
				ins := &fn.Blocks[bi].Instrs[ii]
				if ins.Kind != mir.InstrDrop {
					continue
				}
				if ins.Drop.Shallow {
					shallowReleases++
				} else if len(ins.Drop.Place.Proj) != 0 {
					projectedDrops++
				}
			}
		}
	}
	if projectedDrops != 1 || shallowReleases != 1 {
		t.Errorf("ignored tuple field cleanup = %d projected + %d shallow, want 1 + 1",
			projectedDrops, shallowReleases)
	}
}

func TestOwnershipTupleDestructureRecursesIntoNestedPatterns(t *testing.T) {
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipTupleDestructureSource)
	findings := mir.VerifyOwnership(mod, typesIn, semaRes)
	if got := findingsIn(findings, "destructures_nested"); len(got) != 0 {
		t.Errorf("nested tuple destructure should be ownership-clean, got:\n%s", joinLines(got))
	}

	var movedFields, shallowReleases int
	for _, fn := range mod.Funcs {
		if fn == nil || fn.Name != "destructures_nested" {
			continue
		}
		for bi := range fn.Blocks {
			for ii := range fn.Blocks[bi].Instrs {
				ins := &fn.Blocks[bi].Instrs[ii]
				if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueField &&
					ins.Assign.Src.Field.MoveOut {
					movedFields++
				}
				if ins.Kind == mir.InstrDrop && ins.Drop.Shallow {
					shallowReleases++
				}
			}
		}
	}
	if movedFields != 4 || shallowReleases != 2 {
		t.Errorf("nested tuple cleanup = %d moved fields + %d shallow releases, want 4 + 2",
			movedFields, shallowReleases)
	}
}
