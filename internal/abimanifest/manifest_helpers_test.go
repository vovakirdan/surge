package abimanifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The instruments the manifest tests reach for, kept apart from the questions
// they ask. A helper that answers "find me this record" is not a claim about
// the ABI; the tests above are, and they read better without the machinery
// sitting between them.

func acceptedCrossAllocation(size uintptr) crossAllocationAttempt {
	return crossAllocationAttempt{size: size, align: 1, accepted: true, returnedSize: size, returnedAlign: 1}
}

func refusedCrossAllocation(size uintptr) crossAllocationAttempt {
	return crossAllocationAttempt{size: size, align: 1, accepted: false}
}

// applyCrossAllowanceContract is a small executable oracle for the canonical
// manifest semantics. Production crossing arrives in a later Epic 23b wave;
// this keeps byte/count rollback behavior testable at the ABI-contract layer.
func applyCrossAllowanceContract(plannedBytes, plannedAllocations uintptr, attempts []crossAllocationAttempt) crossAllowanceResult {
	result := crossAllowanceResult{
		status:               crossContractOK,
		remainingBytes:       plannedBytes,
		remainingAllocations: plannedAllocations,
		sourceOwned:          true,
		destinationEmpty:     true,
	}
	rollback := func(status crossContractStatus) crossAllowanceResult {
		result.status = status
		result.sourceOwned = true
		result.destinationEmpty = true
		return result
	}
	for _, attempt := range attempts {
		if attempt.size == 0 || attempt.align == 0 || attempt.size > result.remainingBytes || result.remainingAllocations == 0 {
			return rollback(crossContractPlanMismatch)
		}
		result.callbackCalls++
		if !attempt.accepted {
			return rollback(crossContractCapacity)
		}
		if attempt.returnedSize != attempt.size {
			return rollback(crossContractPlanMismatch)
		}
		if attempt.returnedAlign < attempt.align || attempt.returnedAlign%attempt.align != 0 {
			return rollback(crossContractPlanMismatch)
		}
		result.remainingBytes -= attempt.size
		result.remainingAllocations--
		result.successfulCalls++
		result.destinationEmpty = false
	}
	if result.remainingBytes != 0 || result.remainingAllocations != 0 {
		return rollback(crossContractPlanMismatch)
	}
	result.sourceOwned = false
	return result
}

func cloneManifest(t *testing.T, manifest Manifest) Manifest {
	t.Helper()
	canonical, err := CanonicalBytes(&manifest)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := Parse(canonical)
	if err != nil {
		t.Fatal(err)
	}
	return clone
}

func loadTestManifest(t *testing.T) Manifest {
	t.Helper()
	manifest, _, err := LoadCanonical(filepath.Join(testRepoRoot(t), ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func readCanonicalManifest(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testRepoRoot(t), ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func findFunctionView(t *testing.T, view SchemaView, name string) FunctionView {
	t.Helper()
	for _, function := range view.Functions {
		if function.Name == name {
			return function
		}
	}
	t.Fatalf("function view %q missing", name)
	return FunctionView{}
}

func findCallback(t *testing.T, manifest Manifest, name string) Function {
	t.Helper()
	for _, callback := range manifest.Callbacks {
		if callback.Name == name {
			return callback
		}
	}
	t.Fatalf("callback %q missing", name)
	return Function{}
}

func findRecord(t *testing.T, manifest Manifest, name string) Record {
	t.Helper()
	for _, record := range manifest.Records {
		if record.Name == name {
			return record
		}
	}
	t.Fatalf("record %q missing", name)
	return Record{}
}

func assertSemanticsContains(t *testing.T, subject, semantics string, phrases ...string) {
	t.Helper()
	lower := strings.ToLower(semantics)
	for _, phrase := range phrases {
		if !strings.Contains(lower, strings.ToLower(phrase)) {
			t.Fatalf("%s semantics do not freeze %q: %q", subject, phrase, semantics)
		}
	}
}

func findEnumView(t *testing.T, view SchemaView, name string) EnumView {
	t.Helper()
	for _, enum := range view.Enums {
		if enum.Name == name {
			return enum
		}
	}
	t.Fatalf("enum view %q missing", name)
	return EnumView{}
}

func findRecordView(t *testing.T, view SchemaView, name string) RecordView {
	t.Helper()
	for _, record := range view.Records {
		if record.Name == name {
			return record
		}
	}
	t.Fatalf("record view %q missing", name)
	return RecordView{}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("locate repository root: %v", err)
	}
	return root
}
