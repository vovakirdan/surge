package ownershipgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func compilePolicyLedger(group CompileFailureGroup, signature string) Ledger {
	return Ledger{
		Version:              LedgerVersion,
		CompileFailureGroups: []CompileFailureGroup{group},
		CompileFailures: []CompileFailureAllowance{{
			Fixture:   "showcases/deferred.sg",
			Signature: signature,
			Group:     group.ID,
		}},
		Findings: []FindingAllowance{},
	}
}

func testCompileDebtLedger() Ledger {
	return compilePolicyLedger(CompileFailureGroup{
		ID:          "CF-005",
		Disposition: CompileFailureDebt,
		Reason:      "nested async capture lowering is incomplete",
		Debt:        "RV2-DEBT-103",
	}, "error:MIR lowering failed")
}

func TestCompileFailureGroupDispositionContracts(t *testing.T) {
	valid := []struct {
		name      string
		group     CompileFailureGroup
		signature string
	}{
		{
			name: "expected guard",
			group: CompileFailureGroup{
				ID:              "CF-001",
				Disposition:     CompileFailureExpectedGuard,
				Reason:          "the backend deliberately refuses this form",
				InvalidatedWhen: "the backend supports the form",
			},
			signature: "diagnostics:FUT7019@showcases/deferred.sg:1:1-1:2",
		},
		{
			name: "context only",
			group: CompileFailureGroup{
				ID:              "CF-002",
				Disposition:     CompileFailureContextOnly,
				Reason:          "the source requires directory mode",
				InvalidatedWhen: "the corpus compiles directory targets",
			},
			signature: "error:missing directory context",
		},
		{
			name: "debt",
			group: CompileFailureGroup{
				ID:          "CF-003",
				Disposition: CompileFailureDebt,
				Reason:      "lowering loses a captured symbol",
				Debt:        "RV2-DEBT-123",
			},
			signature: "error:unknown captured symbol",
		},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			ledger := compilePolicyLedger(tc.group, tc.signature)
			if err := ValidateLedger(&ledger); err != nil {
				t.Fatalf("valid %s group rejected: %v", tc.name, err)
			}
		})
	}

	tests := []struct {
		name string
		want string
		make func() Ledger
	}{
		{
			name: "invalid group id",
			want: "invalid compile failure group id",
			make: func() Ledger {
				ledger := testCompileDebtLedger()
				ledger.CompileFailureGroups[0].ID = "group-5"
				ledger.CompileFailures[0].Group = "group-5"
				return ledger
			},
		},
		{
			name: "duplicate group",
			want: "duplicate compile failure group id",
			make: func() Ledger {
				ledger := testCompileDebtLedger()
				ledger.CompileFailureGroups = append(ledger.CompileFailureGroups, ledger.CompileFailureGroups[0])
				return ledger
			},
		},
		{
			name: "invalid disposition",
			want: "invalid disposition",
			make: func() Ledger {
				ledger := testCompileDebtLedger()
				ledger.CompileFailureGroups[0].Disposition = "ignored"
				return ledger
			},
		},
		{
			name: "missing reason",
			want: "requires reason",
			make: func() Ledger {
				ledger := testCompileDebtLedger()
				ledger.CompileFailureGroups[0].Reason = " \t"
				return ledger
			},
		},
		{
			name: "guard missing invalidation",
			want: "requires invalidated_when",
			make: func() Ledger {
				group := CompileFailureGroup{
					ID: "CF-001", Disposition: CompileFailureExpectedGuard, Reason: "guard",
				}
				return compilePolicyLedger(group, "diagnostics:FUT7019@showcases/deferred.sg:1:1-1:2")
			},
		},
		{
			name: "context debt forbidden",
			want: "forbids debt",
			make: func() Ledger {
				group := CompileFailureGroup{
					ID: "CF-001", Disposition: CompileFailureContextOnly, Reason: "context",
					InvalidatedWhen: "context changes", Debt: "RV2-DEBT-123",
				}
				return compilePolicyLedger(group, "error:context")
			},
		},
		{
			name: "debt id required",
			want: "invalid debt id",
			make: func() Ledger {
				ledger := testCompileDebtLedger()
				ledger.CompileFailureGroups[0].Debt = ""
				return ledger
			},
		},
		{
			name: "debt invalidation forbidden",
			want: "forbids invalidated_when",
			make: func() Ledger {
				ledger := testCompileDebtLedger()
				ledger.CompileFailureGroups[0].InvalidatedWhen = "the bug is fixed"
				return ledger
			},
		},
		{
			name: "unknown group reference",
			want: "references unknown group",
			make: func() Ledger {
				ledger := testCompileDebtLedger()
				ledger.CompileFailures[0].Group = "CF-999"
				return ledger
			},
		},
		{
			name: "orphan group",
			want: "is not referenced by any fixture",
			make: func() Ledger {
				ledger := testCompileDebtLedger()
				ledger.CompileFailureGroups = append(ledger.CompileFailureGroups, CompileFailureGroup{
					ID: "CF-006", Disposition: CompileFailureDebt, Reason: "orphan", Debt: "RV2-DEBT-124",
				})
				return ledger
			},
		},
		{
			name: "guard raw error",
			want: "must use a diagnostics signature",
			make: func() Ledger {
				group := CompileFailureGroup{
					ID: "CF-001", Disposition: CompileFailureExpectedGuard, Reason: "guard",
					InvalidatedWhen: "guard is lifted",
				}
				return compilePolicyLedger(group, "error:internal failure")
			},
		},
		{
			name: "duplicate fixture",
			want: "duplicate compile failure fixture",
			make: func() Ledger {
				ledger := testCompileDebtLedger()
				ledger.CompileFailures = append(ledger.CompileFailures, ledger.CompileFailures[0])
				return ledger
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ledger := tc.make()
			if err := ValidateLedger(&ledger); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestCompileFailureDebtReferencesAreBidirectionalAndOpen(t *testing.T) {
	ledger := testCompileDebtLedger()
	if err := ValidateLedger(&ledger); err != nil {
		t.Fatalf("validate test ledger: %v", err)
	}
	valid := strings.Join([]string{
		"## Open Debt",
		"| ID | Debt | Status | Owner | Close Condition |",
		"| --- | --- | --- | --- | --- |",
		"| RV2-DEBT-103 | capture debt ownership-compile-failure:CF-005 | Open | Async lowering | captures lower |",
		"## Closed Debt",
	}, "\n")
	if err := ValidateDebtReferences(&ledger, valid); err != nil {
		t.Fatalf("valid compile debt reference rejected: %v", err)
	}

	tests := []struct {
		name     string
		markdown string
		want     string
		ledger   Ledger
	}{
		{
			name: "missing marker",
			markdown: "## Open Debt\n" +
				"| RV2-DEBT-103 | capture debt | Open | Async lowering | captures lower |",
			want:   "is missing DEBT.md marker",
			ledger: ledger,
		},
		{
			name: "duplicate marker",
			markdown: "## Open Debt\n" +
				"| RV2-DEBT-103 | ownership-compile-failure:CF-005 ownership-compile-failure:CF-005 | Open | Async lowering | captures lower |",
			want:   "has 2 DEBT.md markers",
			ledger: ledger,
		},
		{
			name: "orphan marker",
			markdown: "## Open Debt\n" +
				"| RV2-DEBT-999 | ownership-compile-failure:CF-999 | Open | Owner | close |",
			want:   "has no compile failure group",
			ledger: ledger,
		},
		{
			name: "wrong debt row",
			markdown: "## Open Debt\n" +
				"| RV2-DEBT-999 | ownership-compile-failure:CF-005 | Open | Owner | close |",
			want:   "first column is exactly RV2-DEBT-103",
			ledger: ledger,
		},
		{
			name: "closed debt row",
			markdown: "## Closed Debt\n" +
				"| RV2-DEBT-103 | ownership-compile-failure:CF-005 | Closed | Owner | evidence |",
			want:   "must be on an open debt row",
			ledger: ledger,
		},
		{
			name: "missing owner",
			markdown: "## Open Debt\n" +
				"| RV2-DEBT-103 | ownership-compile-failure:CF-005 | Open | | close |",
			want:   "requires non-empty Owner and Close Condition",
			ledger: ledger,
		},
		{
			name: "missing close condition",
			markdown: "## Open Debt\n" +
				"| RV2-DEBT-103 | ownership-compile-failure:CF-005 | Open | Owner | |",
			want:   "requires non-empty Owner and Close Condition",
			ledger: ledger,
		},
		{
			name: "marker on non-debt group",
			markdown: "## Open Debt\n" +
				"| RV2-DEBT-103 | ownership-compile-failure:CF-001 | Open | Owner | close |",
			want: "references disposition",
			ledger: compilePolicyLedger(CompileFailureGroup{
				ID:              "CF-001",
				Disposition:     CompileFailureContextOnly,
				Reason:          "directory context",
				InvalidatedWhen: "directory mode is supplied",
			}, "error:missing directory context"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateDebtReferences(&tc.ledger, tc.markdown); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("debt validation error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestRepositoryCompileFailureLedgerIsCompleteAndDebtLinked(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	root := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	ledger, err := LoadLedger(filepath.Join(root, "internal", "ownershipgate", "testdata", "allowlist.json"))
	if err != nil {
		t.Fatalf("load repository ledger: %v", err)
	}
	if len(ledger.CompileFailureGroups) != 15 || len(ledger.CompileFailures) != 81 {
		t.Fatalf("repository compile ledger has groups=%d failures=%d, want 15/81",
			len(ledger.CompileFailureGroups), len(ledger.CompileFailures))
	}
	groups := make(map[string]CompileFailureDisposition, len(ledger.CompileFailureGroups))
	for _, group := range ledger.CompileFailureGroups {
		groups[group.ID] = group.Disposition
	}
	counts := map[CompileFailureDisposition]int{}
	for _, failure := range ledger.CompileFailures {
		counts[groups[failure.Group]]++
	}
	if counts[CompileFailureExpectedGuard] != 50 || counts[CompileFailureContextOnly] != 14 ||
		counts[CompileFailureDebt] != 17 {
		t.Fatalf("repository dispositions = %+v, want expected_guard=50 context_only=14 debt=17", counts)
	}
	debtMarkdown, err := os.ReadFile(filepath.Join(root, "docs", "runtime-v2-epics", "DEBT.md"))
	if err != nil {
		t.Fatalf("read repository debt ledger: %v", err)
	}
	if err := ValidateDebtReferences(&ledger, string(debtMarkdown)); err != nil {
		t.Fatalf("repository debt references: %v", err)
	}
}
