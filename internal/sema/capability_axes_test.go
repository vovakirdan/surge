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
		"int64":   in.Builtins().Int64,
		"uint":    in.Builtins().Uint,
		"uint64":  in.Builtins().Uint64,
		"string":  in.Builtins().String,
		"float":   in.Builtins().Float,
		"float64": in.Builtins().Float64,
	}

	// Copy permits duplication; int fields still own counted references.
	shapes["Point"] = capabilityStruct(in, "Point", shapes["int"], shapes["int"])
	res.CopyTypes[shapes["Point"]] = struct{}{}
	shapes["Named"] = capabilityStruct(in, "Named", shapes["int"])
	shapes["Point64"] = capabilityStruct(in, "Point64", shapes["int64"], shapes["int64"])
	res.CopyTypes[shapes["Point64"]] = struct{}{}
	shapes["Named64"] = capabilityStruct(in, "Named64", shapes["int64"])
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

	// `Placement`: an intrinsic nominal that is handle-SHAPED and owns nothing,
	// which is why it answers three axes differently from `Channel` beside it.
	shapes["Placement"] = in.RegisterStructInstance(
		in.Strings.Intern("Placement"), source.Span{File: 1, Start: 9, End: 10}, nil)
	in.MarkRuntimePlacementType(shapes["Placement"])

	shapes["[]int"] = capabilityDynamicArray(in, shapes["int"])
	shapes["[]string"] = capabilityDynamicArray(in, shapes["string"])
	shapes["[]Channel"] = capabilityDynamicArray(in, shapes["Channel"])
	shapes["[]Placement"] = capabilityDynamicArray(in, shapes["Placement"])
	shapes["[4]int"] = in.Intern(types.Type{Kind: types.KindArray, Elem: shapes["int"], Count: 4})
	shapes["[4]int64"] = in.Intern(types.Type{Kind: types.KindArray, Elem: shapes["int64"], Count: 4})
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
		{shape: "int", want: true, reason: "counted heap block"},
		{shape: "uint", want: true, reason: "counted heap block"},
		{shape: "int64", want: false, reason: "nothing inside"},
		{shape: "uint64", want: false, reason: "nothing inside"},
		{shape: "Point64", want: false, reason: "nothing inside"},
		{shape: "Named64", want: false, reason: "nothing inside"},
		{shape: "[4]int64", want: false, reason: "nothing inside"},
		{shape: "float64", want: false, reason: "nothing inside"},
		{shape: "float", want: true, reason: "counted heap block"},
		{shape: "string", want: true, reason: "handle-backed value owns"},
		{shape: "Point", want: true, reason: "requires reclamation"},
		{shape: "Named", want: true, reason: "requires reclamation"},
		{shape: "Text", want: true, reason: "requires reclamation"},
		{shape: "own Text", want: true, reason: "requires reclamation"},
		{shape: "&Text", want: false, reason: "borrow names storage it does not own"},
		{shape: "far Text", want: true, reason: "lease"},
		{shape: "Channel", want: true, reason: "handle-backed value owns"},
		{shape: "[]int", want: true, reason: "handle-backed value owns"},
		{shape: "[4]int", want: true, reason: "requires reclamation"},
		{shape: "[4]string", want: true, reason: "requires reclamation"},
		{shape: "(int, string)", want: true, reason: "requires reclamation"},
	}, func(c Capability) (bool, string, []types.TypeID) {
		return c.CarrierDroppable, c.DroppableReason, nil
	})
}

// The live checker and post-check classifier agree on both counted and fixed fields.
func TestCapabilityDroppableAgreesWithOwnsHeap(t *testing.T) {
	tc, _, syms := newContractChecker(t, `
@copy type Point = { x: int, y: int };
@copy type Point64 = { x: int64, y: int64 };
`)
	for name, want := range map[string]bool{"Point": true, "Point64": false} {
		sym := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern(name))
		if !sym.IsValid() {
			t.Fatalf("%s was not resolved", name)
		}
		ty := syms.Table.Symbols.Get(sym).Type
		if ty == types.NoTypeID {
			t.Fatalf("%s has no type", name)
		}
		if got := tc.isDroppableType(ty); got != want {
			t.Fatalf("%s: live droppable=%v, want %v", name, got, want)
		}
		capability := mustClassify(t, mustClassifier(t, tc.result), ty)
		if capability.CarrierDroppable != want {
			t.Fatalf("%s: carrier droppable=%v, want %v", name, capability.CarrierDroppable, want)
		}
	}
}

