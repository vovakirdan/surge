package sema

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

// capabilityResult is a bare semantic result carrying only what the classifier
// reads, so an axis can be asked about a shape no test program has to spell
// out.
func capabilityResult(in *types.Interner) *Result {
	return &Result{
		TypeInterner:  in,
		CopyTypes:     make(map[types.TypeID]struct{}),
		TypeAttrFacts: make(map[types.TypeID]TypeAttrFacts),
	}
}

func mustClassifier(t *testing.T, res *Result) *CapabilityClassifier {
	t.Helper()
	classifier, err := res.NewCapabilityClassifier()
	if err != nil {
		t.Fatalf("build classifier: %v", err)
	}
	return classifier
}

func mustClassify(t *testing.T, classifier *CapabilityClassifier, id types.TypeID) Capability {
	t.Helper()
	capability, err := classifier.Classify(id)
	if err != nil {
		t.Fatalf("classify type %d: %v", uint32(id), err)
	}
	return capability
}

// capabilityStruct registers a nominal struct whose fields are named f0, f1 and
// so on, in the order they are given.
func capabilityStruct(in *types.Interner, name string, fields ...types.TypeID) types.TypeID {
	id := in.RegisterStruct(in.Strings.Intern(name), source.Span{File: 1, Start: 1, End: 2})
	capabilitySetFields(in, id, fields...)
	return id
}

func capabilitySetFields(in *types.Interner, id types.TypeID, fields ...types.TypeID) {
	out := make([]types.StructField, 0, len(fields))
	for i, field := range fields {
		out = append(out, types.StructField{Name: in.Strings.Intern(fmt.Sprintf("f%d", i)), Type: field})
	}
	in.SetStructFields(id, out)
}

func capabilityDynamicArray(in *types.Interner, elem types.TypeID) types.TypeID {
	return in.Intern(types.Type{Kind: types.KindArray, Elem: elem, Count: types.ArrayDynamicLength})
}

// TestCapabilityCloneSelectorIsBuiltFromCallableCandidates is the two-authorities
// pin. The clone answer must be a function of Result.CallableCandidates, the
// program's single clone authority — so holding everything else fixed and
// changing only that list has to change the answer.
func TestCapabilityCloneSelectorIsBuiltFromCallableCandidates(t *testing.T) {
	in := deferredResolverTestInterner()
	model := capabilityStruct(in, "Model", in.Builtins().String)
	hook := cloneTestHook(in, 10, "app|main.sg:1:2|__clone", model)

	catalogued := capabilityResult(in)
	catalogued.CallableCandidates = []CallableCandidate{hook}
	found := mustClassify(t, mustClassifier(t, catalogued), model).Clone
	if found.State != CloneValidMethod {
		t.Fatalf("clone state with the hook catalogued = %s, want valid-method", found.State)
	}
	if found.MethodKey != hook.BodyKey || found.Method != hook.Symbol {
		t.Fatalf("selected %q/%d, want the catalogued body %q/%d",
			found.MethodKey, found.Method, hook.BodyKey, hook.Symbol)
	}

	// Same interner, same type, same everything the classifier can read —
	// except the catalog it is supposed to be reading.
	missing := mustClassify(t, mustClassifier(t, capabilityResult(in)), model).Clone
	if missing.State != CloneNonClonable {
		t.Fatalf("clone state with an empty catalog = %s, want non-clonable", missing.State)
	}
}

