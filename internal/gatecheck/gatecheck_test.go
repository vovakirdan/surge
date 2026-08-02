package gatecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("gatecheck: get working directory: %v", err)
	}
	for {
		goMod, readErr := os.ReadFile(filepath.Join(dir, "go.mod"))
		makeInfo, statErr := os.Stat(filepath.Join(dir, "Makefile"))
		if readErr == nil && statErr == nil && !makeInfo.IsDir() && hasModuleLine(string(goMod), "surge") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("gatecheck: no repository root with module surge and Makefile above working directory")
		}
		dir = parent
	}
}

func hasModuleLine(goMod, module string) bool {
	for _, line := range strings.Split(goMod, "\n") {
		if strings.TrimSpace(line) == "module "+module {
			return true
		}
	}
	return false
}

// The live integrity check: every gate selection matches at least one real
// test (evaluated by `go test -list` under the gate's own tags — never a
// reimplementation of Go's matching), every tagged runtime-v2 test is
// covered by some gate, and every gate target is reachable from the check
// or runtime-v2-check roots or carries an owned exemption.
func TestGateSelectionsAreLiveAndComplete(t *testing.T) {
	if testing.Short() {
		t.Skip("gate integrity invokes go test -list; skipped in -short")
	}
	root := repoRoot(t)
	makefileBytes, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(makefileBytes)
	exemptBytes, err := os.ReadFile(filepath.Join(root, "internal", "gatecheck", "exemptions.txt"))
	if err != nil {
		t.Fatalf("read exemptions: %v", err)
	}
	exemptions, err := ParseExemptions(string(exemptBytes))
	if err != nil {
		t.Fatalf("parse exemptions: %v", err)
	}

	gates := ParseGates(makefile)
	if len(gates) < 10 {
		t.Fatalf("suspiciously few gates parsed (%d); parser or Makefile shape drifted", len(gates))
	}

	// Bootstrap canary: this package runs under the default tags of
	// `go test ./...`, which the test target must keep running and the
	// check root must keep invoking — otherwise the integrity check itself
	// is unreachable and cannot report anything.
	if !strings.Contains(makefile, "$(GO) test ./...") {
		t.Fatal("the test target no longer runs `go test ./...`; the integrity check would be unreachable")
	}
	reachable := ReachableTargets(makefile, "check", "runtime-v2-check")
	if !reachable["test"] {
		t.Fatal("the check root no longer reaches the test target; the integrity check would be unreachable")
	}

	// Every gate is reachable or exempted with an owner.
	for _, g := range UnreachableGates(gates, reachable, exemptions) {
		t.Errorf("gate target %q (Makefile line %d) is reachable from neither check nor runtime-v2-check and has no owned exemption", g.Target, g.Line)
	}

	// Every explicit selection matches at least one test, under the gate's
	// own tags.
	matchedByKey := map[string][][]string{}
	for _, g := range gates {
		if g.Run == "" || len(g.Packages) == 0 {
			continue
		}
		matched, listErr := ListTests(root, g.Packages, g.Tags, g.Run)
		if listErr != nil {
			t.Errorf("gate %q line %d: %v", g.Target, g.Line, listErr)
			continue
		}
		if len(matched) == 0 {
			t.Errorf("gate %q (Makefile line %d) selects no tests: -tags %q -run %q", g.Target, g.Line, g.Tags, g.Run)
		}
		for _, pkg := range g.Packages {
			key := pkg + "\x00" + g.Tags
			matchedByKey[key] = append(matchedByKey[key], matched)
		}
	}

	// Coverage for the tag-gated runtime suites: default-tag tests ride
	// `go test ./...`; tag-gated tests exist ONLY through explicit gates,
	// so each one must be selected somewhere.
	exemptTags := map[string]bool{}
	for _, e := range exemptions {
		if e.Kind == "tag" {
			exemptTags[e.Name] = true
		}
	}
	taggedInventories := []struct {
		pkg  string
		tags string
	}{
		{pkg: "./internal/vm", tags: "runtime_v2_pending"},
		{pkg: "./internal/vm", tags: "runtime_v2_transport_spine"},
		{pkg: "./internal/ownershipgate", tags: "runtime_v2_ownership_corpus"},
	}
	for _, inventory := range taggedInventories {
		if exemptTags[inventory.tags] {
			continue
		}
		defaultInventory, defErr := ListTests(root, []string{inventory.pkg}, "", ".*")
		if defErr != nil {
			t.Fatalf("default inventory %s: %v", inventory.pkg, defErr)
		}
		defaultSet := make(map[string]bool, len(defaultInventory))
		for _, name := range defaultInventory {
			defaultSet[name] = true
		}
		tagged, invErr := ListTests(root, []string{inventory.pkg}, inventory.tags, ".*")
		if invErr != nil {
			t.Fatalf("inventory %s -tags %s: %v", inventory.pkg, inventory.tags, invErr)
		}
		// A build tag ADDS files to the default set; only the tests that
		// exist solely under the tag depend on explicit gates (the default
		// ones ride `go test ./...`).
		var tagOnly []string
		for _, name := range tagged {
			if !defaultSet[name] {
				tagOnly = append(tagOnly, name)
			}
		}
		key := inventory.pkg + "\x00" + inventory.tags
		for _, name := range UncoveredTests(tagOnly, matchedByKey[key], exemptions) {
			t.Errorf("test %s in %s (-tags %s) is not selected by any gate and has no owned exemption",
				name, inventory.pkg, inventory.tags)
		}
	}
}

