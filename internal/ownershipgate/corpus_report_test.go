//go:build runtime_v2_ownership_corpus

package ownershipgate_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"surge/internal/buildpipeline"
	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/ownershipgate"
	"surge/internal/source"
)

func diagnosticSource(root string, files *source.FileSet, span source.Span) string {
	if files == nil || !files.HasFile(span.File) {
		return fmt.Sprintf("<unknown-file-%d>", span.File)
	}
	file := files.Get(span.File)
	if file == nil || file.Path == "" {
		return fmt.Sprintf("<unnamed-file-%d>", span.File)
	}
	path := file.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(files.BaseDir(), path)
	}
	absRoot, rootErr := filepath.Abs(root)
	absPath, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil {
		return "<unresolved>/" + filepath.Base(path)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "<outside-repo>/" + filepath.Base(absPath)
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

func diagnosticSpanLocation(files *source.FileSet, span source.Span) string {
	if files == nil || !files.HasFile(span.File) {
		return fmt.Sprintf("<invalid-span-file-%d-%d-%d>", span.File, span.Start, span.End)
	}
	file := files.Get(span.File)
	if file == nil || span.Start > span.End || uint64(span.End) > uint64(len(file.Content)) {
		return fmt.Sprintf("<invalid-span-file-%d-%d-%d>", span.File, span.Start, span.End)
	}
	start, end := files.Resolve(span)
	return fmt.Sprintf("%d:%d-%d:%d", start.Line, start.Col, end.Line, end.Col)
}

func fatalDiagnosticEntries(root string, result buildpipeline.CompileResult) []string {
	if result.Diagnose == nil || result.Diagnose.Bag == nil {
		return nil
	}
	var entries []string
	for _, diagnostic := range result.Diagnose.Bag.Items() {
		if diagnostic == nil || diagnostic.Severity < diag.SevError {
			continue
		}
		entries = append(entries, fmt.Sprintf("%s@%s:%s",
			diagnostic.Code.ID(), diagnosticSource(root, result.Diagnose.FileSet, diagnostic.Primary),
			diagnosticSpanLocation(result.Diagnose.FileSet, diagnostic.Primary)))
	}
	sort.Strings(entries)
	return entries
}

func canonicalReportError(root string, err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	roots := []string{root}
	if absolute, absErr := filepath.Abs(root); absErr == nil && absolute != root {
		roots = append(roots, absolute)
	}
	for _, candidate := range roots {
		if candidate == "" {
			continue
		}
		message = strings.ReplaceAll(message, candidate, "<repo>")
		message = strings.ReplaceAll(filepath.ToSlash(message), filepath.ToSlash(candidate), "<repo>")
	}
	return filepath.ToSlash(message)
}

func failureSignature(root string, result buildpipeline.CompileResult, compileErr error) string {
	if entries := fatalDiagnosticEntries(root, result); len(entries) != 0 {
		return "diagnostics:" + strings.Join(entries, ",")
	}
	if compileErr == nil {
		return "error:missing MIR"
	}
	return "error:" + canonicalReportError(root, compileErr)
}

type corpusCensusReport struct {
	Version                    int                               `json:"version"`
	CompileProfile             corpusCompileProfile              `json:"compile_profile"`
	CorpusInventory            corpusInventory                   `json:"corpus_inventory"`
	Attempted                  int                               `json:"attempted"`
	MIRSuccesses               int                               `json:"mir_successes"`
	InvalidFailures            []ownershipgate.CompileFailureKey `json:"invalid_failures"`
	NonInvalidFailures         []ownershipgate.CompileFailureKey `json:"non_invalid_failures"`
	InvalidWithoutDiagnostics  []ownershipgate.CompileFailureKey `json:"invalid_without_diagnostics"`
	FindingNormalizationErrors []string                          `json:"finding_normalization_errors"`
	Findings                   []ownershipgate.FindingKey        `json:"findings"`
}

func normalizeCensusReport(report corpusCensusReport) corpusCensusReport {
	sortFailures := func(failures []ownershipgate.CompileFailureKey) []ownershipgate.CompileFailureKey {
		out := append([]ownershipgate.CompileFailureKey(nil), failures...)
		sort.Slice(out, func(i, j int) bool {
			if out[i].Fixture != out[j].Fixture {
				return out[i].Fixture < out[j].Fixture
			}
			return out[i].Signature < out[j].Signature
		})
		return out
	}
	report.InvalidFailures = sortFailures(report.InvalidFailures)
	report.NonInvalidFailures = sortFailures(report.NonInvalidFailures)
	report.InvalidWithoutDiagnostics = sortFailures(report.InvalidWithoutDiagnostics)
	report.CorpusInventory.Roots = append([]corpusRootSpec(nil), report.CorpusInventory.Roots...)
	sort.Slice(report.CorpusInventory.Roots, func(i, j int) bool {
		return report.CorpusInventory.Roots[i].Path < report.CorpusInventory.Roots[j].Path
	})
	report.FindingNormalizationErrors = append([]string(nil), report.FindingNormalizationErrors...)
	sort.Strings(report.FindingNormalizationErrors)
	report.Findings = ownershipgate.DedupeFindings(report.Findings)
	return report
}

func encodeCensusReport(report corpusCensusReport) ([]byte, error) {
	report = normalizeCensusReport(report)
	if err := validateCensusReport(report); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode ownership census: %w", err)
	}
	return append(data, '\n'), nil
}

