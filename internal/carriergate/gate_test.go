package carriergate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const legacyManifestPath = "testdata/legacy_carriers.json"

func TestLiveRatchetRejectsAddedAndChangedOccurrences(t *testing.T) {
	base := []Finding{
		{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "OKStruct", Evidence: "var _ = OKStruct", Ordinal: 1},
		{Category: categoryVMBoxKind, Path: "internal/vm/b.go", Token: "OKTag", Evidence: "var _ = OKTag", Ordinal: 1},
	}
	manifest, err := newSnapshotManifest(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	added := append(append([]Finding(nil), base...), Finding{Category: categoryVMBoxKind, Path: "internal/vm/c.go", Token: "OKTag", Evidence: "var _ = OKTag", Ordinal: 1})
	if difference := Compare(&manifest, added); len(difference.Unexpected) != 1 || len(difference.Stale) != 0 {
		t.Fatalf("added difference = %+v", difference)
	}
	changed := append([]Finding(nil), base...)
	changed[0].Evidence = "var renamed = OKStruct"
	if difference := Compare(&manifest, changed); len(difference.Stale) != 0 || len(difference.Unexpected) != 1 {
		t.Fatalf("changed difference = %+v", difference)
	}
}

func TestLiveRatchetAllowsLegacyReduction(t *testing.T) {
	base := []Finding{
		{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "OKStruct", Evidence: "var _ = OKStruct", Ordinal: 1},
		{Category: categoryVMBoxKind, Path: "internal/vm/b.go", Token: "OKTag", Evidence: "var _ = OKTag", Ordinal: 1},
	}
	manifest, err := newSnapshotManifest(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if difference := Compare(&manifest, base[:1]); !difference.Empty() {
		t.Fatalf("live reduction rejected: %+v", difference)
	}
	if difference := CompareExact(&manifest, base[:1]); len(difference.Stale) != 1 {
		t.Fatalf("exact-base reduction difference = %+v", difference)
	}
}

func TestLiveRatchetRejectsStaleAllowance(t *testing.T) {
	finding := Finding{Category: categoryLLVMPointerWord, Path: "internal/backend/llvm/a.go", Token: "inttoptr", Evidence: "return inttoptr", Ordinal: 1}
	allowance := Allowance{ID: "test-allow", Finding: finding, Reason: "test", SafeBecause: "test", InvalidatedWhen: "removed"}
	manifest, err := newSnapshotManifest([]Finding{finding}, []Allowance{allowance})
	if err != nil {
		t.Fatal(err)
	}
	invalid := cloneManifest(t, manifest)
	for categoryIndex := range invalid.Categories {
		if len(invalid.Categories[categoryIndex].Allow) > 0 {
			invalid.Categories[categoryIndex].Allow[0].Finding.Evidence = "not in frozen baseline"
		}
	}
	if err := ValidateManifest(&invalid); err == nil {
		t.Fatal("allowance outside frozen baseline was accepted")
	}
	difference := Compare(&manifest, nil)
	if len(difference.StaleAllow) != 1 {
		t.Fatalf("stale allowance difference = %+v", difference)
	}
	for categoryIndex := range manifest.Categories {
		manifest.Categories[categoryIndex].Allow = []Allowance{}
	}
	if err := ValidateManifest(&manifest); err != nil {
		t.Fatalf("remove stale allowance without changing baseline: %v", err)
	}
	if difference := Compare(&manifest, nil); !difference.Empty() {
		t.Fatalf("retired allowed legacy rejected after allowance cleanup: %+v", difference)
	}
}

func TestManifestRejectsSchemaScopeOrderAndPathDrift(t *testing.T) {
	finding := Finding{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "OKStruct", Evidence: "OKStruct", Ordinal: 1}
	manifest, err := newSnapshotManifest([]Finding{finding}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{name: "base", mutate: func(value *Manifest) { value.BaseCommit = strings.Repeat("0", 40) }},
		{name: "scope", mutate: func(value *Manifest) { value.Scope[0].Root = "docs" }},
		{name: "count", mutate: func(value *Manifest) { value.BaselineCount++ }},
		{name: "category order", mutate: func(value *Manifest) {
			value.Categories[0], value.Categories[1] = value.Categories[1], value.Categories[0]
		}},
		{name: "outside path", mutate: func(value *Manifest) {
			for index := range value.Categories {
				if len(value.Categories[index].Legacy) > 0 {
					value.Categories[index].Legacy[0].Path = "../escape.go"
					return
				}
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyValue := cloneManifest(t, manifest)
			test.mutate(&copyValue)
			if err := ValidateManifest(&copyValue); err == nil {
				t.Fatal("drift accepted")
			}
		})
	}
	unknown := filepath.Join(t.TempDir(), "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"version":1,"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(unknown); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error = %v", err)
	}
}

func TestExplicitMakeTargetProbeRejectsCatchAllFalseGreen(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is unavailable")
	}
	makefile := filepath.Join(t.TempDir(), "Makefile")
	catchAll := "all:\n\t@:\n%:\n\t@:\n"
	if err := os.WriteFile(makefile, []byte(catchAll), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("make", "-s", "-f", filepath.Base(makefile), "runtime-v2-carrier-check")
	command.Dir = filepath.Dir(makefile)
	command.Env = []string{"LC_ALL=C", "PATH=" + os.Getenv("PATH")}
	if err := command.Run(); err != nil {
		t.Fatalf("catch-all did not reproduce false green: %v", err)
	}
	if explicit, err := HasExplicitMakeTarget(makefile, "runtime-v2-carrier-check"); err != nil || explicit {
		t.Fatalf("catch-all explicit probe = %t, %v", explicit, err)
	}
	withTarget := catchAll + "runtime-v2-carrier-check:\n\t@:\n"
	if err := os.WriteFile(makefile, []byte(withTarget), 0o600); err != nil {
		t.Fatal(err)
	}
	if explicit, err := HasExplicitMakeTarget(makefile, "runtime-v2-carrier-check"); err != nil || !explicit {
		t.Fatalf("explicit target probe = %t, %v", explicit, err)
	}
}

// The probe asks "is this target explicitly defined", and it asks by making
// make dump its database for a goal that does not exist. Those are two different
// questions and the exit code answers the wrong one: since RV2-DEBT-200 narrowed
// the catch-all, make REFUSES that dummy goal and exits non-zero after printing
// everything the probe needs. Treating that as failure broke the live probe on
// the repository's own Makefile.
//
// What must still be an error is a dump that never reached the file section,
// because then make died before it knew anything - and an empty answer read as
// "no such target" would be a false negative in a gate built to catch false
// positives.
func TestExplicitMakeTargetProbeErrorsWhenMakeCannotAnswer(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is unavailable")
	}
	missing := filepath.Join(t.TempDir(), "Makefile")
	if _, err := HasExplicitMakeTarget(missing, "check"); err == nil {
		t.Fatal("probe returned an answer for a Makefile that does not exist")
	}
}

func TestExplicitMakeTargetProbeOnRepositoryMakefile(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is unavailable")
	}
	makefile := filepath.Join(repositoryRoot(t), "Makefile")
	if explicit, err := HasExplicitMakeTarget(makefile, "check"); err != nil || !explicit {
		t.Fatalf("live explicit target probe = %t, %v", explicit, err)
	}
	if explicit, err := HasExplicitMakeTarget(makefile, "definitely-not-an-explicit-target"); err != nil || explicit {
		t.Fatalf("live catch-all target probe = %t, %v", explicit, err)
	}
}

func cloneManifest(t *testing.T, manifest Manifest) Manifest {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var cloned Manifest
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func TestRequiredCategoriesAreCanonical(t *testing.T) {
	copyValue := append([]string(nil), requiredCategories...)
	slicesSort(copyValue)
	if !reflect.DeepEqual(copyValue, requiredCategories) {
		t.Fatalf("required categories are not sorted: %v", requiredCategories)
	}
	// Named one by one rather than counted, because a count is satisfied by
	// any twelve strings. The manifest is keyed by position on this list, so
	// dropping a category here would silently un-ratchet it instead of
	// failing the load.
	want := []string{
		categoryCompositeBox, categoryLLVMCompositePtr, categoryLLVMWordBridge,
		categoryLLVMPointerWord, categoryNativePayloadBits, categoryNativeWord,
		categoryNumericDrop, categoryFrameOwner, categoryUntypedCaptureState,
		categoryAsyncAny, categoryVMBoxKind, categoryVMUniversalOwner,
	}
	if !reflect.DeepEqual(requiredCategories, want) {
		t.Fatalf("required categories = %v, want %v", requiredCategories, want)
	}
}

func newSnapshotManifest(findings []Finding, allowances []Allowance) (Manifest, error) {
	actual := append([]Finding(nil), findings...)
	for i := range actual {
		actual[i].Line = 0
	}
	sortFindings(actual)
	allowByKey := make(map[findingKey]Allowance, len(allowances))
	actualByKey := make(map[findingKey]Finding, len(actual))
	for i := range actual {
		finding := &actual[i]
		actualByKey[keyFor(finding)] = *finding
	}
	for _, allowance := range allowances {
		allowance.Finding.Line = 0
		key := keyFor(&allowance.Finding)
		if _, exists := actualByKey[key]; !exists {
			return Manifest{}, fmt.Errorf("snapshot allowance %s does not match an actual finding", allowance.ID)
		}
		allowByKey[key] = allowance
	}
	manifest := Manifest{
		Version: ManifestVersion, BaseCommit: EpicBaseCommit, DigestAlgorithm: digestAlgorithm,
		BaselineCount: len(actual), BaselineDigest: Digest(actual), Scope: cloneScopes(requiredScopes),
		Categories: make([]CategoryManifest, 0, len(requiredCategories)),
	}
	for _, categoryID := range requiredCategories {
		category := CategoryManifest{ID: categoryID, RetireToZero: true, Legacy: []Finding{}, Allow: []Allowance{}, Migration: []MigrationCarrier{}}
		baseline := make([]Finding, 0)
		for i := range actual {
			finding := &actual[i]
			if finding.Category != categoryID {
				continue
			}
			baseline = append(baseline, *finding)
			category.Legacy = append(category.Legacy, *finding)
			if allowance, safe := allowByKey[keyFor(finding)]; safe {
				category.Allow = append(category.Allow, allowance)
			}
		}
		sort.Slice(category.Allow, func(i, j int) bool { return category.Allow[i].ID < category.Allow[j].ID })
		category.BaselineCount = len(baseline)
		category.BaselineDigest = Digest(baseline)
		manifest.Categories = append(manifest.Categories, category)
	}
	return manifest, ValidateManifest(&manifest)
}

func cloneScopes(scopes []Scope) []Scope {
	cloned := make([]Scope, len(scopes))
	for i := range scopes {
		cloned[i] = Scope{
			Root:       scopes[i].Root,
			Extensions: cloneStrings(scopes[i].Extensions),
			Excludes:   cloneStrings(scopes[i].Excludes),
		}
	}
	return cloned
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

// addMigration puts one tracked carrier into the category it belongs to.
func addMigration(t *testing.T, manifest Manifest, carrier MigrationCarrier) Manifest {
	t.Helper()
	copied := cloneManifest(t, manifest)
	for index := range copied.Categories {
		if copied.Categories[index].ID != carrier.Finding.Category {
			continue
		}
		copied.Categories[index].Migration = append(copied.Categories[index].Migration, carrier)
		sortMigrations(copied.Categories[index].Migration)
	}
	return copied
}

func trackedCarrier() MigrationCarrier {
	return MigrationCarrier{
		Finding: Finding{
			Category: categoryLLVMPointerWord, Path: "internal/backend/llvm/added.go",
			Token: "ptrtoint", Evidence: "the carrier this epic added", Ordinal: 1,
		},
		RetiredBy: "a later wave",
		TrackedAs: "the row that owns it",
	}
}

// A carrier this epic introduced is known to the live ratchet, counted, and
// absent from the base census — which is what lets the base census keep being
// a census of a commit.
func TestMigrationCarrierIsTrackedNotUnexpected(t *testing.T) {
	base := []Finding{
		{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "OKStruct", Evidence: "var _ = OKStruct", Ordinal: 1},
	}
	manifest, err := newSnapshotManifest(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	carrier := trackedCarrier()
	tracked := addMigration(t, manifest, carrier)
	if err := ValidateManifest(&tracked); err != nil {
		t.Fatalf("tracked migration carrier rejected: %v", err)
	}
	if tracked.BaselineCount != manifest.BaselineCount || tracked.BaselineDigest != manifest.BaselineDigest {
		t.Fatal("a migration carrier changed the base census")
	}

	live := append(append([]Finding(nil), base...), carrier.Finding)
	difference := Compare(&tracked, live)
	if !difference.Empty() {
		t.Fatalf("tracked carrier reported as a mismatch: %+v", difference)
	}
	if difference.MigrationTracked != 1 {
		t.Fatalf("tracked carrier count = %d, want 1", difference.MigrationTracked)
	}
	if !strings.Contains(FormatDifference(&difference), "migration carriers still present: 1") {
		t.Fatalf("the tracked count is not reported:\n%s", FormatDifference(&difference))
	}

	// Untracked, the same finding is exactly what the ratchet exists to catch.
	if difference := Compare(&manifest, live); len(difference.Unexpected) != 1 {
		t.Fatalf("untracked carrier difference = %+v", difference)
	}
}

// Retiring a tracked carrier is the point of tracking it, so it must not trip
// the gate the way a vanished allowance does.
func TestRetiringAMigrationCarrierIsProgress(t *testing.T) {
	base := []Finding{
		{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "OKStruct", Evidence: "var _ = OKStruct", Ordinal: 1},
	}
	manifest, err := newSnapshotManifest(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	tracked := addMigration(t, manifest, trackedCarrier())
	difference := Compare(&tracked, base)
	if !difference.Empty() {
		t.Fatalf("retiring a tracked carrier failed the gate: %+v", difference)
	}
	if difference.MigrationTracked != 0 {
		t.Fatalf("retired carrier still counted: %d", difference.MigrationTracked)
	}
}

// The exact-base comparison scans the base commit, where a migration carrier
// did not exist. It must not go looking for one.
func TestMigrationCarrierIsInvisibleToTheExactBaseComparison(t *testing.T) {
	base := []Finding{
		{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "OKStruct", Evidence: "var _ = OKStruct", Ordinal: 1},
	}
	manifest, err := newSnapshotManifest(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	tracked := addMigration(t, manifest, trackedCarrier())
	if difference := CompareExact(&tracked, base); !difference.Empty() {
		t.Fatalf("exact-base comparison saw a migration carrier: %+v", difference)
	}
}

func TestMigrationCarrierRequiresItsRetirement(t *testing.T) {
	base := []Finding{
		{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "OKStruct", Evidence: "var _ = OKStruct", Ordinal: 1},
	}
	manifest, err := newSnapshotManifest(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range []struct {
		name   string
		mutate func(*MigrationCarrier)
	}{
		{name: "no retiring work", mutate: func(c *MigrationCarrier) { c.RetiredBy = "  " }},
		{name: "no owning row", mutate: func(c *MigrationCarrier) { c.TrackedAs = "" }},
	} {
		t.Run(missing.name, func(t *testing.T) {
			carrier := trackedCarrier()
			missing.mutate(&carrier)
			invalid := addMigration(t, manifest, carrier)
			if err := ValidateManifest(&invalid); err == nil {
				t.Fatal("a carrier nobody promised to remove was accepted")
			}
		})
	}
}

// A finding that IS in the base census belongs in legacy. Accepting it here
// would let a base carrier be re-described as something this epic introduced.
func TestMigrationCarrierRefusesABaseCensusFinding(t *testing.T) {
	base := []Finding{
		{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "OKStruct", Evidence: "var _ = OKStruct", Ordinal: 1},
	}
	manifest, err := newSnapshotManifest(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	invalid := addMigration(t, manifest, MigrationCarrier{
		Finding: base[0], RetiredBy: "a later wave", TrackedAs: "the row that owns it",
	})
	if err := ValidateManifest(&invalid); err == nil {
		t.Fatal("a base-census finding was accepted as a migration carrier")
	}
}
