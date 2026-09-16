package driver

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Private pre-hook input: public admission must still reject unfinished core.
// The real module graph, original bags and all owning units remain intact.
func returnOriginStdlibFixture(t *testing.T, text string, allowEscape bool) *DiagnoseResult {
	t.Helper()
	stdlib := detectStdlibRootFrom(".")
	if stdlib == "" {
		t.Fatal("PRECONDITION: real stdlib unavailable")
	}
	t.Setenv("SURGE_STDLIB", stdlib)
	root := t.TempDir()
	path := filepath.Join(root, "origin.sg")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	files := source.NewFileSetWithBase(root)
	id, err := files.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	file, bag := files.Get(id), diag.NewBag(64)
	strs, interner := source.NewInterner(), types.NewInterner()
	opts := &DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64, KeepArtifacts: true}
	if err := ensureModuleMapping(opts, root); err != nil {
		t.Fatal(err)
	}
	diagnoseTokenize(file, bag)
	builder, fileID := diagnoseParseWithStrings(t.Context(), files, file, bag, strs, parser.DirectiveModeOff)
	exports, rec, records, err := runModuleGraph(t.Context(), files, file, builder, fileID, bag, opts, NewModuleCache(256), interner, strs)
	if err != nil || rec == nil {
		t.Fatalf("PRECONDITION: real stdlib module graph: %v", err)
	}
	resolveModuleRecord(t.Context(), rec, root, exports, interner, opts, nil)
	resolved, ok := rec.Symbols[fileID]
	res := &DiagnoseResult{FileSet: files, File: file, FileID: fileID, Builder: rec.Builder, Bag: rec.Bag,
		Symbols: &resolved, Sema: rec.Sema[fileID], rootRecord: rec, moduleRecords: records}
	checkReturnOriginStdlibBags(t, res, allowEscape)
	if !ok || res.Sema == nil || res.Sema.TypeInterner == nil || len(res.Sema.ExprTypes) == 0 {
		t.Fatal("PRECONDITION: real stdlib source did not retain original typed artifacts")
	}
	return res
}

func checkReturnOriginStdlibBags(t *testing.T, res *DiagnoseResult, allowEscape bool) {
	t.Helper()
	bags := map[*diag.Bag][]string{res.Bag: {"root"}}
	for path, rec := range res.moduleRecords {
		if rec != nil {
			bags[rec.Bag] = append(bags[rec.Bag], path)
		}
	}
	for bag, owners := range bags {
		if bag == nil {
			t.Fatal("PRECONDITION: original module bag is missing")
		}
		slices.Sort(owners)
		logReturnOriginCallEvidence(t, map[string]any{"bag_owners": owners, "original_bag": bag.Items(), "bag_cap": bag.Cap(), "bag_len": bag.Len()})
		for _, d := range bag.Items() {
			// A fixture that deliberately leaks is refused by the eager checker too, now
			// that a window handed back through a call is refused where it escapes. Both
			// refusals are the fixture's own subject, so `allowEscape` tolerates both --
			// in the ROOT file only, so an unrelated refusal still stops the run.
			expected := d != nil && allowEscape && d.Primary.File == res.File.ID &&
				(d.Code == diag.SemaBorrowEscapesReturn || d.Code == diag.SemaFixedArrayViewEscapes)
			if d != nil && d.Severity >= diag.SevError && !expected {
				t.Fatalf("PRECONDITION: real stdlib fixture has an unrelated source refusal: %+v", *d)
			}
		}
	}
}

func checkReturnOriginCloneUnits(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit) (string, map[string]bool) {
	t.Helper()
	core := map[string]bool{"core/array.sg": true, "core/base.sg": true, "core/entrypoint.sg": true, "core/format.sg": true,
		"core/intrinsics.sg": true, "core/map.sg": true, "core/option.sg": true, "core/result.sg": true, "core/string.sg": true, "core/sync.sg": true}
	seen := make(map[string]bool)
	rootKey, primitive := "", 0
	for _, unit := range units {
		file := res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
		logReturnOriginCallEvidence(t, map[string]any{"owning_unit": unit.SourceKey, "path": file.Path,
			"source_sha256": sha256.Sum256(file.Content), "publication": unit.Publication})
		if seen[unit.SourceKey] || (file.ID != res.File.ID && !core[unit.SourceKey]) {
			t.Fatal("PRECONDITION: unexpected or repeated owning source unit")
		}
		seen[unit.SourceKey] = true
		if file.ID == res.File.ID {
			rootKey = unit.SourceKey
			if unit.Builder != res.Builder || unit.Sema != res.Sema || unit.FileID != res.FileID {
				t.Fatal("PRECONDITION: root source lost its original owning artifacts")
			}
		}
		if unit.SourceKey != "core/base.sg" {
			continue
		}
		for _, c := range res.Sema.CallableCandidates {
			if c.Name != "clone" || c.ModulePath != "core/base" || c.Source.File != file.ID || c.SourceKey != "builtin" {
				continue
			}
			if !c.Builtin || !c.Intrinsic || c.HasBody || c.HasSelf || c.ReceiverType != types.NoTypeID || len(c.TemplateParams) != 1 || len(c.ParamTypes) != 1 {
				t.Fatal("PRECONDITION: real core clone has a different primitive shape")
			}
			param, ok := res.Sema.TypeInterner.Lookup(c.ParamTypes[0])
			if !ok || param.Kind != types.KindReference || param.Mutable || param.Elem != c.TemplateParams[0] || c.ResultType != c.TemplateParams[0] {
				t.Fatal("PRECONDITION: real core clone lost its original &T -> T contract")
			}
			for _, identity := range unit.Publication.LocalCallables {
				if identity.BodyKey != c.BodyKey || identity.SourceKey != "builtin" || !slices.Contains(unit.Publication.LocalSymbols(c.Symbol), identity.Symbol) {
					continue
				}
				sym := unit.Symbols.Table.Symbols.Get(identity.Symbol)
				if sym == nil || sym.Flags&symbols.SymbolFlagBuiltin == 0 || sym.Span != c.Source || sym.Decl.SourceFile != file.ID || sym.Decl.ASTFile != unit.FileID {
					t.Fatal("PRECONDITION: canonical clone lost its original physical declaration owner")
				}
				fn, found := unit.Builder.Items.Fn(sym.Decl.Item)
				info, typed := unit.Sema.TypeInterner.FnInfo(sym.Type)
				if !found || fn == nil || fn.Body.IsValid() || fn.NameSpan != c.Source || !typed || info == nil || !slices.Equal(info.Params, c.ParamTypes) || info.Result != c.ResultType {
					t.Fatal("PRECONDITION: original core clone AST/type differs from canonical declaration")
				}
				primitive++
				logReturnOriginCallEvidence(t, map[string]any{"core_clone_candidate": c, "original_builtin_symbol": sym, "local_identity": identity})
			}
		}
	}
	if len(units) != 11 || rootKey == "" || primitive != 1 {
		t.Fatal("PRECONDITION: missing full eleven-unit input or unique original core clone owner")
	}
	for key := range core {
		if !seen[key] {
			t.Fatalf("PRECONDITION: missing original core input %s", key)
		}
	}
	return rootKey, core
}
