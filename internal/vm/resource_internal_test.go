package vm

import (
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

// newResourceFixture builds the shape a runtime-resource type has: a nominal
// struct whose only member is the compiler-private opaque word, plus an
// ordinary struct that does not declare one.
func newResourceFixture(t *testing.T) (*VM, types.TypeID, types.TypeID) {
	t.Helper()
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()
	i64 := interner.Intern(types.Type{Kind: types.KindInt, Width: 64})

	handleType := interner.RegisterStruct(interner.Strings.Intern("Task"), source.Span{})
	interner.SetStructFields(handleType, []types.StructField{
		{Name: interner.Strings.Intern(resourceOpaqueField), Type: i64},
	})
	// A runtime-handle type is a nominal struct that the interner has been told
	// is one. Without that mark the fixture would be an ordinary struct and the
	// verdicts below would answer about the wrong thing.
	interner.MarkRuntimeHandleType(handleType)
	plain := interner.RegisterStruct(interner.Strings.Intern("Point"), source.Span{})
	interner.SetStructFields(plain, []types.StructField{
		{Name: interner.Strings.Intern("x"), Type: i64},
	})

	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{i64, handleType, plain})
	if err != nil {
		t.Fatalf("freezing the fixture layouts must succeed: %v", err)
	}
	machine := New(&mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}}, nil, nil, interner, nil)
	return machine, handleType, plain
}

func TestResourceCarriesOneWordAndNoMembers(t *testing.T) {
	machine, handleType, _ := newResourceFixture(t)

	value, vmErr := machine.resourceValue(9_000_000_042, handleType, "Task")
	if vmErr != nil {
		t.Fatalf("building a resource must succeed: %v", vmErr)
	}
	if value.Kind != VKResource {
		t.Fatalf("a resource is carried as %s, want resource", value.Kind)
	}
	obj := machine.Heap.Get(value.H)
	if obj.Kind != OKResource {
		t.Fatalf("a resource object is a %v, want resource", obj.Kind)
	}
	if obj.ArrLen != 0 || obj.ArrCap != 0 {
		t.Fatalf("a resource must carry no elements, it has %d in %d slots", obj.ArrLen, obj.ArrCap)
	}
	if obj.Resource != 9_000_000_042 {
		t.Fatalf("the opaque word read back as %d", obj.Resource)
	}

	word, vmErr := machine.resourceWord(value, "Task", "task id")
	if vmErr != nil {
		t.Fatalf("reading the word back must succeed: %v", vmErr)
	}
	if word != 9_000_000_042 {
		t.Fatalf("the word round-tripped as %d", word)
	}
}

func TestResourceReleaseReleasesNothingBeneathIt(t *testing.T) {
	machine, handleType, _ := newResourceFixture(t)

	value, vmErr := machine.resourceValue(7, handleType, "Task")
	if vmErr != nil {
		t.Fatalf("building a resource must succeed: %v", vmErr)
	}
	before := machine.heapCounters.freeCount
	machine.Heap.Release(value.H)

	if machine.heapCounters.freeCount != before+1 {
		t.Fatalf("releasing a resource freed %d objects, want exactly 1",
			machine.heapCounters.freeCount-before)
	}
	if machine.heapCounters.allocCount != machine.heapCounters.freeCount {
		t.Fatalf("a resource left the heap unbalanced: %d allocations, %d frees",
			machine.heapCounters.allocCount, machine.heapCounters.freeCount)
	}
}

func TestResourceRefusesATypeThatDeclaresNoOpaqueWord(t *testing.T) {
	machine, _, plain := newResourceFixture(t)

	_, vmErr := machine.resourceValue(1, plain, "Task")
	if vmErr == nil {
		t.Fatal("a type with no private opaque member must not become a resource")
	}
	if !strings.Contains(vmErr.Message, resourceOpaqueField) {
		t.Fatalf("the refusal must name the missing member: %q", vmErr.Message)
	}
}

