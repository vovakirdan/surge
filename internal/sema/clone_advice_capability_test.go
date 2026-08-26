package sema

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

// cloneAdviceWant is what one row of the matrix asserts. Every field is a
// POSITIVE claim: a row that merely said "some error happened" would pass on a
// classifier that had stopped answering at all.
type cloneAdviceWant struct {
	state         CloneState
	reason        string
	canDefineHere bool
	defineReason  string
	// pathLabels is the root-to-culprit chain by label, so the assertion reads
	// the verdict rather than the interner ids the run happened to allocate.
	pathLabels []string
	// hasDecl says the evidence must point at the declaration that settled the
	// negative outcome.
	hasDecl bool
}

// TestLanguageClonableAnswersTheWholeMatrix is the P5 clonability matrix.
//
// It asks the ONE authority — the classifier the whole program already uses for
// its other four axes — every shape the diagnostic contract has to name, and it
// pins the three facts that turn a refusal into an edit: which component
// refused, which declaration was rejected, and whether the author is allowed to
// declare `__clone` for this exact type.
func TestLanguageClonableAnswersTheWholeMatrix(t *testing.T) {
	cases := []struct {
		name  string
		build func(in *types.Interner, res *Result) types.TypeID
		want  cloneAdviceWant
	}{
		{
			name: "copy type duplicates as itself",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Point", in.Builtins().Int)
				res.CopyTypes[model] = struct{}{}
				return model
			},
			want: cloneAdviceWant{state: CloneCopy, reason: copyCloneReason},
		},
		{
			name: "valid local method",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Model", in.Builtins().Int)
				res.CallableCandidates = []CallableCandidate{cloneTestHook(in, 10, "app|main.sg:1:2|__clone", model)}
				return model
			},
			want: cloneAdviceWant{state: CloneValidMethod, reason: cloneSelectionResolved.reason()},
		},
		{
			name: "valid imported public method",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Imported", in.Builtins().Int)
				hook := cloneTestHook(in, 11, "lib|lib/model.sg:1:2|__clone", model)
				hook.Public, hook.ModulePath, hook.SourceKey = true, "lib", "lib/model.sg"
				res.CallableCandidates = []CallableCandidate{hook}
				return model
			},
			want: cloneAdviceWant{state: CloneValidMethod, reason: cloneSelectionResolved.reason()},
		},
		{
			// Visibility is a property of the ASKER, and this authority answers
			// about the type. A private winner is still the implementation this
			// type has; the use site's refusal is SEM3186 and arrives elsewhere.
			name: "module-private method still belongs to the type",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Hidden", in.Builtins().Int)
				hook := cloneTestHook(in, 12, "lib|lib/hidden.sg:1:2|__clone", model)
				hook.Public, hook.FilePrivate = false, true
				hook.ModulePath, hook.SourceKey = "lib", "lib/hidden.sg"
				res.CallableCandidates = []CallableCandidate{hook}
				return model
			},
			want: cloneAdviceWant{state: CloneValidMethod, reason: cloneSelectionResolved.reason()},
		},
		{
			// The alias trap from KNOWN_LIMITATIONS: a `__clone` written against
			// the alias spelling does not clone the type the alias names. The
			// selector still sees it CLAIM that type, so the verdict is a
			// rejected shape pointing at the declaration — which is the fact
			// the author needs — and not an offer to write a second one.
			name: "method declared on an alias alone does not answer for the target",
			build: func(in *types.Interner, res *Result) types.TypeID {
				leaf := capabilityStruct(in, "Leaf", in.Builtins().Int)
				alias := in.RegisterAlias(in.Strings.Intern("Handle"), source.Span{File: 1, Start: 30, End: 36})
				in.SetAliasTarget(alias, leaf)
				res.CallableCandidates = []CallableCandidate{cloneTestHook(in, 13, "app|main.sg:3:4|__clone", alias)}
				return alias
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionShapeRejected.reason(),
				canDefineHere: false, defineReason: cloneAlreadyClaimedReason,
				pathLabels: []string{"Leaf"}, hasDecl: true,
			},
		},
		{
			name: "no declaration claims the type",
			build: func(in *types.Interner, res *Result) types.TypeID {
				return capabilityStruct(in, "NonClone", in.Builtins().Int)
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionAbsent.reason(),
				canDefineHere: true, defineReason: extendableTargetReason,
				pathLabels: []string{"NonClone"},
			},
		},
		{
			name: "wrong receiver is a declaration for another type",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Model", in.Builtins().Int)
				other := capabilityStruct(in, "Other", in.Builtins().Int)
				res.CallableCandidates = []CallableCandidate{cloneTestHook(in, 14, "app|main.sg:1:2|__clone", other)}
				return model
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionAbsent.reason(),
				canDefineHere: true, defineReason: extendableTargetReason,
				pathLabels: []string{"Model"},
			},
		},
		{
			name: "wrong result type",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Model", in.Builtins().Int)
				other := capabilityStruct(in, "Other", in.Builtins().Int)
				hook := cloneTestHook(in, 15, "app|main.sg:1:2|__clone", model)
				hook.ResultType = other
				res.CallableCandidates = []CallableCandidate{hook}
				return model
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionShapeRejected.reason(),
				canDefineHere: false, defineReason: cloneAlreadyClaimedReason,
				pathLabels: []string{"Model"}, hasDecl: true,
			},
		},
		{
			name: "by-value receiver instead of a borrow",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Model", in.Builtins().Int)
				hook := cloneTestHook(in, 16, "app|main.sg:1:2|__clone", model)
				hook.ParamTypes = []types.TypeID{model}
				res.CallableCandidates = []CallableCandidate{hook}
				return model
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionShapeRejected.reason(),
				canDefineHere: false, defineReason: cloneAlreadyClaimedReason,
				pathLabels: []string{"Model"}, hasDecl: true,
			},
		},
		{
			name: "wrong arity",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Model", in.Builtins().Int)
				hook := cloneTestHook(in, 17, "app|main.sg:1:2|__clone", model)
				hook.ParamTypes = append(hook.ParamTypes, in.Builtins().Int)
				hook.Defaults = []bool{false, false}
				hook.Variadic = []bool{false, false}
				res.CallableCandidates = []CallableCandidate{hook}
				return model
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionShapeRejected.reason(),
				canDefineHere: false, defineReason: cloneAlreadyClaimedReason,
				pathLabels: []string{"Model"}, hasDecl: true,
			},
		},
		{
			name: "several equally specific bodies",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Model", in.Builtins().Int)
				left := cloneTestHook(in, 18, "left|left/hook.sg:1:2|__clone", model)
				left.ModulePath, left.SourceKey = "left", "left/hook.sg"
				right := cloneTestHook(in, 19, "right|right/hook.sg:1:2|__clone", model)
				right.ModulePath, right.SourceKey = "right", "right/hook.sg"
				res.CallableCandidates = []CallableCandidate{left, right}
				return model
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionConflict.reason(),
				canDefineHere: false, defineReason: cloneAlreadyClaimedReason,
				pathLabels: []string{"Model"}, hasDecl: true,
			},
		},
		{
			name: "winner with no materializable body",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Model", in.Builtins().Int)
				hook := cloneTestHook(in, 20, "app|main.sg:1:2|__clone", model)
				hook.HasBody, hook.Intrinsic, hook.Builtin = false, false, false
				res.CallableCandidates = []CallableCandidate{hook}
				return model
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionUnmaterializable.reason(),
				canDefineHere: false, defineReason: cloneAlreadyClaimedReason,
				pathLabels: []string{"Model"}, hasDecl: true,
			},
		},
		{
			// The whole point of the path: the outer type is refused, and the
			// member the author has to fix is two hops down.
			name: "nested component refuses for its owner",
			build: func(in *types.Interner, res *Result) types.TypeID {
				leaf := capabilityStruct(in, "Leaf", in.Builtins().Int)
				middle := capabilityStruct(in, "Middle", leaf)
				outer := capabilityStruct(in, "Outer", middle)
				// Middle can clone; Outer and Leaf cannot, and it is Leaf the
				// author has to give a body to.
				res.CallableCandidates = []CallableCandidate{
					cloneTestHook(in, 22, "app|main.sg:3:4|__clone", middle),
				}
				return outer
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionAbsent.reason(),
				canDefineHere: true, defineReason: extendableTargetReason,
				pathLabels: []string{"Outer", "Middle", "Leaf"},
			},
		},
		{
			name: "sealed target refuses definition advice",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Locked", in.Builtins().Int)
				res.TypeAttrFacts[model] = TypeAttrFacts{Sealed: true}
				return model
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionAbsent.reason(),
				canDefineHere: false, defineReason: sealedTargetReason,
				pathLabels: []string{"Locked"},
			},
		},
		{
			name: "runtime-owned target refuses definition advice",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Task")
				in.MarkRuntimeHandleType(model)
				return model
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionAbsent.reason(),
				canDefineHere: false, defineReason: runtimeOwnedTargetReason,
				pathLabels: []string{"Task"},
			},
		},
		{
			name: "structural shape has no name to extend",
			build: func(in *types.Interner, res *Result) types.TypeID {
				return capabilityDynamicArray(in, in.Builtins().String)
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionAbsent.reason(),
				canDefineHere: false, defineReason: structuralTargetReason,
			},
		},
		{
			name: "an exclusive borrow carries no methods of its own",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Model", in.Builtins().Int)
				return in.Intern(types.MakeReference(model, true))
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionAbsent.reason(),
				canDefineHere: false, defineReason: unextendableTargetReason,
			},
		},
		{
			// `far T` stays affine under the crossing contract, so offering to
			// declare a clone for the handle would be advice against the model.
			name: "a far handle is not the language's to duplicate",
			build: func(in *types.Interner, res *Result) types.TypeID {
				model := capabilityStruct(in, "Model", in.Builtins().Int)
				return in.Intern(types.MakeFar(model))
			},
			want: cloneAdviceWant{
				state: CloneNonClonable, reason: cloneSelectionAbsent.reason(),
				canDefineHere: false, defineReason: runtimeOwnedTargetReason,
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			in := deferredResolverTestInterner()
			res := capabilityResult(in)
			subject := testCase.build(in, res)
			classifier := mustClassifier(t, res)
			evidence, err := classifier.LanguageClonable(subject)
			if err != nil {
				t.Fatalf("LanguageClonable(%s): %v", types.Label(in, subject), err)
			}
			assertCloneAdvice(t, classifier, evidence, testCase.want)
		})
	}
}

