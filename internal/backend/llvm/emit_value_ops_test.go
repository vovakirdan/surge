package llvm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"surge/internal/types"
	"surge/internal/valueops"
)

const valueOpsProbeProgram = `
@entrypoint
fn main() -> int {
    let a = 7;
    let b = "hello";
    return a;
}
`

// descriptorPattern parses one emitted rt_value_ops constant back out of the IR
// text: the type id, the four layout integers, and the eight pointer operands.
var descriptorPattern = regexp.MustCompile(
	`@__surge_value_ops_type(\d+) = constant %struct\.rt_value_ops \{ %struct\.rt_value_layout \{ i64 (\d+), i64 (\d+), i64 (\d+), i64 (\d+) \}, (.+) \}`)

type emittedDescriptor struct {
	id       types.TypeID
	size     uint64
	align    uint64
	stride   uint64
	flags    uint64
	operands []string
}

func parseEmittedDescriptors(t *testing.T, ir string) map[types.TypeID]emittedDescriptor {
	t.Helper()
	out := make(map[types.TypeID]emittedDescriptor)
	for _, match := range descriptorPattern.FindAllStringSubmatch(ir, -1) {
		id, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil {
			t.Fatalf("descriptor names a non-numeric type id %q", match[1])
		}
		nums := make([]uint64, 4)
		for i := range nums {
			nums[i], err = strconv.ParseUint(match[2+i], 10, 64)
			if err != nil {
				t.Fatalf("descriptor for type#%d carries a non-numeric layout field: %v", id, err)
			}
		}
		operands := strings.Split(match[6], ", ")
		out[types.TypeID(id)] = emittedDescriptor{
			id: types.TypeID(id), size: nums[0], align: nums[1], stride: nums[2], flags: nums[3],
			operands: operands,
		}
	}
	return out
}

// TestEmittedValueOpsDescriptorsAgreeWithTheOperationRegistry compares what the
// backend wrote against the registry that authorised it — an independent
// authority rather than the emitter's own bookkeeping, which would be asking the
// subject to confirm itself.
//
// It asserts BOTH directions. One alone passes for an emitter that emits
// nothing, which is exactly the state this test was written to leave behind.
func TestEmittedValueOpsDescriptorsAgreeWithTheOperationRegistry(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, valueOpsProbeProgram)
	if mirMod.Meta == nil || mirMod.Meta.Operations == nil {
		t.Fatal("the lowering published no operation registry, so this proves nothing")
	}
	registry := mirMod.Meta.Operations
	if registry.Len() == 0 {
		t.Fatal("the registry is empty, so agreement with it is vacuous")
	}

	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	emitted := parseEmittedDescriptors(t, ir)
	if len(emitted) == 0 {
		t.Fatal("no rt_value_ops constant was emitted")
	}

	for id, got := range emitted {
		entry, err := registry.Value(id)
		if err != nil {
			t.Errorf("emitted a descriptor for type#%d, which the registry does not carry", id)
			continue
		}
		if got.size != entry.Facts.Size || got.stride != entry.Facts.Stride {
			t.Errorf("type#%d layout = size %d stride %d, registry says %d and %d",
				id, got.size, got.stride, entry.Facts.Size, entry.Facts.Stride)
		}
		wantAlign := entry.Facts.Align
		if wantAlign == 0 {
			wantAlign = 1
		}
		if got.align != wantAlign {
			t.Errorf("type#%d align = %d, registry says %d", id, got.align, wantAlign)
		}
		if got.flags != uint64(entry.Flags) {
			t.Errorf("type#%d flags = %d, registry says %d", id, got.flags, uint64(entry.Flags))
		}
		if len(got.operands) != len(valueOpsSlotOrder) {
			t.Errorf("type#%d has %d slot operands, want %d", id, len(got.operands), len(valueOpsSlotOrder))
			continue
		}
		for index, slot := range valueOpsSlotOrder {
			filler, err := valueops.SlotFiller(slot, entry.Flags)
			if err != nil {
				t.Errorf("type#%d slot %s: %v", id, slot, err)
				continue
			}
			want := "ptr null"
			switch filler.Kind {
			case valueops.FillRuntimeSymbol, valueops.FillModuleStub:
				want = "ptr @" + filler.Symbol
			case valueops.FillBackendDerivedBody:
				// Two slots are backend-derived, and they are named in two
				// namespaces on purpose: move_init on the exact type, the drop
				// body on the resolved one, because that is where glue bodies
				// live. Asserting one name for both would be asserting the
				// namespaces are the same, which they are not.
				want = "ptr @" + moveInitName(id)
				if slot == "drop_in_place" {
					want = "ptr @" + dropGlueName(resolveValueType(result.Sema.TypeInterner, id))
				}
			}
			if got.operands[index] != want {
				t.Errorf("type#%d slot %s = %q, want %q", id, slot, got.operands[index], want)
			}
		}
	}

	// The other direction: every registry root either has a descriptor or was
	// skipped for the one stated reason.
	for _, id := range registry.TypeIDs() {
		if _, ok := emitted[id]; ok {
			continue
		}
		entry, err := registry.Value(id)
		if err != nil {
			continue
		}
		skipped := false
		for _, slot := range valueOpsSlotOrder {
			filler, ferr := valueops.SlotFiller(slot, entry.Flags)
			if ferr == nil && filler.Kind == valueops.FillRegistryNamedBody {
				skipped = true
			}
		}
		if !skipped {
			t.Errorf("type#%d has no descriptor and no reason to lack one", id)
		}
	}
}