// Negative controls for both rot modes, on synthetic inputs — the decision
// logic must flag an unlisted test and an empty selection. These never
// touch the real Makefile or gate status.
func TestGateRotNegativeControls(t *testing.T) {
	inventory := []string{"TestCovered", "TestForgotten"}
	matched := [][]string{{"TestCovered"}}
	uncovered := UncoveredTests(inventory, matched, nil)
	if len(uncovered) != 1 || uncovered[0] != "TestForgotten" {
		t.Fatalf("unlisted-test rot not flagged: %v", uncovered)
	}
	exempted := UncoveredTests(inventory, matched, []Exemption{
		{Kind: "test", Name: "TestForgotten", Owner: "o", Reason: "r"},
	})
	if len(exempted) != 0 {
		t.Fatalf("owned exemption not honored: %v", exempted)
	}

	makefile := "phantom-gate:\n\t$(GO) test ./internal/vm -run '^TestDoesNotExist$$' -count=1\n"
	gates := ParseGates(makefile)
	if len(gates) != 1 || gates[0].Run != "^TestDoesNotExist$" || gates[0].Target != "phantom-gate" {
		t.Fatalf("parser drifted: %+v", gates)
	}
	reachable := ReachableTargets(makefile, "check")
	orphans := UnreachableGates(gates, reachable, nil)
	if len(orphans) != 1 {
		t.Fatalf("unreachable-gate rot not flagged: %+v", orphans)
	}
}

func TestListTestsOverridesAmbientInventoryFlags(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("GOFLAGS", "-tags=runtime_v2_ownership_corpus -json")
	packages := []string{"./internal/ownershipgate"}
	pattern := "^TestRuntimeV2OwnershipCorpus$"

	defaultTests, err := ListTests(root, packages, "", pattern)
	if err != nil {
		t.Fatalf("default inventory under ambient GOFLAGS: %v", err)
	}
	if len(defaultTests) != 0 {
		t.Fatalf("ambient tag contaminated default inventory: %v", defaultTests)
	}

	taggedTests, err := ListTests(root, packages, "runtime_v2_ownership_corpus", pattern)
	if err != nil {
		t.Fatalf("tagged inventory under ambient GOFLAGS: %v", err)
	}
	if len(taggedTests) != 1 || taggedTests[0] != "TestRuntimeV2OwnershipCorpus" {
		t.Fatalf("explicit tagged inventory = %v", taggedTests)
	}
}

func TestExemptionLedgerRejectsUnownedEntries(t *testing.T) {
	if _, err := ParseExemptions("gate nameonly\n"); err == nil {
		t.Fatal("an exemption without owner and reason must be rejected")
	}
}
