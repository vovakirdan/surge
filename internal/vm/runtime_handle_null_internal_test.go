package vm

import (
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

// The null runtime handle below the D2 return-origin gate.
//
// At this base the gate refuses every program that reaches a `Task` or a
// `Range` default (runtime_v2_default_runtime_handle_e2e_test.go says which
// reasons), so no end-to-end row can pin those two families. These rows pin
// them one level down, on the VM's own default: the value it builds, what the
// lifetime paths do with it, and how the task and channel operations refuse
// it -- with the native runtime's words, since a program that reaches them
// once the gate lets it through must fail the same way on both backends.

type nullHandleFixture struct {
	machine      *VM
	task         types.TypeID
	channel      types.TypeID
	rangeHandle  types.TypeID
	taskAlias    types.TypeID
	ownedTask    types.TypeID
	plainStructT types.TypeID
}

func newNullHandleFixture(t *testing.T) nullHandleFixture {
	t.Helper()
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()
	i64 := interner.Intern(types.Type{Kind: types.KindInt, Width: 64})
	handle := func(name string) types.TypeID {
		id := interner.RegisterStruct(interner.Strings.Intern(name), source.Span{})
		interner.SetStructFields(id, []types.StructField{
			{Name: interner.Strings.Intern(resourceOpaqueField), Type: i64},
		})
		interner.MarkRuntimeHandleType(id)
		return id
	}
	fx := nullHandleFixture{task: handle("Task"), channel: handle("Channel"), rangeHandle: handle("Range")}
	fx.taskAlias = interner.RegisterAlias(interner.Strings.Intern("Job"), source.Span{})
	interner.SetAliasTarget(fx.taskAlias, fx.task)
	fx.ownedTask = interner.Intern(types.Type{Kind: types.KindOwn, Elem: fx.task})
	// Same one-member shape, NOT marked: the default must not treat a user
	// struct that happens to look like a handle as one.
	fx.plainStructT = interner.RegisterStruct(interner.Strings.Intern("Task"), source.Span{})
	interner.SetStructFields(fx.plainStructT, []types.StructField{
		{Name: interner.Strings.Intern(resourceOpaqueField), Type: i64},
	})

	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{i64, fx.task, fx.channel, fx.rangeHandle, fx.plainStructT})
	if err != nil {
		t.Fatalf("freezing the fixture layouts must succeed: %v", err)
	}
	fx.machine = New(&mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}}, nil, nil, interner, nil)
	// An activation to build composites in, so an ordinary struct's default is
	// really built rather than refused for want of storage.
	fx.machine.Stack = []*Frame{fx.machine.activate(&mir.Func{Blocks: []mir.Block{{}}})}
	return fx
}

func TestDefaultOfARuntimeHandleIsTheNullHandle(t *testing.T) {
	fx := newNullHandleFixture(t)
	rows := []struct {
		name string
		typ  types.TypeID
		kind ValueKind
	}{
		{"Task", fx.task, VKResource},
		{"Channel", fx.channel, VKResource},
		{"Range", fx.rangeHandle, VKHandleRange},
		{"alias of Task", fx.taskAlias, VKResource},
		{"own Task", fx.ownedTask, VKResource},
	}
	for _, row := range rows {
		before := fx.machine.heapCounters
		value, vmErr := fx.machine.defaultValue(row.typ)
		if vmErr != nil {
			// This is the defect: the struct case built the private member and
			// the storage walk refused it ("has 1 members but 0 layout offsets").
			t.Fatalf("the default of %s must be the null handle, got an error: %v", row.name, vmErr)
		}
		if value.Kind != row.kind || value.H != 0 || value.TypeID != row.typ {
			t.Fatalf("the default of %s is %s H=%d type#%d, want %s H=0 type#%d",
				row.name, value.Kind, value.H, value.TypeID, row.kind, row.typ)
		}
		if fx.machine.heapCounters != before {
			t.Fatalf("the default of %s allocated: counters %+v became %+v; the null names no object",
				row.name, before, fx.machine.heapCounters)
		}
	}
}

func TestAnUnmarkedLookalikeIsNotANullHandle(t *testing.T) {
	fx := newNullHandleFixture(t)
	value, vmErr := fx.machine.defaultValue(fx.plainStructT)
	if vmErr != nil {
		t.Fatalf("an ordinary one-member struct must default member by member: %v", vmErr)
	}
	if value.Kind != VKComposite {
		t.Fatalf("a user struct shaped like a handle defaulted to %s; only the marked families are handles", value.Kind)
	}
	fx.machine.dropValue(value)
}

// Everything a program can do to a null without using it: drop it, copy it,
// store it. The native runtime does nothing for each (rt_task_handle_drop,
// rt_channel_handle_drop and rt_channel_handle_retain all return on NULL), and
// neither may the VM.
func TestNullRuntimeHandleIsInertOnEveryLifetimePath(t *testing.T) {
	fx := newNullHandleFixture(t)
	for _, typ := range []types.TypeID{fx.task, fx.channel, fx.rangeHandle} {
		null, vmErr := fx.machine.defaultValue(typ)
		if vmErr != nil {
			t.Fatalf("default of type#%d: %v", typ, vmErr)
		}
		before := fx.machine.heapCounters
		fx.machine.dropValue(null)
		shared, vmErr := fx.machine.cloneForShare(null)
		if vmErr != nil || shared != null {
			t.Fatalf("copying the null of type#%d gave %+v (%v), want the same null", typ, shared, vmErr)
		}
		duplicated, vmErr := fx.machine.duplicateValue(null)
		if vmErr != nil || duplicated != null {
			t.Fatalf("duplicating the null of type#%d gave %+v (%v), want the same null", typ, duplicated, vmErr)
		}
		fx.machine.dropValue(shared)
		if fx.machine.heapCounters != before {
			t.Fatalf("the null of type#%d touched the heap: counters %+v became %+v", typ, before, fx.machine.heapCounters)
		}
		// A handle cell stores the null as zero, and zero is what a cell that
		// was never written holds.
		if bits, err := handleBits(null); err != nil || bits != 0 {
			t.Fatalf("the null of type#%d encodes as %d (%v), want 0", typ, bits, err)
		}
	}
}

// A null that is USED is refused, in both of its spellings -- the typed null a
// default builds, and the `nothing` a zeroed handle cell decodes to -- and in
// the words the native runtime's handle lookup uses.
func TestNullRuntimeHandleIsRefusedWithTheNativeWords(t *testing.T) {
	fx := newNullHandleFixture(t)
	nullTask, _ := fx.machine.defaultValue(fx.task)
	nullChannel, _ := fx.machine.defaultValue(fx.channel)
	for _, spelling := range []Value{nullTask, MakeNothing()} {
		_, vmErr := fx.machine.taskIDFromValue(spelling)
		if vmErr == nil || vmErr.Code != PanicInvalidHandle || vmErr.Message != "invalid task handle" {
			t.Fatalf("a null task (%s) must be refused as the native runtime refuses it, got %v", spelling.Kind, vmErr)
		}
	}
	for _, spelling := range []Value{nullChannel, MakeNothing()} {
		_, vmErr := fx.machine.channelIDFromValue(spelling)
		if vmErr == nil || vmErr.Code != PanicInvalidHandle || vmErr.Message != "async: null channel handle" {
			t.Fatalf("a null channel (%s) must be refused as the native runtime refuses it, got %v", spelling.Kind, vmErr)
		}
	}
}
