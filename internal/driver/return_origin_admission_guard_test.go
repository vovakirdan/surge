package driver

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
)

// These source-backed admission controls use the existing finalization APIs.
// Missing artifacts are deliberate transport corruption, not source programs.
func TestReturnOriginRequiredSingleAdmission(t *testing.T) {
	for _, name := range []string{
		"missing_sema", "missing_symbols", "missing_builder", "missing_bag", "missing_file_id",
		"other_error", "complete_clean", "complete_refusal", "full_bag_refusal",
	} {
		t.Run(name, func(t *testing.T) {
			src := returnOriginSafeSource
			if name == "complete_refusal" || name == "full_bag_refusal" {
				src = returnOriginEscapeSource
			}
			res := returnOriginTypedFixture(t, src)
			var originalError *diag.Diagnostic
			switch name {
			case "missing_sema":
				res.Sema = nil
			case "missing_symbols":
				res.Symbols = nil
			case "missing_builder":
				res.Builder = nil
			case "missing_bag":
				res.Bag = nil
			case "missing_file_id":
				res.FileID = ast.NoFileID
			case "other_error":
				originalError = addReturnOriginOtherError(t, res.Bag)
			case "full_bag_refusal":
				res.Bag = diag.NewBag(0)
			}
			before := returnOriginAdmissionBag(t, res.Bag)
			block, err := finalizeDiagnoseResult(t.Context(), res)
			wantBlock := name != "complete_clean"
			if block != wantBlock {
				t.Fatalf("mandatory admission block=%t, want %t: error=%v", block, wantBlock, err)
			}
			wantError := name != "other_error" && name != "complete_clean" && name != "complete_refusal"
			if (err != nil) != wantError {
				t.Fatalf("mandatory admission error=%v, want error=%t", err, wantError)
			}
			if name == "complete_refusal" {
				if !hasReturnOriginRefusal(res.Bag) {
					t.Fatal("completed local-owner analysis did not publish SEM3139")
				}
			} else if after := returnOriginAdmissionBag(t, res.Bag); !bytes.Equal(before, after) {
				t.Fatalf("admission changed existing diagnostics: before=%s after=%s", before, after)
			}
			if name == "full_bag_refusal" {
				var unpublished *returnOriginUnpublishedError
				if !errors.As(err, &unpublished) || len(unpublished.Diagnostics) == 0 ||
					unpublished.Diagnostics[0].Code != diag.SemaBorrowEscapesReturn {
					t.Fatal("full bag lost the actual source lifetime refusal")
				}
			}
			if originalError != nil {
				if res.Bag.Len() != 1 || res.Bag.Items()[0] != originalError || res.Sema.InstantiationClosure != nil {
					t.Fatal("unrelated refusal changed or ran finalization on invalid prerequisites")
				}
			}
		})
	}
}