// TestCapabilityCloneMapsEveryCanonicalOutcome walks all five outcomes the
// canonical selector can produce and pins what each becomes.
func TestCapabilityCloneMapsEveryCanonicalOutcome(t *testing.T) {
	cases := []struct {
		name      string
		build     func(in *types.Interner) (types.TypeID, []CallableCandidate)
		wantState CloneState
		wantKind  cloneSelectionKind
	}{
		{
			name: "resolved",
			build: func(in *types.Interner) (types.TypeID, []CallableCandidate) {
				model := capabilityStruct(in, "Model", in.Builtins().String)
				return model, []CallableCandidate{cloneTestHook(in, 10, "app|main.sg:1:2|__clone", model)}
			},
			wantState: CloneValidMethod,
			wantKind:  cloneSelectionResolved,
		},
		{
			name: "absent",
			build: func(in *types.Interner) (types.TypeID, []CallableCandidate) {
				return capabilityStruct(in, "Model", in.Builtins().String), nil
			},
			wantState: CloneNonClonable,
			wantKind:  cloneSelectionAbsent,
		},
		{
			name: "shape rejected",
			build: func(in *types.Interner) (types.TypeID, []CallableCandidate) {
				model := capabilityStruct(in, "Model", in.Builtins().String)
				byValue := cloneTestHook(in, 10, "app|main.sg:1:2|__clone", model)
				byValue.ParamTypes = []types.TypeID{model}
				return model, []CallableCandidate{byValue}
			},
			wantState: CloneNonClonable,
			wantKind:  cloneSelectionShapeRejected,
		},
		{
			name: "conflict",
			build: func(in *types.Interner) (types.TypeID, []CallableCandidate) {
				model := capabilityStruct(in, "Model", in.Builtins().String)
				left := cloneTestHook(in, 10, "left|left/hook.sg:1:2|__clone", model)
				left.ModulePath, left.SourceKey = "left", "left/hook.sg"
				right := cloneTestHook(in, 11, "right|right/hook.sg:1:2|__clone", model)
				right.ModulePath, right.SourceKey = "right", "right/hook.sg"
				return model, []CallableCandidate{left, right}
			},
			wantState: CloneNonClonable,
			wantKind:  cloneSelectionConflict,
		},
		{
			name: "unmaterializable",
			build: func(in *types.Interner) (types.TypeID, []CallableCandidate) {
				model := capabilityStruct(in, "Model", in.Builtins().String)
				declared := cloneTestHook(in, 10, "app|main.sg:1:2|__clone", model)
				declared.HasBody, declared.Intrinsic, declared.Builtin = false, false, false
				return model, []CallableCandidate{declared}
			},
			wantState: CloneNonClonable,
			wantKind:  cloneSelectionUnmaterializable,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			in := deferredResolverTestInterner()
			model, candidates := testCase.build(in)
			res := capabilityResult(in)
			res.CallableCandidates = candidates
			evidence := mustClassify(t, mustClassifier(t, res), model).Clone
			if evidence.State != testCase.wantState {
				t.Fatalf("clone state = %s, want %s", evidence.State, testCase.wantState)
			}
			if evidence.Reason != testCase.wantKind.reason() {
				t.Fatalf("clone reason = %q, want the outcome's own reason %q",
					evidence.Reason, testCase.wantKind.reason())
			}
		})
	}
}

// TestCapabilityCloneKeepsAWinnerNoUseSiteCanSee is the Select-not-Resolve
// proof. Whether a `__clone` is VISIBLE from some caller is a property of that
// caller; whether the type has one is a property of the type. The classifier
// answers the second question, so a hook that every use site would be refused
// access to is still this type's clone implementation.
func TestCapabilityCloneKeepsAWinnerNoUseSiteCanSee(t *testing.T) {
	in := deferredResolverTestInterner()
	model := capabilityStruct(in, "Model", in.Builtins().String)
	hidden := cloneTestHook(in, 10, "owner|owner/hook.sg:1:2|__clone", model)
	hidden.Public, hidden.FilePrivate = false, true
	hidden.ModulePath, hidden.SourceKey = "owner", "owner/hook.sg"

	res := capabilityResult(in)
	res.CallableCandidates = []CallableCandidate{hidden}
	evidence := mustClassify(t, mustClassifier(t, res), model).Clone
	if evidence.State != CloneValidMethod || evidence.MethodKey != hidden.BodyKey {
		t.Fatalf("hidden winner classified as %s/%q, want valid-method/%q",
			evidence.State, evidence.MethodKey, hidden.BodyKey)
	}

	// The contrast: the same hook through the use-site path, from a file that
	// cannot see it, is an error. Resolve would have made the capability depend
	// on who was asking.
	selector := newCloneCanonicalSelector([]CallableCandidate{hidden}, in)
	if _, err := selector.Resolve(model, CloneUseView{AccessModule: "app", SourceKey: "main.sg"}, source.Span{}, "Model"); err == nil {
		t.Fatal("expected the use-site path to refuse a file-private hook from another file")
	}
}

// TestCapabilityDefersAnUndecidedType pins that a type still carrying a generic
// parameter gets "not decided yet" on every axis, never a negative verdict.
func TestCapabilityDefersAnUndecidedType(t *testing.T) {
	in := deferredResolverTestInterner()
	param := in.RegisterTypeParam(in.Strings.Intern("T"), 1, 0, false, types.NoTypeID)
	box := in.RegisterStructInstance(in.Strings.Intern("Box"), source.Span{File: 1, Start: 1, End: 2}, []types.TypeID{param})
	capabilitySetFields(in, box, param)

	capability := mustClassify(t, mustClassifier(t, capabilityResult(in)), box)
	if capability.Clone.State != CloneDeferred {
		t.Fatalf("clone state = %s, want deferred", capability.Clone.State)
	}
	if capability.CarrierDroppable || capability.Traceable || capability.ShardMovable || capability.CrossClonable {
		t.Fatalf("an undecided type claimed a structural capability: %+v", capability)
	}
	for name, reason := range map[string]string{
		"droppable":      capability.DroppableReason,
		"traceable":      capability.TraceableReason,
		"shard":          capability.ShardReason,
		"cross-clonable": capability.CrossCloneReason,
	} {
		if reason != deferredStructureReason {
			t.Fatalf("%s reason = %q, want the undecided-structure reason", name, reason)
		}
	}
}