func assertCloneAdvice(t *testing.T, c *CapabilityClassifier, got CloneEvidence, want cloneAdviceWant) {
	t.Helper()
	if got.State != want.state {
		t.Fatalf("clone state = %s, want %s (reason %q)", got.State, want.state, got.Reason)
	}
	if got.Reason != want.reason {
		t.Fatalf("clone reason = %q, want %q", got.Reason, want.reason)
	}
	if got.CanDefineHere != want.canDefineHere {
		t.Fatalf("CanDefineHere = %t (%q), want %t (%q)",
			got.CanDefineHere, got.DefineReason, want.canDefineHere, want.defineReason)
	}
	if want.defineReason != "" && got.DefineReason != want.defineReason {
		t.Fatalf("DefineReason = %q, want %q", got.DefineReason, want.defineReason)
	}
	if want.pathLabels != nil {
		labels := c.labels(got.Path)
		if strings.Join(labels, " -> ") != strings.Join(want.pathLabels, " -> ") {
			t.Fatalf("non-clonable path = %v, want %v", labels, want.pathLabels)
		}
	}
	if want.hasDecl && got.Decl == (source.Span{}) {
		t.Fatalf("a rejected `__clone` exists but the evidence names no declaration: %+v", got)
	}
	if !want.hasDecl && got.Decl != (source.Span{}) {
		t.Fatalf("evidence names a declaration where none settled the outcome: %+v", got)
	}
}

