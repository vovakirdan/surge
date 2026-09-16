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
	"surge/internal/symbols"
	"surge/internal/types"
)

const returnOriginAliasLibrary = `pragma module::lib, no_std;
pub type Narrow = fn(@return_source &string, &string) -> &string;
`

const returnOriginAliasImport = `pragma module::app, no_std;
import lib;
fn first(a: &string, b: &string) -> &string { return a; }
fn second(a: &string, b: &string) -> &string { return b; }
type Narrow = fn(&string, &string) -> &string;
fn probe(outside: &string) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let selected: lib.Narrow = first; let copied = selected;
        ret copied(outside, &owned);
    };
    return 1;
}
`

func TestAnalyzeTypedReturnOriginAliasPromises(t *testing.T) {
	for _, tc := range []struct {
		name, source, want, rhs, note string
		imported, component           bool
	}{
		{"alias_narrow_copy", `fn first(a: &string, b: &string) -> &string { return a; }
fn second(a: &string, b: &string) -> &string { return b; }
type Narrow = fn(@return_source &string, &string) -> &string;
type Wide = fn(&string, &string) -> &string;
fn probe(outside: &string) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let selected: Narrow = first; let copied = selected;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "clean", "", "", false, false},
		{"alias_wide_copy", `fn first(a: &string, b: &string) -> &string { return a; }
fn second(a: &string, b: &string) -> &string { return b; }
type Narrow = fn(@return_source &string, &string) -> &string;
type Wide = fn(&string, &string) -> &string;
fn probe(outside: &string) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let selected: Wide = first; let copied = selected;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "escape", "", "", false, false},
		{"alias_incompatible_binding", `fn first(a: &string, b: &string) -> &string { return a; }
fn second(a: &string, b: &string) -> &string { return b; }
type Narrow = fn(@return_source &string, &string) -> &string;
type Wide = fn(&string, &string) -> &string;
fn probe() -> int { let selected: Narrow = second; return 1; }
`, "mismatch", "second", "fn(@return_source &string, &string) -> &string", false, false},
		{"nested_contravariance_accepted", `fn first(a: &string, b: &string) -> &string { return a; }
fn second(a: &string, b: &string) -> &string { return b; }
fn accepts_wide(f: fn(&string, &string) -> &string) -> int { return 1; }
fn probe() -> int { let selected: fn(fn(@return_source &string, &string) -> &string) -> int = accepts_wide; return 1; }
`, "clean", "", "", false, false},
		{"nested_contravariance_refused", `fn first(a: &string, b: &string) -> &string { return a; }
fn second(a: &string, b: &string) -> &string { return b; }
fn accepts_narrow(f: fn(@return_source &string, &string) -> &string) -> int { return 1; }
fn probe() -> int { let selected: fn(fn(&string, &string) -> &string) -> int = accepts_narrow; return 1; }
`, "mismatch", "accepts_narrow", "fn(fn(&string, &string) -> &string) -> int", false, false},
		{"imported_alias_full_units", returnOriginAliasImport, "clean", "", "", true, false},
		{"imported_alias_component_two_units", returnOriginAliasImport, "clean", "", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_ALIAS_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.source)), tc.source)
			var res *DiagnoseResult
			if tc.imported {
				res = returnOriginAliasModuleFixture(t, tc.source)
			} else {
				res = returnOriginTypedFixtureWithEscapeEvidence(t, tc.source, true)
			}
			inputs, err := collectReturnOriginUnits(res)
			if err != nil {
				t.Fatalf("PRECONDITION: actual owning inputs: %v", err)
			}
			users := logReturnOriginAliasInputs(t, res, inputs)
			if tc.imported {
				for _, bag := range inputs.bags {
					if bag.HasErrors() {
						t.Fatal("PRECONDITION: imported source/module diagnostics above prevent valid analysis")
					}
				}
				if len(users) != 2 || len(inputs.units) <= len(users) {
					t.Fatalf("PRECONDITION: imported fixture lost full units: all=%d users=%d", len(inputs.units), len(users))
				}
			}
			checkReturnOriginAliasTypes(t, res, inputs.units, tc.name)
			selected := inputs.units
			if tc.component {
				selected = users // Deliberately omit core owners; this input cannot authorize the full graph.
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, selected)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "alias_analysis", "case": tc.name,
				"component": tc.component, "all_units": len(inputs.units), "analyzed_units": len(selected),
				"analysis": analysis, "error": errorReturnOriginCallText(err)})
			if err != nil || analysis == nil {
				t.Fatalf("PRECONDITION: analyzer could not complete its input traversal: %v", err)
			}
			if tc.component {
				requireReturnOriginAliasMissingOwners(t, res, inputs, selected, analysis)
			} else if !analysis.Complete() {
				t.Fatalf("alias/callable source promises remain unfinished: %+v", analysis.Pending)
			}
			for _, item := range []struct {
				name string
				slot uint32
			}{{"first", 0}, {"second", 1}} {
				var summary sema.ReturnOriginSummary
				for _, candidate := range analysis.Summaries {
					if candidate.Source.File == res.File.ID && candidate.Name == item.name {
						summary = candidate
						break
					}
				}
				if summary.NoNormalReturn || summary.Unknown || !slices.Equal(summary.ParamSlots, []uint32{item.slot}) {
					t.Fatalf("actual known body lost its source: %+v", summary)
				}
			}
			probe := requireReturnOriginSummary(t, analysis, "probe")
			if probe.Source.File != res.File.ID || probe.NoNormalReturn || probe.Unknown || len(probe.ParamSlots) != 0 {
				t.Fatalf("int result manufactured reference facts: %+v", probe)
			}
			switch tc.want {
			case "clean":
				if len(analysis.Diagnostics) != 0 {
					t.Fatalf("valid alias promise was refused: %+v", analysis.Diagnostics)
				}
			case "escape":
				if len(analysis.Diagnostics) == 0 {
					t.Fatal("wide alias lost its local-owner source")
				}
				for _, d := range analysis.Diagnostics {
					requireReturnOriginValueEscape(t, tc.source, res, d)
				}
			case "mismatch":
				requireReturnOriginAliasMismatch(t, res, analysis, tc.source, tc.rhs, tc.note)
			}
		})
	}
}