// TestCapabilityRefusesWhatItCannotAnswerFor pins the fail-closed entry: the
// classifier never answers about a type it does not hold.
func TestCapabilityRefusesWhatItCannotAnswerFor(t *testing.T) {
	if _, err := (&Result{}).NewCapabilityClassifier(); err == nil {
		t.Fatal("expected a result with no interner to refuse to build a classifier")
	}
	in := deferredResolverTestInterner()
	classifier := mustClassifier(t, capabilityResult(in))
	if _, err := classifier.Classify(types.NoTypeID); err == nil {
		t.Fatal("expected the absent type to be refused")
	}
	if _, err := classifier.Classify(types.TypeID(9999)); err == nil {
		t.Fatal("expected a type outside the interner to be refused")
	}
}

// TestCapabilityClassificationIsDeterministic pins that the answer belongs to
// the program and not to the classifier instance or the order it was asked in.
func TestCapabilityClassificationIsDeterministic(t *testing.T) {
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	probes := capabilityDeterminismProbes(in, res)

	forward := mustClassifier(t, res)
	describe := func(classifier *CapabilityClassifier, ids []types.TypeID) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			rendered, err := classifier.Describe(id)
			if err != nil {
				t.Fatalf("describe type %d: %v", uint32(id), err)
			}
			out = append(out, rendered)
		}
		return out
	}
	first := describe(forward, probes)

	// A second instance, asked in the opposite order, so a memo built along a
	// different path cannot go unnoticed.
	reversed := make([]types.TypeID, len(probes))
	for i, id := range probes {
		reversed[len(probes)-1-i] = id
	}
	backward := describe(mustClassifier(t, res), reversed)
	for i := range backward {
		if backward[len(backward)-1-i] != first[i] {
			t.Fatalf("second instance answered differently:\n%s\n%s", first[i], backward[len(backward)-1-i])
		}
	}
}

// capabilityDeterminismProbes builds a spread of shapes covering every branch
// the axes take, and returns them in declaration order.
func capabilityDeterminismProbes(in *types.Interner, res *Result) []types.TypeID {
	text := in.Builtins().String
	number := in.Builtins().Int
	scalar := in.Builtins().Float
	movable := capabilityStruct(in, "Movable", number)
	res.TypeAttrFacts[movable] = TypeAttrFacts{ShardMovable: true}
	pinned := capabilityStruct(in, "Pinned", number)
	res.TypeAttrFacts[pinned] = TypeAttrFacts{ShardPinned: true}
	holder := capabilityStruct(in, "Holder", text, movable)
	rooted := capabilityStruct(in, "Rooted", number, pinned)
	tree := in.RegisterStruct(in.Strings.Intern("Tree"), source.Span{File: 1, Start: 5, End: 6})
	capabilitySetFields(in, tree, text, capabilityDynamicArray(in, tree))
	return []types.TypeID{
		text, number, scalar, movable, pinned, holder, rooted, tree,
		in.Intern(types.MakeReference(holder, false)),
		in.Intern(types.Type{Kind: types.KindFar, Elem: holder}),
		in.Intern(types.Type{Kind: types.KindOwn, Elem: holder}),
		in.RegisterTuple([]types.TypeID{text, number}),
		capabilityDynamicArray(in, text),
		in.Intern(types.Type{Kind: types.KindArray, Elem: number, Count: 4}),
	}
}

// TestCapabilityDescribeNamesTypesByLabel pins that the rendering carries no
// interner ids. Ids are an artefact of the order types happened to be created
// in, so evidence built from them would compare two builds' bookkeeping rather
// than their verdicts.
func TestCapabilityDescribeNamesTypesByLabel(t *testing.T) {
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	pinned := capabilityStruct(in, "Pinned", in.Builtins().Int)
	res.TypeAttrFacts[pinned] = TypeAttrFacts{ShardPinned: true}
	holder := capabilityStruct(in, "Holder", pinned)

	rendered, err := mustClassifier(t, res).Describe(holder)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	for _, want := range []string{"Holder", "Pinned", "shard-movable=false", "via Holder -> Pinned"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("evidence %q does not carry %q", rendered, want)
		}
	}
	if strings.Contains(rendered, fmt.Sprintf("%d", uint32(pinned))) {
		t.Fatalf("evidence %q leaks an interner id", rendered)
	}
}
