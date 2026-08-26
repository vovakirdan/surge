package sema

import (
	"errors"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// obligationProbe builds a classifier over one non-clonable nominal type and
// returns both, so a row can ask the real authority rather than a stub.
func obligationProbe(t *testing.T, name string) (*CapabilityClassifier, types.TypeID, *types.Interner) {
	t.Helper()
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	subject := capabilityStruct(in, name, in.Builtins().Int)
	return mustClassifier(t, res), subject, in
}

func mustObligationDiagnostic(t *testing.T, err error) *diag.Diagnostic {
	t.Helper()
	if err == nil {
		t.Fatal("an unmet obligation produced no error")
	}
	var carrier *CloneObligationError
	if !errors.As(err, &carrier) {
		t.Fatalf("obligation error does not carry a diagnostic: %v", err)
	}
	diagnostic := carrier.Diagnostic()
	if diagnostic == nil {
		t.Fatalf("obligation error carries a nil diagnostic: %v", err)
	}
	return diagnostic
}

// TestCloneObligationOperationsReportUnderTheirOwnCode is the D2 handoff.
//
// `Task<T>.clone()` and `Map<K, V>.keys()` are the same question asked by two
// operations, and the owner's ruling separates them by number: the task reuses
// SEM3116 so its concrete and instantiated forms are provably one diagnostic,
// while `keys()` reports SEM3204 because what is unavailable is the OPERATION
// at this instantiation, not the type.
//
// The `keys()` CALL SITE is not wired here — it belongs to the map lane, which
// records this obligation at that call in one line. What this row pins is that
// the mechanics are already in place and answer from the one classifier, so
// that lane adds a site rather than a second diagnostic path.
func TestCloneObligationOperationsReportUnderTheirOwnCode(t *testing.T) {
	cases := []struct {
		name      string
		op        CloneObligationOp
		container string
		wantCode  diag.Code
		wantIn    []string
	}{
		{
			name: "task clone reuses the type-not-clonable number",
			op:   CloneObligationTaskClone, container: "Task<Widget>",
			wantCode: diag.SemaTypeNotClonable,
			wantIn: []string{
				"cloning this handle to `Task<Widget>`",
				"owes another independent `Widget`",
				"neither Copy nor validly `__clone`-able",
			},
		},
		{
			name: "map keys reports an operation unavailable at this instantiation",
			op:   CloneObligationMapKeys, container: "Map<Widget, int>",
			wantCode: diag.SemaOperationNeedsClonable,
			wantIn: []string{
				"`keys()` on `Map<Widget, int>`",
				"returns an array that owns its keys",
				"owes an independent `Widget`",
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			classifier, subject, _ := obligationProbe(t, "Widget")
			evidence, err := classifier.LanguageClonable(subject)
			if err != nil {
				t.Fatalf("LanguageClonable: %v", err)
			}
			if evidence.State != CloneNonClonable {
				t.Fatalf("probe subject is %s, want a non-clonable one", evidence.State)
			}
			obligation := &CloneObligation{
				Op: testCase.op, Subject: subject, SubjectLabel: "Widget",
				ContainerLabel: testCase.container,
				Site:           source.Span{File: 1, Start: 10, End: 20},
			}
			diagnostic := mustObligationDiagnostic(t, reportCloneObligation(classifier, obligation, &evidence))
			if diagnostic.Code != testCase.wantCode {
				t.Fatalf("code = %s, want %s", diagnostic.Code.ID(), testCase.wantCode.ID())
			}
			for _, want := range testCase.wantIn {
				if !strings.Contains(diagnostic.Message, want) {
					t.Fatalf("headline %q does not contain %q", diagnostic.Message, want)
				}
			}
			if len(diagnostic.Fixes) != 0 {
				t.Fatalf("an unmet clone obligation carried a fix: %+v", diagnostic.Fixes)
			}
			if diagnostic.Primary != obligation.Site {
				t.Fatalf("primary span = %v, want the operation's own span %v", diagnostic.Primary, obligation.Site)
			}
		})
	}
}

// TestCloneObligationNamesTheFirstRefusingComponent proves the note an author
// can act on. A refusal that stops at the outer type sends them looking for the
// member the compiler had already found.
func TestCloneObligationNamesTheFirstRefusingComponent(t *testing.T) {
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	leaf := capabilityStruct(in, "Leaf", in.Builtins().Int)
	middle := capabilityStruct(in, "Middle", leaf)
	outer := capabilityStruct(in, "Outer", middle)
	res.CallableCandidates = []CallableCandidate{cloneTestHook(in, 40, "app|main.sg:3:4|__clone", middle)}

	classifier := mustClassifier(t, res)
	evidence, err := classifier.LanguageClonable(outer)
	if err != nil {
		t.Fatalf("LanguageClonable: %v", err)
	}
	obligation := &CloneObligation{
		Op: CloneObligationTaskClone, Subject: outer, SubjectLabel: "Outer",
		ContainerLabel: "Task<Outer>", Site: source.Span{File: 1, Start: 1, End: 2},
	}
	diagnostic := mustObligationDiagnostic(t, reportCloneObligation(classifier, obligation, &evidence))
	found := false
	for _, note := range diagnostic.Notes {
		if strings.Contains(note.Msg, "Outer -> Middle -> Leaf") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no note names the path down to the refusing component: %+v", diagnostic.Notes)
	}
}

// TestClonableSubjectsSatisfyTheirObligation is the negative control for the
// checker itself: Copy, a valid method, and a still-undecided generic all pass,
// so the refusal cannot be a function of "the classifier was asked".
func TestClonableSubjectsSatisfyTheirObligation(t *testing.T) {
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	copied := capabilityStruct(in, "Copied", in.Builtins().Int)
	res.CopyTypes[copied] = struct{}{}
	cloned := capabilityStruct(in, "Cloned", in.Builtins().Int)
	res.CallableCandidates = []CallableCandidate{cloneTestHook(in, 41, "app|main.sg:1:2|__clone", cloned)}
	deferredParam := in.Intern(types.Type{Kind: types.KindGenericParam})

	classifier := mustClassifier(t, res)
	for _, subject := range []types.TypeID{copied, cloned, deferredParam, in.Builtins().Int} {
		obligation := &CloneObligation{
			Op: CloneObligationTaskClone, Subject: subject,
			SubjectLabel: types.Label(in, subject), Site: source.Span{File: 1, Start: 1, End: 2},
		}
		if err := checkCloneObligation(classifier, obligation); err != nil {
			t.Fatalf("%s was refused: %v", types.Label(in, subject), err)
		}
	}
}

// TestRequireClonableRoutesConcreteAndGenericSubjects is the D2 handoff shown
// working, without a `keys()` site existing yet.
//
// The map lane adds that site with one call. What this pins is that the call
// does the routing: a concrete subject becomes a recorded obligation the
// post-merge validator answers, and a generic one becomes a deferred edge the
// instantiation walk answers, carrying the operation so the right code and the
// right sentence come out at the other end.
func TestRequireClonableRoutesConcreteAndGenericSubjects(t *testing.T) {
	for _, op := range []CloneObligationOp{CloneObligationTaskClone, CloneObligationMapKeys} {
		t.Run(op.String(), func(t *testing.T) {
			spec, known := op.spec()
			if !known {
				t.Fatalf("%s has no diagnostic spec", op)
			}
			if spec.code == 0 || spec.headline == "" || spec.consumeHelp == "" {
				t.Fatalf("%s is missing part of its spec: %+v", op, spec)
			}
		})
	}
	// The two operations must not report under one number: the task reuses
	// SEM3116 so its concrete and instantiated forms are provably one
	// diagnostic, while `keys()` says an OPERATION is unavailable here.
	taskSpec, _ := CloneObligationTaskClone.spec()
	keysSpec, _ := CloneObligationMapKeys.spec()
	if taskSpec.code == keysSpec.code {
		t.Fatalf("both operations report under %s", taskSpec.code.ID())
	}
}