func requireReturnOriginAliasMissingOwners(t *testing.T, res *DiagnoseResult, inputs returnOriginInputs, selected []sema.ReturnOriginUnit, analysis *sema.ReturnOriginAnalysis) {
	t.Helper()
	var core *sema.ReturnOriginUnit
	for i := range inputs.units {
		if inputs.units[i].SourceKey == "core/array.sg" {
			core = &inputs.units[i]
		}
	}
	if len(inputs.units) != 12 || len(selected) != 2 || core == nil || slices.ContainsFunc(selected, func(u sema.ReturnOriginUnit) bool { return u.SourceKey == core.SourceKey }) {
		t.Fatal("PRECONDITION: partial input did not omit the original core owner from twelve units")
	}
	file := res.FileSet.Get(core.Builder.Files.Get(core.FileID).Span.File)
	if file == nil || fmt.Sprintf("%x", sha256.Sum256(file.Content)) != "532a6cd1bc46d2d665d71f13f988358e42fe30dd39810117278afd46b967ffbc" {
		t.Fatal("PRECONDITION: original core clone source differs from the frozen missing-owner witness")
	}
	var edges []sema.DeferredCallableEdge
	for _, edge := range res.Sema.InstantiationGraph.DeferredCallables() {
		if edge.Kind == sema.DeferredCloneCall {
			edges = append(edges, edge)
		}
	}
	spans := [][2]uint32{{1486, 1499}, {2762, 2777}, {3222, 3236}, {3260, 3274}, {7414, 7427}, {8033, 8047}}
	if len(edges) != len(spans) {
		t.Fatal("PRECONDITION: full canonical graph lost the six original clone edges")
	}
	var want []sema.ReturnOriginPending
	for _, offsets := range spans {
		span := source.Span{File: file.ID, Start: offsets[0], End: offsets[1]}
		matched := 0
		for _, edge := range edges {
			if edge.Witness.SourceKey != core.SourceKey || edge.Witness.Site != span {
				continue
			}
			owners, uses := 0, 0
			for _, candidate := range res.Sema.CallableCandidates {
				if candidate.Symbol != edge.Caller {
					continue
				}
				for _, identity := range core.Publication.LocalCallables {
					mapped := slices.Contains(core.Publication.LocalSymbols(candidate.Symbol), identity.Symbol)
					if len(core.Publication.RootToLocalSymbols) == 0 {
						mapped = identity.Symbol == candidate.Symbol // Existing shared-table publication contract.
					}
					if identity.BodyKey != candidate.BodyKey || identity.SourceKey != candidate.SourceKey || !mapped {
						continue
					}
					owner := core.Symbols.Table.Symbols.Get(identity.Symbol)
					logReturnOriginCallEvidence(t, map[string]any{"missing_owner_edge": edge, "canonical_caller": candidate, "original_identity": identity, "original_owner": owner})
					if owner == nil || !candidate.HasBody || candidate.Source.File != file.ID || owner.Span != candidate.Source || owner.Decl.SourceFile != file.ID || owner.Decl.ASTFile != core.FileID || edge.Witness.Caller != candidate.Symbol {
						t.Fatal("PRECONDITION: retained clone caller lost its original excluded source owner")
					}
					owners++
				}
			}
			for ref, use := range core.Sema.DeferredCallableUses {
				if ref.Kind != sema.DeferredCloneCall || use != edge.UseID {
					continue
				}
				node := core.Builder.Exprs.Get(ref.Expr)
				call, ok := core.Builder.Exprs.Call(ref.Expr)
				logReturnOriginCallEvidence(t, map[string]any{"original_clone_ref": ref, "original_clone_use": use, "original_clone_node": node, "original_clone_type": core.Sema.ExprTypes[ref.Expr]})
				if node == nil || node.Span != span || !ok || call == nil || len(call.Args) != 1 || edge.ExpectedResult == types.NoTypeID || core.Sema.ExprTypes[ref.Expr] != edge.ExpectedResult {
					t.Fatal("PRECONDITION: retained clone edge lacks its actual original typed use")
				}
				uses++
			}
			if owners != 1 || uses != 1 {
				t.Fatal("PRECONDITION: clone edge lacks one original caller and one original local use")
			}
			matched++
		}
		if matched != 1 {
			t.Fatal("PRECONDITION: frozen original clone site lacks its unique retained edge")
		}
		want = append(want, sema.ReturnOriginPending{SourceKey: core.SourceKey, Span: span, Reason: "deferred clone lacks its original owning caller"})
	}
	// The same partial input cannot authorize core's deferred contract method edges either.
	for _, site := range []struct {
		key        string
		start, end uint32
	}{{"core/base.sg", 2225, 2237}, {"core/intrinsics.sg", 11606, 11621}, {"core/intrinsics.sg", 11678, 11693}} {
		var found []sema.DeferredCallableEdge
		for _, edge := range res.Sema.InstantiationGraph.DeferredCallables() {
			if edge.Kind == sema.DeferredMethodCall && edge.Witness.SourceKey == site.key && edge.Witness.Site.Start == site.start && edge.Witness.Site.End == site.end {
				found = append(found, edge)
			}
		}
		if len(found) != 1 || slices.ContainsFunc(selected, func(u sema.ReturnOriginUnit) bool { return u.SourceKey == site.key }) ||
			!slices.ContainsFunc(res.Sema.CallableCandidates, func(c sema.CallableCandidate) bool { return c.Symbol == found[0].Caller && c.HasBody }) {
			t.Fatalf("PRECONDITION: core method edge %s %d:%d lacks its unique excluded body owner", site.key, site.start, site.end)
		}
		want = append(want, sema.ReturnOriginPending{SourceKey: site.key, Span: found[0].Witness.Site, Reason: "deferred method lacks its original owning caller"})
	}
	block, err := returnOriginVerdict(analysis)
	unfinished, ok := err.(*returnOriginUnfinishedError)
	logReturnOriginCallEvidence(t, map[string]any{"expected_missing_owners": want, "partial_input_blocked": block, "verdict_error": errorReturnOriginCallText(err)})
	if analysis.Complete() || !slices.Equal(analysis.Pending, want) || !block || !ok || !slices.Equal(unfinished.Pending, want) {
		t.Fatalf("partial alias input lost its exact nine missing-owner refusals: analysis=%+v block=%t error=%v", analysis, block, err)
	}
}

