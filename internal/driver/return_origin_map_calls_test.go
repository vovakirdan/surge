package driver

import (
	"crypto/sha256"
	"fmt"
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
	"surge/internal/types"
)

// These keep the actual full stdlib graph. A missing typed generic operand or
// unresolved dependency is a PRECONDITION failure, not the auto-borrow defect.
func TestAnalyzeTypedMapReturnOrigins(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"external_shared_local_key", `fn probe(m: &Map<string, string>) -> Option<&string> {
    return { let key: string = "key"; ret m.get_ref(&key); };
}
`},
		{"external_mut_local_key", `fn probe(m: &mut Map<string, string>) -> Option<&mut string> {
    return { let key: string = "key"; ret m.get_mut(&key); };
}
`},
		{"local_shared_external_key", `fn probe(key: &string) -> Option<&string> {
    return {
        let owned: Map<string, string> = Map::<string, string>.new();
        ret owned.get_ref(key);
    };
}
`},
		{"local_mut_external_key", `fn probe(key: &string) -> Option<&mut string> {
    return {
        let mut owned: Map<string, string> = Map::<string, string>.new();
        ret owned.get_mut(key);
    };
}
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_MAP_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.src)), tc.src)
			stdlib := detectStdlibRootFrom(".")
			if stdlib == "" {
				t.Fatal("PRECONDITION: full stdlib is unavailable")
			}
			t.Setenv("SURGE_STDLIB", stdlib)
			path := filepath.Join(t.TempDir(), "origin.sg")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatal(err)
			}
			res := returnOriginMapBeforeHook(t, path)
			inputs, err := collectReturnOriginUnits(res)
			if err != nil {
				t.Fatalf("PRECONDITION: full owning-unit collection: %v", err)
			}
			var root *sema.ReturnOriginUnit
			var inventory []map[string]any
			contracts := 0
			for i := range inputs.units {
				unit := &inputs.units[i]
				file := res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
				if file == nil {
					t.Fatal("PRECONDITION: retained unit has no source file")
				}
				inventory = append(inventory, map[string]any{"source_key": unit.SourceKey, "file": file.ID,
					"source_sha256": fmt.Sprintf("%x", sha256.Sum256(file.Content)), "publication": unit.Publication})
				if unit.Builder == res.Builder && unit.FileID == res.FileID {
					root = unit
				}
				if unit.SourceKey != "core/intrinsics.sg" {
					continue
				}
				for _, id := range unit.Builder.Files.Get(unit.FileID).Items {
					fn, ok := unit.Builder.Items.Fn(id)
					if !ok || fn == nil {
						continue
					}
					name, _ := unit.Builder.StringsInterner.Lookup(fn.Name)
					if name != "rt_map_get_ref" && name != "rt_map_get_mut" {
						continue
					}
					ids := unit.Symbols.ItemSymbols[id]
					if len(ids) != 1 {
						t.Fatal("PRECONDITION: core source declaration lost unique identity")
					}
					sym := unit.Symbols.Table.Symbols.Get(ids[0])
					if sym == nil || sym.Signature == nil || sym.Signature.ReturnSourceSyntax.Sources().IsAllInputs() || !slices.Equal(sym.Signature.ReturnSourceSyntax.Sources().Slots(), []uint32{0}) {
						t.Fatal("PRECONDITION: actual core intrinsic promise is not receiver slot 0")
					}
					contracts++
				}
			}
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "full_units": inventory, "core_contracts": contracts})
			if root == nil || len(inputs.units) < 2 || contracts != 2 {
				t.Fatal("PRECONDITION: full root/core declaration authority is missing")
			}
			var selected ast.ExprID
			for id := range root.Sema.ExprTypes {
				call, ok := root.Builder.Exprs.Call(id)
				if !ok || call == nil {
					continue
				}
				member, ok := root.Builder.Exprs.Member(call.Target)
				if !ok || member == nil {
					continue
				}
				name, _ := root.Builder.StringsInterner.Lookup(member.Field)
				if name != "get_ref" && name != "get_mut" {
					continue
				}
				if selected.IsValid() {
					t.Fatal("PRECONDITION: source contains multiple Map access calls")
				}
				selected = id
				receiverType := root.Sema.ExprTypes[member.Target]
				var certificates []sema.BorrowInfo
				for _, borrow := range root.Sema.Borrows {
					if borrow.Life.FromExpr == member.Target {
						certificates = append(certificates, borrow)
					}
				}
				logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "call": id, "call_type": root.Sema.ExprTypes[id],
					"callee_symbol": root.Symbols.ExprSymbols[id], "receiver": member.Target, "receiver_type": receiverType,
					"arguments": call.Args, "borrow_certificates": certificates})
				if root.Sema.ExprTypes[id] == types.NoTypeID || receiverType == types.NoTypeID || len(call.Args) != 1 {
					t.Fatal("PRECONDITION: the actual Map call/receiver is not fully typed")
				}
				if strings.HasPrefix(tc.name, "local_") {
					kind := sema.BorrowShared
					if strings.Contains(tc.name, "_mut_") {
						kind = sema.BorrowMut
					}
					if len(certificates) != 1 || certificates[0].Kind != kind || certificates[0].Reserved || !certificates[0].Place.IsValid() {
						t.Fatal("PRECONDITION: checker did not admit the expected owned receiver auto-borrow")
					}
				}
			}
			if !selected.IsValid() {
				t.Fatal("PRECONDITION: actual Map call is absent")
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "analysis": analysis, "analysis_error": errorReturnOriginCallText(err)})
			if err != nil || analysis == nil {
				t.Fatalf("PRECONDITION: full-unit origin analysis could not run: %v", err)
			}
			if strings.HasPrefix(tc.name, "local_") {
				if len(analysis.Diagnostics) == 0 {
					if !analysis.Complete() {
						t.Fatal("PRECONDITION: unresolved full-unit obligations; no specific Map escape proof")
					}
					t.Fatal("local Map owner escaped without its required diagnostic")
				}
				for _, d := range analysis.Diagnostics {
					if d.Primary.File != res.File.ID {
						t.Fatal("PRECONDITION: dependency diagnostic is not the Map fixture witness")
					}
					requireReturnOriginCallEscape(t, tc.src, d)
				}
				return
			}
			if !analysis.Complete() || len(analysis.Diagnostics) != 0 {
				t.Fatalf("PRECONDITION: external Map proof is not complete/clean: %+v", analysis)
			}
			summary := requireReturnOriginSummary(t, analysis, "probe")
			if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, []uint32{0}) {
				t.Fatalf("local key contaminated the external Map receiver source: %+v", summary)
			}
		})
	}
}

// Same pre-hook stages as the retained owning-unit census. Snapshot raw bags
// before the existing root warning/info filter, without changing the profile.
func returnOriginMapBeforeHook(t *testing.T, path string) *DiagnoseResult {
	t.Helper()
	files := source.NewFileSet()
	id, err := files.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	file, bag := files.Get(id), diag.NewBag(64)
	strs, interner := source.NewInterner(), types.NewInterner()
	opts := &DiagnoseOptions{Stage: DiagnoseStageAll, MaxDiagnostics: 64, IgnoreWarnings: true, KeepArtifacts: true}
	start := files.BaseDir()
	if start == "" {
		start = filepath.Dir(path)
	}
	if err := ensureModuleMapping(opts, start); err != nil {
		t.Fatal(err)
	}
	diagnoseTokenize(file, bag)
	builder, fileID := diagnoseParseWithStrings(t.Context(), files, file, bag, strs, parser.DirectiveModeOff)
	exports, rec, records, graphErr := runModuleGraph(t.Context(), files, file, builder, fileID, bag, opts, NewModuleCache(256), interner, strs)
	res := &DiagnoseResult{FileSet: files, File: file, FileID: fileID, Builder: builder, Bag: bag, rootRecord: rec, moduleRecords: records}
	if graphErr != nil {
		t.Fatalf("PRECONDITION: module graph: %v", graphErr)
	}
	modulePath := modulePathForFile(files, file, opts.ModuleMapping)
	if rec != nil {
		modulePath = rec.Meta.Path
		resolveModuleRecord(t.Context(), rec, files.BaseDir(), exports, interner, opts, nil)
		if resolved, ok := rec.Symbols[fileID]; ok {
			res.Symbols = &resolved
		}
		res.Sema = rec.Sema[fileID]
	}
	if res.Symbols == nil {
		res.Symbols = diagnoseSymbols(builder, fileID, bag, modulePath, path, files.BaseDir(), exports)
	}
	if res.Sema == nil {
		res.Sema = diagnoseSemaWithTypes(t.Context(), builder, fileID, bag, exports, res.Symbols, interner, modulePath, true, nil)
	}
	logReturnOriginCallEvidence(t, map[string]any{"raw_root_diagnostics": bag.Items()})
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	moduleErrors := false
	for _, key := range keys {
		if records[key] == nil || records[key].Bag == nil {
			t.Fatal("PRECONDITION: owning module has no diagnostic bag")
		}
		logReturnOriginCallEvidence(t, map[string]any{"module": key, "raw_diagnostics": records[key].Bag.Items()})
		moduleErrors = moduleErrors || records[key].Bag.HasErrors()
	}
	bag.Filter(func(d *diag.Diagnostic) bool { return d.Severity != diag.SevWarning && d.Severity != diag.SevInfo })
	if res.Sema == nil || res.Symbols == nil || bag.HasErrors() || moduleErrors || len(res.Sema.ExprTypes) == 0 {
		t.Fatal("PRECONDITION: public stage input did not complete typed analysis")
	}
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: closure: %v", err)
	}
	if bag.HasErrors() || res.Sema.InstantiationClosure == nil {
		t.Fatal("PRECONDITION: pre-hook closure refused")
	}
	return res
}
