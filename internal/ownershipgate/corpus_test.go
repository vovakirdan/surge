//go:build runtime_v2_ownership_corpus

package ownershipgate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"surge/internal/buildpipeline"
	"surge/internal/mir"
	"surge/internal/ownershipgate"
)

func discoverCorpus(t *testing.T, root string) ([]string, []string) {
	t.Helper()
	var (
		fixtures       []string
		inventoryDrift []string
	)
	for _, corpusRoot := range corpusRoots {
		base := filepath.Join(root, filepath.FromSlash(corpusRoot.Path))
		before := len(fixtures)
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && filepath.Ext(path) == ".sg" {
				fixtures = append(fixtures, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("discover %s: %v", corpusRoot.Path, err)
		}
		if got := len(fixtures) - before; got != corpusRoot.PinnedCount {
			inventoryDrift = append(inventoryDrift, fmt.Sprintf(
				"%s corpus count = %d, want %d; update the pinned inventory after reviewing added/removed fixtures",
				corpusRoot.Path, got, corpusRoot.PinnedCount))
		}
	}
	sort.Strings(fixtures)
	return fixtures, inventoryDrift
}

func relativeFixture(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("relative fixture path %s: %v", path, err)
	}
	return filepath.ToSlash(rel)
}

func isInvalidFixture(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "invalid" {
			return true
		}
	}
	return false
}

func loadLedgerAndDebt(t *testing.T, root string) ownershipgate.Ledger {
	t.Helper()
	ledger, err := ownershipgate.LoadLedger(filepath.Join(root, "internal", "ownershipgate", "testdata", "allowlist.json"))
	if err != nil {
		t.Fatalf("load ownership allowlist: %v", err)
	}
	debt, err := os.ReadFile(filepath.Join(root, "docs", "runtime-v2-epics", "DEBT.md"))
	if err != nil {
		t.Fatalf("read ownership debt ledger: %v", err)
	}
	if err := ownershipgate.ValidateDebtReferences(&ledger, string(debt)); err != nil {
		t.Fatalf("validate ownership debt references: %v", err)
	}
	return ledger
}

func logFailureGroups(t *testing.T, label string, failures []ownershipgate.CompileFailureKey, includeFixtures bool) {
	t.Helper()
	groups := map[string][]string{}
	for _, failure := range failures {
		groups[failure.Signature] = append(groups[failure.Signature], failure.Fixture)
	}
	signatures := make([]string, 0, len(groups))
	for signature := range groups {
		signatures = append(signatures, signature)
	}
	sort.Strings(signatures)
	for _, signature := range signatures {
		paths := groups[signature]
		sort.Strings(paths)
		if includeFixtures {
			t.Logf("%s signature=%q count=%d fixtures=%s", label, signature, len(paths), strings.Join(paths, ","))
			continue
		}
		t.Logf("%s signature=%q count=%d", label, signature, len(paths))
	}
}

func logFindingGroups(t *testing.T, findings []ownershipgate.FindingKey) {
	t.Helper()
	groups := map[string][]ownershipgate.FindingKey{}
	for _, finding := range findings {
		origin := "source"
		switch {
		case strings.Contains(finding.Function, "$poll"):
			origin = "poll"
		case strings.HasPrefix(finding.Function, "__"):
			origin = "synthetic"
		}
		key := origin + "/" + finding.ConsumingKind
		groups[key] = append(groups[key], finding)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		limit := min(5, len(group))
		representatives := make([]string, 0, limit)
		for _, finding := range group[:limit] {
			representatives = append(representatives, finding.String())
		}
		t.Logf("finding group=%s count=%d representatives=%s", key, len(group), strings.Join(representatives, " | "))
	}
}

