package sema

import (
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

// capabilityWorld is one interner holding a shape for every branch the four
// structural axes take, named so the tables below read as claims about shapes
// rather than about interner ids.
func capabilityWorld(t *testing.T) (*CapabilityClassifier, map[string]types.TypeID) {
	t.Helper()
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	shapes := map[string]types.TypeID{
		"int":     in.Builtins().Int,
		"string":  in.Builtins().String,
		"float":   in.Builtins().Float,
		"float64": in.Builtins().Float64,
	}

	// A `@copy` struct of two integers: duplicable, and with nothing at all
	// behind it once storage is inline.
	shapes["Point"] = capabilityStruct(in, "Point", shapes["int"], shapes["int"])
	res.CopyTypes[shapes["Point"]] = struct{}{}
	shapes["Named"] = capabilityStruct(in, "Named", shapes["int"])
	shapes["Text"] = capabilityStruct(in, "Text", shapes["string"])

	shapes["Pinned"] = capabilityStruct(in, "Pinned", shapes["int"])
	res.TypeAttrFacts[shapes["Pinned"]] = TypeAttrFacts{ShardPinned: true}
	shapes["NoSend"] = capabilityStruct(in, "NoSend", shapes["int"])
	res.TypeAttrFacts[shapes["NoSend"]] = TypeAttrFacts{NoSend: true}
	shapes["Movable"] = capabilityStruct(in, "Movable", shapes["int"], shapes["string"])
	res.TypeAttrFacts[shapes["Movable"]] = TypeAttrFacts{ShardMovable: true}
	shapes["MovableOverPinned"] = capabilityStruct(in, "MovableOverPinned", shapes["int"], shapes["Pinned"])
	res.TypeAttrFacts[shapes["MovableOverPinned"]] = TypeAttrFacts{ShardMovable: true}

	// A runtime resource: a nominal handle whose payload it owns, and which has
	// no design-defined duplicate.
	shapes["Channel"] = in.RegisterStructInstance(
		in.Strings.Intern("Channel"), source.Span{File: 1, Start: 7, End: 8}, []types.TypeID{shapes["int"]})
	in.MarkRuntimeHandleType(shapes["Channel"])
	shapes["HoldsChannel"] = capabilityStruct(in, "HoldsChannel", shapes["Channel"])

	shapes["&Text"] = in.Intern(types.MakeReference(shapes["Text"], false))
	shapes["far Text"] = in.Intern(types.Type{Kind: types.KindFar, Elem: shapes["Text"]})
	shapes["own Text"] = in.Intern(types.Type{Kind: types.KindOwn, Elem: shapes["Text"]})

	shapes["[]int"] = capabilityDynamicArray(in, shapes["int"])
	shapes["[]string"] = capabilityDynamicArray(in, shapes["string"])
	shapes["[]Channel"] = capabilityDynamicArray(in, shapes["Channel"])
	shapes["[4]int"] = in.Intern(types.Type{Kind: types.KindArray, Elem: shapes["int"], Count: 4})
	shapes["[4]string"] = in.Intern(types.Type{Kind: types.KindArray, Elem: shapes["string"], Count: 4})
	shapes["(int, string)"] = in.RegisterTuple([]types.TypeID{shapes["int"], shapes["string"]})
	shapes["(int, Pinned)"] = in.RegisterTuple([]types.TypeID{shapes["int"], shapes["Pinned"]})

	return mustClassifier(t, res), shapes
}

// axisRow is one claim: this shape answers this way, for this reason.
type axisRow struct {
	shape  string
	want   bool
	reason string
	path   []string
}

func runAxisRows(
	t *testing.T,
	rows []axisRow,
	read func(Capability) (bool, string, []types.TypeID),
) {
	t.Helper()
	classifier, shapes := capabilityWorld(t)
	for _, row := range rows {
		id, ok := shapes[row.shape]
		if !ok {
			t.Fatalf("no shape named %q", row.shape)
		}
		verdict, reason, path := read(mustClassify(t, classifier, id))
		if verdict != row.want {
			t.Errorf("%s: verdict = %t, want %t (reason %q)", row.shape, verdict, row.want, reason)
			continue
		}
		if !strings.Contains(reason, row.reason) {
			t.Errorf("%s: reason = %q, want it to carry %q", row.shape, reason, row.reason)
		}
		if len(row.path) == 0 {
			continue
		}
		got := classifier.labels(path)
		if strings.Join(got, " -> ") != strings.Join(row.path, " -> ") {
			t.Errorf("%s: path = %v, want %v", row.shape, got, row.path)
		}
	}
}

// TestCapabilityDroppableAxis pins the carrier drop question: with the value
// stored inline, does releasing it have to run anything?
func TestCapabilityDroppableAxis(t *testing.T) {
	runAxisRows(t, []axisRow{
		{shape: "int", want: false, reason: "nothing inside"},
		{shape: "float64", want: false, reason: "nothing inside"},
		{shape: "float", want: true, reason: "counted heap block"},
		{shape: "string", want: true, reason: "handle-backed value owns"},
		{shape: "Point", want: false, reason: "nothing inside"},
		{shape: "Named", want: false, reason: "nothing inside"},
		{shape: "Text", want: true, reason: "requires reclamation"},
		{shape: "own Text", want: true, reason: "requires reclamation"},
		{shape: "&Text", want: false, reason: "borrow names storage it does not own"},
		{shape: "far Text", want: true, reason: "lease"},
		{shape: "Channel", want: true, reason: "handle-backed value owns"},
		{shape: "[]int", want: true, reason: "handle-backed value owns"},
		{shape: "[4]int", want: false, reason: "nothing inside"},
		{shape: "[4]string", want: true, reason: "requires reclamation"},
		{shape: "(int, string)", want: true, reason: "requires reclamation"},
	}, func(c Capability) (bool, string, []types.TypeID) {
		return c.CarrierDroppable, c.DroppableReason, nil
	})
}

// TestCapabilityDroppableAgreesWithOwnsHeap is the requirement-6 row, run
// in-package against a real checker so the two predicates answer about one type
// in one program.
//
// The two were designed to DISAGREE here while a value composite was a heap
// box: `ownsHeap` said a `@copy` struct of two integers owned heap (the box),
// the classifier said the same struct was not carrier droppable (bytes in a
// carrier, nothing to reclaim), and this test pinned the disagreement so that
// nobody merged the predicates before the storage moved. The box is gone and
// the axis answers from the members (RV2-DEBT-256), so the same row now pins
// the agreement from both sides — a drop obligation on a struct of integers
// would be a release of bits.
func TestCapabilityDroppableAgreesWithOwnsHeap(t *testing.T) {
	tc, _, syms := newContractChecker(t, `
@copy type Point = { x: int, y: int }
`)
	pointSym := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("Point"))
	if !pointSym.IsValid() {
		t.Fatal("Point was not resolved")
	}
	point := syms.Table.Symbols.Get(pointSym).Type
	if point == types.NoTypeID {
		t.Fatal("Point has no type")
	}

	if tc.isDroppableType(point) {
		t.Fatal("drop obligations treat a Copy struct of two integers as owning heap; " +
			"the axis answers from the members (ownership_axes.go), so a composite of scalars is bits")
	}
	capability := mustClassify(t, mustClassifier(t, tc.result), point)
	if capability.CarrierDroppable {
		t.Fatalf("the classifier calls a Copy struct of integers carrier droppable: %+v", capability)
	}
}

