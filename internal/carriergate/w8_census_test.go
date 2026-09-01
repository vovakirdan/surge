package carriergate

import (
	"fmt"
	"strings"
	"testing"
)

const (
	w8FixnumAllowanceID = "fixnum-inline-tagged-word"
	w8CarrierCensusWant = `suspension-frame-owner=0
llvm-erased-word-bridge=0
llvm-pointer-word-ir allowed=1 unallowed=0`
)

func w8FixnumFindingKey() findingKey {
	return findingKey{
		Category: categoryLLVMPointerWord,
		Path:     "internal/backend/llvm/emit_term.go",
		Token:    "inttoptr",
		Evidence: `return fmt.Sprintf("inttoptr (i64 %d to ptr)", int64(word)) //nolint:gosec // intentional bit reinterpretation`,
		Ordinal:  1,
	}
}

// The live ratchet accepts retired legacy findings when they are absent, but
// that also means it cannot report the exact zeroes Wave D exits on. This row
// keeps those current counts separate from the immutable base census.
func TestW8CarrierCensusReportsEachExitCategory(t *testing.T) {
	manifest, err := LoadManifest(legacyManifestPath)
	if err != nil {
		t.Fatalf("load legacy carrier manifest: %v", err)
	}
	actual, err := Scan(repositoryRoot(t))
	if err != nil {
		t.Fatalf("scan live repository: %v", err)
	}
	report, err := verifyW8CarrierCensus(&manifest, actual)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("W8 carrier census:\n%s", report)

	for _, control := range []struct {
		name     string
		finding  Finding
		wantLine string
	}{
		{name: "suspension frame owner returns", finding: retiredW8Carrier(t, &manifest, categoryFrameOwner), wantLine: "suspension-frame-owner=1"},
		{name: "erased word bridge returns", finding: retiredW8Carrier(t, &manifest, categoryLLVMWordBridge), wantLine: "llvm-erased-word-bridge=1"},
		{name: "unallowed pointer word returns", finding: retiredW8Carrier(t, &manifest, categoryLLVMPointerWord), wantLine: "llvm-pointer-word-ir allowed=1 unallowed=1"},
	} {
		t.Run("negative control/"+control.name, func(t *testing.T) {
			mutated := append([]Finding(nil), actual...)
			mutated = append(mutated, control.finding)
			assertW8CarrierCensusRejects(t, &manifest, mutated, control.wantLine)
		})
	}

	t.Run("negative control/allowed pointer word disappears", func(t *testing.T) {
		mutated := make([]Finding, 0, len(actual)-1)
		removed := false
		for i := range actual {
			if keyFor(&actual[i]) == w8FixnumFindingKey() && !removed {
				removed = true
				continue
			}
			mutated = append(mutated, actual[i])
		}
		if !removed {
			t.Fatal("live scan contained no pinned fixnum pointer-word finding")
		}
		assertW8CarrierCensusRejects(t, &manifest, mutated, "llvm-pointer-word-ir allowed=0 unallowed=0")
	})

	t.Run("negative control/fixnum allowance is rebound", func(t *testing.T) {
		mutated := cloneManifest(t, manifest)
		rebindW8FixnumAllowance(t, &mutated)
		if report, err := verifyW8CarrierCensus(&mutated, actual); err == nil {
			t.Fatalf("rebound fixnum allowance preserved the accepted census:\n%s", report)
		} else if !strings.Contains(err.Error(), "fixnum allowance") {
			t.Fatalf("rebound fixnum allowance error = %v", err)
		}
	})
}

func verifyW8CarrierCensus(manifest *Manifest, actual []Finding) (string, error) {
	if err := verifyW8FixnumAllowance(manifest); err != nil {
		return "", err
	}
	wantFixnum := w8FixnumFindingKey()
	frameOwners, wordBridges, pointerAllowed, pointerUnallowed := 0, 0, 0, 0
	for i := range actual {
		switch actual[i].Category {
		case categoryFrameOwner:
			frameOwners++
		case categoryLLVMWordBridge:
			wordBridges++
		case categoryLLVMPointerWord:
			if keyFor(&actual[i]) == wantFixnum {
				pointerAllowed++
			} else {
				pointerUnallowed++
			}
		}
	}
	report := fmt.Sprintf("%s=%d\n%s=%d\n%s allowed=%d unallowed=%d",
		categoryFrameOwner, frameOwners,
		categoryLLVMWordBridge, wordBridges,
		categoryLLVMPointerWord, pointerAllowed, pointerUnallowed)
	if report != w8CarrierCensusWant {
		return report, fmt.Errorf("W8 carrier census changed:\n%s", report)
	}
	return report, nil
}

func verifyW8FixnumAllowance(manifest *Manifest) error {
	want := w8FixnumFindingKey()
	for categoryIndex := range manifest.Categories {
		category := &manifest.Categories[categoryIndex]
		if category.ID != categoryLLVMPointerWord {
			continue
		}
		if len(category.Allow) != 1 {
			return fmt.Errorf("W8 fixnum allowance count = %d, want 1", len(category.Allow))
		}
		allowance := &category.Allow[0]
		if allowance.ID != w8FixnumAllowanceID || keyFor(&allowance.Finding) != want {
			return fmt.Errorf("W8 fixnum allowance = %q/%v, want %q/%v",
				allowance.ID, keyFor(&allowance.Finding), w8FixnumAllowanceID, want)
		}
		return nil
	}
	return fmt.Errorf("W8 fixnum allowance category %q is absent", categoryLLVMPointerWord)
}

func rebindW8FixnumAllowance(t *testing.T, manifest *Manifest) {
	t.Helper()
	want := w8FixnumFindingKey()
	for categoryIndex := range manifest.Categories {
		category := &manifest.Categories[categoryIndex]
		if category.ID != categoryLLVMPointerWord {
			continue
		}
		for findingIndex := range category.Legacy {
			finding := category.Legacy[findingIndex]
			if keyFor(&finding) != want {
				category.Allow[0].Finding = finding
				return
			}
		}
	}
	t.Fatal("manifest has no alternate frozen pointer-word finding")
}

func retiredW8Carrier(t *testing.T, manifest *Manifest, categoryID string) Finding {
	t.Helper()
	wantFixnum := w8FixnumFindingKey()
	for categoryIndex := range manifest.Categories {
		category := &manifest.Categories[categoryIndex]
		if category.ID != categoryID {
			continue
		}
		for findingIndex := range category.Legacy {
			finding := category.Legacy[findingIndex]
			if keyFor(&finding) == wantFixnum {
				continue
			}
			return finding
		}
	}
	t.Fatalf("manifest has no retired %s carrier for a negative control", categoryID)
	return Finding{}
}

func assertW8CarrierCensusRejects(t *testing.T, manifest *Manifest, actual []Finding, wantLine string) {
	t.Helper()
	report, err := verifyW8CarrierCensus(manifest, actual)
	if err == nil {
		t.Fatalf("negative control preserved the accepted census:\n%s", report)
	}
	if !strings.Contains(report, wantLine) {
		t.Fatalf("negative control report:\n%s\nwant line %q", report, wantLine)
	}
	t.Logf("negative control census:\n%s", report)
}
