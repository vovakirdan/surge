//go:build !golden
// +build !golden

package vm_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
	"surge/internal/vm"
)

// The VM lane's layer-2 gate: a type-aware structural check that every value
// the ordinary-storage corpus creates selects exact layout-owned storage.
//
// Layer 1 is the carrier gate, which reads source shape. Layer 3 is behaviour —
// the corpus running and the memory checkers agreeing. This is the layer
// between: it walks a COMPILED module and asks, of every statically known
// composite slot and of every call signature, which storage the compiler
// selected. A representation that still reached for a universal cell would pass
// layer 1 once the token was renamed away, and might pass layer 3 because the
// answers still come out right; it fails here, because the question is what the
// storage IS rather than what it does.
//
// The corpus is the input on purpose. These are the same fifteen sources both
// backend lanes drive, so the gate measures the program the epic is about
// rather than a shape invented to satisfy it.
//
// The gate asserts two things, and the second is the one that took a cutover to
// become askable.
//
// The storage DECISION: every composite slot in the corpus owns a layout-sized
// extent, at an offset its own alignment divides, sharing no byte with another
// slot, and every call signature classifies every argument and its result.
//
// And the ABSENCE of the alternative. A gate that only checks the arena is
// present is satisfied by a representation that has an arena AND a box, which is
// exactly the half-migrated state this epic forbids — so presence is the weaker
// half and was never the point. Until the cutover the absence was unaskable,
// because the box was still there; now it is asked three ways. No value kind
// names a boxed composite. No heap object carries a member list a composite
// could live in. And every composite the corpus actually creates, at every step
// of its execution, is carried as storage whose generation is still the one its
// arena is on — which is the runtime form of the same claim, and the only one of
// the three that a stale reference could fail.

// storageSlot is one slot as the gate sees it, reduced to the facts the
// structural rules are about.
type storageSlot struct {
	index     int
	composite bool
	offset    uint64
	size      uint64
	align     uint64
}

// checkStoragePlan states the three properties a layout-owned slot must have.
//
// Disjointness is the one that separates real storage from a renamed box: two
// slots served by a single indirection cell would both "have an offset", and
// only two extents that share no byte are two independent values.
func checkStoragePlan(owner string, planSize uint64, slots []storageSlot) error {
	extents := make([]storageSlot, 0, len(slots))
	for _, slot := range slots {
		if !slot.composite {
			if slot.offset != vm.NoStorageOffset {
				return fmt.Errorf("%s slot %d is not a value composite yet owns arena bytes at %d",
					owner, slot.index, slot.offset)
			}
			continue
		}
		if slot.offset == vm.NoStorageOffset {
			return fmt.Errorf("%s slot %d is a value composite and owns no layout storage", owner, slot.index)
		}
		if slot.align == 0 || slot.offset%slot.align != 0 {
			return fmt.Errorf("%s slot %d sits at %d, which its own %d-byte alignment does not divide",
				owner, slot.index, slot.offset, slot.align)
		}
		if slot.offset+slot.size > planSize {
			return fmt.Errorf("%s slot %d spans [%d,+%d) past the %d bytes its owner reserved",
				owner, slot.index, slot.offset, slot.size, planSize)
		}
		if slot.size > 0 {
			extents = append(extents, slot)
		}
	}

	sort.Slice(extents, func(i, j int) bool { return extents[i].offset < extents[j].offset })
	for i := 1; i < len(extents); i++ {
		previous := extents[i-1]
		current := extents[i]
		if current.offset < previous.offset+previous.size {
			return fmt.Errorf("%s slots %d and %d share bytes: [%d,%d) overlaps [%d,%d)",
				owner, previous.index, current.index,
				previous.offset, previous.offset+previous.size,
				current.offset, current.offset+current.size)
		}
	}
	return nil
}