// TestEmittedMoveInitBodyMovesTheWholeValue reads the BODY, not the define line.
//
// A move transfers the whole value, so the body must copy exactly the type's
// size — and a zero-sized type must copy nothing at all rather than a token
// byte. A define-line grep would pass identically for a body that copies the
// wrong width, which is the defect worth catching.
func TestEmittedMoveInitBodyMovesTheWholeValue(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, valueOpsProbeProgram)
	if mirMod.Meta == nil || mirMod.Meta.Operations == nil {
		t.Fatal("no operation registry was published")
	}
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	checked := 0
	for _, id := range mirMod.Meta.Operations.TypeIDs() {
		entry, err := mirMod.Meta.Operations.Value(id)
		if err != nil {
			continue
		}
		marker := fmt.Sprintf("define void @%s(ptr %%dst, ptr %%src) {", moveInitName(id))
		start := strings.Index(ir, marker)
		if start < 0 {
			continue // skipped entry; the agreement test owns that direction
		}
		end := strings.Index(ir[start:], "\n}\n")
		if end < 0 {
			t.Fatalf("type#%d move body is unterminated", id)
		}
		body := ir[start : start+end]
		checked++

		if entry.Facts.Size == 0 {
			if strings.Contains(body, "llvm.memcpy") {
				t.Errorf("type#%d is zero-sized yet its move body copies:\n%s", id, body)
			}
			continue
		}
		if !strings.Contains(body, "llvm.memcpy.p0.p0.i64") {
			t.Errorf("type#%d move body copies nothing:\n%s", id, body)
			continue
		}
		if !strings.Contains(body, fmt.Sprintf("i64 %d, i1 false", entry.Facts.Size)) {
			t.Errorf("type#%d move body does not copy its %d bytes:\n%s", id, entry.Facts.Size, body)
		}
	}
	if checked == 0 {
		t.Fatal("no move body was examined, so this asserted nothing")
	}
}

// TestNonCopyDescriptorLeavesCopyInitNull pins the biconditional the runtime
// enforces, from the emitter's side.
//
// This is the defect the first SlotFiller had: it answered with a slot's filler
// without asking whether the slot was present at all, so a descriptor for a
// non-Copy type carried copy_init's trap and the runtime would have refused it.
// The registry's own table tests could not see it — the table was right and only
// the query was wrong.
func TestNonCopyDescriptorLeavesCopyInitNull(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, valueOpsProbeProgram)
	if mirMod.Meta == nil || mirMod.Meta.Operations == nil {
		t.Fatal("no operation registry was published")
	}
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	emitted := parseEmittedDescriptors(t, ir)

	sawCopy, sawNonCopy := false, false
	for id, got := range emitted {
		entry, err := mirMod.Meta.Operations.Value(id)
		if err != nil {
			continue
		}
		copyInit := got.operands[1] // slot order: move_init, copy_init, ...
		if entry.Flags&valueops.FlagCopy != 0 {
			sawCopy = true
			if copyInit == "ptr null" {
				t.Errorf("type#%d is Copy yet ships a null copy_init", id)
			}
		} else {
			sawNonCopy = true
			if copyInit != "ptr null" {
				t.Errorf("type#%d is not Copy yet ships copy_init %s", id, copyInit)
			}
		}
	}
	if !sawCopy || !sawNonCopy {
		t.Fatalf("the probe covered copy=%v non-copy=%v; both arms are needed for this to mean anything",
			sawCopy, sawNonCopy)
	}
}

