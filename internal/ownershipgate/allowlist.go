package ownershipgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// LedgerVersion is the only ownership corpus ledger schema accepted today.
const LedgerVersion = 3

// CompileFailureDisposition records why a non-invalid fixture is outside the
// MIR-producing ownership corpus.
type CompileFailureDisposition string

// Compile-failure dispositions supported by the ownership corpus ledger.
const (
	CompileFailureExpectedGuard CompileFailureDisposition = "expected_guard"
	CompileFailureContextOnly   CompileFailureDisposition = "context_only"
	CompileFailureDebt          CompileFailureDisposition = "debt"
)

var (
	allowanceIDPattern       = regexp.MustCompile(`^OWN-\d{3}$`)
	compileGroupPattern      = regexp.MustCompile(`^CF-\d{3}$`)
	debtIDPattern            = regexp.MustCompile(`^RV2-DEBT-\d{3}$`)
	debtMarkerPattern        = regexp.MustCompile(`ownership-allowlist:([A-Za-z0-9_-]+)`)
	compileDebtMarkerPattern = regexp.MustCompile(`ownership-compile-failure:([A-Za-z0-9_-]+)`)
)

// Ledger is the versioned set of deliberate corpus compile failures and
// ownership finding exclusions. Empty arrays are meaningful and stay explicit.
type Ledger struct {
	Version              int                       `json:"version"`
	CompileFailureGroups []CompileFailureGroup     `json:"compile_failure_groups"`
	CompileFailures      []CompileFailureAllowance `json:"compile_failures"`
	Findings             []FindingAllowance        `json:"findings"`
}

// CompileFailureKey identifies one fixture that did not produce MIR and the
// stable reason class observed for it.
type CompileFailureKey struct {
	Fixture   string `json:"fixture"`
	Signature string `json:"signature"`
}

// CompileFailureGroup owns the reviewed policy shared by one or more exact
// compile-failure entries.
type CompileFailureGroup struct {
	ID              string                    `json:"id"`
	Disposition     CompileFailureDisposition `json:"disposition"`
	Reason          string                    `json:"reason"`
	InvalidatedWhen string                    `json:"invalidated_when,omitempty"`
	Debt            string                    `json:"debt,omitempty"`
}

// CompileFailureAllowance records one exact non-invalid compile failure and
// points at its reviewed policy group.
type CompileFailureAllowance struct {
	Fixture   string `json:"fixture"`
	Signature string `json:"signature"`
	Group     string `json:"group"`
}

// FindingAllowance is one exact category-3 verifier exclusion and its debt
// ownership contract.
type FindingAllowance struct {
	ID              string     `json:"id"`
	Match           FindingKey `json:"match"`
	Reason          string     `json:"reason"`
	SafeBecause     string     `json:"safe_because"`
	InvalidatedWhen string     `json:"invalidated_when"`
	Debt            string     `json:"debt"`
}

