package ownershipgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/source"
)

func testFindingKey() FindingKey {
	return FindingKey{
		Source:            "pkg/main.sg",
		Function:          "consume",
		LocalID:           2,
		LocalName:         "tmp_value",
		ConsumingKind:     "return",
		ConsumingPosition: "value",
		StartLine:         3,
		StartColumn:       4,
		EndLine:           3,
		EndColumn:         9,
		DefSite:           "bb1#0",
		ConsumingSite:     "bb2#term",
	}
}

func testAllowance() FindingAllowance {
	return FindingAllowance{
		ID:              "OWN-001",
		Match:           testFindingKey(),
		Reason:          "the verifier cannot observe the runtime handoff",
		SafeBecause:     "the runtime consumes exactly one reference",
		InvalidatedWhen: "the runtime handoff contract changes",
		Debt:            "RV2-DEBT-123",
	}
}

func testLedger() Ledger {
	return Ledger{
		Version: LedgerVersion,
		CompileFailureGroups: []CompileFailureGroup{{
			ID:              "CF-001",
			Disposition:     CompileFailureContextOnly,
			Reason:          "feature is postponed",
			InvalidatedWhen: "the feature is implemented",
		}},
		CompileFailures: []CompileFailureAllowance{{
			Fixture: "showcases/deferred.sg", Signature: "error:deferred", Group: "CF-001",
		}},
		Findings: []FindingAllowance{testAllowance()},
	}
}