func validateCensusReport(report corpusCensusReport) error {
	if report.Version != corpusCensusReportVersion {
		return fmt.Errorf("ownership census version %d, want %d", report.Version, corpusCensusReportVersion)
	}
	if report.CompileProfile != ownershipCorpusCompileProfile {
		return fmt.Errorf("ownership census compile profile differs from the canonical corpus profile")
	}
	if report.Attempted < 0 {
		return fmt.Errorf("ownership census attempted must be non-negative, got %d", report.Attempted)
	}
	if report.CorpusInventory.PathCount != report.Attempted {
		return fmt.Errorf("ownership census inventory path_count %d does not match attempted %d",
			report.CorpusInventory.PathCount, report.Attempted)
	}
	if report.MIRSuccesses < 0 || report.MIRSuccesses > report.Attempted {
		return fmt.Errorf("ownership census mir_successes %d is outside [0, %d]",
			report.MIRSuccesses, report.Attempted)
	}
	classified := report.MIRSuccesses + len(report.InvalidFailures) + len(report.NonInvalidFailures)
	if classified != report.Attempted {
		return fmt.Errorf("ownership census classified fixture count %d does not match attempted %d",
			classified, report.Attempted)
	}
	invalidFailures := make(map[ownershipgate.CompileFailureKey]int, len(report.InvalidFailures))
	for _, failure := range report.InvalidFailures {
		invalidFailures[failure]++
	}
	for _, failure := range report.InvalidWithoutDiagnostics {
		if invalidFailures[failure] == 0 {
			return fmt.Errorf("ownership census invalid_without_diagnostics entry %+v is not present in invalid_failures", failure)
		}
		invalidFailures[failure]--
	}
	if report.CorpusInventory.DigestAlgorithm != corpusInventoryDigestV1 {
		return fmt.Errorf("ownership census digest algorithm %q, want %q",
			report.CorpusInventory.DigestAlgorithm, corpusInventoryDigestV1)
	}
	digest, err := hex.DecodeString(report.CorpusInventory.PathDigest)
	if err != nil || len(digest) != sha256.Size || strings.ToLower(report.CorpusInventory.PathDigest) != report.CorpusInventory.PathDigest {
		return fmt.Errorf("ownership census path digest must be a 32-byte lowercase hex SHA-256")
	}
	canonicalRoots := append([]corpusRootSpec(nil), corpusRoots...)
	sort.Slice(canonicalRoots, func(i, j int) bool { return canonicalRoots[i].Path < canonicalRoots[j].Path })
	if len(report.CorpusInventory.Roots) != len(canonicalRoots) {
		return fmt.Errorf("ownership census roots count %d, want %d", len(report.CorpusInventory.Roots), len(canonicalRoots))
	}
	for i := range canonicalRoots {
		if report.CorpusInventory.Roots[i] != canonicalRoots[i] {
			return fmt.Errorf("ownership census root %d = %+v, want %+v",
				i, report.CorpusInventory.Roots[i], canonicalRoots[i])
		}
	}
	return nil
}