// LoadLedger decodes and validates a repository-owned ownership ledger.
func LoadLedger(path string) (Ledger, error) {
	var ledger Ledger
	data, err := os.ReadFile(path) // #nosec G304 -- repository-owned gate input
	if err != nil {
		return ledger, fmt.Errorf("read ownership ledger: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ledger); err != nil {
		return ledger, fmt.Errorf("decode ownership ledger: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ledger, fmt.Errorf("decode ownership ledger: multiple JSON values")
		}
		return ledger, fmt.Errorf("decode ownership ledger trailer: %w", err)
	}
	if err := ValidateLedger(&ledger); err != nil {
		return ledger, err
	}
	return ledger, nil
}

// ValidateLedger checks schema shape, uniqueness, and required explanations.
func ValidateLedger(ledger *Ledger) error {
	if ledger == nil {
		return fmt.Errorf("ownership ledger is nil")
	}
	if ledger.Version != LedgerVersion {
		return fmt.Errorf("ownership ledger version %d, want %d", ledger.Version, LedgerVersion)
	}
	if ledger.CompileFailureGroups == nil || ledger.CompileFailures == nil || ledger.Findings == nil {
		return fmt.Errorf("ownership ledger requires explicit compile_failure_groups, compile_failures, and findings arrays")
	}
	compileGroups, err := validateCompileFailureGroups(ledger.CompileFailureGroups)
	if err != nil {
		return err
	}
	compilePaths := make(map[string]struct{}, len(ledger.CompileFailures))
	referencedGroups := make(map[string]struct{}, len(compileGroups))
	for _, allowance := range ledger.CompileFailures {
		if err := validateRepoSGPath(allowance.Fixture); err != nil {
			return fmt.Errorf("compile failure fixture: %w", err)
		}
		if strings.TrimSpace(allowance.Signature) == "" || strings.TrimSpace(allowance.Group) == "" {
			return fmt.Errorf("compile failure %q requires signature and group", allowance.Fixture)
		}
		group, exists := compileGroups[allowance.Group]
		if !exists {
			return fmt.Errorf("compile failure %q references unknown group %q", allowance.Fixture, allowance.Group)
		}
		if group.Disposition == CompileFailureExpectedGuard && !strings.HasPrefix(allowance.Signature, "diagnostics:") {
			return fmt.Errorf("expected-guard compile failure %q must use a diagnostics signature", allowance.Fixture)
		}
		if _, exists := compilePaths[allowance.Fixture]; exists {
			return fmt.Errorf("duplicate compile failure fixture %q", allowance.Fixture)
		}
		compilePaths[allowance.Fixture] = struct{}{}
		referencedGroups[allowance.Group] = struct{}{}
	}
	for _, group := range ledger.CompileFailureGroups {
		if _, ok := referencedGroups[group.ID]; !ok {
			return fmt.Errorf("compile failure group %s is not referenced by any fixture", group.ID)
		}
	}

	ids := make(map[string]struct{}, len(ledger.Findings))
	matches := make(map[FindingKey]struct{}, len(ledger.Findings))
	for i := range ledger.Findings {
		allowance := &ledger.Findings[i]
		if !allowanceIDPattern.MatchString(allowance.ID) {
			return fmt.Errorf("invalid ownership allowance id %q", allowance.ID)
		}
		if _, exists := ids[allowance.ID]; exists {
			return fmt.Errorf("duplicate ownership allowance id %q", allowance.ID)
		}
		ids[allowance.ID] = struct{}{}
		if err := validateFindingKey(&allowance.Match); err != nil {
			return fmt.Errorf("ownership allowance %s: %w", allowance.ID, err)
		}
		if _, exists := matches[allowance.Match]; exists {
			return fmt.Errorf("duplicate ownership allowance match for %s", allowance.Match)
		}
		matches[allowance.Match] = struct{}{}
		if strings.TrimSpace(allowance.Reason) == "" || strings.TrimSpace(allowance.SafeBecause) == "" ||
			strings.TrimSpace(allowance.InvalidatedWhen) == "" {
			return fmt.Errorf("ownership allowance %s requires reason, safe_because, and invalidated_when", allowance.ID)
		}
		if !debtIDPattern.MatchString(allowance.Debt) {
			return fmt.Errorf("ownership allowance %s has invalid debt id %q", allowance.ID, allowance.Debt)
		}
	}
	return nil
}

func validateCompileFailureGroups(groups []CompileFailureGroup) (map[string]CompileFailureGroup, error) {
	byID := make(map[string]CompileFailureGroup, len(groups))
	for _, group := range groups {
		if !compileGroupPattern.MatchString(group.ID) {
			return nil, fmt.Errorf("invalid compile failure group id %q", group.ID)
		}
		if _, exists := byID[group.ID]; exists {
			return nil, fmt.Errorf("duplicate compile failure group id %q", group.ID)
		}
		if strings.TrimSpace(group.Reason) == "" {
			return nil, fmt.Errorf("compile failure group %s requires reason", group.ID)
		}
		switch group.Disposition {
		case CompileFailureExpectedGuard, CompileFailureContextOnly:
			if strings.TrimSpace(group.InvalidatedWhen) == "" {
				return nil, fmt.Errorf("compile failure group %s disposition %q requires invalidated_when",
					group.ID, group.Disposition)
			}
			if group.Debt != "" {
				return nil, fmt.Errorf("compile failure group %s disposition %q forbids debt",
					group.ID, group.Disposition)
			}
		case CompileFailureDebt:
			if !debtIDPattern.MatchString(group.Debt) {
				return nil, fmt.Errorf("compile failure group %s has invalid debt id %q", group.ID, group.Debt)
			}
			if group.InvalidatedWhen != "" {
				return nil, fmt.Errorf("compile failure group %s disposition %q forbids invalidated_when; use the DEBT.md close condition",
					group.ID, group.Disposition)
			}
		default:
			return nil, fmt.Errorf("compile failure group %s has invalid disposition %q", group.ID, group.Disposition)
		}
		byID[group.ID] = group
	}
	return byID, nil
}

func validateFindingKey(key *FindingKey) error {
	if err := validateRepoSGPath(key.Source); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if strings.TrimSpace(key.Function) == "" || strings.TrimSpace(key.ConsumingKind) == "" ||
		strings.TrimSpace(key.ConsumingPosition) == "" || strings.TrimSpace(key.DefSite) == "" ||
		strings.TrimSpace(key.ConsumingSite) == "" {
		return fmt.Errorf("function, consuming_kind, consuming_position, def_site, and consuming_site are required")
	}
	if key.StartLine == 0 || key.StartColumn == 0 || key.EndLine == 0 || key.EndColumn == 0 {
		return fmt.Errorf("source span must use 1-based positions")
	}
	return nil
}

func validateRepoSGPath(path string) error {
	if path == "" || filepath.IsAbs(path) {
		return fmt.Errorf("path %q must be repository-relative", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean != path || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path %q is not normalized", path)
	}
	if filepath.Ext(path) != ".sg" {
		return fmt.Errorf("path %q is not a .sg fixture", path)
	}
	return nil
}

// ValidateDebtReferences enforces both sides of every category-3 exclusion
// and every compile-failure debt group.
func ValidateDebtReferences(ledger *Ledger, debtMarkdown string) error {
	if ledger == nil {
		return fmt.Errorf("ownership ledger is nil")
	}
	if err := validateFindingDebtReferences(ledger, debtMarkdown); err != nil {
		return err
	}
	return validateCompileFailureDebtReferences(ledger, debtMarkdown)
}

func validateFindingDebtReferences(ledger *Ledger, debtMarkdown string) error {
	byID := make(map[string]int, len(ledger.Findings))
	for i := range ledger.Findings {
		byID[ledger.Findings[i].ID] = i
	}
	seen := make(map[string]int, len(ledger.Findings))
	for _, line := range strings.Split(debtMarkdown, "\n") {
		for _, match := range debtMarkerPattern.FindAllStringSubmatch(line, -1) {
			id := match[1]
			allowanceIndex, ok := byID[id]
			if !ok {
				return fmt.Errorf("DEBT.md marker ownership-allowlist:%s has no ledger entry", id)
			}
			allowance := &ledger.Findings[allowanceIndex]
			firstColumn, ok := markdownFirstColumn(line)
			if !ok || firstColumn != allowance.Debt {
				return fmt.Errorf("DEBT.md marker ownership-allowlist:%s must be on a row whose first column is exactly %s",
					id, allowance.Debt)
			}
			seen[id]++
		}
	}
	for i := range ledger.Findings {
		allowance := &ledger.Findings[i]
		switch seen[allowance.ID] {
		case 0:
			return fmt.Errorf("ownership allowance %s is missing DEBT.md marker ownership-allowlist:%s", allowance.ID, allowance.ID)
		case 1:
		default:
			return fmt.Errorf("ownership allowance %s has %d DEBT.md markers", allowance.ID, seen[allowance.ID])
		}
	}
	return nil
}

func validateCompileFailureDebtReferences(ledger *Ledger, debtMarkdown string) error {
	byID := make(map[string]CompileFailureGroup, len(ledger.CompileFailureGroups))
	for _, group := range ledger.CompileFailureGroups {
		byID[group.ID] = group
	}
	seen := make(map[string]int, len(byID))
	section := ""
	for _, line := range strings.Split(debtMarkdown, "\n") {
		switch strings.TrimSpace(line) {
		case "## Open Debt":
			section = "open"
		case "## Closed Debt":
			section = "closed"
		}
		for _, match := range compileDebtMarkerPattern.FindAllStringSubmatch(line, -1) {
			id := match[1]
			group, ok := byID[id]
			if !ok {
				return fmt.Errorf("DEBT.md marker ownership-compile-failure:%s has no compile failure group", id)
			}
			if group.Disposition != CompileFailureDebt {
				return fmt.Errorf("DEBT.md marker ownership-compile-failure:%s references disposition %q, want %q",
					id, group.Disposition, CompileFailureDebt)
			}
			firstColumn, ok := markdownFirstColumn(line)
			if !ok || firstColumn != group.Debt {
				return fmt.Errorf("DEBT.md marker ownership-compile-failure:%s must be on a row whose first column is exactly %s",
					id, group.Debt)
			}
			if section != "open" {
				return fmt.Errorf("DEBT.md marker ownership-compile-failure:%s must be on an open debt row", id)
			}
			columns, ok := markdownColumns(line)
			if !ok || len(columns) < 5 {
				return fmt.Errorf("DEBT.md marker ownership-compile-failure:%s must be on a five-column open debt row", id)
			}
			owner := strings.TrimSpace(columns[len(columns)-2])
			closeCondition := strings.TrimSpace(columns[len(columns)-1])
			if owner == "" || closeCondition == "" {
				return fmt.Errorf("DEBT.md marker ownership-compile-failure:%s requires non-empty Owner and Close Condition", id)
			}
			seen[id]++
		}
	}
	for _, group := range ledger.CompileFailureGroups {
		if group.Disposition != CompileFailureDebt {
			continue
		}
		switch seen[group.ID] {
		case 0:
			return fmt.Errorf("compile failure group %s is missing DEBT.md marker ownership-compile-failure:%s",
				group.ID, group.ID)
		case 1:
		default:
			return fmt.Errorf("compile failure group %s has %d DEBT.md markers", group.ID, seen[group.ID])
		}
	}
	return nil
}

func markdownFirstColumn(line string) (string, bool) {
	columns, ok := markdownColumns(line)
	if !ok || len(columns) == 0 || columns[0] == "" {
		return "", false
	}
	return columns[0], true
}

func markdownColumns(line string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.Contains(strings.TrimPrefix(line, "|"), "|") {
		return nil, false
	}
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

// FindingComparison separates unallowlisted observations from dead allowances.
type FindingComparison struct {
	Unexpected []FindingKey
	Stale      []FindingAllowance
}

// CompareFindings applies exact matches to an already deduplicated observation
// set and reports stale entries.
func CompareFindings(observed []FindingKey, allowed []FindingAllowance) FindingComparison {
	byMatch := make(map[FindingKey]int, len(allowed))
	for i := range allowed {
		byMatch[allowed[i].Match] = i
	}
	matched := make(map[string]struct{}, len(allowed))
	var comparison FindingComparison
	for i := range observed {
		finding := &observed[i]
		allowanceIndex, ok := byMatch[*finding]
		if !ok {
			comparison.Unexpected = append(comparison.Unexpected, *finding)
			continue
		}
		matched[allowed[allowanceIndex].ID] = struct{}{}
	}
	for i := range allowed {
		allowance := &allowed[i]
		if _, ok := matched[allowance.ID]; !ok {
			comparison.Stale = append(comparison.Stale, *allowance)
		}
	}
	sort.Slice(comparison.Stale, func(i, j int) bool {
		return comparison.Stale[i].ID < comparison.Stale[j].ID
	})
	return comparison
}

// CompileFailureMismatch pairs one changed observed signature with its ledger
// expectation.
type CompileFailureMismatch struct {
	Expected CompileFailureAllowance
	Observed CompileFailureKey
}

// CompileFailureComparison separates new, changed, and stale compile failures.
type CompileFailureComparison struct {
	Unexpected []CompileFailureKey
	Mismatched []CompileFailureMismatch
	Stale      []CompileFailureAllowance
}

// CompareCompileFailures requires exact fixture and signature matches.
func CompareCompileFailures(observed []CompileFailureKey, allowed []CompileFailureAllowance) CompileFailureComparison {
	byFixture := make(map[string]CompileFailureAllowance, len(allowed))
	for _, allowance := range allowed {
		byFixture[allowance.Fixture] = allowance
	}
	seen := make(map[string]struct{}, len(observed))
	var comparison CompileFailureComparison
	for _, failure := range observed {
		allowance, ok := byFixture[failure.Fixture]
		if !ok {
			comparison.Unexpected = append(comparison.Unexpected, failure)
			continue
		}
		seen[failure.Fixture] = struct{}{}
		if allowance.Signature != failure.Signature {
			comparison.Mismatched = append(comparison.Mismatched, CompileFailureMismatch{
				Expected: allowance,
				Observed: failure,
			})
		}
	}
	for _, allowance := range allowed {
		if _, ok := seen[allowance.Fixture]; !ok {
			comparison.Stale = append(comparison.Stale, allowance)
		}
	}
	sortCompileFailureComparison(&comparison)
	return comparison
}

func sortCompileFailureComparison(comparison *CompileFailureComparison) {
	sort.Slice(comparison.Unexpected, func(i, j int) bool {
		return comparison.Unexpected[i].Fixture < comparison.Unexpected[j].Fixture
	})
	sort.Slice(comparison.Mismatched, func(i, j int) bool {
		return comparison.Mismatched[i].Observed.Fixture < comparison.Mismatched[j].Observed.Fixture
	})
	sort.Slice(comparison.Stale, func(i, j int) bool {
		return comparison.Stale[i].Fixture < comparison.Stale[j].Fixture
	})
}
