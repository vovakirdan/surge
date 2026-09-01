package carriergate

import (
	"strings"
	"testing"
)

func TestStructuralOwnerCensusKeysRecursionByEffectiveBindings(t *testing.T) {
	t.Run("universal carrier", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/direct_carrier.go": []byte(`package vm
type Direct[P any] struct {
	next *Direct[Value]
	value P
}
type token struct { root *Direct[int] }
`),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan changing carrier instantiation: %v", err)
		}
		const want = "token.root->Direct.next->Direct.value->Value"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("carrier recursion missed token %q", want)
		}
	})

	t.Run("general slot", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/direct_slot.go": []byte(`package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint64 }
type Direct[P any] struct {
	next *Direct[generalSlot]
	value P
}
type VM struct { pool map[uint64]*Direct[int] }
`),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan changing slot instantiation: %v", err)
		}
		const want = "VM.pool->general-slot(generalSlot)"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("general-slot recursion missed token %q", want)
		}
	})
}

func TestStructuralOwnerCensusRequiresCoherentTypedRegion(t *testing.T) {
	t.Run("unrelated facts are not a region", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/region_decoy.go": []byte(`package vm
type slotState uint8
type valueLayout struct { size, align, stride uint64; flags uint32 }
type looseOps struct { first func(); second func() }
type foreignSlot struct { state slotState; generation uint64 }
type decoyRegion struct {
	facts valueLayout
	callbacks looseOps
	scratch []byte
	waiters []foreignSlot
}
type VM struct { owners map[uint64]*decoyRegion }
`),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan incoherent region: %v", err)
		}
		const want = "VM.owners->general-slot(foreignSlot)"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("incoherent typed-region decoy hid token %q", want)
		}
	})

	t.Run("canonical D8 region is a boundary", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/async_owner_region.go": []byte(canonicalD8OwnerFixture),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan canonical region: %v", err)
		}
		if hasFinding(findings, categoryVMUniversalOwner, "VM.owners->general-slot(asyncPayloadSlot)") {
			t.Fatalf("canonical D8 typed owner region became a general pool: %+v", findings)
		}
	})
}

func TestStructuralOwnerCensusResolvesEmbeddedMethodlessInterfaces(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/embedded_interfaces.go": []byte(`package vm
type fog interface{}
type mist interface { fog }
type haze interface { mist }
type methodful interface { Run() }
type methodfulEnvelope interface { methodful }
type integerish interface { ~int | ~int64 }
type cycleA interface { cycleB }
type cycleB interface { cycleA }
type token struct { payload haze }
type control struct { payload methodfulEnvelope }
type constrained[P integerish] struct { payload P }
type cyclicControl struct { payload cycleA }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan embedded interfaces: %v", err)
	}
	const want = "token.payload->universal"
	if !hasFinding(findings, categoryVMUniversalOwner, want) {
		t.Errorf("embedded methodless interface missed token %q", want)
	}
	for _, finding := range findings {
		if strings.HasPrefix(finding.Token, "control.payload->") ||
			strings.HasPrefix(finding.Token, "constrained.payload->") ||
			strings.HasPrefix(finding.Token, "cyclicControl.payload->") {
			t.Errorf("methodful, non-basic, or cyclic interface became universal: %+v", finding)
		}
	}
}

func TestStructuralOwnerCensusWalksRootMapKeysBeforeValues(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/map_key_pool.go": []byte(`package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint64 }
type VM struct { pool map[*generalSlot]struct{} }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan map-key slot pool: %v", err)
	}
	const want = "VM.pool->general-slot(generalSlot)"
	if !hasFinding(findings, categoryVMUniversalOwner, want) {
		t.Errorf("map-key walk missed token %q", want)
	}
}

func TestStructuralOwnerCensusResolvesCanonicalRootIdentity(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		source   string
		category string
		token    string
	}{
		{
			name: "VM alias", path: "internal/vm/alias_root.go",
			source: `package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint64 }
type vmCore struct { pool map[uint64]*generalSlot }
type VM = vmCore
`, category: categoryVMUniversalOwner, token: "VM.pool->general-slot(generalSlot)",
		},
		{
			name: "VM named underlying", path: "internal/vm/named_root.go",
			source: `package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint64 }
type vmCore struct { pool map[uint64]*generalSlot }
type VM vmCore
`, category: categoryVMUniversalOwner, token: "VM.pool->general-slot(generalSlot)",
		},
		{
			name: "Executor alias", path: "internal/asyncrt/alias_root.go",
			source: `package asyncrt
type slotState uint8
type generalSlot struct { state slotState; generation uint64 }
type executorCore struct { pool map[uint64]*generalSlot }
type Executor = executorCore
`, category: categoryAsyncAny, token: "Executor.pool->general-slot(generalSlot)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := buildFixtureTree(t, map[string][]byte{test.path: []byte(test.source)}, false)
			findings, err := Scan(root)
			if err != nil {
				t.Fatalf("scan canonical root: %v", err)
			}
			if !hasFinding(findings, test.category, test.token) {
				t.Errorf("canonical root walk missed token %q", test.token)
			}
		})
	}
}
