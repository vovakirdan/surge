package llvm

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"surge/internal/sema"
	"surge/internal/types"
)

// The drop question has two independent answers in this compiler, and this file
// makes them face each other.
//
// Sema's `CapabilityClassifier` answers it semantically: with the value stored
// inline, does releasing it have to run anything, and what inside it makes that
// so. The backend answers it by emitting code: `@drop.typeN` is the program's
// actual reclamation, and what it calls is what really gets released.
//
// The comparison is deliberately NOT of the two VERDICTS. `CarrierDroppable`
// and `ownsHeap` were designed to disagree on verdicts while a composite was a
// box — a Copy struct of two integers owned the box and nothing else — and a
// test demanding equal booleans would have demanded the design be wrong for
// that whole period. They agree now that storage is inline
// (`TestCapabilityDroppableAgreesWithOwnsHeap` in `internal/sema` pins it),
// and this leg still compares what it always did. What may not differ is the
// SET OF OWNING LEAVES each side reaches: whatever the verdict, the same
// strings, counted scalars and container buffers have to be named by both.
//
// It is also deliberately not a query against `dropGlueNeeded`. That map is
// written by `requireDropGlue` unconditionally, for every type anyone mentions,
// so reading it back asks the subject to confirm its own bookkeeping: break the
// classification completely and such a test stays green. Everything below reads
// either the classifier's published verdicts or the emitted IR — inputs the
// test supplies and outputs the compiler produced, never the notes it kept.

// The leaf families this compiler has a reclamation for. A family is named
// once, here, on both sides of the comparison: the sema side derives it from
// the type, the backend side from the runtime helper the emitted body calls.
const (
	leafString        = "string"
	leafCountedScalar = "counted scalar"
	leafElementBuffer = "container element buffer"
	// The runtime-owned objects a handle names, one family per release
	// helper. They were one family, "opaque runtime resource", for as long
	// as none of them had a release; the channel's arrival split it, because
	// a family the glue reclaims has to be matched helper by helper.
	leafChannelHandle = "channel handle"
	leafTaskHandle    = "task handle"
	leafMap           = "map"
	leafRange         = "range"

	// The lease a far channel handle holds on the owner shard's registry
	// entry; its release is `rt_far_channel_handle_drop`, reached by the glue
	// since 2026-09-03 (before that only a far LOCAL's scope-exit drop reached
	// it, and a far handle held as a FIELD was never released).
	leafFarLease = "far lease"
)

// unreclaimedFamilies is the standing list of leaf families sema calls
// droppable and this backend cannot reclaim at all. It is written once and read
// by both legs: the agreement leg refuses to compare across one of them, and the
// empty-body leg requires every one of them to appear as a recorded row.
//
// A family leaves this list only when a reclamation for it is emitted, and the
// day it does, both legs start failing until their rows are updated — which is
// the intended amount of noise for a carrier family becoming real. The channel
// left it on 2026-08-26: its release is `rt_channel_handle_drop`. The far lease
// left it on 2026-09-03: its release is `rt_far_channel_handle_drop`. The list
// is empty and stays declared: a family that loses its release goes back in
// here with a recorded excuse, and both legs read the list rather than assume
// it.
var unreclaimedFamilies = []string{}

func isUnreclaimedFamily(family string) bool {
	for _, unreclaimed := range unreclaimedFamilies {
		if family == unreclaimed {
			return true
		}
	}
	return false
}

