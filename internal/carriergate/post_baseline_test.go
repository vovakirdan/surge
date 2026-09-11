package carriergate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPostBaselineAllowanceUsesExactIdentityAndSeparateCounts(t *testing.T) {
	manifest, allowance := postBaselineFixture(t)
	category := postBaselineCategory(t, &manifest, categoryLLVMPointerWord)
	actual := []Finding{category.Legacy[0], category.Migration[0].Finding, allowance.Finding}
	actual[2].Line = 918 // Diagnostic locations do not authorize identities.
	difference := Compare(&manifest, actual)
	if !difference.Empty() || difference.PostBaselineAllowed != 1 || difference.MigrationTracked != 1 {
		t.Fatalf("live reviewed identities = %+v", difference)
	}
	if report := FormatDifference(&difference); !strings.Contains(report, "post-baseline allowances present: 1") ||
		!strings.Contains(report, "migration carriers still present: 1") {
		t.Fatalf("separate live counts missing from report: %s", report)
	}
	for _, change := range []struct {
		name   string
		mutate func(*Finding)
	}{
		{name: "category", mutate: func(f *Finding) { f.Category = categoryLLVMWordBridge }},
		{name: "path", mutate: func(f *Finding) { f.Path = "internal/backend/llvm/unreviewed.go" }},
		{name: "token", mutate: func(f *Finding) { f.Token = "inttoptr" }},
		{name: "evidence", mutate: func(f *Finding) { f.Evidence += " changed" }},
		{name: "ordinal", mutate: func(f *Finding) { f.Ordinal++ }},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := append([]Finding(nil), actual...)
			change.mutate(&changed[2])
			got := Compare(&manifest, changed)
			if got.Empty() || len(got.Unexpected) != 1 || len(got.StalePostBaselineAllow) != 1 ||
				got.PostBaselineAllowed != 0 || got.MigrationTracked != 1 {
				t.Fatalf("changed identity accepted or miscounted: %+v", got)
			}
		})
	}
	third := allowance.Finding
	third.Path = "internal/backend/llvm/unrelated.go"
	got := Compare(&manifest, append(actual, third))
	if got.Empty() || len(got.Unexpected) != 1 || got.Unexpected[0].Path != third.Path ||
		len(got.StalePostBaselineAllow) != 0 || got.PostBaselineAllowed != 1 {
		t.Fatalf("unrelated third site was authorized: %+v", got)
	}
}

func TestPostBaselineAllowanceMustRemainLiveUntilRemoved(t *testing.T) {
	manifest, _ := postBaselineFixture(t)
	category := postBaselineCategory(t, &manifest, categoryLLVMPointerWord)
	actual := append([]Finding(nil), category.Legacy...)
	difference := Compare(&manifest, actual)
	if difference.Empty() || len(difference.StalePostBaselineAllow) != 1 || difference.PostBaselineAllowed != 0 ||
		len(difference.StaleAllow) != 0 || len(difference.Unexpected) != 0 {
		t.Fatalf("missing post-baseline identity was not stale: %+v", difference)
	}
	if report := FormatDifference(&difference); !strings.Contains(report, "stale post-baseline allow:") ||
		!strings.Contains(report, "internal/backend/llvm/reviewed.go") {
		t.Fatalf("missing actionable stale allowance report: %s", report)
	}
	category.PostBaselineAllow = nil
	if err := ValidateManifest(&manifest); err != nil {
		t.Fatal(err)
	}
	if got := Compare(&manifest, actual); !got.Empty() {
		t.Fatalf("allowance removal did not retire stale authorization: %+v", got)
	}
}