func jsonObjectKeys(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode JSON object: %v", err)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func TestOwnershipCorpusCensusReportContract(t *testing.T) {
	late := ownershipgate.FindingKey{Source: "z.sg", Function: "z", ConsumingPosition: "value"}
	early := ownershipgate.FindingKey{Source: "a.sg", Function: "a", ConsumingPosition: "value"}
	report := corpusCensusReport{
		Version:        corpusCensusReportVersion,
		CompileProfile: ownershipCorpusCompileProfile,
		CorpusInventory: corpusInventory{
			Roots:           append([]corpusRootSpec(nil), corpusRoots...),
			PathCount:       3,
			DigestAlgorithm: corpusInventoryDigestV1,
			PathDigest:      strings.Repeat("a", 64),
		},
		Attempted:    3,
		MIRSuccesses: 1,
		InvalidFailures: []ownershipgate.CompileFailureKey{
			{Fixture: "z.sg", Signature: "diagnostics:Z"},
			{Fixture: "a.sg", Signature: "diagnostics:A"},
		},
		FindingNormalizationErrors: []string{"z", "a"},
		Findings:                   []ownershipgate.FindingKey{late, early, late},
	}
	for left, right := 0, len(report.CorpusInventory.Roots)-1; left < right; left, right = left+1, right-1 {
		report.CorpusInventory.Roots[left], report.CorpusInventory.Roots[right] =
			report.CorpusInventory.Roots[right], report.CorpusInventory.Roots[left]
	}
	first, err := encodeCensusReport(report)
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	second, err := encodeCensusReport(report)
	if err != nil {
		t.Fatalf("encode report again: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("identical census reports did not encode deterministically")
	}

	var schema map[string]json.RawMessage
	if err := json.Unmarshal(first, &schema); err != nil {
		t.Fatalf("decode report schema: %v", err)
	}
	keys := make([]string, 0, len(schema))
	for key := range schema {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	wantKeys := "attempted,compile_profile,corpus_inventory,finding_normalization_errors,findings,invalid_failures,invalid_without_diagnostics,mir_successes,non_invalid_failures,version"
	if strings.Join(keys, ",") != wantKeys {
		t.Fatalf("report schema keys = %s, want %s", strings.Join(keys, ","), wantKeys)
	}
	if got := jsonObjectKeys(t, schema["compile_profile"]); got !=
		"allow_diagnostics_error,analysis,backend,base_dir_policy,crossing_forms_override,dir_info_enabled,max_diagnostics,root_kind,standard_library_path_policy" {
		t.Fatalf("compile profile schema keys = %s", got)
	}
	if got := jsonObjectKeys(t, schema["corpus_inventory"]); got !=
		"digest_algorithm,path_count,path_digest,roots" {
		t.Fatalf("corpus inventory schema keys = %s", got)
	}
	var inventorySchema map[string]json.RawMessage
	if err := json.Unmarshal(schema["corpus_inventory"], &inventorySchema); err != nil {
		t.Fatalf("decode corpus inventory schema: %v", err)
	}
	var encodedRoots []json.RawMessage
	if err := json.Unmarshal(inventorySchema["roots"], &encodedRoots); err != nil || len(encodedRoots) == 0 {
		t.Fatalf("decode corpus roots: count=%d err=%v", len(encodedRoots), err)
	}
	if got := jsonObjectKeys(t, encodedRoots[0]); got != "path,pinned_count" {
		t.Fatalf("corpus root schema keys = %s", got)
	}
	var encodedProfile corpusCompileProfile
	if err := json.Unmarshal(schema["compile_profile"], &encodedProfile); err != nil {
		t.Fatalf("decode compile profile: %v", err)
	}
	if encodedProfile != ownershipCorpusCompileProfile {
		t.Fatalf("encoded compile profile = %+v, want %+v", encodedProfile, ownershipCorpusCompileProfile)
	}
	var encodedVersion int
	if err := json.Unmarshal(schema["version"], &encodedVersion); err != nil || encodedVersion != corpusCensusReportVersion {
		t.Fatalf("encoded report version = %d err=%v, want %d", encodedVersion, err, corpusCensusReportVersion)
	}

	normalized := normalizeCensusReport(report)
	if normalized.InvalidFailures[0].Fixture != "a.sg" || normalized.FindingNormalizationErrors[0] != "a" {
		t.Fatalf("report lists are not sorted: %+v", normalized)
	}
	if normalized.CorpusInventory.Roots[0].Path != "core" {
		t.Fatalf("report corpus roots are not sorted: %+v", normalized.CorpusInventory.Roots)
	}
	if len(normalized.Findings) != 2 || normalized.Findings[0] != early {
		t.Fatalf("report findings are not sorted and deduplicated: %+v", normalized.Findings)
	}

	root := t.TempDir()
	path, err := writeCensusReport(root, report)
	if err != nil {
		t.Fatalf("write report: %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if string(written) != string(first) {
		t.Fatal("written report differs from deterministic encoding")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat report: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("report mode = %04o, want 0600", got)
	}
	temporaryFiles, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".ownership-corpus-census-*.tmp"))
	if err != nil {
		t.Fatalf("glob temporary reports: %v", err)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("successful report left temporary files: %v", temporaryFiles)
	}
	drifted := report
	drifted.CompileProfile.Backend = ""
	if _, err := encodeCensusReport(drifted); err == nil || !strings.Contains(err.Error(), "compile profile") {
		t.Fatalf("drifted profile error = %v", err)
	}
	drifted = report
	drifted.CorpusInventory.PathCount++
	if _, err := encodeCensusReport(drifted); err == nil || !strings.Contains(err.Error(), "does not match attempted") {
		t.Fatalf("drifted inventory count error = %v", err)
	}

	blockedRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(blockedRoot, "target"), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("prepare blocked report root: %v", err)
	}
	if _, err := writeCensusReport(blockedRoot, report); err == nil || !strings.Contains(err.Error(), "create ownership census directory") {
		t.Fatalf("blocked report error = %v", err)
	}

	left := canonicalReportError("/checkout/one", fmt.Errorf("bad span in /checkout/one/core/sync.sg"))
	right := canonicalReportError("/workspace/two", fmt.Errorf("bad span in /workspace/two/core/sync.sg"))
	if left != right || left != "bad span in <repo>/core/sync.sg" {
		t.Fatalf("checkout-specific report errors: left=%q right=%q", left, right)
	}
}