// dropGlueAgreementSource is one program holding every leaf family the backend
// can reclaim, once as a root and once buried inside a composite.
//
// Every type is USED, not merely declared: layout roots are collected from what
// the final MIR mentions, so an unused type has no frozen layout and no glue
// could be emitted for it whatever this test asked.
const dropGlueAgreementSource = `
type Text = { note: string }

type Holder = {
	label: string,
	inner: Text,
	cells: string[2],
	words: string[],
	scale: float,
}

@copy type Point = { x: int, y: int }

fn build() -> int {
	let h: Holder = Holder{
		label: "label",
		inner: Text{ note: "note" },
		cells: ["first", "second"],
		words: ["only"],
		scale: 1.5,
	};
	let p: Point = Point{ x: 1, y: 2 };
	return len(h.label):int + p.x;
}

// The carrier families. The channel is reclaimed -- by the handle release,
// as a root and as the field of Gate, the composite that HOLDS a carrier, in
// the shape core/sync.sg gives every primitive. The far handle is reclaimed
// the same way -- by its lease release, as a root and as the field of
// FarGate -- and both are in the fixture so that the legs ask about them by
// name instead of leaving them to a future reader to discover.
//
// watcher and keeper are never called and touch nothing: a far handle only
// takes an operation inside an accepted remote context, and the legs need
// the far TYPES to exist with a frozen layout, not a remote call.
@copy type Gate = { ch: Channel<int> }

type FarGate = { ch: far Channel<int> }

fn carriers() -> int {
	let ch: own Channel<int> = Channel::<int>::new(1:uint);
	ch.close();
	let g: Gate = Gate{ ch: Channel::<int>::new(1:uint) };
	g.ch.close();
	return 0;
}

async fn watcher(remote: far Channel<int>) -> nothing {
	print("watching\n");
}

async fn keeper(held: FarGate) -> nothing {
	print("keeping\n");
}

@entrypoint
fn main() -> int { return build() + carriers(); }
`

// dropAgreementRow is one type the two sides are asked about.
type dropAgreementRow struct {
	// typeName is the interner label, which is how a fixture type is named
	// without depending on the id it happened to be interned at.
	typeName string
	// families is the leaf set BOTH sides must reach.
	families []string
	// semaDiverges, when set, says this row is a KNOWN divergence rather than
	// an agreement, and holds what sema reaches instead. A row is only allowed
	// to carry it with the note below saying why the divergence is real.
	semaDiverges []string
	note         string
}

// TestDropGlueReachesTheLeavesSemaSaysAreThere is the agreement leg.
//
// For each row it collects, from two sources that derive the answer by
// different means:
//
//   - the families of every owning leaf the CLASSIFIER reaches, by walking the
//     component relation it published and asking its droppable verdict at each
//     node;
//   - the families of every leaf the EMITTED GLUE reaches, by following the
//     calls one generated body makes into the next until only runtime helpers
//     are left.
//
// The two walks are not fully independent and the table does not pretend they
// are: both bottom out in `types.Interner` predicates (`IsRefCountedScalar` is
// consulted by `leafFamilyOf` here and by `emitDropHandle` there), so breaking
// one of those moves both sides at once. What catches that common mode is the
// `families` column: each row states its leaf set in words, so a shared
// predicate that stops answering takes both walks down while the row keeps
// saying what should have been reached.
//
// A root whose glue reclaims nothing has an empty second set, which is how the
// missing default arm shows up here as a difference rather than as a verdict.
func TestDropGlueReachesTheLeavesSemaSaysAreThere(t *testing.T) {
	rows := []dropAgreementRow{
		{typeName: "string", families: []string{leafString}},
		{typeName: "float", families: []string{leafCountedScalar}},
		{typeName: "Array<string>", families: []string{leafElementBuffer, leafString}},
		{typeName: "Text", families: []string{leafString}},
		{
			typeName: "Holder",
			families: []string{leafString, leafElementBuffer, leafCountedScalar},
		},
		{typeName: "ArrayFixed<string, const 2, 2>", families: []string{leafString}},
		// The channel as a root and one level down. Both reach the handle
		// release and nothing else: the ELEMENT type is the channel's to
		// destroy through its own descriptor, not this glue's to walk.
		{typeName: "Channel<int>", families: []string{leafChannelHandle}},
		{typeName: "Gate", families: []string{leafChannelHandle}},
		// The far handle as a root and one level down: both reach the lease
		// release and nothing else, for the same reason as the channel.
		{typeName: "far Channel<int>", families: []string{leafFarLease}},
		{typeName: "FarGate", families: []string{leafFarLease}},
	}

	e, result := prepareEmitterAndResultForTest(t, dropGlueAgreementSource)
	classifier := result.Sema.Capabilities
	if classifier == nil {
		t.Fatal("the compilation published no capability classifier, so there is only one side to compare")
	}

	ids := make(map[string]types.TypeID, len(rows))
	for _, row := range rows {
		id := findNamedType(t, e, row.typeName)
		ids[row.typeName] = id
		e.requireDropGlue(id)
	}
	if err := e.emitDropGlue(); err != nil {
		t.Fatalf("emit drop glue: %v", err)
	}
	ir := e.buf.String()

	for _, row := range rows {
		id := ids[row.typeName]
		fromSema := semaLeafFamilies(t, classifier, e.types, id)
		fromGlue := emittedLeafFamilies(t, ir, dropGlueName(id))

		wantSema := row.families
		if row.semaDiverges != nil {
			if row.note == "" {
				t.Fatalf("%s: a recorded divergence must say why it is one", row.typeName)
			}
			wantSema = row.semaDiverges
		}
		if got := sortedFamilies(fromSema); !equalFamilies(got, wantSema) {
			t.Errorf("%s: the classifier reaches %v, the row says %v", row.typeName, got, wantSema)
		}
		if got := sortedFamilies(fromGlue); !equalFamilies(got, sortedStrings(row.families)) {
			t.Errorf("%s: the emitted glue reclaims %v, the row says %v",
				row.typeName, got, sortedStrings(row.families))
		}
		if row.semaDiverges != nil {
			if equalFamilies(sortedFamilies(fromSema), sortedFamilies(fromGlue)) {
				t.Errorf("%s: the recorded divergence is gone — the two sides now agree on %v. "+
					"Delete the divergence from this row rather than re-recording it.\n%s",
					row.typeName, sortedFamilies(fromSema), row.note)
			}
			continue
		}
		if !equalFamilies(sortedFamilies(fromSema), sortedFamilies(fromGlue)) {
			t.Errorf("%s: sema reaches %v, the emitted glue reclaims %v",
				row.typeName, sortedFamilies(fromSema), sortedFamilies(fromGlue))
		}
	}
}

