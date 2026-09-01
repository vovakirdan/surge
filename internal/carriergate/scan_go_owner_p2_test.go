package carriergate

import (
	"strings"
	"testing"
)

const canonicalD8OwnerFixture = `package vm
import "surge/internal/types"
type cellKind uint8
type storageMember struct {
	Offset uint64
	Size uint64
	Align uint64
	TypeID types.TypeID
	Kind cellKind
}
type Location struct{}
type Arena struct {
	bytes []byte
	refs []Location
	refIndex map[Location]uint64
	gen uint32
	pins uint32
}
type asyncOwnerKind uint8
type asyncOwnerID struct { kind asyncOwnerKind; id uint64; arm uint32 }
type asyncPayloadState uint8
type asyncSlotRole uint8
type asyncPayloadSlot struct {
	state asyncPayloadState
	generation uint32
	role asyncSlotRole
	parkSeq uint64
}
type asyncOwnerRegion struct {
	id asyncOwnerID
	generation uint32
	typeID types.TypeID
	cell storageMember
	stride uint64
	arena Arena
	slots []asyncPayloadSlot
	retiring bool
	destroying bool
	retired bool
}
type VM struct { owners map[uint64]*asyncOwnerRegion }
`

func TestStructuralOwnerCensusRequiresCanonicalVMAsyncOwner(t *testing.T) {
	t.Run("callback shape spoof", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/spoof_region.go": []byte(`package vm
type valueLayout struct { size, align, stride uint64; flags uint32 }
type fakeOps struct {
	layout valueLayout
	first func(uintptr, uintptr)
	second func(uintptr, uint8) uint64
}
type slotState uint8
type foreignSlot struct { state slotState; generation uint32 }
type spoofRegion struct { descriptor fakeOps; memory []byte; entries []foreignSlot }
type VM struct { owners map[uint64]*spoofRegion }
`),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan callback-shape spoof: %v", err)
		}
		const want = "VM.owners->general-slot(foreignSlot)"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("callback-shape spoof hid token %q", want)
		}
	})

	t.Run("exact D8 owner", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/async_owner_region.go": []byte(canonicalD8OwnerFixture),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan exact D8 owner: %v", err)
		}
		if hasFinding(findings, categoryVMUniversalOwner, "VM.owners->general-slot(asyncPayloadSlot)") {
			t.Fatalf("exact D8 typed owner became a general pool: %+v", findings)
		}
	})

	t.Run("closed owner rejects extra callback", func(t *testing.T) {
		source := strings.Replace(
			canonicalD8OwnerFixture,
			"\tretired bool\n",
			"\tretired bool\n\tcopy func(uintptr, uintptr)\n",
			1,
		)
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/extra_callback_region.go": []byte(source),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan D8 owner with extra callback: %v", err)
		}
		const want = "VM.owners->general-slot(asyncPayloadSlot)"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("extra callback spoof hid token %q", want)
		}
	})

	t.Run("shadowed primitive fails closed", func(t *testing.T) {
		source := strings.Replace(
			canonicalD8OwnerFixture,
			"import \"surge/internal/types\"\n",
			"import \"surge/internal/types\"\ntype uint32 = uint64\n",
			1,
		)
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/shadowed_primitive_region.go": []byte(source),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan D8 owner with shadowed primitive: %v", err)
		}
		const want = "VM.owners->general-slot(asyncPayloadSlot)"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("shadowed primitive spoof hid token %q", want)
		}
	})

	t.Run("near miss fails closed", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/near_miss_region.go": []byte(`package vm
type cellKind uint8
type storageMember struct { Offset, Size, Align uint64; TypeID uint32; Kind cellKind }
type Arena struct { bytes []byte; gen uint32 }
type asyncPayloadState uint8
type asyncSlotRole uint8
type asyncPayloadSlot struct { state asyncPayloadState; generation uint32; role asyncSlotRole; parkSeq uint64 }
type asyncOwnerRegion struct { typeID uint32; cell storageMember; stride uint64; arena Arena; slots []asyncPayloadSlot }
type VM struct { owners map[uint64]*asyncOwnerRegion }
`),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan nominal near miss: %v", err)
		}
		const want = "VM.owners->general-slot(asyncPayloadSlot)"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("wrong semantic field types hid token %q", want)
		}
	})
}

func TestStructuralOwnerCensusInstantiatesMethodlessInterfaces(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/generic_interfaces.go": []byte(`package vm
type empty[P any] interface{}
type emptyPair[A, B any] interface{}
type embedded interface { empty[int] }
type embeddedPair interface { emptyPair[int, string] }
type token struct { payload embedded }
type pairToken struct { payload embeddedPair }
type constrained[P empty[int]] struct { payload P }
type methodful[P any] interface { Run(P) }
type methodfulControl interface { methodful[int] }
type narrowed[P any] interface { ~[]P }
type narrowedControl[P narrowed[int]] struct { payload P }
type cycle[P any] interface { cycle[P] }
type cyclicControl struct { payload cycle[int] }
type control struct { payload methodfulControl }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan generic interfaces: %v", err)
	}
	for _, want := range []string{
		"token.payload->universal",
		"pairToken.payload->universal",
		"constrained.payload->universal",
	} {
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("generic methodless interface missed token %q", want)
		}
	}
	for _, finding := range findings {
		if strings.HasPrefix(finding.Token, "control.payload->") ||
			strings.HasPrefix(finding.Token, "narrowedControl.payload->") ||
			strings.HasPrefix(finding.Token, "cyclicControl.payload->") {
			t.Errorf("methodful, narrowed, or cyclic generic became universal: %+v", finding)
		}
	}
}