func returnOriginAliasModuleFixture(t *testing.T, text string) *DiagnoseResult {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{"app/main.sg": text, "lib/lib.sg": returnOriginAliasLibrary} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("RETURN_ORIGIN_ALIAS_MODULE path=%s sha256=%x source=%q", name, sha256.Sum256([]byte(content)), content)
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
	logReturnOriginCallEvidence(t, map[string]any{"stage": "alias_module_graph", "diagnostics": bag.Items(), "error": errorReturnOriginCallText(err)})
	if err != nil || rec == nil {
		t.Fatalf("PRECONDITION: actual module graph: %v", err)
	}
	resolveModuleRecord(t.Context(), rec, root, exports, interner, opts, nil)
	resolved := rec.Symbols[fileID]
	res := &DiagnoseResult{FileSet: files, File: file, FileID: fileID, Builder: rec.Builder,
		Bag: rec.Bag, Symbols: &resolved, Sema: rec.Sema[fileID], rootRecord: rec, moduleRecords: records}
	res.finalizationIndex, err = buildFinalizationPublicationIndex(res)
	if err != nil {
		t.Fatalf("PRECONDITION: actual pre-merge publication: %v", err)
	}
	err = FinalizeInstantiationClosure(t.Context(), res, 64)
	logReturnOriginCallEvidence(t, map[string]any{"stage": "alias_closure", "diagnostics": res.Bag.Items(),
		"error": errorReturnOriginCallText(err)})
	if err != nil || res.Sema == nil || res.Sema.InstantiationClosure == nil {
		t.Fatalf("PRECONDITION: actual module closure: %v", err)
	}
	return res
}