func TestNormalizeFindingUsesTrueSourceAndDedupeIsDeterministic(t *testing.T) {
	root := t.TempDir()
	files := source.NewFileSetWithBase(root)
	fileID := files.Add(filepath.Join(root, "lib", "actual.sg"), []byte("first\nsecond value\n"), 0)
	raw := mir.OwnershipFinding{
		Function:          "read",
		Local:             4,
		LocalName:         "payload",
		DefSite:           "bb0#1",
		ConsumingSite:     "bb1#term",
		ConsumingPosition: "value",
		ConsumingKind:     mir.OwnershipSinkReturn,
		Span:              source.Span{File: fileID, Start: 6, End: 12},
	}

	key, err := NormalizeFinding(root, files, &raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if key.Source != "lib/actual.sg" {
		t.Fatalf("source = %q, want true source path", key.Source)
	}
	if key.ConsumingPosition != "value" {
		t.Fatalf("consuming position = %q, want value", key.ConsumingPosition)
	}
	if key.StartLine != 2 || key.StartColumn != 1 || key.EndLine != 2 || key.EndColumn != 7 {
		t.Fatalf("resolved span = %d:%d-%d:%d", key.StartLine, key.StartColumn, key.EndLine, key.EndColumn)
	}

	other := key
	other.Source = "a/earlier.sg"
	got := DedupeFindings([]FindingKey{key, other, key})
	if len(got) != 2 || got[0] != other || got[1] != key {
		t.Fatalf("dedupe order = %+v", got)
	}
}

func TestNormalizeFindingRejectsUnknownAndOutsideSources(t *testing.T) {
	root := t.TempDir()
	files := source.NewFileSetWithBase(root)
	if _, err := NormalizeFinding(root, files, nil); err == nil || !strings.Contains(err.Error(), "missing finding") {
		t.Fatalf("missing finding error = %v", err)
	}
	rootFileID := files.Add(filepath.Join(root, "root.sg"), []byte("x"), 0)
	missing := mir.OwnershipFinding{}
	if _, err := NormalizeFinding(root, files, &missing); err == nil || !strings.Contains(err.Error(), "missing source span") {
		t.Fatalf("missing source span error = %v", err)
	}
	raw := mir.OwnershipFinding{Span: source.Span{File: 7}}
	if _, err := NormalizeFinding(root, files, &raw); err == nil || !strings.Contains(err.Error(), "unknown file id") {
		t.Fatalf("unknown file error = %v", err)
	}

	outside := t.TempDir()
	fileID := files.Add(filepath.Join(outside, "outside.sg"), []byte("x"), 0)
	raw.Span = source.Span{File: fileID, Start: 0, End: 1}
	if _, err := NormalizeFinding(root, files, &raw); err == nil || !strings.Contains(err.Error(), "outside repository") {
		t.Fatalf("outside source error = %v", err)
	}

	missingPosition := mir.OwnershipFinding{Span: source.Span{File: rootFileID, Start: 0, End: 1}}
	if _, err := NormalizeFinding(root, files, &missingPosition); err == nil || !strings.Contains(err.Error(), "missing consuming position") {
		t.Fatalf("missing consuming position error = %v", err)
	}
}

func TestCompareFindingsRequiresExactMatchAndFlagsStale(t *testing.T) {
	allowance := testAllowance()
	adjacent := allowance.Match
	adjacent.ConsumingSite = "bb3#term"

	observed := DedupeFindings([]FindingKey{allowance.Match, adjacent, allowance.Match})
	comparison := CompareFindings(observed, []FindingAllowance{allowance})
	if len(comparison.Unexpected) != 1 || comparison.Unexpected[0] != adjacent {
		t.Fatalf("unexpected findings = %+v", comparison.Unexpected)
	}
	if len(comparison.Stale) != 0 {
		t.Fatalf("matched allowance reported stale: %+v", comparison.Stale)
	}

	comparison = CompareFindings(nil, []FindingAllowance{allowance})
	if len(comparison.Stale) != 1 || comparison.Stale[0].ID != allowance.ID {
		t.Fatalf("stale allowances = %+v", comparison.Stale)
	}
}

func TestFindingPositionPreventsOneAllowanceFromMaskingAnotherOperand(t *testing.T) {
	first := testFindingKey()
	first.ConsumingKind = "call_arg"
	first.ConsumingPosition = "arg[0]"
	first.DefSite = "use"
	first.ConsumingSite = "bb2#0"
	second := first
	second.ConsumingPosition = "arg[1]"

	observed := DedupeFindings([]FindingKey{second, first, first})
	if len(observed) != 2 || observed[0] != first || observed[1] != second {
		t.Fatalf("position-distinct findings collapsed or sorted unstably: %+v", observed)
	}
	allowance := testAllowance()
	allowance.Match = first
	comparison := CompareFindings(observed, []FindingAllowance{allowance})
	if len(comparison.Unexpected) != 1 || comparison.Unexpected[0] != second {
		t.Fatalf("allowing arg[0] masked arg[1]: %+v", comparison.Unexpected)
	}
	if len(comparison.Stale) != 0 {
		t.Fatalf("matched arg[0] allowance reported stale: %+v", comparison.Stale)
	}
}

func TestFindingDefinitionPreventsOneAllowanceFromMaskingAnotherRoot(t *testing.T) {
	first := testFindingKey()
	first.ConsumingKind = "drop"
	first.ConsumingPosition = "place"
	first.DefSite = "bb1#0"
	first.ConsumingSite = "bb3#0"
	second := first
	second.DefSite = "bb2#0"

	observed := DedupeFindings([]FindingKey{second, first})
	allowance := testAllowance()
	allowance.Match = first
	comparison := CompareFindings(observed, []FindingAllowance{allowance})
	if len(comparison.Unexpected) != 1 || comparison.Unexpected[0] != second {
		t.Fatalf("allowing first blame root masked second: %+v", comparison.Unexpected)
	}
	if len(comparison.Stale) != 0 {
		t.Fatalf("matched first-root allowance reported stale: %+v", comparison.Stale)
	}
}

func TestCompareCompileFailuresDistinguishesUnexpectedChangedAndStale(t *testing.T) {
	allowed := []CompileFailureAllowance{
		{Fixture: "showcases/a.sg", Signature: "diagnostics:SEM3001", Group: "CF-001"},
		{Fixture: "showcases/stale.sg", Signature: "error:old", Group: "CF-002"},
	}
	observed := []CompileFailureKey{
		{Fixture: "showcases/a.sg", Signature: "diagnostics:SEM3002"},
		{Fixture: "showcases/new.sg", Signature: "error:new"},
	}
	comparison := CompareCompileFailures(observed, allowed)
	if len(comparison.Unexpected) != 1 || comparison.Unexpected[0].Fixture != "showcases/new.sg" {
		t.Fatalf("unexpected failures = %+v", comparison.Unexpected)
	}
	if len(comparison.Mismatched) != 1 || comparison.Mismatched[0].Observed.Signature != "diagnostics:SEM3002" {
		t.Fatalf("mismatched failures = %+v", comparison.Mismatched)
	}
	if len(comparison.Stale) != 1 || comparison.Stale[0].Fixture != "showcases/stale.sg" {
		t.Fatalf("stale failures = %+v", comparison.Stale)
	}
}

func TestLedgerValidationAndDebtReferencesAreBidirectional(t *testing.T) {
	ledger := testLedger()
	if err := ValidateLedger(&ledger); err != nil {
		t.Fatalf("validate ledger: %v", err)
	}
	debt := "| RV2-DEBT-123 | exact exclusion ownership-allowlist:OWN-001 | Open | Epic 25 | remove exclusion |"
	if err := ValidateDebtReferences(&ledger, debt); err != nil {
		t.Fatalf("validate debt reference: %v", err)
	}

	if err := ValidateDebtReferences(&ledger, "| RV2-DEBT-123 | no marker |"); err == nil || !strings.Contains(err.Error(), "missing DEBT.md marker") {
		t.Fatalf("missing marker error = %v", err)
	}
	if err := ValidateDebtReferences(&Ledger{Version: LedgerVersion}, debt); err == nil || !strings.Contains(err.Error(), "has no ledger entry") {
		t.Fatalf("reverse marker error = %v", err)
	}
	wrongRow := "| RV2-DEBT-999 | ownership-allowlist:OWN-001 |"
	if err := ValidateDebtReferences(&ledger, wrongRow); err == nil || !strings.Contains(err.Error(), "first column is exactly") {
		t.Fatalf("wrong debt row error = %v", err)
	}
	hiddenDebt := "| RV2-DEBT-999 | mentions RV2-DEBT-123 ownership-allowlist:OWN-001 |"
	if err := ValidateDebtReferences(&ledger, hiddenDebt); err == nil || !strings.Contains(err.Error(), "first column is exactly") {
		t.Fatalf("debt id outside first column error = %v", err)
	}
}

func TestLedgerValidationRejectsWhitespaceRequiredText(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Ledger)
	}{
		{name: "compile signature", mutate: func(ledger *Ledger) { ledger.CompileFailures[0].Signature = " \t" }},
		{name: "compile group", mutate: func(ledger *Ledger) { ledger.CompileFailures[0].Group = " \n" }},
		{name: "compile group reason", mutate: func(ledger *Ledger) { ledger.CompileFailureGroups[0].Reason = " \n" }},
		{name: "compile group invalidation", mutate: func(ledger *Ledger) { ledger.CompileFailureGroups[0].InvalidatedWhen = " \n" }},
		{name: "finding reason", mutate: func(ledger *Ledger) { ledger.Findings[0].Reason = " \t" }},
		{name: "finding safety", mutate: func(ledger *Ledger) { ledger.Findings[0].SafeBecause = " \n" }},
		{name: "finding invalidation", mutate: func(ledger *Ledger) { ledger.Findings[0].InvalidatedWhen = " \t" }},
		{name: "finding consuming position", mutate: func(ledger *Ledger) { ledger.Findings[0].Match.ConsumingPosition = " \t" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ledger := testLedger()
			test.mutate(&ledger)
			if err := ValidateLedger(&ledger); err == nil {
				t.Fatal("whitespace-only required text was accepted")
			}
		})
	}
}

func TestLoadLedgerRejectsUnknownFieldsAndWrongVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.json")
	if err := os.WriteFile(path, []byte(`{"version":3,"compile_failure_groups":[],"compile_failures":[],"findings":[],"extra":true}`), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
	if _, err := LoadLedger(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":3,"compile_failure_groups":[{"id":"CF-001","disposition":"context_only","reason":"context","invalidated_when":"context changes","extra":true}],"compile_failures":[],"findings":[]}`), 0o600); err != nil {
		t.Fatalf("write nested unknown field ledger: %v", err)
	}
	if _, err := LoadLedger(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("nested unknown field error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"compile_failure_groups":[],"compile_failures":[],"findings":[]}`), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
	if _, err := LoadLedger(path); err == nil || !strings.Contains(err.Error(), "version 2, want 3") {
		t.Fatalf("wrong version error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":3}`), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
	if _, err := LoadLedger(path); err == nil || !strings.Contains(err.Error(), "explicit compile_failure_groups, compile_failures, and findings arrays") {
		t.Fatalf("missing arrays error = %v", err)
	}
}