// emptyBodyRow is one type the second leg asks about, and what this compiler is
// entitled to answer for it today.
type emptyBodyRow struct {
	typeName string
	// excused, when set, records that this type belongs to a family sema calls
	// droppable and the BACKEND HAS NO RECLAMATION FOR AT ALL: `emitDropHandle`
	// has no call for it, and `typeOwnsHeap` does not count it as owning
	// anything, so its empty body is a recorded gap rather than a defect this
	// leg can attribute. The text says which family and why.
	//
	// An excuse is not a way out of the leg. It is checked four ways: only a
	// family in `unreclaimedFamilies` may carry one, EVERY family in that list
	// must be carried by some row, the row fails if the type stops being
	// droppable (the excuse would then describe a question nobody is asking),
	// and it fails the moment the body DOES reclaim something — at which point
	// the excuse is to be deleted, not re-recorded.
	excused string
}

// TestNoDroppableTypeGetsAnEmptyGlueBody is the second leg.
//
// It asks nothing about WHICH leaves are reached, only that a type sema calls
// droppable does not receive a body that reclaims nothing. The two legs fail
// together on a missing arm and separately on other faults: this one also
// catches a body emitted for a type whose leaf family the first leg does not
// model, which is exactly where an agreement table stops looking.
//
// `entry -> br ret -> ret void` is a body that passes every structural check a
// carrier descriptor can make — FLAG_DROPPABLE set, `drop_in_place` non-null,
// the runtime's "droppable implies a drop function" invariant satisfied — and
// leaks the value.
//
// The carrier family this backend cannot reclaim is IN the table rather than
// absent from it. Leaving it out would have made the leg say "eight named
// types are fine" while `far Channel<int>` was a one-line green-to-red
// counterexample sitting outside the list. `Channel<int>` was the other such
// row until its release existed; it is an ordinary row now, and so is Gate —
// the COMPOSITE THAT HOLDS a carrier, which used to be unrepresentable here:
// `leafFamilyOf` answers "" for a composite, so the
// excuse table could not name it, and the struct arm walked into an
// `emitDropHandle` with no channel case and emitted a body that freed nothing.
// The arm exists now, the first leg compares the row by the families of the
// leaves it holds, and this leg refuses the empty body it used to get.
func TestNoDroppableTypeGetsAnEmptyGlueBody(t *testing.T) {
	rows := []emptyBodyRow{
		{typeName: "string"},
		{typeName: "float"},
		{typeName: "Array<string>"},
		{typeName: "Text"},
		{typeName: "Holder"},
		// Point is the control. A Copy struct of two integers is NOT droppable,
		// so the empty body it receives is the right answer — and observing
		// that this run really did produce one proves the emptiness detector
		// can still say "empty". Without it a detector that had quietly stopped
		// recognising the shape would report no violation and read as a pass.
		{typeName: "Point"},
		{typeName: "Channel<int>"},
		{typeName: "Gate"},
		// The far handle and the composite holding one were the last excused
		// row and the last unrepresentable shape; ordinary rows since the glue
		// reaches `rt_far_channel_handle_drop` and the walk counts the lease.
		{typeName: "far Channel<int>"},
		{typeName: "FarGate"},
	}

	e, result := prepareEmitterAndResultForTest(t, dropGlueAgreementSource)
	classifier := result.Sema.Capabilities
	if classifier == nil {
		t.Fatal("the compilation published no capability classifier, so nothing says which types are droppable")
	}

	// Each row's body is emitted on its own and read back from its own slice of
	// the buffer. Draining the whole request set with `emitDropGlue` instead
	// would make ONE type this compiler cannot lay out end the run before any
	// row was judged — and the carrier rows below are exactly the types where
	// that happens, so the leg would go quiet precisely where it was extended.
	checkedDroppable := 0
	excusedFamilies := make(map[string]struct{}, len(unreclaimedFamilies))
	sawAnEmptyBody := false
	for _, row := range rows {
		id := findNamedType(t, e, row.typeName)
		capability, err := classifier.Classify(id)
		if err != nil {
			t.Fatalf("%s: classify: %v", row.typeName, err)
		}

		before := e.buf.Len()
		if err := e.emitDropGlueBody(id); err != nil {
			t.Errorf("%s: this compiler emitted no drop glue body for it at all: %v",
				row.typeName, err)
			continue
		}
		body := findLLVMFuncBody(t, e.buf.String()[before:], dropGlueName(id))
		empty := glueBodyReclaimsNothing(body)
		if empty {
			// Only an UNEXCUSED empty body counts as evidence the detector
			// still works. The excused carrier rows are empty by design, so
			// counting them would satisfy this flag unconditionally and turn
			// the control below into decoration — which is what happened when
			// the carrier rows were first added.
			if row.excused == "" {
				sawAnEmptyBody = true
			}
			// A body that reclaims nothing has no business READING the value
			// either. The read is typed `ptr`, so on a sub-word root it is a
			// machine-word load out of a one- or four-byte slot; the bytes past
			// the value belong to whatever is laid out next.
			if load := glueBodyLoads(body); load != "" {
				t.Errorf("%s: the glue reclaims nothing yet reads the value (%s):\n%s",
					row.typeName, strings.TrimSpace(load), body)
			}
		} else if !capability.CarrierDroppable {
			// Deliberately not an error. A body that reclaims MORE than the
			// classifier accounts for is the over-reclaim direction, and the
			// first leg judges it per type against a recorded leaf set —
			// including the one place the two really do part, the nominal
			// fixed array. Repeating the judgement here as a blanket rule
			// would make this leg fail for the classifier's gap rather than
			// for the backend's.
			t.Logf("%s is not droppable (%s) and its glue reclaims something:\n%s",
				row.typeName, capability.DroppableReason, body)
		}

		if row.excused != "" {
			// An excuse may only stand for a family this backend has no
			// reclamation for at all. Anything else — a string, a container, a
			// composite — has one, so an empty body for it is the defect this
			// leg exists to catch and no note may silence it.
			family := leafFamilyOf(e.types, id)
			if !isUnreclaimedFamily(family) {
				t.Errorf("%s carries an excuse, but its leaf family is %q, which this backend "+
					"does reclaim; only %v may be excused:\n%s",
					row.typeName, family, unreclaimedFamilies, row.excused)
			}
			if !capability.CarrierDroppable {
				t.Errorf("%s carries an excuse for a droppable type, but sema does not call it "+
					"droppable (%s). The excuse now describes a question nobody asks — delete it:\n%s",
					row.typeName, capability.DroppableReason, row.excused)
				continue
			}
			excusedFamilies[family] = struct{}{}
			if !empty {
				t.Errorf("%s was excused as a family this backend cannot reclaim, and its glue now "+
					"reclaims something. Delete the excuse rather than re-recording it:\n%s\n%s",
					row.typeName, body, row.excused)
			}
			continue
		}

		if !capability.CarrierDroppable {
			continue
		}
		checkedDroppable++
		if empty {
			t.Errorf("%s is droppable (%s) and its glue reclaims nothing:\n%s",
				row.typeName, capability.DroppableReason, body)
		}
	}
	// A run that examined no droppable type would pass without having asked
	// anything, and one that never saw an empty body proves nothing about a
	// detector that only ever answers "not empty".
	if checkedDroppable == 0 {
		t.Fatal("the fixture offered no droppable type, so this leg asked nothing")
	}
	if got := sortedFamilies(excusedFamilies); !equalFamilies(got, unreclaimedFamilies) {
		t.Fatalf("every family this backend cannot reclaim has to be reached here by a recorded, "+
			"still-droppable row: this run reached %v, the standing list is %v. A family that "+
			"stops appearing here goes back to being invisible",
			got, sortedStrings(unreclaimedFamilies))
	}
	if !sawAnEmptyBody {
		t.Fatal("no glue body in this run read as empty, so the shape this leg refuses was never actually recognised")
	}
}