// checkLiveComposite states what a composite must BE while a program holds one.
//
// The plan check above proves an offset was assigned. This proves the value at
// that offset is carried as the storage itself: not a handle to something, and
// not a reference into an arena that has moved on. The generation is the part
// that only a running program can be wrong about — a plan cannot go stale, and a
// value can.
func checkLiveComposite(owner string, index int, value vm.Value) error {
	ref, isComposite := value.Storage()
	if !isComposite {
		return fmt.Errorf("%s slot %d holds a composite carried as %s, not as its storage",
			owner, index, value.Kind)
	}
	if ref.Arena == nil {
		return fmt.Errorf("%s slot %d is a composite that names no arena", owner, index)
	}
	if ref.Gen != ref.Arena.Generation() {
		return fmt.Errorf("%s slot %d names generation %d of an arena now at %d",
			owner, index, ref.Gen, ref.Arena.Generation())
	}
	if ref.Align == 0 || ref.Offset%ref.Align != 0 {
		return fmt.Errorf("%s slot %d sits at %d, which its own %d-byte alignment does not divide",
			owner, index, ref.Offset, ref.Align)
	}
	return nil
}

// TestRuntimeV2VMOrdinaryStorageCarriesNoBoxedRepresentation is the absence
// half, asked of the types rather than of a program.
//
// A program can only show that the box was not used on the paths it took. These
// two assertions show there is nothing to use: a kind that named a boxed
// composite would have to exist for one to be built, and a member list on the
// heap object would have to exist for one to be stored.
func TestRuntimeV2VMOrdinaryStorageCarriesNoBoxedRepresentation(t *testing.T) {
	// No value kind names a boxed composite. The kinds are a small dense enum,
	// so walking past the end costs nothing and catches one appended later.
	composites := 0
	for raw := range 256 {
		switch name := vm.ValueKind(raw).String(); name { //nolint:gosec // bounded by the loop
		case "struct", "tag":
			t.Fatalf("value kind %d is named %q, so a composite can still be carried as a box", raw, name)
		case "composite":
			composites++
		}
	}
	if composites != 1 {
		t.Fatalf("%d value kinds render as \"composite\", want exactly one", composites)
	}

	// No heap object carries a member list a composite could live in. Named
	// exactly, because the point is not that these particular fields are gone
	// but that nothing on the object holds Values for a composite to be spread
	// across.
	object := reflect.TypeOf(vm.Object{})
	for i := range object.NumField() {
		field := object.Field(i)
		switch field.Name {
		case "Fields", "Tag":
			t.Fatalf("Object.%s survives, so a composite still has a box to live in", field.Name)
		}
		if strings.Contains(field.Type.String(), "TagObject") {
			t.Fatalf("Object.%s is a %s, which is the tagged-union box", field.Name, field.Type)
		}
	}
}

func TestRuntimeV2VMOrdinaryStorageSelectsLayoutOwnedStorage(t *testing.T) {
	if testing.Short() {
		t.Skip("the layer-2 gate compiles the whole corpus")
	}

	composites, signatures := 0, 0
	for _, fixture := range ordinaryStorageFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			module, files, interner := compileToMIR(t, filepath.Join(repoRoot(t), filepath.FromSlash(fixture.relPath())))
			if module.Meta == nil || module.Meta.Layouts == nil {
				t.Fatal("a finalized module must carry its layout registry")
			}
			machine := vm.New(module, nil, files, interner, nil)

			globals := machine.GlobalStorage()
			globalTypes := make([]types.TypeID, len(module.Globals))
			for i := range module.Globals {
				globalTypes[i] = module.Globals[i].Type
			}
			found := auditSlots(t, "globals", machine, interner, &globals, globalTypes)

			for _, fn := range module.Funcs {
				if fn == nil {
					continue
				}
				slotTypes := make([]types.TypeID, len(fn.Locals))
				for i := range fn.Locals {
					slotTypes[i] = fn.Locals[i].Type
				}
				plan := machine.FunctionStorage(fn)
				found += auditSlots(t, fn.Name, machine, interner, &plan, slotTypes)
				auditSignature(t, fn, module, interner)
				signatures++
			}

			if found == 0 {
				t.Fatalf("%s creates no composite values, so it is not evidence for this gate", fixture.name)
			}
			composites += found

			live := auditRunningComposites(t, module, files, interner)
			if live == 0 {
				t.Fatalf("%s held no composite while running, so the runtime half saw nothing", fixture.name)
			}
		})
	}

	if composites == 0 {
		t.Fatal("the corpus produced no composite slots at all, which means the walk found nothing")
	}
	t.Logf("layer 2: %d composite slots and %d call signatures, all layout-owned", composites, signatures)
}

