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
			name: "cross-file neutral pointer to interface alias",
			files: map[string][]byte{
				"internal/vm/fog.go":   []byte("package vm\ntype fog interface{}\n"),
				"internal/vm/x.go":     []byte("package vm\ntype x = fog\n"),
				"internal/vm/token.go": []byte("package vm\ntype token struct { q *x }\n"),
			},
			category: categoryVMUniversalOwner,
			token:    "token.q->universal",
		},
		{
			name: "cross-file neutral pointer to Value alias",
			files: map[string][]byte{
				"internal/vm/x.go":     []byte("package vm\ntype x = Value\n"),
				"internal/vm/token.go": []byte("package vm\ntype token struct { q *x }\n"),
			},
			category: categoryVMUniversalOwner,
			token:    "token.q->Value",
		},
		{
			name: "arbitrary-depth neutral record path",
			files: map[string][]byte{
				"internal/vm/leaf.go":   []byte("package vm\ntype leaf struct { r Value }\n"),
				"internal/vm/middle.go": []byte("package vm\ntype middle struct { p *leaf }\n"),
				"internal/vm/token.go":  []byte("package vm\ntype token struct { q *middle }\n"),
			},
			category: categoryVMUniversalOwner,
			token:    "token.q->middle.p->leaf.r->Value",
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
type channelRegion struct { cells map[uint64]*pooledSlot }
type VM struct { payloads channelRegion }
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

func TestStructuralOwnerCensusHandlesRecursiveGenericsPrecisely(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/recursive_generic.go": []byte(`package vm
type Node[P any] struct { next *Branch[P] }
type Branch[Q any] struct {
	back *Node[Q]
	value Q
}
type Phantom[P any] struct { next *Phantom[P] }
type token struct {
	root *Node[Value]
	phantom *Phantom[Value]
}
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan recursive generics: %v", err)
	}
	if !hasFinding(findings, categoryVMUniversalOwner, "token.root->Node.next->Branch.value->Value") {
		t.Fatalf("recursive generic guard hid a concrete carrier argument: %+v", findings)
	}
	for _, finding := range findings {
		if strings.HasPrefix(finding.Token, "token.phantom->") {
			t.Fatalf("phantom recursive argument became a carrier: %+v", finding)
		}
	}
}

func TestStructuralOwnerManifestClassifiesOnlyReviewedMigrations(t *testing.T) {
	manifest, err := LoadManifest(legacyManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	frameReachability := 0
	d8AsyncOwner := 0
	for _, category := range manifest.Categories {
		for _, allowance := range category.Allow {
			if strings.Contains(allowance.Finding.Token, "asyncPayload") ||
				allowance.Finding.Token == "tagScrutinee.value->Value" ||
				allowance.Finding.Token == "VM.Async->Value" ||
				strings.HasSuffix(allowance.Finding.Token, "Frame.Locals->LocalSlot.V->Value") {
				t.Fatalf("structural owner was hidden by an allowance: %+v", allowance)
			}
		}
		for _, migration := range category.Migration {
			if strings.Contains(migration.Finding.Token, "asyncPayload") {
				t.Fatalf("D8 owner was hidden by a migration: %+v", migration)
			}
			if migration.Finding.Token == "tagScrutinee.value->Value" {
				t.Fatalf("closed tag-scrutinee owner remained a migration: %+v", migration)
			}
			if migration.Finding.Token == "VM.Async->Value" {
				if migration.TrackedAs != "RV2-DEBT-151" ||
					migration.RetiredBy != "wave-d-d8-vm-exact-async-owner" {
					t.Fatalf("VM async owner migration classification = %+v", migration)
				}
				d8AsyncOwner++
			}
			if strings.HasSuffix(migration.Finding.Token, "Frame.Locals->LocalSlot.V->Value") {
				if migration.TrackedAs != "RV2-DEBT-318" ||
					migration.RetiredBy != "wave-f-frame-locals-typed-owner" {
					t.Fatalf("frame reachability migration classification = %+v", migration)
				}
				frameReachability++
			}
		}
	}
	if d8AsyncOwner != 1 || frameReachability != 21 {
		t.Fatalf("post-base structural migrations = D8:%d frame-reachability:%d, want 1/21",
			d8AsyncOwner, frameReachability)
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