// semaLeafFamilies is the classifier's side: the families of every owning leaf
// reachable from id through the component relation sema publishes.
//
// The walk asks `Classify` at each node and never re-derives a verdict, so a
// classifier that stops calling strings droppable takes this set down with it.
func semaLeafFamilies(
	t *testing.T,
	classifier *sema.CapabilityClassifier,
	typesIn *types.Interner,
	id types.TypeID,
) map[string]struct{} {
	t.Helper()
	found := make(map[string]struct{})
	seen := map[types.TypeID]struct{}{}
	queue := []types.TypeID{id}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		if _, already := seen[at]; already {
			continue
		}
		seen[at] = struct{}{}

		capability, err := classifier.Classify(at)
		if err != nil {
			t.Fatalf("classify %s: %v", types.Label(typesIn, at), err)
		}
		if capability.CarrierDroppable {
			switch family := leafFamilyOf(typesIn, at); family {
			case "":
				// A composite owns through its members; its own row adds
				// nothing.
			default:
				if isUnreclaimedFamily(family) {
					t.Fatalf("%s reaches %s, a family this backend has no reclamation for at all; "+
						"comparing leaf sets across it would compare two silences",
						types.Label(typesIn, id), family)
				}
				found[family] = struct{}{}
			}
		}
		queue = append(queue, classifier.Components(at)...)
	}
	return found
}

