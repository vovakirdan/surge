package llvm

import (
	"strings"
	"testing"

	"surge/internal/types"
)

// The relinquishing walk, emitted against a real interner and a real layout
// registry: it reads member offsets and union case layouts, so a synthetic
// entry the way the plan rows use would prove nothing about the shapes here.
//
// The program declares the shapes and nothing crosses in it. That is on
// purpose for now: sema refuses to cross a value that may share a counted
// block (the stop-gap of Epic 22 step 4), so the walk has no call site yet and
// these rows pin the BODY it will emit when the relinquishing sites are wired.
const unshareWalkProbeProgram = `
type Counted = { v: float, n: int };

type Plain = { a: int, b: int };

type Nested = { inner: Counted, label: int };

tag Held(Counted);
tag Bare(Plain);
type Sum = Held(Counted) | Bare(Plain);

type WithArray = { xs: float[], n: int };

fn probe(c: Counted, p: Plain, n: Nested, s: Sum, w: WithArray, f: float) -> int {
    return p.a;
}

@entrypoint
fn main() -> int { return 0; }
`

// unshareProbe compiles the program above, then demands the walk for the types
// named by label and drains the fixpoint. It returns the emitted text, the
// label-to-id map, and the emitter, so a row can also ask the predicates.
func unshareProbe(t *testing.T, labels ...string) (string, map[string]types.TypeID, *Emitter) {
	t.Helper()
	mirMod, result := lowerMIRFromSource(t, unshareWalkProbeProgram)
	if mirMod.Meta == nil || mirMod.Meta.Layouts == nil {
		t.Fatal("no finalized layout registry was published")
	}
	in := result.Sema.TypeInterner
	e := &Emitter{mod: mirMod, types: in}

	ids := make(map[string]types.TypeID, len(labels))
	want := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		want[label] = struct{}{}
	}
	for id := types.TypeID(1); ; id++ {
		if _, ok := in.Lookup(id); !ok {
			break
		}
		label := types.Label(in, id)
		if _, ok := want[label]; !ok {
			continue
		}
		if _, seen := ids[label]; seen {
			continue
		}
		ids[label] = id
	}
	for _, label := range labels {
		if _, ok := ids[label]; !ok {
			t.Fatalf("the probe program never produced %q, so a row would pin nothing", label)
		}
	}
	for _, label := range labels {
		e.requireUnshareGlue(ids[label])
	}
	if err := e.emitUnshareGlue(); err != nil {
		t.Fatalf("emit unshare glue: %v", err)
	}
	return e.buf.String(), ids, e
}

// bodyOf returns the text of one emitted body, so a row can count calls inside
// it rather than across the whole module.
func bodyOf(t *testing.T, ir, name string) string {
	t.Helper()
	head := "define void @" + name + "(ptr %val) {"
	start := strings.Index(ir, head)
	if start < 0 {
		t.Fatalf("no body %q in:\n%s", name, ir)
	}
	rest := ir[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("body %q is unterminated in:\n%s", name, ir)
	}
	return rest[:end]
}

// A counted scalar member is read, made private, and written back. The store is
// the half that matters: a walk that called the helper and dropped its answer
// would leave the shared block in place and read as green.
func TestUnshareWalkMakesACountedFieldPrivate(t *testing.T) {
	ir, ids, _ := unshareProbe(t, "Counted")
	body := bodyOf(t, ir, unshareWalkName(ids["Counted"]))
	if n := strings.Count(body, "@rt_bigfloat_unshare("); n != 1 {
		t.Fatalf("Counted has one counted field; the walk called rt_bigfloat_unshare %d times:\n%s", n, body)
	}
	call := strings.Index(body, "@rt_bigfloat_unshare(")
	load := strings.LastIndex(body[:call], "load ptr")
	store := strings.Index(body[call:], "store ptr")
	if load < 0 {
		t.Fatalf("the walk called rt_bigfloat_unshare without loading the field:\n%s", body)
	}
	if store < 0 {
		t.Fatalf("the walk did not store the private reference back:\n%s", body)
	}
}

// A type with nothing counted in it emits a body that does nothing. The row
// exists so "the walk is cheap where there is no block" is a fact rather than
// an intention: a walk that visited plain members would cost every crossing.
func TestUnshareWalkOfAPlainTypeDoesNothing(t *testing.T) {
	ir, ids, _ := unshareProbe(t, "Plain")
	body := bodyOf(t, ir, unshareWalkName(ids["Plain"]))
	if strings.Contains(body, "call") {
		t.Fatalf("a type with no counted member emitted a call:\n%s", body)
	}
}

// A nested composite recurses into its OWN walk, and the fixpoint emits that
// body too. Inlining the nested members here instead would duplicate the walk
// at every enclosing type and drift from it.
func TestUnshareWalkRecursesIntoANestedComposite(t *testing.T) {
	ir, ids, _ := unshareProbe(t, "Nested", "Counted")
	outer := bodyOf(t, ir, unshareWalkName(ids["Nested"]))
	inner := unshareWalkName(ids["Counted"])
	if !strings.Contains(outer, "call void @"+inner+"(") {
		t.Fatalf("Nested did not recurse into %s:\n%s", inner, outer)
	}
	if strings.Contains(outer, "@rt_bigfloat_unshare(") {
		t.Fatalf("Nested inlined the nested walk instead of calling it:\n%s", outer)
	}
	if !strings.Contains(ir, "define void @"+inner+"(ptr %val) {") {
		t.Fatalf("the fixpoint did not emit the nested body %s:\n%s", inner, ir)
	}
}

// A union arm holding a counted scalar is reached. This is exactly where the
// emitter's own predicate must not be sema's ContainsRefCountedScalar, which
// stops at unions on purpose: a union crosses by move, and a move is what this
// walk serves.
func TestUnshareWalkReachesAUnionArm(t *testing.T) {
	ir, ids, e := unshareProbe(t, "Sum", "Counted")
	body := bodyOf(t, ir, unshareWalkName(ids["Sum"]))
	if !strings.Contains(body, "switch i32") {
		t.Fatalf("the union walk read no discriminant:\n%s", body)
	}
	if !strings.Contains(body, "call void @"+unshareWalkName(ids["Counted"])+"(") &&
		!strings.Contains(body, "@rt_bigfloat_unshare(") {
		t.Fatalf("the union walk reached no counted payload:\n%s", body)
	}
	if !e.typeMayShareCountedBlock(ids["Sum"]) {
		t.Fatal("the emitter's predicate says a union carrying a counted scalar shares nothing")
	}
}

// The one shape the walk cannot serve is refused at the caller, not skipped in
// silence. A container of counted elements needs a runtime iteration over its
// buffer, which is unbuilt; the pair below is what keeps a future call site
// from emitting a no-op for it and calling the value private.
func TestUnshareRefusesAContainerOfCountedElements(t *testing.T) {
	_, ids, e := unshareProbe(t, "WithArray", "Counted", "Plain")
	if !e.typeMayShareCountedBlock(ids["WithArray"]) {
		t.Fatal("a struct holding float[] must be reported as possibly sharing a counted block")
	}
	if e.canUnshareValue(ids["WithArray"]) {
		t.Fatal("the walk claimed it can make a container of counted elements private; it has no buffer walk")
	}
	if !e.canUnshareValue(ids["Counted"]) || !e.canUnshareValue(ids["Plain"]) {
		t.Fatal("the refusal spread to shapes the walk does serve")
	}
}
