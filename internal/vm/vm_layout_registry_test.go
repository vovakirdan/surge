package vm

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestTypedHeapAccountingFailsClosedOnInvalidLayoutState(t *testing.T) {
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	typed := typesIn.RegisterStruct(typesIn.Strings.Intern("Typed"), source.Span{})
	typesIn.SetStructFields(typed, []types.StructField{{Type: typesIn.Builtins().Int32}})

	for _, kind := range []ObjectKind{OKArray, OKMap, OKRange, OKResource} {
		t.Run(fmt.Sprintf("kind_%d", kind), func(t *testing.T) {
			instance := New(&mir.Module{Meta: &mir.ModuleMeta{}}, NewTestRuntime(nil, ""), nil, typesIn, nil)
			instance.Heap.alloc(kind, typed)
			if _, err := instance.heapStatsSnapshot(); err == nil || !strings.Contains(err.Error(), "finalized layout registry") {
				t.Fatalf("heap stats error = %v", err)
			}
			if _, err := instance.heapDumpString(); err == nil || !strings.Contains(err.Error(), "finalized layout registry") {
				t.Fatalf("heap dump error = %v", err)
			}
		})
	}

	registry, err := layout.FinalizeRegistry(
		layout.New(layout.X86_64LinuxGNU(), typesIn),
		[]types.TypeID{typesIn.Builtins().Bool},
	)
	if err != nil {
		t.Fatal(err)
	}
	instance := New(&mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}}, NewTestRuntime(nil, ""), nil, typesIn, nil)
	instance.Heap.alloc(OKRange, typed)
	if _, err := instance.heapStatsSnapshot(); err == nil || !strings.Contains(err.Error(), "missing finalized registry entry") {
		t.Fatalf("missing-entry error = %v", err)
	}
	if _, err := instance.heapDumpString(); err == nil || !strings.Contains(err.Error(), "missing finalized registry entry") {
		t.Fatalf("missing-entry dump error = %v", err)
	}
}

func TestHeapDebugIntrinsicsReturnDeterministicInternalErrors(t *testing.T) {
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	typed := typesIn.RegisterStruct(typesIn.Strings.Intern("Typed"), source.Span{})
	typesIn.SetStructFields(typed, []types.StructField{{Type: typesIn.Builtins().Int32}})
	statsType := typesIn.RegisterStruct(typesIn.Strings.Intern("HeapStats"), source.Span{})
	statsFields := []string{"alloc_count", "free_count", "live_blocks", "live_bytes", "rc_increments", "rc_decrements"}
	fields := make([]types.StructField, len(statsFields))
	for i, name := range statsFields {
		fields[i] = types.StructField{Name: typesIn.Strings.Intern(name), Type: typesIn.Builtins().Uint}
	}
	typesIn.SetStructFields(statsType, fields)
	entryOnlyRegistry, err := layout.FinalizeRegistry(
		layout.New(layout.X86_64LinuxGNU(), typesIn),
		[]types.TypeID{typesIn.Builtins().Bool},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name     string
		registry *layout.Registry
		cause    string
	}{
		{name: "missing registry", cause: "finalized layout registry"},
		{name: "missing exact entry", registry: entryOnlyRegistry, cause: "missing finalized registry entry"},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := New(
				&mir.Module{Meta: &mir.ModuleMeta{Layouts: test.registry}},
				NewTestRuntime(nil, ""), nil, typesIn, nil,
			)
			instance.Heap.alloc(OKRange, typed)
			statsFrame := NewFrame(&mir.Func{Locals: []mir.Local{{Type: statsType}}})
			dumpFrame := NewFrame(&mir.Func{Locals: []mir.Local{{Type: typesIn.Builtins().String}}})
			call := &mir.CallInstr{HasDst: true, Dst: mir.Place{Kind: mir.PlaceLocal, Local: 0}}
			var writes []LocalWrite
			statsErr := instance.handleHeapStats(statsFrame, call, &writes)
			if statsErr == nil || statsErr.Code != PanicUnimplemented ||
				!strings.Contains(statsErr.Message, "rt_heap_stats internal error") ||
				!strings.Contains(statsErr.Message, test.cause) {
				t.Fatalf("heap stats VM error = %v", statsErr)
			}
			dumpErr := instance.handleHeapDump(dumpFrame, call, &writes)
			if dumpErr == nil || dumpErr.Code != PanicUnimplemented ||
				!strings.Contains(dumpErr.Message, "rt_heap_dump internal error") ||
				!strings.Contains(dumpErr.Message, test.cause) {
				t.Fatalf("heap dump VM error = %v", dumpErr)
			}
		})
	}
}