// TestCapabilityTraceableAxis pins the new axis: does a tracing visitor have to
// enter this value to reach runtime-managed storage it owns?
func TestCapabilityTraceableAxis(t *testing.T) {
	runAxisRows(t, []axisRow{
		{shape: "int", want: false, reason: "no reference into runtime-managed storage"},
		{shape: "float", want: true, reason: "counted heap block"},
		{shape: "string", want: true, reason: "runtime-managed storage a visitor has to enter"},
		{shape: "Point", want: false, reason: "no reference into runtime-managed storage"},
		{shape: "Text", want: true, reason: "names runtime-managed storage"},
		{shape: "own Text", want: true, reason: "names runtime-managed storage"},
		{shape: "&Text", want: false, reason: "reached through its owner"},
		{shape: "Channel", want: true, reason: "runtime-managed storage a visitor has to enter"},
		{shape: "[4]int", want: false, reason: "no reference into runtime-managed storage"},
		{shape: "(int, string)", want: true, reason: "names runtime-managed storage"},
	}, func(c Capability) (bool, string, []types.TypeID) {
		return c.Traceable, c.TraceableReason, nil
	})
}

// TestCapabilityTraceableAndDroppablePartOnFarHandles pins where the two axes
// stop agreeing, because a single predicate serving both would be wrong on this
// row in one direction or the other. A far handle's lease has to be returned,
// so it is droppable; the storage it names lives in another shard, which this
// shard's visitor cannot reach, so it is not traceable.
func TestCapabilityTraceableAndDroppablePartOnFarHandles(t *testing.T) {
	classifier, shapes := capabilityWorld(t)
	capability := mustClassify(t, classifier, shapes["far Text"])
	if !capability.CarrierDroppable {
		t.Fatalf("a far handle stopped being droppable: %q", capability.DroppableReason)
	}
	if capability.Traceable {
		t.Fatalf("a far handle became a trace root of this shard: %q", capability.TraceableReason)
	}
}

