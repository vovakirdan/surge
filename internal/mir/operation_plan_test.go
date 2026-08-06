package mir

import (
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/mono"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// operationPlanFixture is one module holding a single plain struct root, which
// is all these tests need: every one of them is about whether publication is
// refused, not about what a published entry says.
func operationPlanFixture(t *testing.T) (*Module, *types.Interner) {
	t.Helper()
	typesIn := newLayoutTestInterner()
	pair := typesIn.RegisterStruct(typesIn.Strings.Intern("Pair"), source.Span{})
	typesIn.SetStructFields(pair, []types.StructField{
		{Type: typesIn.Builtins().Bool},
		{Type: typesIn.Builtins().Uint64},
	})
	return moduleWithLayoutGlobal(pair), typesIn
}

// classifierFor builds a capability authority over one interner. It answers for
// the fixture's types; what it answers does not matter here.
func classifierFor(t *testing.T, typesIn *types.Interner) *sema.CapabilityClassifier {
	t.Helper()
	classifier, err := (&sema.Result{TypeInterner: typesIn}).NewCapabilityClassifier()
	if err != nil {
		t.Fatalf("build capability classifier: %v", err)
	}
	return classifier
}

// TestFinalizeModuleMetaWithoutAnOperationPlanPublishesLayoutsOnly pins the
// absence semantics every unit-test caller and the single-file MIR dump depend
// on: no plan input means no operation registry, and the layout registry is
// finalized exactly as before.
//
// Absence is not an empty registry. A reader that asks the nil registry anything
// gets its fail-closed answer, so "this module has no plans" and "this module
// says every type can do nothing" never wear the same face.
func TestFinalizeModuleMetaWithoutAnOperationPlanPublishesLayoutsOnly(t *testing.T) {
	mod, typesIn := operationPlanFixture(t)
	if err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU(), nil); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	if mod.Meta.Layouts == nil {
		t.Fatal("layout finalization published no layout registry")
	}
	if mod.Meta.Operations != nil {
		t.Fatal("layout finalization published an operation registry with no plan input")
	}
	if _, err := mod.Meta.Operations.Value(mod.Globals[0].Type); err == nil {
		t.Fatal("the absent operation registry answered a value query")
	}
}

// TestFinalizeModuleMetaRefusesAPlanWithoutACapabilityAuthority pins that a
// half-built input is refused rather than silently publishing plans derived from
// whatever parts it did carry.
func TestFinalizeModuleMetaRefusesAPlanWithoutACapabilityAuthority(t *testing.T) {
	mod, typesIn := operationPlanFixture(t)
	err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU(), &OperationPlanInput{})
	if err == nil {
		t.Fatal("publication accepted a plan input carrying no capability authority")
	}
	if !strings.Contains(err.Error(), "capability authority") {
		t.Fatalf("refusal does not name what is missing: %v", err)
	}
	if mod.Meta.Operations != nil {
		t.Fatal("a refused publication left an operation registry behind")
	}
}

// TestFinalizeModuleMetaRefusesAPlanWithoutACloneEmissionIndex pins the same for
// the index that answers which clone bodies the compiler emits. Its zero value
// is no index at all, and consulting it would call every clonable type
// unaccountable.
func TestFinalizeModuleMetaRefusesAPlanWithoutACloneEmissionIndex(t *testing.T) {
	mod, typesIn := operationPlanFixture(t)
	err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU(), &OperationPlanInput{
		Capabilities: classifierFor(t, typesIn),
	})
	if err == nil {
		t.Fatal("publication accepted a plan input carrying no clone emission index")
	}
	if !strings.Contains(err.Error(), "clone emission index") {
		t.Fatalf("refusal does not name what is missing: %v", err)
	}
}

// TestFinalizeModuleMetaRefusesAClosureFreeCallableMap is the fail-closed rule
// that matters most of the three, because a closure-free map ANSWERS.
//
// A CallableMap built without a finalized instantiation closure keys callables
// by raw symbol, a compatibility spelling for isolated low-level callers. It
// would resolve some lookups and miss others for reasons that have nothing to do
// with the program, so publication refuses it by name instead of publishing
// plans built on confident wrong answers.
func TestFinalizeModuleMetaRefusesAClosureFreeCallableMap(t *testing.T) {
	mod, typesIn := operationPlanFixture(t)
	err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU(), &OperationPlanInput{
		Capabilities: classifierFor(t, typesIn),
		Clones:       (&sema.Result{TypeInterner: typesIn}).NewCloneEmissionIndex(),
		Callables:    mono.CallableMap{},
	})
	if err == nil {
		t.Fatal("publication accepted a callable map with no canonical instantiation identity")
	}
	if !strings.Contains(err.Error(), "canonical instantiation identity") {
		t.Fatalf("refusal does not name what is missing: %v", err)
	}
	if mod.Meta.Layouts == nil {
		t.Fatal("a refused operation plan also lost the layout registry")
	}
}

// TestNewOperationPlanInputAnswersNilWithoutAnAuthority pins the constructor's
// half of the absence contract: a result that never reached a whole-program
// authority site produces no plan input, and therefore no registry, rather than
// an input that would be refused as half-built.
func TestNewOperationPlanInputAnswersNilWithoutAnAuthority(t *testing.T) {
	if plan := NewOperationPlanInput(nil, nil); plan != nil {
		t.Fatal("a nil semantic result produced a plan input")
	}
	if plan := NewOperationPlanInput(&sema.Result{}, &mono.MonoModule{}); plan != nil {
		t.Fatal("a result carrying no capability authority produced a plan input")
	}
	if plan := NewOperationPlanInput(&sema.Result{Capabilities: &sema.CapabilityClassifier{}}, nil); plan != nil {
		t.Fatal("a missing monomorphized module produced a plan input")
	}
}

// TestOperationRoleRefusesARootThatIsNotAValue pins the role translation. Every
// census root is a value root; a root that says otherwise is refused rather than
// published with a role the registry would then reject less legibly.
func TestOperationRoleRefusesARootThatIsNotAValue(t *testing.T) {
	if _, err := operationRole(RootKey, 7); err == nil {
		t.Fatal("a root reached only as a key was accepted as an operation root")
	}
	if _, err := operationRole(RootValue|RootKey<<4, 7); err == nil {
		t.Fatal("a root carrying unknown roles was accepted")
	}
	role, err := operationRole(RootValue|RootKey, 7)
	if err != nil {
		t.Fatalf("operationRole: %v", err)
	}
	if role != 0b11 {
		t.Fatalf("role = %d, want both the value and key bits", role)
	}
}