// leafFamilyOf names the family of storage a type owns IN ITS OWN RIGHT, or ""
// when whatever it owns it owns through a member.
func leafFamilyOf(typesIn *types.Interner, id types.TypeID) string {
	resolved := resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(resolved)
	if !ok {
		return ""
	}
	switch {
	case typesIn.IsRefCountedScalar(resolved):
		return leafCountedScalar
	case typesIn.IsRefCountedHandle(resolved):
		return leafChannelHandle
	case tt.Kind == types.KindString:
		return leafString
	case isDynamicElementBuffer(typesIn, resolved):
		return leafElementBuffer
	case tt.Kind == types.KindFar:
		return leafFarLease
	case isTaskType(typesIn, resolved):
		return leafTaskHandle
	case isMapType(typesIn, resolved):
		return leafMap
	}
	if _, isRange := rangeElemType(typesIn, resolved); isRange {
		return leafRange
	}
	return ""
}

func isMapType(typesIn *types.Interner, resolved types.TypeID) bool {
	_, _, isMap := typesIn.MapInfo(resolved)
	return isMap
}

// isDynamicElementBuffer reports the container that owns a resizable element
// buffer, in both spellings: the structural `KindArray` with a dynamic length,
// and the nominal `Array<T>`.
func isDynamicElementBuffer(typesIn *types.Interner, resolved types.TypeID) bool {
	if _, ok := typesIn.ArrayInfo(resolved); ok {
		return true
	}
	tt, ok := typesIn.Lookup(resolved)
	return ok && tt.Kind == types.KindArray && tt.Count == types.ArrayDynamicLength
}