// TestCapabilityShardMovableAxis pins the owned-move verdict and the path down
// to whatever refuses it.
func TestCapabilityShardMovableAxis(t *testing.T) {
	runAxisRows(t, []axisRow{
		{shape: "int", want: true, reason: "builtin scalar or string travels"},
		{shape: "string", want: true, reason: "builtin scalar or string travels"},
		{shape: "far Text", want: true, reason: "far handle moves between shards"},
		{shape: "&Text", want: false, reason: "borrowed value cannot cross"},
		{shape: "Named", want: false, reason: "not marked `@shard_movable`"},
		{shape: "Pinned", want: false, reason: "`@shard_pinned`"},
		{shape: "NoSend", want: false, reason: "`@nosend`"},
		{shape: "Movable", want: true, reason: "every member may move with it"},
		{shape: "own Text", want: false, reason: "not marked `@shard_movable`"},
		{shape: "MovableOverPinned", want: false, reason: "may not move between shards",
			path: []string{"MovableOverPinned", "Pinned"}},
		{shape: "(int, string)", want: true, reason: "every element may move"},
		{shape: "(int, Pinned)", want: false, reason: "may not move between shards",
			path: []string{"(int, Pinned)", "Pinned"}},
		{shape: "[4]int", want: true, reason: "every element may move"},
	}, func(c Capability) (bool, string, []types.TypeID) {
		return c.ShardMovable, c.ShardReason, c.ShardPath
	})
}

// TestCapabilityShardMovableIsOnlyTheOwnedMoveVerdict pins that the use-site
// branches judging a BORROWED or COPIED capture are not folded into this bit.
//
// A reference-counted scalar is the row that separates them. Copying its word
// into a crossing is refused, because two shards would then race one non-atomic
// count — but an owned MOVE transfers the reference instead of sharing it, and
// is exactly what cross_move_init does. Folding the copy refusal in here would
// make the move impossible for every type that reaches a `float`.
func TestCapabilityShardMovableIsOnlyTheOwnedMoveVerdict(t *testing.T) {
	classifier, shapes := capabilityWorld(t)
	scalar := mustClassify(t, classifier, shapes["float"])
	if !scalar.ShardMovable {
		t.Fatalf("the copy-capture refusal was folded into the owned-move verdict: %q", scalar.ShardReason)
	}
	// The use-site rule the classifier must NOT be repeating still holds: a
	// copied capture of this same type is refused, and refused elsewhere.
	res := capabilityResult(classifier.types)
	if !res.ContainsRefCountedScalar(shapes["float"]) {
		t.Fatal("the use-site copy refusal no longer recognises a reference-counted scalar")
	}
}

// TestCapabilityShardMovableSeedsACycleMovable pins the greatest fixpoint. A
// `@shard_movable` type that reaches itself must stay movable — parity with the
// attribute validator's coinductive `if visiting[resolved] { return true }`.
// Seeding false would make every recursive marked type immovable for no reason
// other than that it is recursive.
func TestCapabilityShardMovableSeedsACycleMovable(t *testing.T) {
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	tree := in.RegisterStruct(in.Strings.Intern("Tree"), source.Span{File: 1, Start: 1, End: 2})
	capabilitySetFields(in, tree, in.Builtins().String, capabilityDynamicArray(in, tree))
	res.TypeAttrFacts[tree] = TypeAttrFacts{ShardMovable: true}

	capability := mustClassify(t, mustClassifier(t, res), tree)
	if !capability.ShardMovable {
		t.Fatalf("a recursive `@shard_movable` type was refused: %q via %v",
			capability.ShardReason, capability.ShardPath)
	}

	// The seed is a seed, not an answer: a real refusal inside the cycle still
	// settles the type as immovable.
	pinned := capabilityStruct(in, "Pinned", in.Builtins().Int)
	res.TypeAttrFacts[pinned] = TypeAttrFacts{ShardPinned: true}
	rooted := in.RegisterStruct(in.Strings.Intern("Rooted"), source.Span{File: 1, Start: 3, End: 4})
	capabilitySetFields(in, rooted, pinned, capabilityDynamicArray(in, rooted))
	res.TypeAttrFacts[rooted] = TypeAttrFacts{ShardMovable: true}

	refused := mustClassify(t, mustClassifier(t, res), rooted)
	if refused.ShardMovable {
		t.Fatal("a cycle holding a `@shard_pinned` member was called movable")
	}
	if got := strings.Join(mustClassifier(t, res).labels(refused.ShardPath), " -> "); got != "Rooted -> Pinned" {
		t.Fatalf("refusal path = %q, want Rooted -> Pinned", got)
	}
}