func TestOwnershipCorpusCensusReportAccountingContract(t *testing.T) {
	valid := corpusCensusReport{
		Version:        corpusCensusReportVersion,
		CompileProfile: ownershipCorpusCompileProfile,
		CorpusInventory: corpusInventory{
			Roots:           append([]corpusRootSpec(nil), corpusRoots...),
			PathCount:       2,
			DigestAlgorithm: corpusInventoryDigestV1,
			PathDigest:      strings.Repeat("0", 64),
		},
		Attempted:    2,
		MIRSuccesses: 1,
		InvalidFailures: []ownershipgate.CompileFailureKey{
			{Fixture: "invalid.sg", Signature: "diagnostics:BAD"},
		},
	}
	if _, err := encodeCensusReport(valid); err != nil {
		t.Fatalf("valid accounting rejected: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*corpusCensusReport)
		wantErr string
	}{
		{
			name: "negative MIR successes",
			mutate: func(report *corpusCensusReport) {
				report.MIRSuccesses = -1
			},
			wantErr: "mir_successes",
		},
		{
			name: "MIR successes exceed attempted",
			mutate: func(report *corpusCensusReport) {
				report.MIRSuccesses = report.Attempted + 1
			},
			wantErr: "mir_successes",
		},
		{
			name: "fixture missing from classifications",
			mutate: func(report *corpusCensusReport) {
				report.InvalidFailures = nil
			},
			wantErr: "classified fixture count",
		},
		{
			name: "fixture classified twice",
			mutate: func(report *corpusCensusReport) {
				report.NonInvalidFailures = []ownershipgate.CompileFailureKey{{Fixture: "other.sg", Signature: "error:no MIR"}}
			},
			wantErr: "classified fixture count",
		},
		{
			name: "missing-diagnostic failure is not invalid",
			mutate: func(report *corpusCensusReport) {
				report.InvalidWithoutDiagnostics = []ownershipgate.CompileFailureKey{{Fixture: "other.sg", Signature: "error:no diagnostic"}}
			},
			wantErr: "not present in invalid_failures",
		},
		{
			name: "missing-diagnostic multiplicity exceeds invalid failures",
			mutate: func(report *corpusCensusReport) {
				report.InvalidWithoutDiagnostics = []ownershipgate.CompileFailureKey{
					report.InvalidFailures[0], report.InvalidFailures[0],
				}
			},
			wantErr: "not present in invalid_failures",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := valid
			test.mutate(&report)
			if _, err := encodeCensusReport(report); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("accounting error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestOwnershipCorpusCensusReportInvalidationAndAtomicFailure(t *testing.T) {
	root := t.TempDir()
	path := censusReportPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create stale report directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatalf("seed stale report: %v", err)
	}
	if err := invalidateCensusReport(root); err != nil {
		t.Fatalf("invalidate stale report: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale report still exists: %v", err)
	}

	blockedRoot := t.TempDir()
	blockedPath := censusReportPath(blockedRoot)
	if err := os.MkdirAll(blockedPath, 0o750); err != nil {
		t.Fatalf("create blocking final-report directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blockedPath, "keep"), []byte("sentinel"), 0o600); err != nil {
		t.Fatalf("make blocking final-report directory non-empty: %v", err)
	}
	report := corpusCensusReport{
		Version:        corpusCensusReportVersion,
		CompileProfile: ownershipCorpusCompileProfile,
		CorpusInventory: corpusInventory{
			Roots:           append([]corpusRootSpec(nil), corpusRoots...),
			DigestAlgorithm: corpusInventoryDigestV1,
			PathDigest:      strings.Repeat("0", 64),
		},
	}
	if _, err := writeCensusReport(blockedRoot, report); err == nil || !strings.Contains(err.Error(), "publish ownership census") {
		t.Fatalf("blocked atomic publish error = %v", err)
	}
	temporaryFiles, err := filepath.Glob(filepath.Join(filepath.Dir(blockedPath), ".ownership-corpus-census-*.tmp"))
	if err != nil {
		t.Fatalf("glob failed-publish temporary reports: %v", err)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("failed publish left temporary files: %v", temporaryFiles)
	}
	if data, err := os.ReadFile(filepath.Join(blockedPath, "keep")); err != nil || string(data) != "sentinel" {
		t.Fatalf("failed publish damaged existing final target: data=%q err=%v", data, err)
	}
	if err := invalidateCensusReport(blockedRoot); err == nil || !strings.Contains(err.Error(), "remove stale ownership census") {
		t.Fatalf("non-removable stale target error = %v", err)
	}
}

func TestOwnershipCorpusFailureSignatureContract(t *testing.T) {
	root := t.TempDir()
	files := source.NewFileSetWithBase(root)
	fileID := files.Add(filepath.Join(root, "pkg", "fixture.sg"), []byte("0123456789"), 0)
	resultFor := func(spans ...source.Span) buildpipeline.CompileResult {
		bag := diag.NewBag(len(spans))
		for _, span := range spans {
			bag.Add(diag.NewError(diag.SemaError, span, "message is intentionally excluded"))
		}
		return buildpipeline.CompileResult{Diagnose: &driver.DiagnoseResult{FileSet: files, Bag: bag}}
	}
	firstSpan := source.Span{File: fileID, Start: 0, End: 1}
	secondSpan := source.Span{File: fileID, Start: 4, End: 5}
	first := failureSignature(root, resultFor(firstSpan), fmt.Errorf("compile failed"))
	second := failureSignature(root, resultFor(secondSpan), fmt.Errorf("compile failed"))
	if first == second {
		t.Fatalf("same diagnostic code at different spans has one signature: %q", first)
	}
	if !strings.Contains(first, "@pkg/fixture.sg:1:1-1:2") {
		t.Fatalf("signature lacks repository-relative primary span: %q", first)
	}
	two := failureSignature(root, resultFor(firstSpan, secondSpan), fmt.Errorf("compile failed"))
	reversed := failureSignature(root, resultFor(secondSpan, firstSpan), fmt.Errorf("compile failed"))
	if two != reversed {
		t.Fatalf("diagnostic multiset signature depends on insertion order:\n%s\n%s", two, reversed)
	}
	if two == first {
		t.Fatalf("diagnostic multiset signature lost count: one=%q two=%q", first, two)
	}
	invalid := failureSignature(root, resultFor(source.Span{File: 99}), fmt.Errorf("compile failed"))
	if !strings.Contains(invalid, "@<unknown-file-99>:<invalid-span-file-99-0-0>") {
		t.Fatalf("invalid diagnostic span lacks explicit stable token: %q", invalid)
	}
}