func TestHeapAccountingAcceptsTrueZSTAndRejectsOverflow(t *testing.T) {
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	zst := typesIn.RegisterStruct(typesIn.Strings.Intern("Empty"), source.Span{})
	registry, err := layout.FinalizeRegistry(layout.New(layout.X86_64LinuxGNU(), typesIn), []types.TypeID{zst})
	if err != nil {
		t.Fatal(err)
	}
	instance := New(&mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}}, NewTestRuntime(nil, ""), nil, typesIn, nil)
	instance.Heap.alloc(OKRange, zst)
	snapshot, err := instance.heapStatsSnapshot()
	if err != nil || snapshot.liveBlocks != 1 || snapshot.liveBytes != 0 {
		t.Fatalf("ZST snapshot = %+v, %v", snapshot, err)
	}
	dump, err := instance.heapDumpString()
	if err != nil || !strings.Contains(dump, "size=0") {
		t.Fatalf("ZST dump = %q, %v", dump, err)
	}
	if _, err := checkedHeapAdd(math.MaxUint64, 1, "live byte total"); err == nil {
		t.Fatal("live byte addition overflow was accepted")
	}
	if _, err := checkedHeapMul(math.MaxUint64, 2, "array length * element size"); err == nil {
		t.Fatal("element-size multiplication overflow was accepted")
	}
}

func TestVMSizeOfFailsClosedWithoutFinalizedRegistryOrExactEntry(t *testing.T) {
	typesIn := types.NewInterner()
	const callee = symbols.SymbolID(1)
	call := &mir.CallInstr{
		Callee: mir.Callee{Kind: mir.CalleeSym, Sym: callee},
		Dst:    mir.Place{Local: 0},
		HasDst: true,
	}
	frame := NewFrame(&mir.Func{Locals: []mir.Local{{Type: typesIn.Builtins().Uint}}})

	t.Run("missing registry", func(t *testing.T) {
		mod := &mir.Module{Meta: &mir.ModuleMeta{
			FuncTypeArgs: map[symbols.SymbolID][]types.TypeID{callee: {typesIn.Builtins().Int32}},
		}}
		instance := New(mod, NewTestRuntime(nil, ""), nil, typesIn, nil)
		err := instance.handleSizeOfAlignOf(frame, call, nil, "size_of")
		if err == nil || !strings.Contains(err.Message, "missing finalized layout registry") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing exact entry", func(t *testing.T) {
		registry, registryErr := layout.FinalizeRegistry(
			layout.New(layout.X86_64LinuxGNU(), typesIn),
			[]types.TypeID{typesIn.Builtins().Bool},
		)
		if registryErr != nil {
			t.Fatal(registryErr)
		}
		mod := &mir.Module{Meta: &mir.ModuleMeta{
			Layouts:      registry,
			FuncTypeArgs: map[symbols.SymbolID][]types.TypeID{callee: {typesIn.Builtins().Int32}},
		}}
		instance := New(mod, NewTestRuntime(nil, ""), nil, typesIn, nil)
		vmErr := instance.handleSizeOfAlignOf(frame, call, nil, "size_of")
		if vmErr == nil || !strings.Contains(vmErr.Message, "missing finalized registry entry") {
			t.Fatalf("error = %v", vmErr)
		}
	})
}