// TestLanguageClonableDefersAGenericReceiver keeps the deferred state from
// collapsing into "no".
//
// An emitter that read CloneDeferred as non-clonable would refuse a program
// that is about to be legal, and would put a clonability constraint on a type
// parameter whose only crime is not being substituted yet.
func TestLanguageClonableDefersAGenericReceiver(t *testing.T) {
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	param := in.Intern(types.Type{Kind: types.KindGenericParam})
	classifier := mustClassifier(t, res)
	evidence, err := classifier.LanguageClonable(param)
	if err != nil {
		t.Fatalf("LanguageClonable(generic param): %v", err)
	}
	if evidence.State != CloneDeferred {
		t.Fatalf("generic parameter clone state = %s, want %s", evidence.State, CloneDeferred)
	}
	if evidence.Reason != deferredCloneReason {
		t.Fatalf("deferred reason = %q, want %q", evidence.Reason, deferredCloneReason)
	}
	if evidence.CanDefineHere {
		t.Fatal("a type parameter was offered a `__clone` definition")
	}
	if len(evidence.Path) != 0 {
		t.Fatalf("a deferred verdict named a refusing component: %v", classifier.labels(evidence.Path))
	}
}

// TestLanguageClonableAgreesWithClassify proves there is ONE answer, not two.
//
// LanguageClonable is the query the diagnostics ask; Capability.Clone is the
// field the carrier ABI reads. If those two could disagree, the compiler would
// refuse a clone the descriptor advertises, or advertise one the compiler
// refused.
func TestLanguageClonableAgreesWithClassify(t *testing.T) {
	in := deferredResolverTestInterner()
	res := capabilityResult(in)
	leaf := capabilityStruct(in, "Leaf", in.Builtins().Int)
	outer := capabilityStruct(in, "Outer", leaf)
	copied := capabilityStruct(in, "Copied", in.Builtins().Int)
	res.CopyTypes[copied] = struct{}{}
	res.CallableCandidates = []CallableCandidate{cloneTestHook(in, 30, "app|main.sg:1:2|__clone", leaf)}

	classifier := mustClassifier(t, res)
	for _, id := range []types.TypeID{leaf, outer, copied, in.Builtins().String} {
		direct, err := classifier.LanguageClonable(id)
		if err != nil {
			t.Fatalf("LanguageClonable(%s): %v", types.Label(in, id), err)
		}
		full := mustClassify(t, classifier, id)
		if direct.State != full.Clone.State || direct.Reason != full.Clone.Reason {
			t.Fatalf("%s: query says %s(%q), Classify says %s(%q)",
				types.Label(in, id), direct.State, direct.Reason, full.Clone.State, full.Clone.Reason)
		}
	}
}