// auditRunningComposites RUNS the program and checks every composite any
// activation holds, at every step, returning how many it inspected.
//
// It steps rather than running to completion because the invariant is about
// values while they are live: a composite that goes stale is wrong at the moment
// it is read, and a program that has finished is holding nothing. The step
// budget bounds a corpus row that loops; a row that stops early for any reason
// still contributes every step it took, and stopping is not a failure here —
// this gate is about what the storage IS, and the corpus rows prove that they
// RUN somewhere else.
func auditRunningComposites(t *testing.T, module *mir.Module, files *source.FileSet, interner *types.Interner) int {
	t.Helper()
	const stepBudget = 20000

	machine := vm.New(module, vm.NewTestRuntime(nil, ""), files, interner, nil)
	if vmErr := machine.Start(); vmErr != nil {
		t.Fatalf("the corpus row must start: %v", vmErr)
	}
	seen := 0
	for range stepBudget {
		for _, frame := range machine.Stack {
			if frame == nil {
				continue
			}
			for index := range frame.Locals {
				slot := &frame.Locals[index]
				if !slot.IsInit || slot.IsMoved || slot.IsDropped {
					continue
				}
				if _, isComposite := slot.V.Storage(); !isComposite {
					continue
				}
				if err := checkLiveComposite(frame.Func.Name, index, slot.V); err != nil {
					t.Fatal(err)
				}
				seen++
			}
		}
		if machine.Halted {
			break
		}
		if vmErr := machine.Step(); vmErr != nil {
			break
		}
	}
	return seen
}

// auditSlots reduces one plan to storage facts taken from the registry and
// hands them to the rules, returning how many composites it saw.
func auditSlots(
	t *testing.T,
	owner string,
	machine *vm.VM,
	interner *types.Interner,
	plan *vm.StoragePlan,
	slotTypes []types.TypeID,
) int {
	t.Helper()
	slots := make([]storageSlot, 0, len(slotTypes))
	composites := 0
	for index, slotType := range slotTypes {
		slot := storageSlot{index: index, offset: plan.OffsetOf(index), composite: interner.IsValueComposite(slotType)}
		if slot.composite {
			composites++
			facts, err := machine.Layouts.Require(slotType)
			if err != nil {
				t.Fatalf("%s slot %d holds type#%d, which has no usable layout: %v", owner, index, slotType, err)
			}
			slot.size = facts.Size
			slot.align = facts.Align
		}
		slots = append(slots, slot)
	}
	if err := checkStoragePlan(owner, plan.Size, slots); err != nil {
		t.Fatal(err)
	}
	return composites
}

// auditSignature proves the call contract classifies every argument and the
// result, and that a composite travels through a destination rather than as a
// value of some universal kind.
//
// The classification is asked for by SIGNATURE, which is the point of the
// contract being a pure function of the callee's type: a direct call and an
// indirect call to one callee reach this same answer, so proving it once per
// signature proves it for both.
func auditSignature(t *testing.T, fn *mir.Func, module *mir.Module, interner *types.Interner) {
	t.Helper()
	params := make([]types.TypeID, 0, fn.ParamCount)
	for i := 0; i < fn.ParamCount && i < len(fn.Locals); i++ {
		params = append(params, fn.Locals[i].Type)
	}
	contract, err := module.Meta.CallLayouts.OfSignature(params, fn.Result, mir.ABIDomainSurge)
	if err != nil {
		t.Fatalf("%s has no call classification: %v", fn.Name, err)
	}
	abi, err := contract.Surge()
	if err != nil {
		t.Fatalf("%s is generated and must be governed by the generated contract: %v", fn.Name, err)
	}

	for i, param := range abi.Params {
		if param.Class == mir.ParamGoverned {
			t.Fatalf("%s argument %d was never classified", fn.Name, i)
		}
		if !interner.IsValueComposite(params[i]) {
			continue
		}
		want := mir.ParamByval
		if param.Size == 0 {
			want = mir.ParamElidedZST
		}
		if param.Class != want {
			t.Fatalf("%s argument %d is composite type#%d of %d bytes and travels as %s, want %s",
				fn.Name, i, params[i], param.Size, param.Class, want)
		}
		if want == mir.ParamByval && param.Align == 0 {
			t.Fatalf("%s argument %d travels by address with no stated alignment", fn.Name, i)
		}
	}

	if abi.Ret.Class == mir.RetGoverned {
		t.Fatalf("%s result was never classified", fn.Name)
	}
	if fn.Result != types.NoTypeID && interner.IsValueComposite(fn.Result) {
		want := mir.RetSret
		if abi.Ret.Size == 0 {
			want = mir.RetVoid
		}
		if abi.Ret.Class != want {
			t.Fatalf("%s returns composite type#%d of %d bytes as %s, want %s",
				fn.Name, fn.Result, abi.Ret.Size, abi.Ret.Class, want)
		}
	}
}