func TestStructuralOwnerCensusPreservesGenericRootBindings(t *testing.T) {
	tests := []struct {
		name, path, source, category, token string
	}{
		{
			name: "VM alias", path: "internal/vm/generic_alias_root.go",
			source: `package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint32 }
type vmCore[P any] struct { pool map[uint64]*P }
type VM = vmCore[generalSlot]
`, category: categoryVMUniversalOwner, token: "VM.pool->general-slot(generalSlot)",
		},
		{
			name: "VM named multi argument", path: "internal/vm/generic_named_root.go",
			source: `package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint32 }
type vmCore[K, V any] struct { pool map[K]*V }
type VM vmCore[uint64, generalSlot]
`, category: categoryVMUniversalOwner, token: "VM.pool->general-slot(generalSlot)",
		},
		{
			name: "Executor alias", path: "internal/asyncrt/generic_alias_root.go",
			source: `package asyncrt
type slotState uint8
type generalSlot struct { state slotState; generation uint32 }
type executorCore[P any] struct { pool map[uint64]*P }
type Executor = executorCore[generalSlot]
`, category: categoryAsyncAny, token: "Executor.pool->general-slot(generalSlot)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := buildFixtureTree(t, map[string][]byte{test.path: []byte(test.source)}, false)
			findings, err := Scan(root)
			if err != nil {
				t.Fatalf("scan generic root: %v", err)
			}
			if !hasFinding(findings, test.category, test.token) {
				t.Errorf("generic root lost token %q", test.token)
			}
		})
	}

	t.Run("cycle stops", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/generic_root_cycle.go": []byte(`package vm
type rootCycle[P any] rootCycle[P]
type VM = rootCycle[int]
`),
		}, false)
		if _, err := Scan(root); err != nil {
			t.Fatalf("scan generic root cycle: %v", err)
		}
	})
}

func TestStructuralOwnerCensusSubstitutesCompositeActualsBeforeShadowing(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/composite_actuals.go": []byte(`package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint32 }
type Box[P any] struct { value P }
type Wrap[P any] struct { nested Box[*P] }
type Pair[A, B any] struct { first A; second B }
type MultiWrap[A, B any] struct { nested Pair[*B, *A] }
type VM struct {
	one map[uint64]Wrap[generalSlot]
	two map[uint64]MultiWrap[generalSlot, int]
}
type carrierToken struct { root Wrap[Value] }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan composite actual substitution: %v", err)
	}
	for _, want := range []string{
		"VM.one->general-slot(generalSlot)",
		"VM.two->general-slot(generalSlot)",
		"carrierToken.root->Wrap.nested->Box.value->Value",
	} {
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("immutable outer substitution missed token %q", want)
		}
	}
}
