package carriergate

import (
	"fmt"
	"strings"
	"testing"
)

const (
	step7NumericHeapGuardAllowanceID = "numeric-lifecycle-heap-tag-test"
	step7FixnumDecodeAllowanceID     = "numeric-fixnum-fast-word-decode"
	step7FixnumEncodeAllowanceID     = "numeric-fixnum-fast-word-encode"
	step7NumericHeapGuardCensusWant  = "llvm-pointer-word-ir post-baseline-allowed=3"
)

// These are the shared lifecycle tag test and the two conversions the inline
// fixnum fast paths need -- not permission for arbitrary pointer conversions.
// Keep their identities separate from W8's historical fixnum constant.
func step7NumericHeapGuardFindingKey() findingKey {
	return findingKey{
		Category: categoryLLVMPointerWord,
		Path:     "internal/backend/llvm/emit_numeric_lifecycle.go",
		Token:    "ptrtoint",
		Evidence: `fmt.Fprintf(out, "  %s = ptrtoint ptr %s to i64\n", word, val)`,
		Ordinal:  1,
	}
}

func step7FixnumDecodeFindingKey() findingKey {
	return findingKey{
		Category: categoryLLVMPointerWord,
		Path:     "internal/backend/llvm/emit_numeric_fixnum_fast.go",
		Token:    "ptrtoint",
		Evidence: `fmt.Fprintf(&fe.emitter.buf, "  %s = ptrtoint ptr %s to i64\n", word, val)`,
		Ordinal:  1,
	}
}

func step7FixnumEncodeFindingKey() findingKey {
	return findingKey{
		Category: categoryLLVMPointerWord,
		Path:     "internal/backend/llvm/emit_numeric_fixnum_fast.go",
		Token:    "inttoptr",
		Evidence: `fmt.Fprintf(&fe.emitter.buf, "  %s = inttoptr i64 %s to ptr\n", word, boxed)`,
		Ordinal:  1,
	}
}

type step7Allowance struct {
	id  string
	key findingKey
}

// step7PostBaselineAllowances is the exact reviewed set, in the manifest's
// canonical id order.
func step7PostBaselineAllowances() []step7Allowance {
	return []step7Allowance{
		{step7FixnumDecodeAllowanceID, step7FixnumDecodeFindingKey()},
		{step7FixnumEncodeAllowanceID, step7FixnumEncodeFindingKey()},
		{step7NumericHeapGuardAllowanceID, step7NumericHeapGuardFindingKey()},
	}
}

func step7PostBaselineFindingKeys() map[findingKey]bool {
	keys := make(map[findingKey]bool)
	for _, allowance := range step7PostBaselineAllowances() {
		keys[allowance.key] = true
	}
	return keys
}

