package carriergate

import "testing"

func TestStructuralOwnerCensusResolvesScopedPackageSelectors(t *testing.T) {
	t.Run("universal alias", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/sidecar/fog.go": []byte(`package sidecar
type Fog = interface{}
`),
			"internal/vm/token.go": []byte(`package vm
import "surge/internal/vm/sidecar"
type token struct { q *sidecar.Fog }
`),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan qualified universal alias: %v", err)
		}
		const want = "token.q->universal"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("scoped selector alias hid token %q: %+v", want, findings)
		}
	})

	t.Run("root lifecycle slot", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/sidecar/slot.go": []byte(`package sidecar
type slotState uint8
type Slot struct { state slotState; generation uint32 }
`),
			"internal/vm/root.go": []byte(`package vm
import "surge/internal/vm/sidecar"
type VM struct { pool map[uint64]*sidecar.Slot }
`),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan qualified root slot: %v", err)
		}
		const want = "VM.pool->general-slot(Slot)"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("scoped selector slot hid token %q: %+v", want, findings)
		}
	})

	t.Run("dot-imported alias", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/sidecar/fog.go": []byte("package sidecar\ntype Fog = interface{}\n"),
			"internal/vm/token.go": []byte(`package vm
import . "surge/internal/vm/sidecar"
type token struct { q *Fog }
`),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan dot-imported universal alias: %v", err)
		}
		const want = "token.q->universal"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("dot-imported scoped alias hid token %q: %+v", want, findings)
		}
	})

	t.Run("declared package qualifier", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/vm/oddpath/fog.go": []byte("package carrier\ntype Fog = interface{}\n"),
			"internal/vm/token.go": []byte(`package vm
import "surge/internal/vm/oddpath"
type token struct { q *carrier.Fog }
`),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan declared package qualifier: %v", err)
		}
		const want = "token.q->universal"
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("declared package qualifier hid token %q: %+v", want, findings)
		}
	})
}

func TestStructuralOwnerCensusScansNestedScopedPackageStructs(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/sidecar/cell.go": []byte("package sidecar\ntype Cell struct { payload interface{} }\n"),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan nested scoped package: %v", err)
	}
	const want = "Cell.payload->universal"
	if !hasFinding(findings, categoryVMUniversalOwner, want) {
		t.Errorf("nested scoped package hid token %q: %+v", want, findings)
	}
}

func TestStructuralOwnerCensusKeepsScopedSelectorControlsNegative(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/sidecar/control.go": []byte(`package sidecar
type Service interface { Run() }
type slotState uint8
type Control struct { state slotState; epoch uint32 }
`),
		"internal/vm/root.go": []byte(`package vm
import "surge/internal/vm/sidecar"
type token struct { service sidecar.Service }
type VM struct { controls map[uint64]*sidecar.Control }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan qualified selector controls: %v", err)
	}
	for _, finding := range findings {
		if finding.Category == categoryVMUniversalOwner {
			t.Fatalf("qualified methodful/non-lifecycle control became an owner: %+v", finding)
		}
	}
}