func TestCompareExactDoesNotUsePostBaselineAllowances(t *testing.T) {
	manifest, allowance := postBaselineFixture(t)
	baseline := postBaselineCategory(t, &manifest, categoryLLVMPointerWord).Legacy
	difference := CompareExact(&manifest, baseline)
	if !difference.Empty() || difference.PostBaselineAllowed != 0 || difference.MigrationTracked != 0 {
		t.Fatalf("historical census required current-only entries: %+v", difference)
	}
	difference = CompareExact(&manifest, append(append([]Finding(nil), baseline...), allowance.Finding))
	if difference.Empty() || len(difference.Unexpected) != 1 || difference.PostBaselineAllowed != 0 ||
		len(difference.StalePostBaselineAllow) != 0 {
		t.Fatalf("exact census authorized a post-baseline entry: %+v", difference)
	}
}

func TestPostBaselineAllowancePreservesFrozenCensus(t *testing.T) {
	manifest, err := LoadManifest(legacyManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	original := cloneManifest(t, manifest)
	_, allowance := postBaselineFixture(t)
	postBaselineCategory(t, &manifest, allowance.Finding.Category).PostBaselineAllow = []Allowance{allowance}
	if err = ValidateManifest(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.BaselineCount != frozenBaseCount || manifest.BaselineDigest != frozenBaseDigest {
		t.Fatalf("frozen census changed to %d/%s", manifest.BaselineCount, manifest.BaselineDigest)
	}
	var baseline []Finding
	for i, category := range manifest.Categories {
		if category.BaselineCount != original.Categories[i].BaselineCount || category.BaselineDigest != original.Categories[i].BaselineDigest {
			t.Fatalf("category %s frozen census changed", category.ID)
		}
		baseline = append(baseline, category.Legacy...)
	}
	if len(baseline) != frozenBaseCount || Digest(baseline) != frozenBaseDigest {
		t.Fatal("reviewed authorization entered the historical finding set")
	}
	if difference := CompareExact(&manifest, baseline); !difference.Empty() {
		t.Fatalf("frozen census rejected with current authorization: %+v", difference)
	}
}

func TestManifestRejectsInvalidPostBaselineAllowances(t *testing.T) {
	manifest, _ := postBaselineFixture(t)
	for _, test := range []struct {
		name   string
		mutate func(*CategoryManifest)
		want   string
	}{
		{name: "legacy overlap", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding = c.Legacy[0] }, want: "overlaps the frozen"},
		{name: "migration overlap", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding = c.Migration[0].Finding }, want: "overlaps a migration"},
		{name: "legacy id collision", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].ID = c.Allow[0].ID }, want: "duplicate carrier allowance id"},
		{name: "post id collision", mutate: func(c *CategoryManifest) {
			duplicate := c.PostBaselineAllow[0]
			duplicate.Finding.Ordinal++
			c.PostBaselineAllow = append(c.PostBaselineAllow, duplicate)
		}, want: "duplicate carrier allowance id"},
		{name: "post identity collision", mutate: func(c *CategoryManifest) {
			duplicate := c.PostBaselineAllow[0]
			duplicate.ID += "-two"
			c.PostBaselineAllow = append(c.PostBaselineAllow, duplicate)
		}, want: "duplicate post-baseline allowance finding"},
		{name: "noncanonical ids", mutate: func(c *CategoryManifest) {
			second := c.PostBaselineAllow[0]
			second.ID = "a-first"
			second.Finding.Ordinal++
			c.PostBaselineAllow = append(c.PostBaselineAllow, second)
		}, want: "not canonical"},
		{name: "missing id", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].ID = "" }, want: "invalid id"},
		{name: "invalid id", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].ID = "Invalid ID" }, want: "invalid id"},
		{name: "missing reason", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].Reason = "" }, want: "requires rationale"},
		{name: "missing safety", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].SafeBecause = "\t" }, want: "requires rationale"},
		{name: "missing invalidation", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].InvalidatedWhen = "\n" }, want: "requires rationale"},
		{name: "category mismatch", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Category = categoryVMBoxKind }, want: "invalid carrier finding"},
		{name: "missing token", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Token = "" }, want: "invalid carrier finding"},
		{name: "missing evidence", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Evidence = "" }, want: "invalid carrier finding"},
		{name: "zero ordinal", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Ordinal = 0 }, want: "invalid carrier finding"},
		{name: "outside scope", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Path = "docs/carrier.go" }, want: "outside production scope"},
		{name: "noncanonical path", mutate: func(c *CategoryManifest) { c.PostBaselineAllow[0].Finding.Path = "internal/backend/llvm/../carrier.go" }, want: "invalid carrier finding path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := cloneManifest(t, manifest)
			test.mutate(postBaselineCategory(t, &changed, categoryLLVMPointerWord))
			validationErr := ValidateManifest(&changed)
			if validationErr == nil || !strings.Contains(validationErr.Error(), test.want) {
				t.Fatalf("invalid allowance error = %v, want %q", validationErr, test.want)
			}
		})
	}
}

