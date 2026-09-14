package driver

import (
	"crypto/sha256"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const selectedOwnerAliases = `pragma module::app, no_std;
import dep as Left;
import ../dep as Again;
import alt as Foreign;
fn probe(a: &string, b: &string) -> &string {
    let _ = Left.first(a, b);
    let _ = Again.first(a, b);
    return Foreign.first(a, b);
}
`

type selectedOwnerFact struct {
	Selected, Physical, Canonical symbols.SymbolID
	Identity                      sema.FinalizationCallableIdentity
	OwnerPath, SelectedPath       string
}

// This tests producer association independently of the later origin readers.
func TestSelectedModuleOwnerAssociation(t *testing.T) {
	for _, name := range []string{"direct_owner", "aliases_distinct_owner", "missing_export_owner"} {
		t.Run(name, func(t *testing.T) {
			text := selectedCallableApp + selectedCallableDirect
			calls := []string{"Other.first(a, b)"}
			wantKeys := strings.Fields("app/main.sg core/array.sg core/base.sg core/entrypoint.sg core/format.sg core/intrinsics.sg core/map.sg core/option.sg core/result.sg core/string.sg core/sync.sg dep/main.sg")
			if name == "aliases_distinct_owner" {
				text, calls = selectedOwnerAliases, []string{"Left.first(a, b)", "Again.first(a, b)", "Foreign.first(a, b)"}
				wantKeys = append(wantKeys, "alt/main.sg")
				slices.Sort(wantKeys)
			}
			if name == "missing_export_owner" {
				selectedCallableModuleFixture(t, text) // Ordinary admission before the detached metadata mutation.
			}
			res := selectedCallableModuleFixture(t, text, name)
			err := FinalizeInstantiationClosure(t.Context(), res, 64)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "owner_closure", "case": name, "error": errorReturnOriginCallText(err)})
			checkReturnOriginStdlibBags(t, res, false)
			inputs, unitErr := collectReturnOriginUnits(res)
			if err != nil || unitErr != nil || res.Sema.InstantiationClosure == nil {
				t.Fatalf("PRECONDITION: full owner closure: %v/%v", err, unitErr)
			}
			var root sema.ReturnOriginUnit
			var keys []string
			for _, u := range inputs.units {
				f := res.FileSet.Get(u.Builder.Files.Get(u.FileID).Span.File)
				keys = append(keys, u.SourceKey)
				logReturnOriginCallEvidence(t, map[string]any{"stage": "owner_unit", "source_key": u.SourceKey, "sha256": sha256.Sum256(f.Content), "bag": inputs.bags[f.ID].Items()})
				if f.ID == res.File.ID {
					root = u
				}
			}
			if !slices.Equal(keys, wantKeys) || root.Builder == nil {
				t.Fatalf("PRECONDITION: full source owners: got=%v want=%v", keys, wantKeys)
			}
			var facts []selectedOwnerFact
			for _, callText := range calls {
				callID := selectedCallableSite(t, res, root, callText, ast.ExprCall)
				call, _ := root.Builder.Exprs.Call(callID)
				memberText := strings.Split(callText, "(")[0]
				selectedCallableSite(t, res, root, memberText, ast.ExprMember)
				member := captureSelectedOwner(t, res, root, call.Target)
				actual := captureSelectedOwner(t, res, root, callID)
				facts = append(facts, actual)
				memberExpr, _ := root.Builder.Exprs.Member(call.Target)
				module := root.Symbols.Table.Symbols.Get(root.Symbols.ExprSymbols[memberExpr.Target])
				if module == nil || module.Kind != symbols.SymbolModule {
					t.Fatal("PRECONDITION: syntactic module target missing")
				}
				if member.Selected != actual.Selected || member.Identity != actual.Identity {
					t.Errorf("member/call producers diverge: member=%+v call=%+v", member, actual)
				}
				if name == "missing_export_owner" {
					if module.ModulePath != "app/dep" || actual.SelectedPath != "app/dep" || member.SelectedPath != "app/dep" || actual.OwnerPath != "dep" || actual.Selected == actual.Canonical {
						t.Errorf("missing export owner invented an association: member=%+v call=%+v", member, actual)
					}
				} else if member.Selected != member.Canonical || actual.Selected != actual.Canonical {
					t.Errorf("selected producers lack physical association: member=%+v call=%+v", member, actual)
				}
			}
			if len(facts) == 3 && (facts[0].Identity != facts[1].Identity || facts[0].Selected != facts[1].Selected || facts[0].Identity.BodyKey == facts[2].Identity.BodyKey || facts[0].Selected == facts[2].Selected) {
				t.Errorf("same-owner aliases or distinct same-signature owners conflated: %+v", facts)
			}
		})
	}
}