// TestRuntimeV2VMOrdinaryStorageGateRejectsStorageItShouldReject is the gate's
// own negative control. A gate that has only ever passed proves nothing about
// the thing it watches, so this hands the same rules the four shapes a broken
// representation would produce and requires each to be named.
func TestRuntimeV2VMOrdinaryStorageGateRejectsStorageItShouldReject(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  uint64
		slots []storageSlot
		says  string
	}{
		{
			name: "two composites sharing one cell",
			size: 16,
			slots: []storageSlot{
				{index: 0, composite: true, offset: 0, size: 16, align: 8},
				{index: 1, composite: true, offset: 0, size: 16, align: 8},
			},
			says: "share bytes",
		},
		{
			name: "a composite with no storage of its own",
			size: 16,
			slots: []storageSlot{
				{index: 0, composite: true, offset: vm.NoStorageOffset, size: 16, align: 8},
			},
			says: "owns no layout storage",
		},
		{
			name: "a composite at an offset its alignment does not divide",
			size: 32,
			slots: []storageSlot{
				{index: 0, composite: true, offset: 4, size: 16, align: 8},
			},
			says: "alignment does not divide",
		},
		{
			name: "a composite running past its owner",
			size: 8,
			slots: []storageSlot{
				{index: 0, composite: true, offset: 0, size: 16, align: 8},
			},
			says: "past the 8 bytes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkStoragePlan("control", tc.size, tc.slots)
			if err == nil {
				t.Fatal("the gate accepted storage it must reject")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("the refusal must name what is wrong; %q lacks %q", err, tc.says)
			}
		})
	}

	if err := checkStoragePlan("control", 16, []storageSlot{
		{index: 0, composite: true, offset: 0, size: 8, align: 8},
		{index: 1, composite: true, offset: 8, size: 8, align: 8},
		{index: 2, offset: vm.NoStorageOffset},
	}); err != nil {
		t.Fatalf("the gate must accept two disjoint, aligned extents beside a scalar: %v", err)
	}
}

// TestRuntimeV2VMOrdinaryStorageGateRejectsCompositesItShouldReject is the same
// control for the runtime rule, and the first case is the one that matters: a
// composite carried as a handle is the box coming back, and a gate that could
// not name it would be asserting nothing.
func TestRuntimeV2VMOrdinaryStorageGateRejectsCompositesItShouldReject(t *testing.T) {
	// An arena that has never been rewound is on generation 0, so a reference
	// claiming any other generation is stale by construction.
	arena := &vm.Arena{}

	for _, tc := range []struct {
		name  string
		value vm.Value
		says  string
	}{
		{
			name:  "a composite carried as a handle",
			value: vm.MakeHandleString(1, types.NoTypeID),
			says:  "not as its storage",
		},
		{
			name:  "a composite naming no arena",
			value: vm.MakeComposite(vm.StorageRef{Offset: 0, Align: 8}),
			says:  "names no arena",
		},
		{
			name:  "a reference into an arena that has moved on",
			value: vm.MakeComposite(vm.StorageRef{Arena: arena, Gen: 1, Offset: 0, Align: 8}),
			says:  "names generation 1 of an arena now at 0",
		},
		{
			name:  "a composite at an offset its alignment does not divide",
			value: vm.MakeComposite(vm.StorageRef{Arena: arena, Offset: 4, Align: 8}),
			says:  "alignment does not divide",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkLiveComposite("control", 0, tc.value)
			if err == nil {
				t.Fatal("the gate accepted a composite it must reject")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("the refusal must name what is wrong; %q lacks %q", err, tc.says)
			}
		})
	}

	if err := checkLiveComposite("control", 0,
		vm.MakeComposite(vm.StorageRef{Arena: arena, Offset: 8, Align: 8})); err != nil {
		t.Fatalf("the gate must accept a live, aligned composite: %v", err)
	}
}