func TestPostBaselineAllowanceIDsAreGlobal(t *testing.T) {
	manifest, allowance := postBaselineFixture(t)
	allowance.Finding = Finding{Category: categoryVMBoxKind, Path: "internal/vm/reviewed.go", Token: "OKStruct", Evidence: "OKStruct", Ordinal: 1}
	postBaselineCategory(t, &manifest, allowance.Finding.Category).PostBaselineAllow = []Allowance{allowance}
	if err := ValidateManifest(&manifest); err == nil || !strings.Contains(err.Error(), "duplicate carrier allowance id") {
		t.Fatalf("cross-category duplicate allowance id error = %v", err)
	}
}

func TestPostBaselineAllowanceRejectsSerializedLine(t *testing.T) {
	manifest, allowance := postBaselineFixture(t)
	entries, err := json.Marshal([]Allowance{allowance})
	if err != nil {
		t.Fatal(err)
	}
	withLine := strings.Replace(string(entries), `"ordinal":1`, `"ordinal":1,"line":42`, 1)
	if withLine == string(entries) {
		t.Fatal("line injection did not modify the allowance document")
	}
	_, err = LoadManifest(writePostBaselineDocument(t, manifest, json.RawMessage(withLine)))
	if err == nil || !strings.Contains(err.Error(), `unknown field "line"`) {
		t.Fatalf("serialized diagnostic line error = %v", err)
	}
}

func postBaselineFixture(t *testing.T) (Manifest, Allowance) {
	t.Helper()
	baseline := Finding{Category: categoryLLVMPointerWord, Path: "internal/backend/llvm/baseline.go", Token: "inttoptr", Evidence: "baseline fixnum", Ordinal: 1}
	legacyAllow := Allowance{ID: "legacy-pointer", Finding: baseline, Reason: "fixnum", SafeBecause: "tagged immediate", InvalidatedWhen: "tag layout changes"}
	manifest, err := newSnapshotManifest([]Finding{baseline}, []Allowance{legacyAllow})
	if err != nil {
		t.Fatal(err)
	}
	manifest = addMigration(t, manifest, trackedCarrier())
	allowance := Allowance{
		ID:      "reviewed-pointer",
		Finding: Finding{Category: categoryLLVMPointerWord, Path: "internal/backend/llvm/reviewed.go", Token: "ptrtoint", Evidence: "reviewed pointer conversion", Ordinal: 1},
		Reason:  "native pointer representation", SafeBecause: "used only for a tag test", InvalidatedWhen: "tag representation changes",
	}
	postBaselineCategory(t, &manifest, allowance.Finding.Category).PostBaselineAllow = []Allowance{allowance}
	if err = ValidateManifest(&manifest); err != nil {
		t.Fatal(err)
	}
	return manifest, allowance
}

func postBaselineCategory(t *testing.T, manifest *Manifest, id string) *CategoryManifest {
	t.Helper()
	for index := range manifest.Categories {
		if manifest.Categories[index].ID == id {
			return &manifest.Categories[index]
		}
	}
	t.Fatalf("missing category %s", id)
	return nil
}