func logReturnOriginAliasInputs(t *testing.T, res *DiagnoseResult, inputs returnOriginInputs) []sema.ReturnOriginUnit {
	t.Helper()
	var users []sema.ReturnOriginUnit
	library := filepath.Join(filepath.Dir(filepath.Dir(res.File.Path)), "lib", "lib.sg")
	for _, unit := range inputs.units {
		file := res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
		var requests, aliases []map[string]any
		for _, req := range unit.Sema.ReturnSourceDeclarations {
			requests = append(requests, map[string]any{"request": req, "params": req.Params(), "result": req.Result(),
				"syntax_span": req.Syntax.Span(), "syntax_params": req.Syntax.Params(), "syntax_result": req.Syntax.Result(),
				"markers": req.Syntax.Markers(), "all_inputs": req.Syntax.Sources().IsAllInputs(), "slots": req.Syntax.Sources().Slots()})
		}
		for _, sym := range unit.Symbols.Table.Symbols.Data() {
			if sym.Decl.SourceFile == file.ID {
				if info, ok := unit.Sema.TypeInterner.AliasInfo(sym.Type); ok {
					aliases = append(aliases, map[string]any{"symbol": sym, "type": sym.Type, "alias": info})
				}
			}
		}
		logReturnOriginCallEvidence(t, map[string]any{"stage": "alias_full_input", "source_key": unit.SourceKey,
			"path": file.Path, "source": string(file.Content), "sha256": sha256.Sum256(file.Content), "file_id": file.ID,
			"publication": unit.Publication, "expr_types": unit.Sema.ExprTypes, "expr_symbols": unit.Symbols.ExprSymbols,
			"bindings": unit.Sema.BindingTypes, "aliases": aliases, "requests": requests, "diagnostics": inputs.bags[file.ID].Items()})
		if file.ID == res.File.ID || file.Path == library {
			users = append(users, unit)
		}
	}
	return users
}

