package carriergate

import "testing"

func TestStructuralOwnerCensusKeepsControlAndOwnerRegionsDistinct(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/owner_regions.go": []byte(`package vm
type service interface { Run() }
type slotState uint8
type layoutSpec struct { size, align, stride uint64; flags uint32 }
type valueOps struct {
	layout layoutSpec
	moveInit func(uintptr, uintptr)
	planCross func(uintptr, uint8) uint64
}
type ownerSlot struct { state slotState; generation uint64; offset uint64 }
type ownerRegion struct {
	descriptor valueOps
	backing []byte
	slots []ownerSlot
}
type neutralWrapper struct { q *ownerRegion }
type VM struct {
	runtime service
	owners map[uint64]*neutralWrapper
}
type controlCell struct { ready bool }
type controlToken struct { cell *controlCell }
`),
		"internal/asyncrt/control_generic.go": []byte(`package asyncrt
type orderedQueue[P comparable] struct { value P }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan non-carrier controls: %v", err)
	}
	for _, finding := range findings {
		if finding.Category == categoryVMUniversalOwner || finding.Category == categoryAsyncAny {
			t.Fatalf("control or owner-specific region became a carrier: %+v", finding)
		}
	}
}

func TestStructuralOwnerCensusRequiresProvenOwnerRegionBoundary(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/untyped_owner_region.go": []byte(`package vm
type slotState uint8
type Arena struct { bytes []byte }
type ownerSlot struct { state slotState; generation uint64; offset uint64 }
type taskRegion struct {
	backing Arena
	slots []ownerSlot
}
type neutralWrapper struct { q *taskRegion }
type VM struct { owners map[uint64]*neutralWrapper }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan layout-less owner region: %v", err)
	}
	if !hasFinding(findings, categoryVMUniversalOwner, "VM.owners->general-slot(ownerSlot)") {
		t.Fatalf("layout-less named owner region hid a root general slot: %+v", findings)
	}
}

func TestStructuralOwnerCensusFollowsGenericSlotArguments(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/generic_slot_pool.go": []byte(`package vm
type slotState uint8
type ownerSlot struct { state slotState; generation uint64 }
type Wrapper[P any] struct { q P }
type VM struct { owners map[uint64]Wrapper[ownerSlot] }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan generic slot pool: %v", err)
	}
	if !hasFinding(findings, categoryVMUniversalOwner, "VM.owners->general-slot(ownerSlot)") {
		t.Fatalf("generic wrapper hid a root general slot: %+v", findings)
	}
	if hasFinding(findings, categoryVMUniversalOwner, "VM.owners->Wrapper.q->universal") {
		t.Fatalf("concrete non-carrier argument fell back to its any constraint: %+v", findings)
	}
}

func TestStructuralOwnerCensusRejectsDescriptorSpellingDecoy(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/descriptor_decoy.go": []byte(`package vm
type slotState uint8
type foreignSlot struct { state slotState; generation uint64 }
type decoyRegion struct {
	descriptor evil.Descriptor
	backing []byte
	slots []foreignSlot
}
type VM struct { owners map[uint64]*decoyRegion }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan descriptor spelling decoy: %v", err)
	}
	if !hasFinding(findings, categoryVMUniversalOwner, "VM.owners->general-slot(foreignSlot)") {
		t.Fatalf("descriptor spelling decoy hid a root general slot: %+v", findings)
	}
}

func TestStructuralOwnerCensusConservativelyTracksPointerReachability(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/borrowed_root.go": []byte(`package vm
type foreignRoot struct {
	values []Value
	control bool
}
type observer struct { root *foreignRoot }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan borrowed root: %v", err)
	}
	if !hasFinding(findings, categoryVMUniversalOwner, "foreignRoot.values->Value") {
		t.Fatalf("direct owning container was not counted: %+v", findings)
	}
	if !hasFinding(findings, categoryVMUniversalOwner, "observer.root->foreignRoot.values->Value") {
		t.Fatalf("unmarked pointer reachability hid a possible indirect owner: %+v", findings)
	}
}