func TestResourceWordAcceptsTheNumbersIntrinsicsHandBack(t *testing.T) {
	machine, _, _ := newResourceFixture(t)

	word, vmErr := machine.resourceWord(MakeInt(12, types.NoTypeID), "Task", "task id")
	if vmErr != nil || word != 12 {
		t.Fatalf("a plain id read back as %d (%v)", word, vmErr)
	}

	_, vmErr = machine.resourceWord(MakeBool(true, types.NoTypeID), "Task", "task id")
	if vmErr == nil {
		t.Fatal("a value that is neither a number nor a resource must be refused")
	}
	if !strings.Contains(vmErr.Message, "Task") {
		t.Fatalf("the refusal must name the type it wanted: %q", vmErr.Message)
	}
}

// The guard on this carrier change is that it is behaviour-neutral for every
// runtime-handle type. The resource-id lifecycle is pinned by the existing
// suites that open, pass and close these resources end to end. What was NOT
// pinned anywhere is the handful of verdicts below, which the carrier could
// plausibly have moved, so they are pinned here.

func TestResourceIsNotAValueCompositeAndClonesByCountingAReference(t *testing.T) {
	machine, handleType, _ := newResourceFixture(t)

	if machine.isValueCompositeType(handleType) {
		t.Fatal("a runtime handle must not be a value composite, or copying one would duplicate the resource")
	}
	if kind := machine.storageCellKind(handleType); kind != cellHandle {
		t.Fatalf("a runtime handle is stored as %s, want handle", kind)
	}

	value, vmErr := machine.resourceValue(5, handleType, "Task")
	if vmErr != nil {
		t.Fatalf("building a resource must succeed: %v", vmErr)
	}
	cloned, vmErr := machine.cloneValueComposite(value)
	if vmErr != nil {
		t.Fatalf("cloning a resource must succeed: %v", vmErr)
	}
	if cloned.H != value.H {
		t.Fatalf("cloning a resource made a second object (%d vs %d); the runtime owns one resource, not two",
			cloned.H, value.H)
	}
	if got := machine.Heap.Get(value.H).RefCount; got != 2 {
		t.Fatalf("cloning a resource counted %d references, want 2", got)
	}
}

func TestResourceEqualityComparesIdentityNotItsWord(t *testing.T) {
	machine, handleType, _ := newResourceFixture(t)

	first, vmErr := machine.resourceValue(7, handleType, "Task")
	if vmErr != nil {
		t.Fatalf("building a resource must succeed: %v", vmErr)
	}
	second, vmErr := machine.resourceValue(7, handleType, "Task")
	if vmErr != nil {
		t.Fatalf("building a second resource must succeed: %v", vmErr)
	}

	same, vmErr := machine.evalEqual(first, first)
	if vmErr != nil || !same.Bool {
		t.Fatalf("a resource must equal itself, got %v (%v)", same.Bool, vmErr)
	}
	other, vmErr := machine.evalEqual(first, second)
	if vmErr != nil {
		t.Fatalf("comparing two resources must succeed: %v", vmErr)
	}
	if other.Bool {
		t.Fatal("two separately built resources must not compare equal; equality is identity here, as it was when they were struct boxes")
	}
}

func TestResourceNamesItselfWhereverItIsRendered(t *testing.T) {
	machine, handleType, _ := newResourceFixture(t)

	value, vmErr := machine.resourceValue(3, handleType, "Task")
	if vmErr != nil {
		t.Fatalf("building a resource must succeed: %v", vmErr)
	}
	if got := value.Kind.String(); got != "resource" {
		t.Fatalf("the value kind renders as %q, want \"resource\"", got)
	}
	if got := value.String(); got != "resource" {
		t.Fatalf("the value renders as %q, want \"resource\"", got)
	}
	if got := machine.objectKindLabel(OKResource); got != "resource" {
		t.Fatalf("a leaked resource would be reported as %q, want \"resource\"", got)
	}
	if !value.IsHeap() {
		t.Fatal("a resource is heap-backed; a walk that thought otherwise would leak it")
	}
}