func checkReturnOriginAliasTypes(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit, name string) {
	t.Helper()
	var selected types.TypeID
	for _, sym := range res.Symbols.Table.Symbols.Data() {
		text, _ := res.Builder.StringsInterner.Lookup(sym.Name)
		if sym.Decl.SourceFile == res.File.ID && text == "selected" {
			selected = sym.Type
		}
	}
	in := res.Sema.TypeInterner
	if strings.HasPrefix(name, "nested_") {
		expected, ok := in.FnInfo(selected)
		actualName := "accepts_wide"
		accepted := strings.HasSuffix(name, "accepted")
		if !accepted {
			actualName = "accepts_narrow"
		}
		_, actual := returnOriginValueFunction(t, res, actualName)
		if !ok || len(expected.Params) != 1 || len(actual.Params) != 1 {
			t.Fatal("PRECONDITION: nested callable shape")
		}
		e, eok := in.FnInfo(expected.Params[0])
		a, aok := in.FnInfo(actual.Params[0])
		row := map[string]any{"stage": "nested_promises", "expected": expected, "actual": actual,
			"expected_input": e, "actual_input": a}
		if eok && aok {
			row["expected_all"], row["expected_slots"] = e.ReturnSources().IsAllInputs(), e.ReturnSources().Slots()
			row["actual_all"], row["actual_slots"] = a.ReturnSources().IsAllInputs(), a.ReturnSources().Slots()
		}
		logReturnOriginCallEvidence(t, row)
		if !eok || !aok || !slices.Equal(e.Params, a.Params) || e.Result != a.Result || expected.Result != actual.Result ||
			e.ReturnSources().IsAllInputs() == accepted || a.ReturnSources().IsAllInputs() != accepted {
			t.Fatal("PRECONDITION: variance witness lost its opposite promises")
		}
		return
	}
	alias, ok := in.AliasInfo(selected)
	if !ok {
		t.Fatal("PRECONDITION: selected binding lost its nominal callable alias")
	}
	info, ok := in.FnInfo(alias.Target)
	if !ok {
		t.Fatal("PRECONDITION: selected alias lost its function target")
	}
	if name != "alias_incompatible_binding" {
		copies := 0
		for _, sym := range res.Symbols.Table.Symbols.Data() {
			text, _ := res.Builder.StringsInterner.Lookup(sym.Name)
			if sym.Decl.SourceFile == res.File.ID && text == "copied" && sym.Type == selected {
				copies++
			}
		}
		if copies == 0 {
			t.Fatal("PRECONDITION: copied binding lost the selected declared alias type")
		}
	}
	wide := name == "alias_wide_copy"
	if info.ReturnSources().IsAllInputs() != wide || (!wide && !slices.Equal(info.ReturnSources().Slots(), []uint32{0})) {
		t.Fatal("PRECONDITION: selected alias has the wrong promise")
	}
	found := 0
	for _, unit := range units {
		if unit.Builder.Files.Get(unit.FileID).Span.File != alias.Decl.File {
			continue
		}
		if strings.HasPrefix(name, "imported_") {
			for _, id := range unit.Builder.Files.Get(unit.FileID).Items {
				if unit.Builder.Items.Get(id).Kind == ast.ItemFn {
					t.Fatal("PRECONDITION: alias-only library acquired a function body")
				}
			}
		}
		for _, id := range unit.Builder.Files.Get(unit.FileID).Items {
			item, typed := unit.Builder.Items.Type(id)
			if !typed || item.Span != alias.Decl {
				continue
			}
			decl := unit.Builder.Items.TypeAlias(item)
			if decl == nil {
				t.Fatal("PRECONDITION: alias owner is not the original alias AST")
			}
			node := unit.Builder.Types.Get(decl.Target)
			if node == nil || node.Kind != ast.TypeExprFn {
				t.Fatal("PRECONDITION: alias lost original callable syntax")
			}
			found++
			matched := wide
			for _, req := range unit.Sema.ReturnSourceDeclarations {
				if req.TypeExpr == decl.Target && req.Syntax.Span() == node.Span && req.Owner == symbols.NoSymbolID &&
					req.Syntax.Sources().Equal(info.ReturnSources()) && slices.Equal(req.Params(), info.Params) && req.Result() == info.Result && unit.Symbols.Table.Scopes.Get(req.Scope) != nil {
					matched = true
				}
			}
			if !matched {
				t.Fatal("PRECONDITION: selected alias lacks its original owning typed request")
			}
		}
	}
	if found != 1 {
		t.Fatalf("PRECONDITION: alias has %d original source owners", found)
	}
	if strings.HasPrefix(name, "imported_") {
		if alias.Decl.File == res.File.ID {
			t.Fatal("PRECONDITION: imported alias resolved to root declaration")
		}
		local := 0
		for _, sym := range res.Symbols.Table.Symbols.Data() {
			text, _ := res.Builder.StringsInterner.Lookup(sym.Name)
			if sym.Kind != symbols.SymbolType || sym.Decl.SourceFile != res.File.ID || text != "Narrow" ||
				sym.Decl.ASTFile != res.FileID || !sym.Decl.Item.IsValid() {
				continue
			}
			other, exists := in.AliasInfo(sym.Type)
			item, typed := res.Builder.Items.Type(sym.Decl.Item)
			if !exists || !typed || item.Span != other.Decl || sym.Type == selected || other.Decl == alias.Decl {
				t.Fatal("PRECONDITION: aliases share manufactured identity")
			}
			fn, exists := in.FnInfo(other.Target)
			if !exists || !fn.ReturnSources().IsAllInputs() {
				t.Fatal("PRECONDITION: root namesake lost AllInputs")
			}
			local++
		}
		if local != 1 {
			t.Fatal("PRECONDITION: root alias namesake missing")
		}
	}
}

func requireReturnOriginAliasMismatch(t *testing.T, res *DiagnoseResult, analysis *sema.ReturnOriginAnalysis, text, rhs, noteText string) {
	t.Helper()
	if len(analysis.Diagnostics) != 1 {
		t.Fatalf("expected one callable source mismatch: %+v", analysis.Diagnostics)
	}
	d := analysis.Diagnostics[0]
	start := strings.LastIndex(text, " = "+rhs+";") + len(" = ")
	note := strings.LastIndex(text, noteText)
	if d.Code != diag.SemaReturnSourceIncompatible || d.Severity != diag.SevError || d.Message != returnOriginCallableMismatch ||
		d.Primary.File != res.File.ID || int(d.Primary.Start) != start || int(d.Primary.End) != start+len(rhs) || len(d.Help) == 0 {
		t.Fatalf("wrong callable RHS diagnostic: %+v", d)
	}
	for _, n := range d.Notes {
		if n.Span.File == res.File.ID && int(n.Span.Start) == note && int(n.Span.End) == note+len(noteText) {
			return
		}
	}
	t.Fatal("callable mismatch lost the original destination type promise")
}
