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
	if len(obj.Fields) != 0 {
		t.Fatalf("a resource must carry no member list, it has %d", len(obj.Fields))
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
