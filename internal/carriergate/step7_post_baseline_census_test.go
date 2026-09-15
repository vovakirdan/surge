package carriergate

import (
	"fmt"
	"strings"
	"testing"
)

const (
	step7NumericHeapGuardAllowanceID = "numeric-lifecycle-heap-tag-test"
	step7NumericHeapGuardCensusWant  = "llvm-pointer-word-ir post-baseline-allowed=1"
)

// This is the single shared tag test, not permission for arbitrary pointer
// conversions. Keep its identity separate from W8's historical fixnum constant.
func step7NumericHeapGuardFindingKey() findingKey {
	return findingKey{
		Category: categoryLLVMPointerWord,
		Path:     "internal/backend/llvm/emit_numeric_lifecycle.go",
		Token:    "ptrtoint",
		Evidence: `fmt.Fprintf(out, "  %s = ptrtoint ptr %s to i64\n", word, val)`,
		Ordinal:  1,
	}
}

func verifyStep7NumericHeapGuardAllowance(manifest *Manifest) error {
	for i := range manifest.Categories {
		category := &manifest.Categories[i]
		if category.ID != categoryLLVMPointerWord {
			continue
		}
		if len(category.PostBaselineAllow) != 1 {
			return fmt.Errorf("Step 7 numeric heap guard allowance count = %d, want 1", len(category.PostBaselineAllow))
		}
		allowance := &category.PostBaselineAllow[0]
		want := step7NumericHeapGuardFindingKey()
		if allowance.ID != step7NumericHeapGuardAllowanceID || keyFor(&allowance.Finding) != want {
			return fmt.Errorf("Step 7 numeric heap guard allowance = %q/%v, want %q/%v",
				allowance.ID, keyFor(&allowance.Finding), step7NumericHeapGuardAllowanceID, want)
		}
		return nil
	}
	return fmt.Errorf("Step 7 numeric heap guard allowance category is absent")
}

func TestStep7NumericHeapGuardPostBaselineCensus(t *testing.T) {
	manifest, err := LoadManifest(legacyManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := Scan(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	report, err := verifyW8CarrierCensus(&manifest, actual)
	if err != nil || report != w8CarrierCensusWant+"\n"+step7NumericHeapGuardCensusWant {
		t.Fatalf("live Step 7 census: %v\n%s", err, report)
	}
	difference := Compare(&manifest, actual)
	if !difference.Empty() || difference.PostBaselineAllowed != 1 {
		t.Fatalf("live Step 7 allowance: %+v\n%s", difference, FormatDifference(&difference))
	}
	t.Logf("live Step 7 census:\n%s", report)
	guardIndex := -1
	for i := range actual {
		if keyFor(&actual[i]) == step7NumericHeapGuardFindingKey() {
			guardIndex = i
			break
		}
	}
	if guardIndex < 0 {
		t.Fatal("live scan missed the required numeric heap guard")
	}
	t.Run("missing_finding", func(t *testing.T) {
		changed := append([]Finding(nil), actual[:guardIndex]...)
		changed = append(changed, actual[guardIndex+1:]...)
		assertW8CarrierCensusRejects(t, &manifest, changed, "post-baseline-allowed=0")
	})
	t.Run("duplicate_finding", func(t *testing.T) {
		changed := append(append([]Finding(nil), actual...), actual[guardIndex])
		assertW8CarrierCensusRejects(t, &manifest, changed, "post-baseline-allowed=2")
	})
	t.Run("unrelated_third_site", func(t *testing.T) {
		third := actual[guardIndex]
		third.Path = "internal/backend/llvm/unreviewed.go"
		changed := append(append([]Finding(nil), actual...), third)
		assertW8CarrierCensusRejects(t, &manifest, changed, "allowed=1 unallowed=1")
		got := Compare(&manifest, changed)
		if got.Empty() || len(got.Unexpected) != 1 || keyFor(&got.Unexpected[0]) != keyFor(&third) {
			t.Fatalf("third site escaped the live ratchet: %+v", got)
		}
	})
}

func TestStep7NumericHeapGuardAllowanceIdentity(t *testing.T) {
	manifest, actual := step7NumericHeapGuardFixture(t)
	for _, row := range []struct {
		name   string
		change func(*CategoryManifest)
	}{
		{"missing_allowance", func(c *CategoryManifest) { c.PostBaselineAllow = nil }},
		{"rebound_id", func(c *CategoryManifest) { c.PostBaselineAllow[0].ID = "different-guard" }},
		{"category", func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Category = categoryLLVMWordBridge }},
		{"path", func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Path = "internal/backend/llvm/unreviewed.go" }},
		{"token", func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Token = "inttoptr" }},
		{"evidence", func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Evidence += " changed" }},
		{"ordinal", func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Ordinal++ }},
		{"extra_allowance", func(c *CategoryManifest) {
			extra := c.PostBaselineAllow[0]
			extra.ID = "unrelated-third-site"
			extra.Finding.Path = "internal/backend/llvm/unreviewed.go"
			c.PostBaselineAllow = append(c.PostBaselineAllow, extra)
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			changed := cloneManifest(t, manifest)
			row.change(postBaselineCategory(t, &changed, categoryLLVMPointerWord))
			report, err := verifyW8CarrierCensus(&changed, actual)
			if err == nil || !strings.Contains(err.Error(), "numeric heap guard allowance") {
				t.Fatalf("changed authorization %s: err=%v\n%s", row.name, err, report)
			}
		})
	}
}

func TestStep7NumericHeapGuardFindingIdentity(t *testing.T) {
	manifest, actual := step7NumericHeapGuardFixture(t)
	for _, row := range []struct {
		name   string
		change func(*Finding)
	}{
		{"category", func(f *Finding) { f.Category = categoryLLVMWordBridge }},
		{"path", func(f *Finding) { f.Path = "internal/backend/llvm/unreviewed.go" }},
		{"token", func(f *Finding) { f.Token = "inttoptr" }},
		{"evidence", func(f *Finding) { f.Evidence += " changed" }},
		{"ordinal", func(f *Finding) { f.Ordinal++ }},
	} {
		t.Run(row.name, func(t *testing.T) {
			changed := append([]Finding(nil), actual...)
			row.change(&changed[1])
			assertW8CarrierCensusRejects(t, &manifest, changed, "post-baseline-allowed=0")
		})
	}
}

func step7NumericHeapGuardFixture(t *testing.T) (Manifest, []Finding) {
	t.Helper()
	manifest, err := LoadManifest(legacyManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyW8FixnumAllowance(&manifest); err != nil {
		t.Fatal(err)
	}
	if err := verifyStep7NumericHeapGuardAllowance(&manifest); err != nil {
		t.Fatal(err)
	}
	category := postBaselineCategory(t, &manifest, categoryLLVMPointerWord)
	return manifest, []Finding{category.Allow[0].Finding, category.PostBaselineAllow[0].Finding}
}