func TestOwnershipCorpusLLVMBackendContract(t *testing.T) {
	root := ownershipRepoRoot(t)
	applyCorpusCompileEnvironment(t, root)
	positive := filepath.Join(root, "testdata", "golden", "crossing", "block02", "valid",
		"on_positive_async_crosses_fn.sg")

	emptyBackend, emptyErr := buildpipeline.Compile(context.Background(), &buildpipeline.CompileRequest{
		TargetPath:     positive,
		BaseDir:        root,
		MaxDiagnostics: 500,
		Analysis:       true,
	})
	if emptyErr == nil || !strings.Contains(emptyErr.Error(), "crossing reached HIR lowering without explicit lowering capability") {
		t.Fatalf("empty backend crossing error = %v", emptyErr)
	}
	if emptyBackend.MIR != nil {
		t.Fatal("empty backend unexpectedly produced crossing MIR")
	}

	llvm, llvmErr := buildpipeline.Compile(context.Background(), corpusCompileRequest(root, positive))
	if llvmErr != nil || llvm.MIR == nil {
		t.Fatalf("LLVM corpus request did not produce crossing MIR: mir=%v err=%v", llvm.MIR != nil, llvmErr)
	}

	unsupported := filepath.Join(root, "testdata", "golden", "crossing", "block03", "invalid",
		"_spawn_on_negative_backend_unavailable.sg")
	rejected, rejectedErr := buildpipeline.Compile(context.Background(), corpusCompileRequest(root, unsupported))
	if rejectedErr == nil || rejected.MIR != nil {
		t.Fatalf("unsupported sync-context crossing was not rejected: mir=%v err=%v", rejected.MIR != nil, rejectedErr)
	}
	entries := fatalDiagnosticEntries(root, rejected)
	if len(entries) == 0 || !strings.HasPrefix(entries[0], "FUT7019@") {
		t.Fatalf("unsupported sync-context diagnostic = %v, want FUT7019", entries)
	}
}