// TestOneCloneStateAuthorityExists is the source-shape census behind
// RV2-DEBT-134.
//
// The four states are canonical, and the risk the debt names is a SECOND
// classifier appearing beside them — most plausibly a private three-way
// clonable/non-clonable/deferred enum in whichever package needed an answer
// next. This walks the compiler's own source and refuses any other declaration
// of clone states.
func TestOneCloneStateAuthorityExists(t *testing.T) {
	roots := []string{".", "../driver", "../mono", "../mir", "../hir", "../diag", "../lsp", "../fix", "../types", "../buildpipeline"}
	fset := token.NewFileSet()
	declarations := make([]string, 0, 1)
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(root, name)
			file, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				t.Fatalf("parse %s: %v", path, parseErr)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				spec, ok := node.(*ast.TypeSpec)
				if !ok || spec.Name == nil {
					return true
				}
				if !declaresCloneStates(spec.Name.Name) {
					return true
				}
				declarations = append(declarations, path+"::"+spec.Name.Name)
				return true
			})
		}
	}
	if len(declarations) != 1 || !strings.HasSuffix(declarations[0], "capability_classifier.go::CloneState") {
		t.Fatalf("clone-state declarations = %v, want exactly sema.CloneState", declarations)
	}
}

// declaresCloneStates names the shape the census refuses: a type whose name
// says it enumerates how a value is cloned. `CloneEvidence` and
// `cloneSelectionKind` are not that — the first carries the verdict this
// authority produced, and the second is the selector's own outcome, which the
// one authority translates.
func declaresCloneStates(name string) bool {
	lowered := strings.ToLower(name)
	if !strings.Contains(lowered, "clon") {
		return false
	}
	for _, suffix := range []string{"state", "states", "verdict", "capability", "class", "classification"} {
		if strings.HasSuffix(lowered, suffix) {
			return true
		}
	}
	return false
}