// TestCapabilityTraceableAxis pins the new axis: does a tracing visitor have to
// enter this value to reach runtime-managed storage it owns?
func TestCapabilityTraceableAxis(t *testing.T) {
	runAxisRows(t, []axisRow{
		{shape: "int", want: true, reason: "counted heap block"},
		{shape: "uint", want: true, reason: "counted heap block"},
		{shape: "int64", want: false, reason: "no reference into runtime-managed storage"},
		{shape: "uint64", want: false, reason: "no reference into runtime-managed storage"},
		{shape: "Point64", want: false, reason: "no reference into runtime-managed storage"},
		{shape: "Named64", want: false, reason: "no reference into runtime-managed storage"},
		{shape: "[4]int64", want: false, reason: "no reference into runtime-managed storage"},
		{shape: "float", want: true, reason: "counted heap block"},
		{shape: "string", want: true, reason: "runtime-managed storage a visitor has to enter"},
		{shape: "Point", want: true, reason: "counted heap block"},
		{shape: "Text", want: true, reason: "names runtime-managed storage"},
		{shape: "own Text", want: true, reason: "names runtime-managed storage"},
		{shape: "&Text", want: false, reason: "reached through its owner"},
		{shape: "Channel", want: true, reason: "runtime-managed storage a visitor has to enter"},
		{shape: "[4]int", want: true, reason: "counted heap block"},
		{shape: "(int, string)", want: true, reason: "counted heap block"},
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
		// The dynamic-array rows the capture gate now depends on. The axis has
		// answered an array by its element since it was written; nothing pinned
		// it, and the `on` capture rule reads exactly these three answers.
		{shape: "[]int", want: true, reason: "every element may move"},
		{shape: "[]string", want: true, reason: "every element may move"},
		{shape: "[]Channel", want: false, reason: "may not move between shards",
			path: []string{"[Channel<int>]", "Channel<int>"}},
		// `Placement` travels as itself -- a tagged word with no storage on
		// either shard -- so an array of them travels too. Without the arm that
		// says so the axis fell through to "not marked `@shard_movable`", and an
		// `on` capture of `Placement[]` was refused by a sentence naming a core
		// `@intrinsic` type the reader cannot mark, one program away from a bare
		// `Placement` that crosses.
		{shape: "Placement", want: true, reason: "travels as itself"},
		{shape: "[]Placement", want: true, reason: "every element may move"},
	}, func(c Capability) (bool, string, []types.TypeID) {
		return c.ShardMovable, c.ShardReason, c.ShardPath
	})
}

// TestShardMovableCaptureGateAgreesWithTheAxis holds the two legs of the
// ShardMovable question to one answer, the way TestCapabilityDroppableAgreesWithOwnsHeap
// holds Droppable and ownsHeap.
//
// The legs exist because they are asked at different times. The whole-program
// CapabilityClassifier reads a merged fact table that is not populated until
// the scope stack finalizes, so a capture site cannot consult it; the checker
// leg (isShardMovableMemberType) walks one file's live attributes and is what
// `on`'s capture gate asks. Both are compared here on ONE program, and on both
// halves of what the gate does with the answer: the ELEMENT the gate looks up,
// and the ARRAY the axis answers about as a whole. Only the array-level column
// catches the two legs drifting on the array rule itself.
//
// The table carries no function-pointer shape on purpose. `(fn(int) -> int)[]`
// is a KNOWN divergence -- the classifier calls a function pointer movable and
// the checker leg has no KindFn arm, so it says no -- and closing it means
// widening the `@shard_movable` FIELD validator at every declaration site,
// which is a different change on different evidence. It is carried in the debt
// ledger rather than left as a note here.
func TestShardMovableCaptureGateAgreesWithTheAxis(t *testing.T) {
	tc, _, syms := newContractChecker(t, `
type Plain = { id: int }
@nosend type LocalOnly = { id: int }
@shard_pinned type Pinned = { id: int }
@shard_movable type Movable = { id: int }
type Placed = { __opaque: int }

type Shapes = {
    ints: int[],
    strs: string[],
    plains: Plain[],
    locals: LocalOnly[],
    pins: Pinned[],
    movables: Movable[],
    places: Placed[],
}
`)
	// `Placement` is registered by identity in core rather than by an attribute,
	// so the row that carries it is registered the same way here. It is the one
	// unmarked nominal both legs must call movable, and before they did an `on`
	// capture of `Placement[]` was refused with advice -- mark it
	// `@shard_movable` -- that names a core `@intrinsic` type nobody can edit.
	placedSym := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("Placed"))
	if !placedSym.IsValid() {
		t.Fatal("Placed was not resolved")
	}
	tc.types.MarkRuntimePlacementType(syms.Table.Symbols.Get(placedSym).Type)

	shapesSym := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("Shapes"))
	if !shapesSym.IsValid() {
		t.Fatal("Shapes was not resolved")
	}
	shapes := syms.Table.Symbols.Get(shapesSym).Type
	fields := tc.types.StructFields(shapes)
	if len(fields) != 7 {
		t.Fatalf("Shapes has %d fields, want 7", len(fields))
	}
	classifier := mustClassifier(t, tc.result)
	accepted, refused := 0, 0
	for _, field := range fields {
		name := tc.lookupName(field.Name)
		elem, ok := tc.types.DynamicArrayElem(field.Type)
		if !ok {
			t.Fatalf("field %s of type %s is not a dynamic array", name, tc.typeLabel(field.Type))
		}
		gate := tc.shardMovableElement(elem)
		axisElem := mustClassify(t, classifier, elem).ShardMovable
		axisArray := mustClassify(t, classifier, field.Type).ShardMovable
		if gate != axisElem {
			t.Errorf("%s: capture gate says element %s is movable=%t, the axis says %t",
				name, tc.typeLabel(elem), gate, axisElem)
		}
		if gate != axisArray {
			t.Errorf("%s: capture gate accepts %s = %t, the axis answers the whole array %t",
				name, tc.typeLabel(field.Type), gate, axisArray)
		}
		if gate {
			accepted++
		} else {
			refused++
		}
	}
	// Two legs that both answered "yes" to everything would agree here too, so
	// the table has to reach both verdicts to mean anything.
	if accepted != 4 || refused != 3 {
		t.Fatalf("table answered %d accepted / %d refused, want 4 / 3; agreement on one verdict proves nothing",
			accepted, refused)
	}
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
