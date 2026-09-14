package driver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

const returnOriginSafeSource = "fn keep(value: &int64) -> &int64 { return value; }\n"
const returnOriginEscapeSource = "fn escape() -> int64 { let alias = { let owned: int64 = 7; ret &owned; }; return 1; }\n"

// These are real parse/resolve/check artifacts. Only the driver publication
// under test is withheld; no built module record is cleared to make a fixture.
func returnOriginTypedFixture(t *testing.T, text string) *DiagnoseResult {
	return returnOriginTypedFixtureWithEscapeEvidence(t, text, false)
}

func returnOriginTypedFixtureWithEscapeEvidence(t *testing.T, text string, allowOldEscape bool) *DiagnoseResult {
	root := t.TempDir()
	files := source.NewFileSetWithBase(root)
	file := files.Get(files.AddVirtual(filepath.Join(root, "origin.sg"), []byte(text)))
	bag := diag.NewBag(64)
	builder, fileID := diagnoseParseWithStrings(t.Context(), files, file, bag, source.NewInterner(), parser.DirectiveModeOff)
	resolved := diagnoseSymbols(builder, fileID, bag, "origin", file.Path, root, nil)
	checked := diagnoseSema(t.Context(), builder, fileID, bag, nil, resolved, "origin", false, nil)
	diagnostics, marshalErr := json.Marshal(map[string]any{"source": text, "diagnostics": bag.Items()})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	t.Logf("RETURN_ORIGIN_TYPED_DIAGNOSTICS=%s", diagnostics)
	res := &DiagnoseResult{FileSet: files, File: file, FileID: fileID, Builder: builder, Bag: bag, Symbols: resolved, Sema: checked}
	if allowOldEscape {
		// This admits evidence to the private call analysis, not publication.
		// The original bag is retained, including every old escape diagnostic.
		if res.Sema == nil || res.Symbols == nil || res.Builder == nil || res.Sema.TypeInterner == nil || len(res.Sema.ExprTypes) == 0 {
			t.Fatal("fixture did not retain typed source evidence")
		}
		for _, d := range bag.Items() {
			if d.Severity == diag.SevError && d.Code != diag.SemaBorrowEscapesReturn {
				t.Fatalf("private call fixture has an unrelated source refusal: %+v", *d)
			}
		}
	} else {
		requireReturnOriginTyped(t, res)
	}
	var err error
	res.finalizationIndex, err = buildFinalizationPublicationIndex(res)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func requireReturnOriginTyped(t *testing.T, res *DiagnoseResult) {
	t.Helper()
	if res == nil || res.Bag == nil || res.Bag.HasErrors() || res.Sema == nil || res.Symbols == nil ||
		res.Builder == nil || res.Sema.TypeInterner == nil || len(res.Sema.ExprTypes) == 0 {
		t.Fatalf("fixture did not reach valid typed source: %+v", res)
	}
}

func returnOriginModuleFixture(t *testing.T, imported bool, dependency string) *DiagnoseResult {
	t.Helper()
	root := t.TempDir()
	texts := map[string]string{
		"pkg/a.sg": "pragma module::pkg, no_std;\n" + returnOriginSafeSource,
		"pkg/b.sg": "pragma module::pkg, no_std;\nfn second(value: &int64) -> &int64 { return value; }\n",
	}
	if imported {
		delete(texts, "pkg/b.sg")
		texts["pkg/a.sg"] = "pragma module::pkg, no_std;\nimport dep as Other;\n" + returnOriginSafeSource
		texts["dep/main.sg"] = "pragma module::dep, no_std;\npub fn second(value: &int64) -> &int64 { return value; }\n"
		if dependency != "" {
			texts["dep/main.sg"] = "pragma module::dep, no_std;\n" + dependency
		}
	}
	for name, text := range texts {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files := source.NewFileSetWithBase(root)
	id, err := files.Load(filepath.Join(root, "pkg/a.sg"))
	if err != nil {
		t.Fatal(err)
	}
	file, bag := files.Get(id), diag.NewBag(64)
	strings, interner := source.NewInterner(), types.NewInterner()
	builder, fileID := diagnoseParseWithStrings(t.Context(), files, file, bag, strings, parser.DirectiveModeOff)
	opts := &DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64, KeepArtifacts: true}
	exports, rec, records, err := runModuleGraph(t.Context(), files, file, builder, fileID, bag, opts, NewModuleCache(8), interner, strings)
	if err != nil || rec == nil {
		t.Fatalf("actual module graph: record=%v error=%v", rec, err)
	}
	resolveModuleRecord(t.Context(), rec, root, exports, interner, opts, nil)
	resolved := rec.Symbols[fileID]
	res := &DiagnoseResult{FileSet: files, File: file, FileID: fileID, Builder: rec.Builder,
		Bag: rec.Bag, Symbols: &resolved, Sema: rec.Sema[fileID], rootRecord: rec, moduleRecords: records}
	requireReturnOriginTyped(t, res)
	res.finalizationIndex, err = buildFinalizationPublicationIndex(res)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestReturnOriginUnitsUseOwnedArtifacts(t *testing.T) {
	for _, name := range []string{"single_file", "module", "import_alias", "bodyless_declaration"} {
		t.Run(name, func(t *testing.T) {
			var res *DiagnoseResult
			var expected map[string]bool
			want := 1
			switch name {
			case "module", "import_alias":
				res = returnOriginModuleFixture(t, name == "import_alias", "")
				res.moduleRecords["same-record-alias"] = res.rootRecord
				// no_std controls the prelude; runModuleGraph still retains core.
				// These ten keys match the retained census and this source tree.
				expected = map[string]bool{
					"core/array.sg": true, "core/base.sg": true,
					"core/entrypoint.sg": true, "core/format.sg": true,
					"core/intrinsics.sg": true, "core/map.sg": true,
					"core/option.sg": true, "core/result.sg": true,
					"core/string.sg": true, "core/sync.sg": true,
				}
				expected[res.File.Path] = true
				sibling := filepath.Join(filepath.Dir(res.File.Path), "b.sg")
				if name == "import_alias" {
					sibling = filepath.Join(filepath.Dir(filepath.Dir(res.File.Path)), "dep", "main.sg")
				}
				expected[sibling], want = true, 12
			case "bodyless_declaration":
				res = returnOriginTypedFixture(t, "@intrinsic fn opaque(value: &int64) -> &int64;\n"+returnOriginSafeSource)
				file := res.Builder.Files.Get(res.FileID)
				fn, ok := res.Builder.Items.Fn(file.Items[0])
				if !ok || fn.Body.IsValid() {
					t.Fatal("fixture must retain a declaration without a body")
				}
			default:
				res = returnOriginTypedFixture(t, returnOriginSafeSource)
			}
			inputs, err := collectReturnOriginUnits(res)
			if err != nil || len(inputs.units) != want {
				t.Fatalf("units=%d want=%d error=%v", len(inputs.units), want, err)
			}
			for _, unit := range inputs.units {
				file := unit.Builder.Files.Get(unit.FileID)
				key, err := canonicalInstantiationSourceResolver(res)(file.Span.File)
				if err != nil || unit.SourceKey != key || unit.Publication.SourceKey != key || len(unit.Publication.LocalCallables) == 0 {
					t.Fatalf("unit lost canonical owner/publication: %+v error=%v", unit, err)
				}
				if expected != nil {
					identity := unit.SourceKey
					if !expected[identity] {
						identity = res.FileSet.Get(file.Span.File).Path
					}
					if !expected[identity] {
						t.Fatalf("unexpected or repeated owning unit %q (%s)", unit.SourceKey, identity)
					}
					delete(expected, identity)
				}
				if res.rootRecord == nil {
					if unit.Builder != res.Builder || unit.Sema != res.Sema || unit.Symbols != res.Symbols {
						t.Fatal("single-file artifacts were substituted")
					}
				} else {
					found := false
					for _, rec := range finalizationPublicationRecords(res) {
						found = found || (rec.Builder == unit.Builder && rec.Sema[unit.FileID] == unit.Sema)
					}
					if !found || len(unit.Sema.ExprTypes) == 0 {
						t.Fatal("unit is not an original typed file of this module graph")
					}
				}
			}
			if len(expected) != 0 {
				t.Fatalf("missing retained owning units: %v", expected)
			}
		})
	}
}

func TestReturnOriginUnitIdentityRefusesAmbiguity(t *testing.T) {
	for _, name := range []string{"missing_symbols", "foreign_type_arena", "missing_identity_snapshot"} {
		t.Run(name, func(t *testing.T) {
			res := returnOriginTypedFixture(t, returnOriginSafeSource)
			switch name {
			case "missing_symbols":
				res.Symbols = nil
			case "foreign_type_arena":
				inputs, err := collectReturnOriginUnits(res)
				if err != nil {
					t.Fatal(err)
				}
				copy := *inputs.units[0].Sema
				copy.TypeInterner = types.NewInterner()
				inputs.units[0].Sema = &copy
				if _, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units); err == nil {
					t.Fatal("foreign type arena accepted as original facts")
				}
				return
			default:
				res.finalizationIndex = nil
			}
			if _, err := collectReturnOriginUnits(res); err == nil {
				t.Fatal("missing original identity accepted")
			}
		})
	}
}

func returnOriginAnalyzedFixture(t *testing.T, text string) (*DiagnoseResult, returnOriginInputs, *sema.ReturnOriginAnalysis) {
	t.Helper()
	res := returnOriginTypedFixture(t, text)
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	if err != nil || analysis == nil || !analysis.Complete() || len(analysis.Summaries) == 0 {
		t.Fatalf("source analysis did not complete: %+v error=%v", analysis, err)
	}
	return res, inputs, analysis
}

func TestReturnOriginVerdictPreservesDistinctStates(t *testing.T) {
	for _, name := range []string{"not_run", "complete_clean", "complete_with_diagnostics", "pending"} {
		t.Run(name, func(t *testing.T) {
			if name == "not_run" {
				if block, err := returnOriginVerdict(nil); !block || err == nil {
					t.Fatal("absence of analysis authorized HIR")
				}
				return
			}
			text := returnOriginSafeSource
			if name == "complete_with_diagnostics" {
				text = returnOriginEscapeSource
			}
			res, inputs, analysis := returnOriginAnalyzedFixture(t, text)
			if name == "pending" {
				// Inject unfinished transport metadata, not a claimed C2 decision.
				analysis.Pending = []sema.ReturnOriginPending{{SourceKey: inputs.units[0].SourceKey,
					Span: res.Builder.Files.Get(res.FileID).Span, Reason: "test obligation awaiting finalization"}}
			}
			block, err := returnOriginVerdict(analysis)
			if block != (name != "complete_clean") || (err != nil) != (name == "pending") {
				t.Fatalf("state %s: block=%t error=%v", name, block, err)
			}
			if name == "complete_with_diagnostics" && (len(analysis.Diagnostics) == 0 || analysis.Diagnostics[0].Code != diag.SemaBorrowEscapesReturn) {
				t.Fatal("source did not produce the specific lifetime refusal")
			}
			if name == "pending" {
				var unfinished *returnOriginUnfinishedError
				if !errors.As(err, &unfinished) || len(unfinished.Pending) != 1 || unfinished.Pending[0] != analysis.Pending[0] || len(analysis.Diagnostics) != 0 {
					t.Fatal("pending lost its source evidence or became a language refusal")
				}
			}
		})
	}
}

func TestReturnOriginPublicationUsesOwningBag(t *testing.T) {
	for _, name := range []string{"owning_bag", "repeated_pass"} {
		t.Run(name, func(t *testing.T) {
			res, inputs, analysis := returnOriginAnalyzedFixture(t, returnOriginEscapeSource)
			before := res.Bag.Len()
			reporters := make(returnOriginReporters)
			for range 2 {
				if err := publishReturnOriginDiagnostics(analysis, inputs, nil, reporters); err != nil {
					t.Fatal(err)
				}
			}
			if res.Bag.Len() != before+len(analysis.Diagnostics) || !hasReturnOriginRefusal(res.Bag) {
				t.Fatalf("publication dropped or repeated source diagnostics: %+v", res.Bag.Items())
			}
			if name == "owning_bag" {
				// A dependency's bag can be absent from the requested file list.
				// Its actual refusal must still make that returned result unusable.
				var unpublished *returnOriginUnpublishedError
				err := requireReturnOriginPublication(returnOriginOutcome{analysis: analysis}, diag.NewBag(64))
				if !errors.Is(err, ErrDiagnosticsReported) || !errors.As(err, &unpublished) ||
					len(unpublished.Diagnostics) != len(analysis.Diagnostics) || unpublished.Diagnostics[0].Primary != analysis.Diagnostics[0].Primary {
					t.Fatal("an unreturned owning bag erased the actual lifetime refusal")
				}
				inputs.bags[analysis.Diagnostics[0].Primary.File] = diag.NewBag(0)
				err = publishReturnOriginDiagnostics(analysis, inputs, nil, make(returnOriginReporters))
				if !errors.As(err, &unpublished) || len(unpublished.Diagnostics) != len(analysis.Diagnostics) {
					t.Fatal("full diagnostic capacity silently erased the refusal")
				}
				project := returnOriginModuleFixture(t, true, returnOriginEscapeSource)
				if err := FinalizeInstantiationClosure(t.Context(), project, 64); err != nil {
					t.Fatal(err)
				}
				all, err := collectReturnOriginUnits(project)
				if err != nil {
					t.Fatal(err)
				}
				whole, err := sema.AnalyzeReturnOrigins(t.Context(), project.Sema, all.units)
				if err != nil || !whole.Complete() || len(whole.Diagnostics) == 0 {
					t.Fatalf("imported owner was not actually diagnosed: %+v error=%v", whole, err)
				}
				for _, diagnostic := range whole.Diagnostics {
					if diagnostic.Primary.File == project.File.ID || diagnostic.Code != diag.SemaBorrowEscapesReturn {
						t.Fatal("fixture must refuse only a real imported owner")
					}
				}
				if err := publishReturnOriginDiagnostics(whole, all, nil, make(returnOriginReporters)); err != nil || project.Bag.HasErrors() {
					t.Fatalf("owning-bag publication changed the clean requested file: %v", err)
				}
				outcome := returnOriginOutcome{analysis: whole, identity: project.Sema.InstantiationIdentity, closure: project.Sema.InstantiationClosure}
				requested := []DiagnoseDirResult{{Path: project.File.Path, FileID: project.File.ID, Sema: project.Sema, Bag: project.Bag}}
				err = requireParallelReturnOriginPublication(returnOriginPass{project.Sema: outcome}, requested)
				if !errors.Is(err, ErrDiagnosticsReported) || !errors.As(err, &unpublished) || len(unpublished.Diagnostics) != len(whole.Diagnostics) {
					t.Fatal("directory aggregate lost the refusal outside its requested files")
				}
			}
			if name == "repeated_pass" {
				count := res.Bag.Len()
				if block, err := finalizeDiagnoseResult(t.Context(), res); !block || err != nil || res.Bag.Len() != count {
					t.Fatalf("old SEM3139 was lost or repeated: block=%t error=%v", block, err)
				}
				res.Symbols = nil
				if block, err := finalizeDiagnoseResult(t.Context(), res); !block || err != nil {
					t.Fatal("missing later artifacts reopened an already refused lifetime")
				}
				other := returnOriginTypedFixture(t, returnOriginSafeSource)
				// An unrelated error also blocks mandatory lifetime analysis;
				// retain its diagnostics without publishing HIR or a closure.
				other.Bag.Add(&diag.Diagnostic{Severity: diag.SevError, Code: diag.UnknownCode, Message: "other stage refused"})
				if block, err := finalizeDiagnoseResult(t.Context(), other); !block || err != nil || other.Sema.InstantiationClosure != nil {
					t.Fatal("unrelated semantic error authorized HIR or a closure")
				}
			}
		})
	}
}

func TestReturnOriginFinalizationIsMandatory(t *testing.T) {
	for _, name := range []string{"emit_hir_false", "emit_hir_true", "existing_closure", "directory"} {
		t.Run(name, func(t *testing.T) {
			if name == "existing_closure" {
				res := returnOriginTypedFixture(t, returnOriginEscapeSource)
				if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
					t.Fatal(err)
				}
				if res.Sema.InstantiationClosure == nil || res.Bag.HasErrors() {
					t.Fatal("fixture did not reach clean closure before the new hook")
				}
				if block, err := finalizeDiagnoseResult(t.Context(), res); !block || err != nil || !hasReturnOriginRefusal(res.Bag) {
					t.Fatalf("idempotent closure bypassed lifetime analysis: block=%t error=%v", block, err)
				}
				clean := returnOriginTypedFixture(t, returnOriginSafeSource)
				if err := FinalizeInstantiationClosure(t.Context(), clean, 64); err != nil {
					t.Fatal(err)
				}
				inputs, err := collectReturnOriginUnits(clean)
				if err != nil {
					t.Fatal(err)
				}
				analysis, err := sema.AnalyzeReturnOrigins(t.Context(), clean.Sema, inputs.units)
				if err != nil || !analysis.Complete() {
					t.Fatalf("finalized clean analysis: %+v error=%v", analysis, err)
				}
				outcome := returnOriginOutcome{analysis: analysis, identity: clean.Sema.InstantiationIdentity, closure: clean.Sema.InstantiationClosure}
				if !outcome.matches(clean.Sema) {
					t.Fatal("actual finalized snapshot was not recognized")
				}
				replacement := *clean.Sema.InstantiationClosure
				clean.Sema.InstantiationClosure = &replacement
				results := []DiagnoseDirResult{{Path: clean.File.Path, Sema: clean.Sema, Bag: clean.Bag, Symbols: clean.Symbols}}
				if err := finalizeParallelFileResults(t.Context(), clean.FileSet, results, returnOriginPass{clean.Sema: outcome}); err == nil {
					t.Fatal("old carried acceptance survived a replaced authority snapshot")
				}
				return
			}
			for _, text := range []string{returnOriginSafeSource, returnOriginEscapeSource} {
				root := t.TempDir()
				path := filepath.Join(root, "origin.sg")
				if err := os.WriteFile(path, []byte("pragma module::origin, no_std;\n"+text), 0o600); err != nil {
					t.Fatal(err)
				}
				opts := &DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64, KeepArtifacts: true, EmitHIR: name == "emit_hir_true"}
				wantRefusal := text == returnOriginEscapeSource
				if name == "directory" {
					for _, full := range []bool{false, true} {
						opts.FullModuleGraph = full
						_, results, err := DiagnoseDirWithOptions(t.Context(), root, opts, 1)
						if err != nil || len(results) != 1 || hasReturnOriginRefusal(results[0].Bag) != wantRefusal || results[0].Sema == nil {
							t.Fatalf("directory full=%t refusal=%t: results=%+v error=%v", full, wantRefusal, results, err)
						}
					}
					continue
				}
				res, err := DiagnoseWithOptions(t.Context(), path, opts)
				if err != nil || res == nil || res.Sema == nil || hasReturnOriginRefusal(res.Bag) != wantRefusal {
					t.Fatalf("public hook refusal=%t: result=%+v error=%v", wantRefusal, res, err)
				}
				if (res.HIR != nil) != (opts.EmitHIR && !wantRefusal) {
					t.Fatal("HIR did not follow the lifetime verdict")
				}
			}
		})
	}
}