// Read original AST declarations and the existing publication/cache; never build a remap.
func captureSelectedOwner(t *testing.T, res *DiagnoseResult, u sema.ReturnOriginUnit, expr ast.ExprID) selectedOwnerFact {
	t.Helper()
	selected := u.Symbols.ExprSymbols[expr]
	sym := u.Symbols.Table.Symbols.Get(selected)
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil || sym.Signature.HasSelf {
		t.Fatal("PRECONDITION: selected source lacks its free-function symbol")
	}
	info, known := u.Sema.TypeInterner.FnInfo(sym.Type)
	if !known || info == nil {
		t.Fatal("PRECONDITION: selected function descriptor missing")
	}
	matches := 0
	var fact selectedOwnerFact
	for _, rec := range finalizationPublicationRecords(res) {
		for _, fileID := range rec.FileIDs {
			file := rec.Builder.Files.Get(fileID)
			for _, item := range file.Items {
				for _, local := range rec.Symbols[fileID].ItemSymbols[item] {
					own := rec.Table.Symbols.Get(local)
					if own == nil || own.Kind != symbols.SymbolFunction || own.Span != sym.Span {
						continue
					}
					if own.Decl.SourceFile != file.Span.File || own.Decl.ASTFile != fileID || own.Decl.Item != item || rec.Meta == nil || rec.Exports == nil {
						t.Fatal("PRECONDITION: selected span lacks original AST owner")
					}
					mapping, cached := cachedInstantiationSymbolRemap(rec, res.Symbols.Table)
					before := selectedCallableDigest(t, mapping)
					identityCount, candidateCount := 0, 0
					var identity sema.FinalizationCallableIdentity
					for _, original := range res.finalizationIndex[rec] {
						if original.Symbol == local {
							identity, identityCount = original, identityCount+1
						}
					}
					var candidate *sema.CallableCandidate
					for i := range res.Sema.CallableCandidates {
						c := &res.Sema.CallableCandidates[i]
						if c.Symbol == mapping[local] && c.BodyKey == identity.BodyKey && c.SourceKey == identity.SourceKey {
							candidate, candidateCount = c, candidateCount+1
						}
					}
					fact = selectedOwnerFact{selected, local, mapping[local], identity, rec.Meta.Path, sym.ModulePath}
					f := res.FileSet.Get(file.Span.File)
					logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_owner_tuple", "expr": expr, "site": u.Builder.Exprs.Get(expr).Span, "fact": fact,
						"physical_ast_file": fileID, "physical_item": item, "physical_source_sha256": sha256.Sum256(f.Content), "selected_symbol": sym, "physical_symbol": own,
						"exports_path": rec.Exports.Path, "cached": cached, "remap_sha256": before, "candidate": candidate, "fn_info": info, "all_inputs": info.ReturnSources().IsAllInputs(), "slots": info.ReturnSources().Slots()})
					if !cached || !fact.Canonical.IsValid() || identityCount != 1 || candidateCount != 1 || own.Type != sym.Type || own.Signature == nil || own.Signature.HasSelf != sym.Signature.HasSelf || own.Signature.HasBody != sym.Signature.HasBody || candidate.Source != own.Span || !slices.Equal(info.Params, candidate.ParamTypes) || info.Result != candidate.ResultType || !info.ReturnSources().Equal(candidate.ReturnSources) {
						t.Fatal("PRECONDITION: physical declaration/cache/candidate facts are incomplete")
					}
					if before != selectedCallableDigest(t, mapping) {
						t.Fatal("owner observation mutated the cached remap")
					}
					matches++
				}
			}
		}
	}
	if matches != 1 {
		t.Fatalf("PRECONDITION: original AST owner count=%d", matches)
	}
	return fact
}