func TestReturnOriginRequiredParallelAdmission(t *testing.T) {
	for _, name := range []string{
		"missing_sema", "missing_symbols", "missing_bag", "other_error",
		"missing_file_outcome", "missing_published_outcome", "changed_closure",
	} {
		t.Run(name, func(t *testing.T) {
			if name == "missing_published_outcome" {
				checkReturnOriginMissingPublishedOutcome(t)
				return
			}
			res := returnOriginTypedFixture(t, returnOriginSafeSource)
			var pass returnOriginPass
			var originalError *diag.Diagnostic
			switch name {
			case "missing_sema":
				res.Sema = nil
			case "missing_symbols":
				res.Symbols = nil
			case "missing_bag":
				res.Bag = nil
			case "other_error":
				originalError = addReturnOriginOtherError(t, res.Bag)
			case "missing_file_outcome":
				pass = make(returnOriginPass)
			case "changed_closure":
				if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
					t.Fatal(err)
				}
				inputs, err := collectReturnOriginUnits(res)
				if err != nil {
					t.Fatal(err)
				}
				analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
				if err != nil || analysis == nil || !analysis.Complete() || len(analysis.Diagnostics) != 0 {
					t.Fatalf("PRECONDITION: real clean proof missing: analysis=%+v error=%v", analysis, err)
				}
				outcome := returnOriginOutcome{analysis: analysis, identity: res.Sema.InstantiationIdentity, closure: res.Sema.InstantiationClosure}
				if !outcome.matches(res.Sema) {
					t.Fatal("PRECONDITION: actual complete proof must match before corruption")
				}
				pass = returnOriginPass{res.Sema: outcome}
				changed := *res.Sema.InstantiationClosure
				res.Sema.InstantiationClosure = &changed
			}
			before := returnOriginAdmissionBag(t, res.Bag)
			results := []DiagnoseDirResult{returnOriginAdmissionFile(res)}
			err := finalizeParallelFileResults(t.Context(), res.FileSet, results, pass)
			if err == nil {
				t.Fatal("mandatory parallel admission accepted missing proof or invalid artifacts")
			}
			if after := returnOriginAdmissionBag(t, res.Bag); !bytes.Equal(before, after) {
				t.Fatalf("parallel admission changed diagnostics: before=%s after=%s", before, after)
			}
			if originalError != nil && (res.Bag.Len() != 1 || res.Bag.Items()[0] != originalError) {
				t.Fatal("parallel admission lost the original unrelated refusal")
			}
		})
	}
}

// This is the publication seam used by FullModuleGraph. First obtain a real
// module pass, then omit only the requested file's already-proven outcome.
func checkReturnOriginMissingPublishedOutcome(t *testing.T) {
	t.Helper()
	res := returnOriginModuleFixture(t, true, "")
	paths := make([]string, 0, len(res.moduleRecords))
	for path := range res.moduleRecords {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	pass, err := finalizeParallelModuleRecords(t.Context(), res.FileSet, paths, res.moduleRecords)
	if err != nil {
		t.Fatalf("PRECONDITION: actual module finalization: %v", err)
	}
	outcome, found := pass[res.Sema]
	if !found || !outcome.matches(res.Sema) || !outcome.analysis.Complete() || len(outcome.analysis.Diagnostics) != 0 {
		t.Fatal("PRECONDITION: actual requested file lacks a clean matching module proof")
	}
	results := []DiagnoseDirResult{returnOriginAdmissionFile(res)}
	if err := requireParallelReturnOriginPublication(pass, results); err != nil {
		t.Fatalf("PRECONDITION: intact module proof failed publication: %v", err)
	}
	t.Log("completed actual module pass and intact publication; omitting one requested outcome")
	before := returnOriginAdmissionBag(t, res.Bag)
	delete(pass, res.Sema)
	if err := requireParallelReturnOriginPublication(pass, results); err == nil {
		t.Fatal("full-module publication accepted a requested file with no matching outcome")
	}
	if after := returnOriginAdmissionBag(t, res.Bag); !bytes.Equal(before, after) {
		t.Fatalf("missing outcome changed diagnostics: before=%s after=%s", before, after)
	}
	t.Log("completed actual module pass -> matching publication -> one omitted requested outcome")
}

func returnOriginAdmissionFile(res *DiagnoseResult) DiagnoseDirResult {
	return DiagnoseDirResult{Path: res.File.Path, FileID: res.File.ID, ASTFile: res.FileID,
		Builder: res.Builder, Bag: res.Bag, Symbols: res.Symbols, Sema: res.Sema}
}

func addReturnOriginOtherError(t *testing.T, bag *diag.Bag) *diag.Diagnostic {
	t.Helper()
	d := &diag.Diagnostic{Severity: diag.SevError, Code: diag.UnknownCode, Message: "other stage refused"}
	if !bag.Add(d) {
		t.Fatal("PRECONDITION: unrelated diagnostic was not stored")
	}
	return d
}

func returnOriginAdmissionBag(t *testing.T, bag *diag.Bag) []byte {
	t.Helper()
	if bag == nil {
		return []byte("null")
	}
	data, err := json.Marshal(bag.Items())
	if err != nil {
		t.Fatal(err)
	}
	return data
}
