package carriergate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// This test uses the pre-extension API so it can run against the old loader.
// The old code must fail at LoadManifest's unknown-field check, not compilation.
func TestLoadManifestAcceptsPostBaselineAllowance(t *testing.T) {
	baseline := Finding{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "OKStruct", Evidence: "OKStruct", Ordinal: 1}
	manifest, err := newSnapshotManifest([]Finding{baseline}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post := Finding{Category: categoryLLVMPointerWord, Path: "internal/backend/llvm/reviewed.go", Token: "ptrtoint", Evidence: "reviewed pointer conversion", Ordinal: 1}
	allowance := Allowance{ID: "reviewed-pointer", Finding: post, Reason: "native pointer representation", SafeBecause: "used only for a tag test", InvalidatedWhen: "tag representation changes"}
	entries, err := json.Marshal([]Allowance{allowance})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(writePostBaselineDocument(t, manifest, entries))
	if err != nil {
		t.Fatalf("load reviewed post-baseline allowance: %v", err)
	}
	if loaded.BaselineCount != manifest.BaselineCount || loaded.BaselineDigest != manifest.BaselineDigest {
		t.Fatal("post-baseline allowance changed the frozen census")
	}
	if difference := Compare(&loaded, []Finding{baseline, post}); !difference.Empty() {
		t.Fatalf("reviewed live finding rejected: %s", FormatDifference(&difference))
	}
	if difference := CompareExact(&loaded, []Finding{baseline}); !difference.Empty() {
		t.Fatalf("historical census demanded a post-baseline finding: %s", FormatDifference(&difference))
	}
}

func TestLoadManifestAllowsAbsentOrEmptyPostBaselineAllow(t *testing.T) {
	manifest, err := newSnapshotManifest(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct {
		name string
		data json.RawMessage
	}{{name: "absent"}, {name: "empty", data: json.RawMessage(`[]`)}, {name: "null", data: json.RawMessage(`null`)}} {
		t.Run(value.name, func(t *testing.T) {
			loaded, loadErr := LoadManifest(writePostBaselineDocument(t, manifest, value.data))
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if difference := Compare(&loaded, nil); !difference.Empty() {
				t.Fatalf("empty optional allowance set: %+v", difference)
			}
		})
	}
}

func writePostBaselineDocument(t *testing.T, manifest Manifest, entries json.RawMessage) string {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var categories []map[string]json.RawMessage
	if err = json.Unmarshal(document["categories"], &categories); err != nil {
		t.Fatal(err)
	}
	for _, category := range categories {
		var id string
		if err = json.Unmarshal(category["id"], &id); err != nil {
			t.Fatal(err)
		}
		if id == categoryLLVMPointerWord && entries != nil {
			category["post_baseline_allow"] = entries
		}
	}
	if document["categories"], err = json.Marshal(categories); err != nil {
		t.Fatal(err)
	}
	if data, err = json.Marshal(document); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