// Existing real module fixture, with one extra owner or one detached Path fault.
// The negative changes no source or original export container.
func selectedCallableModuleFixture(t *testing.T, text string, ownerCase ...string) *DiagnoseResult {
	t.Helper()
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	root := t.TempDir()
	sourceTexts := map[string]string{"app/main.sg": text, "dep/main.sg": selectedCallableDependency}
	if len(ownerCase) > 0 && ownerCase[0] == "aliases_distinct_owner" {
		sourceTexts["alt/main.sg"] = strings.Replace(selectedCallableDependency, "module::dep", "module::alt", 1)
	}
	for name, content := range sourceTexts {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_module_source", "path": name, "source": content, "sha256": sha256.Sum256([]byte(content))})
	}
	files := source.NewFileSetWithBase(root)
	id, err := files.Load(filepath.Join(root, "app/main.sg"))
	if err != nil {
		t.Fatal(err)
	}
	file, bag := files.Get(id), diag.NewBag(64)
	interner, texts := types.NewInterner(), source.NewInterner()
	builder, fileID := diagnoseParseWithStrings(t.Context(), files, file, bag, texts, parser.DirectiveModeOff)
	opts := &DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64, KeepArtifacts: true}
	exports, rec, records, err := runModuleGraph(t.Context(), files, file, builder, fileID, bag, opts, NewModuleCache(8), interner, texts)
	logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_module_graph", "bag": bag.Items(), "error": errorReturnOriginCallText(err)})
	if err != nil || rec == nil {
		t.Fatalf("PRECONDITION: actual module graph: %v", err)
	}
	original := exports["app/dep"]
	if original == nil || original.Path != "dep" {
		t.Fatal("PRECONDITION: actual dependency export owner missing")
	}
	before := selectedCallableDigest(t, original)
	if len(ownerCase) > 0 && ownerCase[0] == "aliases_distinct_owner" && (exports["dep"] != original || exports["app/alt"] == original || exports["app/alt"] == nil) {
		t.Fatal("PRECONDITION: aliases do not share their real owner container")
	}
	if len(ownerCase) > 0 && ownerCase[0] == "missing_export_owner" {
		detached := *original
		detached.Path, detached.Symbols = "", maps.Clone(original.Symbols)
		for name, overloads := range detached.Symbols {
			detached.Symbols[name] = slices.Clone(overloads)
		}
		exports = maps.Clone(exports)
		for key, exp := range exports {
			if exp == original {
				exports[key] = &detached
			}
		}
	}
	for key, exp := range exports {
		if strings.HasPrefix(key, "core") {
			continue
		}
		var owners []string
		for path, owner := range records {
			if owner.Exports == exp {
				owners = append(owners, path)
			}
		}
		slices.Sort(owners)
		logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_export_owner", "lookup_path": key, "exports_path": exp.Path, "same_container_owners": owners, "original_path": original.Path, "original_sha256": before})
	}
	resolveModuleRecord(t.Context(), rec, root, exports, interner, opts, nil)
	if original.Path != "dep" || before != selectedCallableDigest(t, original) {
		t.Fatal("root typing changed original exports")
	}
	resolved := rec.Symbols[fileID]
	res := &DiagnoseResult{FileSet: files, File: file, FileID: fileID, Builder: rec.Builder, Bag: rec.Bag,
		Symbols: &resolved, Sema: rec.Sema[fileID], rootRecord: rec, moduleRecords: records}
	checkReturnOriginStdlibBags(t, res, false)
	requireReturnOriginTyped(t, res)
	return res
}
