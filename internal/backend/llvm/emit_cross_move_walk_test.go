package llvm

import (
	"regexp"
	"strings"
	"testing"

	"surge/internal/types"
)

// The relinquishing walk, emitted against a real interner and a real layout
// registry: it reads member offsets and union case layouts, so a synthetic
// entry the way the plan rows use would prove nothing about the shapes here.
//
// The program declares the shapes and nothing crosses in it, on purpose: the
// relinquishing sites are wired (emit_unshare_sites_test.go pins where each
// call sits), and these rows pin the BODY a shape gets, in isolation from any
// site, so a body that drifts is red by its own name.
const unshareWalkProbeProgram = `
type Counted = { v: float, n: int64 };

type Plain = { a: int64, b: int64 };

type Nested = { inner: Counted, label: int64 };

tag Held(Counted);
tag Bare(Plain);
type Sum = Held(Counted) | Bare(Plain);

type WithArray = { xs: float[], n: int64 };

type WithChannel = { ch: Channel<float>, n: int64 };

type CountedInt = { v: int };
type CountedUint = { v: uint };
type WithCountedArraySibling = { xs: float[], n: int };

fn probe(c: Counted, p: Plain, n: Nested, s: Sum, w: WithArray, f: float, xss: float[][], wc: WithChannel, m: Map<int, float>,
         xo: Option<float>[], ci: CountedInt, cu: CountedUint, ca: WithCountedArraySibling) -> int {
    return p.a to int;
}

@entrypoint
fn main() -> int { return 0; }
`

// unshareProbe compiles the program above, then demands the walk for the types
// named by label and drains the fixpoint. It returns the emitted text, the
// label-to-id map, and the emitter, so a row can also ask the predicates.
func unshareProbe(t *testing.T, labels ...string) (string, map[string]types.TypeID, *Emitter) {
	t.Helper()
	return unshareProbeFrom(t, unshareWalkProbeProgram, labels...)
}

// unshareProbeFrom is unshareProbe over a caller-supplied program, so a family
// of shapes that needs its own declarations -- the arrays whose elements have
// nothing to make private -- gets them without growing the program above, whose
// rows are about counted members.
func unshareProbeFrom(t *testing.T, program string, labels ...string) (string, map[string]types.TypeID, *Emitter) {
	t.Helper()
	mirMod, result := lowerMIRFromSource(t, program)
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

// Fixed-width neighbours preserve the old single-leaf/no-op probes above.
// These twins retain the unbounded numeric shapes: their heap arm must still
// update the original slot, including a counted sibling beside an array slot.
func TestUnshareWalkKeepsCountedNumericFields(t *testing.T) {
	for _, tc := range []struct{ label, kind string }{
		{"CountedInt", "int"}, {"CountedUint", "uint"}, {"WithCountedArraySibling", "int"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			ir, ids, e := unshareProbe(t, tc.label)
			if !e.typeMayShareCountedBlock(ids[tc.label]) || !e.typeNeedsRelinquishWalk(ids[tc.label]) || !e.canUnshareValue(ids[tc.label]) {
				t.Fatalf("%s must demand a supported counted walk", tc.label)
			}
			body := bodyOf(t, ir, unshareWalkName(ids[tc.label]))
			assertTaggedUnshareLeaf(t, body, tc.kind)
			if tc.label == "WithCountedArraySibling" {
				if calls := arrayWalkCalls(t, body); len(calls) != 1 || calls[0][3] == "null" {
					t.Fatalf("the float array sibling must still pass its element walk: %v\n%s", calls, body)
				}
				if !strings.Contains(body, "getelementptr inbounds i8, ptr %val, i64 8\n") {
					t.Fatalf("the counted sibling's slot must be at offset eight:\n%s", body)
				}
			}
		})
	}
}