func TestRuntimeV2OwnershipCorpus(t *testing.T) {
	started := time.Now()
	root := ownershipRepoRoot(t)
	if err := invalidateCensusReport(root); err != nil {
		t.Fatalf("invalidate stale ownership census before run: %v", err)
	}
	applyCorpusCompileEnvironment(t, root)
	fixtures, inventoryDrift := discoverCorpus(t, root)
	inventory, err := snapshotCorpusInventory(root, fixtures)
	if err != nil {
		t.Fatalf("snapshot ownership corpus inventory: %v", err)
	}
	ledger := loadLedgerAndDebt(t, root)

	var (
		invalidFailures    []ownershipgate.CompileFailureKey
		nonInvalidFailures []ownershipgate.CompileFailureKey
		invalidNoDiag      []ownershipgate.CompileFailureKey
		normalized         []ownershipgate.FindingKey
		normalizeErrors    []string
		mirSuccesses       int
	)

	for _, fixture := range fixtures {
		rel := relativeFixture(t, root, fixture)
		result, compileErr := buildpipeline.Compile(context.Background(), corpusCompileRequest(root, fixture))
		if compileErr != nil || result.MIR == nil {
			failure := ownershipgate.CompileFailureKey{
				Fixture:   rel,
				Signature: failureSignature(root, result, compileErr),
			}
			if isInvalidFixture(rel) {
				invalidFailures = append(invalidFailures, failure)
				if len(fatalDiagnosticEntries(root, result)) == 0 {
					invalidNoDiag = append(invalidNoDiag, failure)
				}
			} else {
				nonInvalidFailures = append(nonInvalidFailures, failure)
			}
			continue
		}
		mirSuccesses++
		if result.Diagnose == nil || result.Diagnose.Sema == nil || result.Diagnose.FileSet == nil {
			normalizeErrors = append(normalizeErrors, fmt.Sprintf("%s: successful compile returned incomplete analysis", rel))
			continue
		}
		findings := mir.VerifyOwnership(result.MIR, result.Diagnose.Sema.TypeInterner, result.Diagnose.Sema)
		for i := range findings {
			key, err := ownershipgate.NormalizeFinding(root, result.Diagnose.FileSet, &findings[i])
			if err != nil {
				normalizeErrors = append(normalizeErrors,
					fmt.Sprintf("%s: %s", rel, canonicalReportError(root, err)))
				continue
			}
			normalized = append(normalized, key)
		}
	}

	normalized = ownershipgate.DedupeFindings(normalized)
	findingComparison := ownershipgate.CompareFindings(normalized, ledger.Findings)
	failureComparison := ownershipgate.CompareCompileFailures(nonInvalidFailures, ledger.CompileFailures)
	report := corpusCensusReport{
		Version:                    corpusCensusReportVersion,
		CompileProfile:             ownershipCorpusCompileProfile,
		CorpusInventory:            inventory,
		Attempted:                  len(fixtures),
		MIRSuccesses:               mirSuccesses,
		InvalidFailures:            invalidFailures,
		NonInvalidFailures:         nonInvalidFailures,
		InvalidWithoutDiagnostics:  invalidNoDiag,
		FindingNormalizationErrors: normalizeErrors,
		Findings:                   normalized,
	}
	if err := validateCensusReport(normalizeCensusReport(report)); err != nil {
		t.Fatalf("validate complete deterministic ownership census: %v", err)
	}

	t.Logf("ownership corpus run attempted=%d mir_success=%d invalid_failures=%d non_invalid_failures=%d normalized_findings=%d duration=%s",
		len(fixtures), mirSuccesses, len(invalidFailures), len(nonInvalidFailures), len(normalized), time.Since(started).Round(time.Millisecond))
	logFailureGroups(t, "invalid compile failure", invalidFailures, false)
	logFailureGroups(t, "non-invalid compile failure", nonInvalidFailures, true)
	logFindingGroups(t, normalized)

	gateErrors := append([]string(nil), inventoryDrift...)
	for _, failure := range invalidNoDiag {
		gateErrors = append(gateErrors, fmt.Sprintf(
			"invalid fixture failed without a source diagnostic: %s signature=%q", failure.Fixture, failure.Signature))
	}
	for _, message := range normalizeErrors {
		gateErrors = append(gateErrors, "ownership finding normalization failed: "+message)
	}
	for _, failure := range failureComparison.Unexpected {
		gateErrors = append(gateErrors, fmt.Sprintf(
			"unrecorded non-invalid compile failure: %s signature=%q", failure.Fixture, failure.Signature))
	}
	for _, mismatch := range failureComparison.Mismatched {
		gateErrors = append(gateErrors, fmt.Sprintf(
			"non-invalid compile failure signature changed: %s got=%q want=%q",
			mismatch.Observed.Fixture, mismatch.Observed.Signature, mismatch.Expected.Signature))
	}
	for _, failure := range failureComparison.Stale {
		gateErrors = append(gateErrors, fmt.Sprintf(
			"stale non-invalid compile failure allowance: %s signature=%q", failure.Fixture, failure.Signature))
	}
	if len(findingComparison.Unexpected) != 0 {
		for _, finding := range findingComparison.Unexpected {
			t.Logf("untriaged ownership finding: %s", finding.String())
		}
		gateErrors = append(gateErrors, fmt.Sprintf(
			"untriaged ownership findings: %d", len(findingComparison.Unexpected)))
	}
	for _, allowance := range findingComparison.Stale {
		gateErrors = append(gateErrors, fmt.Sprintf(
			"stale ownership finding allowance: %s match=%s", allowance.ID, allowance.Match))
	}
	if len(gateErrors) != 0 {
		t.Logf("final deterministic census withheld because the corpus gate found %d issue(s)", len(gateErrors))
		for _, message := range gateErrors {
			t.Error(message)
		}
		return
	}

	reportPath, err := writeCensusReport(root, report)
	if err != nil {
		t.Fatalf("write complete deterministic ownership census: %v", err)
	}
	t.Logf("complete deterministic census: %s", reportPath)
}