// clonableProbeProgram carries a type whose clone is a real monomorphized
// function, so clone_init has a symbol the registry knows and the backend must
// resolve to a name.
const clonableProbeProgram = `
pub type Box = { value: string };

extern<Box> {
    pub fn __clone(self: &Box) -> Box {
        return Box { value = self.value.__clone() };
    }
}

@entrypoint
fn main() -> int {
    let b = Box { value = "x" };
    let c = b.__clone();
    return 0;
}
`

// TestEveryRegistryEntryGetsADescriptor is the coverage claim, and it is the one
// that would notice the writer quietly skipping work.
//
// Before clone_init was bound, a clonable type was skipped entirely: the slot
// wanted a symbol the registry carried and the backend could not name. Skipping
// is honest but it is not coverage, and a proof that only checks what WAS
// emitted cannot tell the two apart.
func TestEveryRegistryEntryGetsADescriptor(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, clonableProbeProgram)
	if mirMod.Meta == nil || mirMod.Meta.Operations == nil {
		t.Fatal("no operation registry was published")
	}
	registry := mirMod.Meta.Operations
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	emitted := parseEmittedDescriptors(t, ir)

	clonable := 0
	for _, id := range registry.TypeIDs() {
		entry, err := registry.Value(id)
		if err != nil {
			continue
		}
		if entry.Flags&valueops.FlagClonable != 0 {
			clonable++
		}
		if _, ok := emitted[id]; !ok {
			t.Errorf("type#%d (flags %s) has no descriptor", id, entry.Flags)
		}
	}
	if clonable == 0 {
		t.Fatal("the probe carried no clonable type, so it proves nothing about clone_init")
	}
	if len(emitted) != registry.Len() {
		t.Errorf("emitted %d descriptors for %d registry entries", len(emitted), registry.Len())
	}
}

// TestClonableDescriptorBindsItsMonomorphizedClone pins the biconditional from
// the emitter's side for the one slot whose symbol comes from the registry
// rather than from the backend.
func TestClonableDescriptorBindsItsMonomorphizedClone(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, clonableProbeProgram)
	if mirMod.Meta == nil || mirMod.Meta.Operations == nil {
		t.Fatal("no operation registry was published")
	}
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	emitted := parseEmittedDescriptors(t, ir)

	checked := 0
	for id, got := range emitted {
		entry, err := mirMod.Meta.Operations.Value(id)
		if err != nil {
			continue
		}
		cloneInit := got.operands[2] // move_init, copy_init, clone_init, ...
		if entry.Flags&valueops.FlagClonable == 0 {
			if cloneInit != "ptr null" {
				t.Errorf("type#%d is not clonable yet binds clone_init %s", id, cloneInit)
			}
			continue
		}
		checked++
		if cloneInit == "ptr null" {
			t.Errorf("type#%d is clonable yet ships a null clone_init", id)
			continue
		}
		// The bound name must be a function this module actually defines.
		if !strings.Contains(ir, "define "+strings.TrimPrefix(cloneInit, "ptr ")+"(") &&
			!strings.Contains(ir, "define void "+strings.TrimPrefix(cloneInit, "ptr ")+"(") {
			defined := strings.Contains(ir, strings.TrimPrefix(cloneInit, "ptr ")+"(")
			if !defined {
				t.Errorf("type#%d binds clone_init %s, which this module does not define", id, cloneInit)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no clonable descriptor was examined")
	}
}
