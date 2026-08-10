package llvm

import (
	"strconv"
	"strings"
	"testing"
)

// Glue emission used to walk the requested-type sets as maps, so two
// compilations of one unchanged program placed the same drop and clone bodies
// at different points in the module. Nothing about the program changed between
// the runs; only Go's map iteration order did.
//
// The cost is not cosmetic. Comparing two builds is how a representation change
// is shown to have changed only what it meant to change, and against output
// that moves on its own that comparison cannot be made — every diff is noise
// plus signal with no way to separate them.
//
// Repeating the emission in one process is what makes this test able to fail:
// Go randomizes map iteration per range, so a single re-emission would agree by
// luck often enough to be useless.
func TestEmittedGlueOrderIsStableAcrossCompilations(t *testing.T) {
	const src = `
type Alpha = { note: string, id: int }
type Beta = { label: string, alpha: Alpha }
type Gamma = { mark: string, beta: Beta, alpha: Alpha }

fn build() -> int {
	let a: Alpha = Alpha{ note: "a", id: 1 };
	let b: Beta = Beta{ label: "b", alpha: Alpha{ note: "n", id: 2 } };
	let g: Gamma = Gamma{ mark: "g", beta: b, alpha: a };
	return g.alpha.id;
}

@entrypoint
fn main() -> int { return build(); }
`
	mirMod, result := lowerMIRFromSource(t, src)

	first, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// A program whose glue set has one member cannot order it wrongly, so the
	// fixture is checked to actually pose the question before the answer is
	// trusted.
	if got := strings.Count(first, "define void @drop.type"); got < 3 {
		t.Fatalf("fixture emits %d composite drop bodies, need at least 3 to order", got)
	}

	const compilations = 24
	for i := 1; i < compilations; i++ {
		again, emitErr := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
		if emitErr != nil {
			t.Fatalf("emit %d: %v", i, emitErr)
		}
		if again != first {
			t.Fatalf("compilation %d emitted different IR for the same module:\n%s",
				i, firstDifferingLine(first, again))
		}
	}
}

// firstDifferingLine names where two emissions parted, so a failure reports the
// divergence rather than two whole modules.
func firstDifferingLine(left, right string) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")
	for i := range leftLines {
		if i >= len(rightLines) {
			return "second emission ended early at line " + strconv.Itoa(i+1)
		}
		if leftLines[i] != rightLines[i] {
			return "line " + strconv.Itoa(i+1) + ":\n  first:  " + leftLines[i] + "\n  second: " + rightLines[i]
		}
	}
	if len(rightLines) > len(leftLines) {
		return "first emission ended early at line " + strconv.Itoa(len(leftLines)+1)
	}
	return "no differing line found"
}