func verifyStep7NumericHeapGuardAllowance(manifest *Manifest) error {
	want := step7PostBaselineAllowances()
	for i := range manifest.Categories {
		category := &manifest.Categories[i]
		if category.ID != categoryLLVMPointerWord {
			continue
		}
		if len(category.PostBaselineAllow) != len(want) {
			return fmt.Errorf("Step 7 numeric heap guard allowance count = %d, want %d", len(category.PostBaselineAllow), len(want))
		}
		for j := range want {
			allowance := &category.PostBaselineAllow[j]
			if allowance.ID != want[j].id || keyFor(&allowance.Finding) != want[j].key {
				return fmt.Errorf("Step 7 numeric heap guard allowance %d = %q/%v, want %q/%v",
					j, allowance.ID, keyFor(&allowance.Finding), want[j].id, want[j].key)
			}
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
	if !difference.Empty() || difference.PostBaselineAllowed != 3 {
		t.Fatalf("live Step 7 allowance: %+v\n%s", difference, FormatDifference(&difference))
	}
	t.Logf("live Step 7 census:\n%s", report)
	for _, allowance := range step7PostBaselineAllowances() {
		index := -1
		for i := range actual {
			if keyFor(&actual[i]) == allowance.key {
				index = i
				break
			}
		}
		if index < 0 {
			t.Fatalf("live scan missed the required %s finding", allowance.id)
		}
		t.Run(allowance.id+"/missing_finding", func(t *testing.T) {
			changed := append([]Finding(nil), actual[:index]...)
			changed = append(changed, actual[index+1:]...)
			assertW8CarrierCensusRejects(t, &manifest, changed, "post-baseline-allowed=2")
		})
		t.Run(allowance.id+"/duplicate_finding", func(t *testing.T) {
			changed := append(append([]Finding(nil), actual...), actual[index])
			assertW8CarrierCensusRejects(t, &manifest, changed, "post-baseline-allowed=4")
		})
		t.Run(allowance.id+"/unrelated_third_site", func(t *testing.T) {
			third := actual[index]
			third.Path = "internal/backend/llvm/unreviewed.go"
			changed := append(append([]Finding(nil), actual...), third)
			assertW8CarrierCensusRejects(t, &manifest, changed, "allowed=1 unallowed=1")
			got := Compare(&manifest, changed)
			if got.Empty() || len(got.Unexpected) != 1 || keyFor(&got.Unexpected[0]) != keyFor(&third) {
				t.Fatalf("third site escaped the live ratchet: %+v", got)
			}
		})
	}
}

func TestStep7NumericHeapGuardAllowanceIdentity(t *testing.T) {
	manifest, actual := step7NumericHeapGuardFixture(t)
	for index := range step7PostBaselineAllowances() {
		for _, row := range []struct {
			name   string
			change func(*Allowance)
		}{
			{"rebound_id", func(a *Allowance) { a.ID = "different-guard" }},
			{"category", func(a *Allowance) { a.Finding.Category = categoryLLVMWordBridge }},
			{"path", func(a *Allowance) { a.Finding.Path = "internal/backend/llvm/unreviewed.go" }},
			{"token", func(a *Allowance) { a.Finding.Token = "bitcast" }},
			{"evidence", func(a *Allowance) { a.Finding.Evidence += " changed" }},
			{"ordinal", func(a *Allowance) { a.Finding.Ordinal++ }},
		} {
			t.Run(fmt.Sprintf("allowance_%d/%s", index, row.name), func(t *testing.T) {
				changed := cloneManifest(t, manifest)
				row.change(&postBaselineCategory(t, &changed, categoryLLVMPointerWord).PostBaselineAllow[index])
				report, err := verifyW8CarrierCensus(&changed, actual)
				if err == nil || !strings.Contains(err.Error(), "numeric heap guard allowance") {
					t.Fatalf("changed authorization %s: err=%v\n%s", row.name, err, report)
				}
			})
		}
	}
	for _, row := range []struct {
		name   string
		change func(*CategoryManifest)
	}{
		{"missing_allowances", func(c *CategoryManifest) { c.PostBaselineAllow = nil }},
		{"missing_one_allowance", func(c *CategoryManifest) { c.PostBaselineAllow = c.PostBaselineAllow[1:] }},
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
	for index := range step7PostBaselineAllowances() {
		for _, row := range []struct {
			name   string
			change func(*Finding)
		}{
			{"category", func(f *Finding) { f.Category = categoryLLVMWordBridge }},
			{"path", func(f *Finding) { f.Path = "internal/backend/llvm/unreviewed.go" }},
			{"token", func(f *Finding) { f.Token = "bitcast" }},
			{"evidence", func(f *Finding) { f.Evidence += " changed" }},
			{"ordinal", func(f *Finding) { f.Ordinal++ }},
		} {
			t.Run(fmt.Sprintf("finding_%d/%s", index, row.name), func(t *testing.T) {
				changed := append([]Finding(nil), actual...)
				row.change(&changed[1+index])
				assertW8CarrierCensusRejects(t, &manifest, changed, "post-baseline-allowed=2")
			})
		}
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
	actual := make([]Finding, 0, 1+len(category.PostBaselineAllow))
	actual = append(actual, category.Allow[0].Finding)
	for i := range category.PostBaselineAllow {
		actual = append(actual, category.PostBaselineAllow[i].Finding)
	}
	return manifest, actual
}