func assertTaggedUnshareLeaf(t *testing.T, body, kind string) {
	t.Helper()
	callRe := regexp.MustCompile(`(%g\d+) = call ptr @rt_big` + kind + `_unshare\(ptr (%g\d+)\)`)
	calls := callRe.FindAllStringSubmatch(body, -1)
	if len(calls) != 1 {
		t.Fatalf("want one %s unshare leaf, got %d:\n%s", kind, len(calls), body)
	}
	call := strings.Index(body, calls[0][0])
	guard := body[:call]
	if !strings.Contains(guard, "and i64 ") || !strings.Contains(guard, "icmp ne ptr "+calls[0][2]+", null") || !strings.Contains(guard, "br i1 ") {
		t.Fatalf("the numeric leaf must be behind both tag and NULL guards:\n%s", body)
	}
	if !strings.Contains(body[call:], "store ptr "+calls[0][1]+",") {
		t.Fatalf("the numeric leaf's private reference was not stored back:\n%s", body)
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

// A dynamic array's elements sit in a buffer the runtime owns, at no offset
// this walk can address, so the body hands the array's SLOT to the runtime
// together with the element stride and the element's own walk body, and the
// runtime makes each element private in place. The member is not read out:
// the runtime's array helpers take the slot, never the handle word. Exactly
// one such call per array member, and no inline counted-leaf work for it --
// an inline rt_bigfloat_unshare here would be un-sharing the handle word.
func TestUnshareWalkHandsAContainersBufferToTheRuntime(t *testing.T) {
	ir, ids, e := unshareProbe(t, "WithArray", "float")
	if !e.typeMayShareCountedBlock(ids["WithArray"]) || !e.canUnshareValue(ids["WithArray"]) {
		t.Fatal("a struct holding float[] must be reported as sharing and as one the walk can make private")
	}
	body := bodyOf(t, ir, unshareWalkName(ids["WithArray"]))
	elemBody := unshareWalkName(ids["float"])
	walkCall := regexp.MustCompile(`call void @rt_array_unshare_walk\(ptr %g\d+, i64 8, ptr @` + regexp.QuoteMeta(elemBody) + `\)`)
	if n := len(walkCall.FindAllString(body, -1)); n != 1 {
		t.Fatalf("WithArray has one array member; the walk handed the runtime a buffer %d times (want one call with stride 8 and the float body):\n%s", n, body)
	}
	if strings.Contains(body, "@rt_bigfloat_unshare(") {
		t.Fatalf("the struct's walk un-shared inline where only the runtime can reach the elements:\n%s", body)
	}
	if strings.Contains(body, "load ptr") {
		t.Fatalf("the walk read the handle word out; the runtime takes the slot:\n%s", body)
	}
	elem := bodyOf(t, ir, elemBody)
	if !strings.Contains(elem, "load ptr, ptr ") || !strings.Contains(elem, "call ptr @rt_bigfloat_unshare(") || !strings.Contains(elem, "store ptr ") {
		t.Fatalf("the element body the runtime calls per slot must load, un-share and store back:\n%s", elem)
	}
}

// Nesting drains through the one worklist: the outer array's body walks its
// buffer with the inner array's body, whose own body walks ITS buffer with the
// float body. Three bodies, two runtime calls, each naming the next one down.
func TestUnshareWalkNestsThroughAnArrayOfArrays(t *testing.T) {
	ir, ids, e := unshareProbe(t, "Array<Array<float>>", "Array<float>", "float")
	if !e.canUnshareValue(ids["Array<Array<float>>"]) {
		t.Fatal("the walk must serve an array of arrays of floats through nesting")
	}
	outer := bodyOf(t, ir, unshareWalkName(ids["Array<Array<float>>"]))
	innerName := unshareWalkName(ids["Array<float>"])
	if n := strings.Count(outer, "call void @rt_array_unshare_walk("); n != 1 || !strings.Contains(outer, "ptr @"+innerName+")") {
		t.Fatalf("the outer body must hand its buffer to the runtime once, with the inner array's body (%d calls):\n%s", n, outer)
	}
	inner := bodyOf(t, ir, innerName)
	if !strings.Contains(inner, "getelementptr inbounds i8, ptr %val, i64 0\n") {
		t.Fatalf("the inner body receives one element slot and must walk it at offset zero:\n%s", inner)
	}
	floatName := unshareWalkName(ids["float"])
	if n := strings.Count(inner, "call void @rt_array_unshare_walk("); n != 1 || !strings.Contains(inner, "ptr @"+floatName+")") {
		t.Fatalf("the inner body must hand its buffer to the runtime once, with the float body (%d calls):\n%s", n, inner)
	}
	if !strings.Contains(ir, "define void @"+floatName+"(ptr %val) {") {
		t.Fatalf("the fixpoint did not emit the float body %s:\n%s", floatName, ir)
	}
}

// The element body of an array whose element is a UNION the program never
// builds: `Option<float>[]` reaches the probe only as a parameter, no
// expression in it names `Option<float>`, and the union's membership is
// therefore something the module must publish from the array's TYPE alone.
// The walk reads that membership to switch on the tag, and it fails closed
// without it -- so before the lowering looked through a handle to its
// payload, this shape was admitted by sema and refused by the emitter. The
// outer body hands the buffer to the runtime with the union's stride; the
// union body switches on the discriminant and un-shares the float payload.
func TestUnshareWalkReadsAnElementUnionTheFunctionNeverBuilds(t *testing.T) {
	ir, ids, e := unshareProbe(t, "Array<Option<float>>", "Option<float>")
	if !e.canUnshareValue(ids["Array<Option<float>>"]) {
		t.Fatal("the walk must serve an array of optional floats; its element union's membership comes from the array's type, not from an expression that builds one")
	}
	outer := bodyOf(t, ir, unshareWalkName(ids["Array<Option<float>>"]))
	elemBody := unshareWalkName(ids["Option<float>"])
	walkCall := regexp.MustCompile(`call void @rt_array_unshare_walk\(ptr %g\d+, i64 16, ptr @` + regexp.QuoteMeta(elemBody) + `\)`)
	if n := len(walkCall.FindAllString(outer, -1)); n != 1 {
		t.Fatalf("the outer body must hand its buffer to the runtime once, with the union's 16-byte stride and the union body (%d such calls):\n%s", n, outer)
	}
	elem := bodyOf(t, ir, elemBody)
	if !strings.Contains(elem, "switch i32") {
		t.Fatalf("the union body read no discriminant, so it cannot know which slot holds a float:\n%s", elem)
	}
	if n := strings.Count(elem, "call ptr @rt_bigfloat_unshare("); n != 1 {
		t.Fatalf("Option<float> holds one counted payload; the union body un-shared %d:\n%s", n, elem)
	}
}

// What the walk still cannot serve is refused at the caller, not skipped in
// silence: a handle whose payload may share and whose storage no per-element
// walk reaches -- a channel's ring, a map's table. The pair below keeps a
// relinquishing site from emitting a no-op for one and calling it private.
func TestUnshareRefusesAHandleWhoseRingStaysBehind(t *testing.T) {
	_, ids, e := unshareProbe(t, "WithChannel", "Map<int, float>", "Counted", "Plain")
	for _, label := range []string{"WithChannel", "Map<int, float>"} {
		if !e.typeMayShareCountedBlock(ids[label]) {
			t.Fatalf("%s must be reported as possibly sharing a counted block", label)
		}
		if e.canUnshareValue(ids[label]) {
			t.Fatalf("the walk claimed it can make %s private; nothing reaches a ring or a table", label)
		}
	}
	if !e.canUnshareValue(ids["Counted"]) || !e.canUnshareValue(ids["Plain"]) {
		t.Fatal("the refusal spread to shapes the walk does serve")
	}
}