// TestCapabilityCrossClonableSeedsACycleRefused pins the least fixpoint and, in
// the same fixture, that classification terminates on a cyclic type graph at
// all. `RuntimeHandlePayloads` makes cycles ordinary — a handle's payloads are
// components of the handle — so bare memoized recursion would not return here.
func TestCapabilityCrossClonableSeedsACycleRefused(t *testing.T) {
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	tree := in.RegisterStruct(in.Strings.Intern("Tree"), source.Span{File: 1, Start: 1, End: 2})
	capabilitySetFields(in, tree, in.Builtins().String, capabilityDynamicArray(in, tree))

	classifier := mustClassifier(t, res)
	capability := mustClassify(t, classifier, tree)
	if capability.CrossClonable {
		t.Fatal("a self-referential type claimed a crossing duplicate nothing in it earns")
	}
	// The other two least-fixpoint axes are earned by real components inside the
	// same cycle, so the seed is not simply refusing everything.
	if !capability.CarrierDroppable || !capability.Traceable {
		t.Fatalf("the cycle's own string was not credited: %+v", capability)
	}
	// And the cycle really is one.
	if len(classifier.components(capabilityDynamicArray(in, tree))) != 1 {
		t.Fatal("the array component edge back to Tree is missing, so this fixture proves nothing")
	}
}

// TestCapabilityCrossClonableIsIndependentOfClone pins requirement 6 in both
// directions: the crossing duplicate is a design rule, and a user `__clone`
// neither supplies one nor is required by one.
func TestCapabilityCrossClonableIsIndependentOfClone(t *testing.T) {
	in := deferredResolverTestInterner()
	res := capabilityResult(in)

	// No `__clone` anywhere, and still cross-clonable: the crossing duplicates
	// a pair of strings by design.
	pair := capabilityStruct(in, "Pair", in.Builtins().String, in.Builtins().String)

	// A perfectly good `__clone`, and still not cross-clonable: an opaque
	// runtime resource has no design-defined duplicate, and the crossing is
	// forbidden from calling that `__clone` to find one.
	channel := in.RegisterStructInstance(
		in.Strings.Intern("Channel"), source.Span{File: 1, Start: 7, End: 8}, []types.TypeID{in.Builtins().Int})
	in.MarkRuntimeHandleType(channel)
	holder := capabilityStruct(in, "Holder", channel)
	res.CallableCandidates = []CallableCandidate{cloneTestHook(in, 10, "app|main.sg:1:2|__clone", holder)}

	classifier := mustClassifier(t, res)
	clonable := mustClassify(t, classifier, pair)
	if clonable.Clone.State != CloneNonClonable {
		t.Fatalf("Pair clone state = %s, want non-clonable", clonable.Clone.State)
	}
	if !clonable.CrossClonable {
		t.Fatalf("a struct of two strings was refused a crossing duplicate: %q", clonable.CrossCloneReason)
	}

	cloned := mustClassify(t, classifier, holder)
	if cloned.Clone.State != CloneValidMethod {
		t.Fatalf("Holder clone state = %s, want valid-method", cloned.Clone.State)
	}
	if cloned.CrossClonable {
		t.Fatal("a user `__clone` was allowed to supply the crossing duplicate")
	}
	if got := strings.Join(classifier.labels(cloned.CrossClonePath), " -> "); !strings.HasPrefix(got, "Holder -> ") {
		t.Fatalf("refusal path = %q, want it to start at Holder and name the resource", got)
	}
}

// TestCapabilityCrossClonableAxis pins the remaining rows.
func TestCapabilityCrossClonableAxis(t *testing.T) {
	runAxisRows(t, []axisRow{
		{shape: "int", want: true, reason: "plain bits"},
		{shape: "float", want: true, reason: "plain bits"},
		{shape: "string", want: true, reason: "duplicates a string by design"},
		{shape: "Point", want: true, reason: "member by member"},
		{shape: "Text", want: true, reason: "member by member"},
		{shape: "own Text", want: true, reason: "member by member"},
		{shape: "&Text", want: false, reason: "borrowed value cannot cross"},
		{shape: "far Text", want: false, reason: "lease has one holder"},
		{shape: "NoSend", want: false, reason: "`@nosend`"},
		{shape: "Pinned", want: false, reason: "`@shard_pinned`"},
		{shape: "Channel", want: false, reason: "opaque runtime resource"},
		{shape: "HoldsChannel", want: false, reason: "cannot duplicate on its own",
			path: []string{"HoldsChannel", "Channel<int>"}},
		{shape: "[]string", want: true, reason: "duplicating what it holds"},
		{shape: "[]Channel", want: false, reason: "cannot duplicate on its own",
			path: []string{"[Channel<int>]", "Channel<int>"}},
		{shape: "[4]string", want: true, reason: "member by member"},
	}, func(c Capability) (bool, string, []types.TypeID) {
		return c.CrossClonable, c.CrossCloneReason, c.CrossClonePath
	})
}
