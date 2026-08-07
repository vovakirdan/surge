package llvm

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The two alignments written as constants must stay the alignments of the types
// they stand for. They exist so that an emission whose LLVM type is already
// fixed at the call site cannot fail on that type; that convenience is only
// safe while the two spellings agree.
func TestLiteralAlignmentsMatchTheirTypes(t *testing.T) {
	for _, tc := range []struct {
		llvmTy string
		want   uint64
	}{
		{"ptr", alignPtr},
		{"i64", alignWord},
	} {
		got, err := naturalAlign(tc.llvmTy)
		if err != nil {
			t.Fatalf("naturalAlign(%q): %v", tc.llvmTy, err)
		}
		if got != tc.want {
			t.Errorf("naturalAlign(%q) = %d, but the constant used for it is %d", tc.llvmTy, got, tc.want)
		}
	}
}

// naturalAlign refuses a type it does not know rather than defaulting, because
// a defaulted alignment is exactly the guess the explicit operands exist to
// remove — and a wrong one is silent until a target that cannot fix up a
// misaligned access runs the code.
func TestNaturalAlignRefusesAnUnknownType(t *testing.T) {
	for _, llvmTy := range []string{"", "[16 x i8]", "%struct.Thing", "void"} {
		if _, err := naturalAlign(llvmTy); err == nil {
			t.Errorf("naturalAlign(%q) answered instead of refusing", llvmTy)
		}
	}
}

const alignmentFixture = `
@copy type Leaf = { x: int, y: int }
type Holder = { note: string, leaf: Leaf, cells: Leaf[3] }

fn build(note: string) -> Holder {
	return Holder{
		note: note,
		leaf: Leaf{ x: 1, y: 2 },
		cells: [Leaf{x=1,y=1}, Leaf{x=2,y=2}, Leaf{x=3,y=3}],
	};
}

fn total(h: Holder) -> int { return h.leaf.x + h.cells[1].y; }

@entrypoint
fn main() -> int {
	let h: Holder = build("h");
	let arr: int[4] = [1, 2, 3, 4];
	let mut sum: int = 0;
	for v in arr { sum = sum + v; }
	return total(h) + sum;
}
`

var unattributedAlloca = regexp.MustCompile(`^\s*%\S+ = alloca [^,]+$`)

// No reserved storage anywhere in an emitted module leaves its alignment to be
// guessed. This one is stated over the WHOLE module, with no exceptions, and it
// is the half of the rule that matters most: an `alloca` with no alignment
// takes the preferred alignment of what it holds, and for a byte array — the
// shape inline value storage takes — that is align 1, while an unattributed
// `store i64` through the same address still claims align 8. The two halves of
// one access then disagree, and nothing reports it.
func TestEmittedAllocasCarryAnAlignment(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, alignmentFixture)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	var offenders []string
	for i, line := range strings.Split(ir, "\n") {
		if unattributedAlloca.MatchString(line) {
			offenders = append(offenders, strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("%d allocas carry no alignment:\n%s", len(offenders), strings.Join(offenders, "\n"))
	}
}

// ordinaryStorageEmitters are the files that move a Surge VALUE between
// storage and a register: locals and globals, the place walk that addresses
// them, and the literal, assignment, access, call and return paths that read
// and write through it.
//
// This is the memory whose alignment stops being a scalar's natural alignment
// once a composite is stored inline at its own layout, so it is the memory the
// rule has to hold over. The rest of the backend moves runtime words — status
// codes, handles, transport slots — through protocols of their own, and those
// surfaces convert with the waves that own them; the list is a ratchet and
// grows, and no name may leave it.
var ordinaryStorageEmitters = []string{
	"emit_access.go",
	"emit_call_site.go",
	"emit_func.go",
	"emit_globals.go",
	"emit_helpers_place.go",
	"emit_instr.go",
	// Building a value from its default is building a value. It was missing
	// here, and the omission is why this file kept writing composites through a
	// register and stores with no alignment for a whole wave after the literal
	// path stopped: a list that names the literal emitter but not the default
	// emitter describes half of how a value comes into existence.
	"emit_intrinsics_default.go",
	"emit_literals.go",
	"emit_term.go",
}

var rawMemoryOperation = regexp.MustCompile(`"\s*(?:%s = alloca|%s = load|store )[^"]*\\n"`)

// Every memory operation on the ordinary value path is written through the
// helpers that carry an alignment, rather than formatted at the call site.
//
// This is checked against the SOURCE rather than against emitted IR on purpose.
// Emitted IR only shows the paths a fixture happened to take, so a formatting
// site nothing in the fixture reaches would read as compliance; the source
// shows every site there is.
func TestOrdinaryStorageEmittersUseTheAlignedHelpers(t *testing.T) {
	for _, name := range ordinaryStorageEmitters {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for i, line := range strings.Split(string(source), "\n") {
			if !strings.Contains(line, "Fprintf") || !rawMemoryOperation.MatchString(line) {
				continue
			}
			if strings.Contains(line, ", align ") {
				// A literal type carries its alignment inline; the constants it
				// uses are pinned by TestLiteralAlignmentsMatchTheirTypes.
				continue
			}
			t.Errorf("%s:%d formats a memory operation without an alignment:\n\t%s",
				name, i+1, strings.TrimSpace(line))
		}
	}
}
