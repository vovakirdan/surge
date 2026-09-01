package carriergate

import (
	"strings"
	"testing"
)

// Rule 13: every row is a carrier shape the old basename switch missed.  Keep
// the expected path precise so a broad identifier search cannot satisfy the
// test while losing the type edge that makes the field an owner.
func TestStructuralOwnerCensusRejectsHiddenCarriers(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string][]byte
		category string
		token    string
	}{
		{
			name: "direct Value in a moved file",
			files: map[string][]byte{
				"internal/vm/moved_owner.go": []byte("package vm\ntype movedOwner struct { value Value }\n"),
			},
			category: categoryVMUniversalOwner,
			token:    "movedOwner.value->Value",
		},
		{
			name: "alias pointer and container sidecar",
			files: map[string][]byte{
				"internal/vm/renamed_owner.go": []byte(`package vm
type renamedCarrier = Value
type payloadCell struct { payload renamedCarrier }
type renamedOwner struct { cells []*payloadCell }
`),
			},
			category: categoryVMUniversalOwner,
			token:    "renamedOwner.cells->payloadCell.payload->Value",
		},
		{
			name: "generic Payload owner",
			files: map[string][]byte{
				"internal/asyncrt/generic_owner.go": []byte(`package asyncrt
type Payload interface{}
type payloadQueue[P Payload] struct { value P }
`),
			},
			category: categoryAsyncAny,
			token:    "payloadQueue.value->universal",
		},
		{
			name: "token interface payload",
			files: map[string][]byte{
				"internal/vm/interface_token.go": []byte("package vm\ntype interfaceToken struct { payload interface{} }\n"),
			},
			category: categoryVMUniversalOwner,
			token:    "interfaceToken.payload->universal",
		},
		{
			name: "token pointer to interface payload",
			files: map[string][]byte{
				"internal/vm/pointer_token.go": []byte(`package vm
type tokenCell struct { body interface{} }
type pointerToken struct { payload *tokenCell }
`),
			},
			category: categoryVMUniversalOwner,
			token:    "pointerToken.payload->tokenCell.body->universal",
		},
		{
			name: "root general slot pool",
			files: map[string][]byte{
				"internal/vm/root_slot_pool.go": []byte(`package vm
type slotState uint8
type Arena struct { bytes []byte }
type pooledSlot struct {
	state slotState
	generation uint64
	arena Arena
}
type pooledSlots struct { cells map[uint64]*pooledSlot }
type VM struct { payloads pooledSlots }
`),
			},
			category: categoryVMUniversalOwner,
			token:    "VM.payloads->general-slot(pooledSlot)",
		},
	}

	missed := make([]string, 0, len(tests))
	for _, test := range tests {
		root := buildFixtureTree(t, test.files, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("%s: scan fixture: %v", test.name, err)
		}
		if !hasFinding(findings, test.category, test.token) {
			missed = append(missed, test.name)
		}
	}
	if len(missed) != 0 {
		t.Fatalf("structural owner controls missed = %d, want 0: %s", len(missed), strings.Join(missed, ", "))
	}
}

func TestStructuralOwnerIdentitySurvivesPayloadSpelling(t *testing.T) {
	oldRoot := buildFixtureTree(t, map[string][]byte{
		"internal/asyncrt/owner.go": []byte("package asyncrt\ntype Payload interface{}\ntype queue struct { value any }\n"),
	}, false)
	newRoot := buildFixtureTree(t, map[string][]byte{
		"internal/asyncrt/owner.go": []byte("package asyncrt\ntype Payload interface{}\ntype queue[P Payload] struct { value P }\n"),
	}, false)
	oldFindings, err := Scan(oldRoot)
	if err != nil {
		t.Fatal(err)
	}
	newFindings, err := Scan(newRoot)
	if err != nil {
		t.Fatal(err)
	}
	oldStructural := structuralOwnerFindings(oldFindings)
	newStructural := structuralOwnerFindings(newFindings)
	if len(oldStructural) != 1 || len(newStructural) != 1 || Digest(oldStructural) != Digest(newStructural) {
		t.Fatalf("payload spelling changed structural identity: old=%+v new=%+v", oldStructural, newStructural)
	}
}

func TestStructuralOwnerManifestTracksOnlyThePreexistingTempDebt(t *testing.T) {
	manifest, err := LoadManifest(legacyManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	foundDebt316 := false
	for _, category := range manifest.Categories {
		for _, allowance := range category.Allow {
			if strings.Contains(allowance.Finding.Token, "asyncPayload") ||
				allowance.Finding.Token == "tagScrutinee.value->Value" {
				t.Fatalf("structural owner was hidden by an allowance: %+v", allowance)
			}
		}
		for _, migration := range category.Migration {
			if strings.Contains(migration.Finding.Token, "asyncPayload") {
				t.Fatalf("D8 owner was hidden by a migration: %+v", migration)
			}
			if migration.Finding.Token == "tagScrutinee.value->Value" {
				if migration.TrackedAs != "RV2-DEBT-316" {
					t.Fatalf("tag scrutinee tracked as %q", migration.TrackedAs)
				}
				foundDebt316 = true
			}
		}
	}
	if !foundDebt316 {
		t.Fatal("pre-existing tagScrutinee temporary owner is not tracked as RV2-DEBT-316")
	}
}

func TestStructuralOwnerCensusKeepsControlAndOwnerRegionsDistinct(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/owner_regions.go": []byte(`package vm
type service interface { Run() }
type slotState uint8
type Arena struct { bytes []byte }
type taskSlot struct { state slotState; generation uint64; arena Arena }
type taskRegion struct { result taskSlot }
type VM struct {
	runtime service
	tasks map[uint64]*taskRegion
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

func hasFinding(findings []Finding, category, token string) bool {
	for _, finding := range findings {
		if finding.Category == category && finding.Token == token {
			return true
		}
	}
	return false
}

func structuralOwnerFindings(findings []Finding) []Finding {
	structural := make([]Finding, 0)
	for _, finding := range findings {
		if strings.HasPrefix(finding.Evidence, "structural owner field ") {
			finding.Line = 0
			structural = append(structural, finding)
		}
	}
	return structural
}
