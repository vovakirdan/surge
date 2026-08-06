//go:build !golden
// +build !golden

package vm_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"surge/internal/mir"
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
// What this proves today is the storage DECISION: every composite slot in the
// corpus owns a layout-sized extent, at an offset its own alignment divides,
// sharing no byte with another slot, and every call signature classifies every
// argument and its result. The other half of the layer-2 wording — that no slot
// is ALSO reachable as a universal value — is a claim about a field that still
// exists, and it becomes checkable here the moment the cutover removes it. That
// assertion belongs to the cutover commit, and this comment is where to add it.

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
		})
	}

	if composites == 0 {
		t.Fatal("the corpus produced no composite slots at all, which means the walk found nothing")
	}
	t.Logf("layer 2: %d composite slots and %d call signatures, all layout-owned", composites, signatures)
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