// generatedGlueCall matches one generated body reaching another, whether it
// CALLS it or hands it over as a function pointer: an element drop travels to
// `rt_array_free_elems` as an argument, and a walk that only followed calls
// would stop at the container and never see what its elements own.
var generatedGlueCall = regexp.MustCompile(`@(drop\.type\d+|drop_elem\.type\d+)\b`)

// runtimeCall matches a call to a runtime helper.
var runtimeCall = regexp.MustCompile(`call [^@]*@(rt_[A-Za-z0-9_]+)\(`)

// leafFamilyByHelper maps the runtime reclamations to the families they
// perform. The map is exhaustive on purpose: a helper missing from it stops
// this test rather than being ignored, because a new leaf family the glue
// learns to reclaim is precisely what the comparison must be extended for.
var leafFamilyByHelper = map[string]string{
	"rt_string_free":             leafString,
	"rt_bigfloat_release":        leafCountedScalar,
	"rt_array_free":              leafElementBuffer,
	"rt_array_free_elems":        leafElementBuffer,
	"rt_channel_handle_drop":     leafChannelHandle,
	"rt_task_handle_drop":        leafTaskHandle,
	"rt_far_channel_handle_drop": leafFarLease,
	"rt_map_free":                leafMap,
	"rt_range_free":              leafRange,
}

// emittedLeafFamilies is the backend's side: the families the emitted glue
// actually reclaims, found by following generated calls from `entry` until only
// runtime helpers remain.
func emittedLeafFamilies(t *testing.T, ir, entry string) map[string]struct{} {
	t.Helper()
	found := make(map[string]struct{})
	seen := map[string]struct{}{}
	queue := []string{entry}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		if _, already := seen[at]; already {
			continue
		}
		seen[at] = struct{}{}

		body := glueBodyInstructions(findLLVMFuncBody(t, ir, at))
		for _, match := range runtimeCall.FindAllStringSubmatch(body, -1) {
			family, known := leafFamilyByHelper[match[1]]
			if !known {
				t.Fatalf("%s calls %s, which this test has no family for; "+
					"a new reclamation has to be added to leafFamilyByHelper or it is compared against nothing:\n%s",
					at, match[1], body)
			}
			found[family] = struct{}{}
		}
		for _, match := range generatedGlueCall.FindAllStringSubmatch(body, -1) {
			if match[1] == at {
				continue
			}
			queue = append(queue, match[1])
		}
	}
	return found
}

// glueBodyReclaimsNothing reports the `entry -> br ret -> ret void` shape: a
// body whose only block with anything in it is the return.
func glueBodyReclaimsNothing(body string) bool {
	instructions := glueBodyInstructions(body)
	return !runtimeCall.MatchString(instructions) && !generatedGlueCall.MatchString(instructions)
}

// valueLoad matches an instruction that reads the value's storage.
var valueLoad = regexp.MustCompile(`(?m)^.*= load .*$`)

// glueBodyLoads returns the first instruction in the body that reads the
// value's storage, or "" if the body reads nothing.
//
// A body that reclaims nothing should also read nothing: the read is typed
// `ptr`, which is a machine word, and the slot it reads from is only as wide as
// the value's own layout says.
func glueBodyLoads(body string) string {
	return valueLoad.FindString(glueBodyInstructions(body))
}

// glueBodyInstructions drops the `define` header, which names the function
// itself. Left in, every drop glue body reads as reaching itself and no body is
// ever empty.
func glueBodyInstructions(body string) string {
	_, instructions, found := strings.Cut(body, "\n")
	if !found {
		return ""
	}
	return instructions
}

func sortedFamilies(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for family := range set {
		out = append(out, family)
	}
	sort.Strings(out)
	return out
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func equalFamilies(left, right []string) bool {
	return strings.Join(sortedStrings(left), "|") == strings.Join(sortedStrings(right), "|")
}
