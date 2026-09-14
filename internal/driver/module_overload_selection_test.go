package driver

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const moduleOverloadDependency = `pragma module::dep, no_std;
pub fn pick(value: int) -> int { return value; }
@overload pub fn pick(value: string) -> string { return value; }
pub fn with_default(@allow_to value: int = 1) -> int { return value; }
pub fn pack(...values: int) -> int { return 0; }
`

const moduleOverloadApp = `pragma module::app, no_std;
import dep as selected;
fn probe() -> nothing {
    let a: int = selected.pick(7);
    let b: string = selected.pick("x");
    let c: int = selected.with_default();
    let d: int = selected.pack(1, 2);
    return nothing;
}
`

func TestModuleQualifiedOverloadPreservesSelectedSignature(t *testing.T) {
	res, dep := moduleOverloadFixture(t)
	physical := moduleOverloadDeclarations(t, res, dep)
	in := res.Sema.TypeInterner
	u := sema.ReturnOriginUnit{Builder: res.Builder, FileID: res.FileID, Sema: res.Sema, Symbols: res.Symbols}
	for _, tc := range []struct {
		name, text              string
		start, end              uint32
		winner, member, actuals int
		result                  types.TypeID
	}{
		{"pick_int", "selected.pick(7)", 93, 109, 0, 0, 1, in.Builtins().Int},
		{"pick_string", "selected.pick(\"x\")", 131, 149, 1, 0, 1, in.Builtins().String},
		{"omitted_default", "selected.with_default()", 168, 191, 2, 2, 0, in.Builtins().Int},
		{"variadic_tail", "selected.pack(1, 2)", 210, 229, 3, 3, 2, in.Builtins().Int},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := selectedCallableSite(t, res, u, tc.text, ast.ExprCall)
			node := res.Builder.Exprs.Get(id)
			call, ok := res.Builder.Exprs.Call(id)
			if !ok || call == nil || node.Span != (source.Span{File: res.File.ID, Start: tc.start, End: tc.end}) || len(call.Args) != tc.actuals {
				t.Fatal("PRECONDITION: frozen typed call/physical actual count changed")
			}
			member, ok := res.Builder.Exprs.Member(call.Target)
			if !ok || member == nil {
				t.Fatal("PRECONDITION: module call lost its member expression")
			}
			module := res.Symbols.Table.Symbols.Get(res.Symbols.ExprSymbols[member.Target])
			if module == nil || module.Kind != symbols.SymbolModule || module.ModulePath != "app/dep" {
				t.Fatal("PRECONDITION: original syntactic module alias missing")
			}
			selectedID, memberID := res.Symbols.ExprSymbols[id], res.Symbols.ExprSymbols[call.Target]
			selected, memberSymbol := res.Symbols.Table.Symbols.Get(selectedID), res.Symbols.Table.Symbols.Get(memberID)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "module_overload_selection", "case": tc.name, "call": id, "member": call.Target,
				"call_span": node.Span, "selected_id": selectedID, "member_id": memberID, "selected": selected, "member_symbol": memberSymbol,
				"physical": physical[tc.winner], "exports_path": dep.Exports.Path, "lookup_path": module.ModulePath})
			checkModuleOverloadSymbol(t, selected, physical[tc.winner])
			checkModuleOverloadSymbol(t, memberSymbol, physical[tc.member])
			info, known := in.FnInfo(selected.Type)
			if !known || info == nil || info.Result != tc.result || res.Sema.ExprTypes[id] != tc.result {
				t.Fatal("module overload selected the wrong typed result")
			}
			for i, arg := range call.Args {
				actual := res.Sema.ExprTypes[arg.Value]
				logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "actual_slot": i, "expr": arg.Value, "type": actual, "fn_info": info})
				if actual != tc.result || arg.Name != source.NoStringID {
					t.Fatal("typed module call lost its positional actual type")
				}
			}
		})
	}
}

// Preserve the ordinary module typing stage; owner remapping has a separate oracle.
func moduleOverloadFixture(t *testing.T) (*DiagnoseResult, *moduleRecord) {
	t.Helper()
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	root := t.TempDir()
	for name, text := range map[string]string{"app/main.sg": moduleOverloadApp, "dep/main.sg": moduleOverloadDependency} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		logReturnOriginCallEvidence(t, map[string]any{"stage": "module_overload_source", "path": name, "source": text, "sha256": sha256.Sum256([]byte(text))})
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
	logReturnOriginCallEvidence(t, map[string]any{"stage": "module_overload_graph", "bag": bag.Items(), "error": errorReturnOriginCallText(err)})
	if err != nil || rec == nil {
		t.Fatalf("PRECONDITION: actual module graph: %v", err)
	}
	resolveModuleRecord(t.Context(), rec, root, exports, interner, opts, nil)
	resolved := rec.Symbols[fileID]
	res := &DiagnoseResult{FileSet: files, File: file, FileID: fileID, Builder: rec.Builder, Bag: rec.Bag,
		Symbols: &resolved, Sema: rec.Sema[fileID], rootRecord: rec, moduleRecords: records}
	checkReturnOriginStdlibBags(t, res, false)
	requireReturnOriginTyped(t, res)
	var dependency *moduleRecord
	for _, owner := range finalizationPublicationRecords(res) {
		if owner.Meta != nil && owner.Meta.Path == "dep" && owner.Exports == exports["app/dep"] {
			if dependency != nil {
				t.Fatal("PRECONDITION: dependency owner is ambiguous")
			}
			dependency = owner
		}
	}
	if dependency == nil || dependency.Exports == nil || dependency.Exports.Path != "dep" || len(dependency.FileIDs) != 1 ||
		files.Get(dependency.Builder.Files.Get(dependency.FileIDs[0]).Span.File).Path != filepath.Join(root, "dep/main.sg") {
		t.Fatal("PRECONDITION: physical dependency/export container missing")
	}
	return res, dependency
}

func moduleOverloadDeclarations(t *testing.T, res *DiagnoseResult, dep *moduleRecord) []*symbols.Symbol {
	t.Helper()
	fileID := dep.FileIDs[0]
	file := dep.Builder.Files.Get(fileID)
	content := res.FileSet.Get(file.Span.File).Content
	if string(content) != moduleOverloadDependency {
		t.Fatal("PRECONDITION: dependency source bytes changed")
	}
	var out []*symbols.Symbol
	for _, item := range file.Items {
		fn, ok := dep.Builder.Items.Fn(item)
		if !ok || fn == nil {
			continue
		}
		i := len(out)
		if i >= 4 || fn.FnKeywordSpan.Start != []uint32{32, 91, 146, 217}[i] || fn.NameSpan.Start != []uint32{35, 94, 149, 220}[i] ||
			fn.NameSpan.End != []uint32{39, 98, 161, 224}[i] || !fn.Body.IsValid() {
			t.Fatal("PRECONDITION: frozen original function declaration changed")
		}
		ids := dep.Symbols[fileID].ItemSymbols[item]
		if len(ids) != 1 {
			t.Fatal("PRECONDITION: original function symbol is not unique")
		}
		sym := dep.Table.Symbols.Get(ids[0])
		if sym == nil || sym.Decl.Item != item || sym.Decl.ASTFile != fileID || sym.Decl.SourceFile != file.Span.File || sym.Span != fn.NameSpan {
			t.Fatal("PRECONDITION: original symbol lost physical declaration ownership")
		}
		params := dep.Builder.Items.GetFnParamIDs(fn)
		if len(params) != 1 {
			t.Fatal("PRECONDITION: expected exactly one physical formal")
		}
		param := dep.Builder.Items.FnParam(params[0])
		if param == nil {
			t.Fatal("PRECONDITION: original physical formal is missing")
		}
		allowTo := false
		for _, attr := range dep.Builder.Items.CollectAttrs(param.AttrStart, param.AttrCount) {
			allowTo = allowTo || dep.Builder.StringsInterner.MustLookup(attr.Name) == "allow_to"
		}
		sig := sym.Signature
		key := symbols.TypeKey("int")
		expected := res.Sema.TypeInterner.Builtins().Int
		if i == 1 {
			key = "string"
			expected = res.Sema.TypeInterner.Builtins().String
		}
		if sig == nil || !slices.Equal(sig.Params, []symbols.TypeKey{key}) || sig.Result != key || !slices.Equal(sig.ParamNames, []source.StringID{param.Name}) ||
			moduleOverloadBit(t, sig.Defaults) != (i == 2) || moduleOverloadBit(t, sig.Variadic) != (i == 3) || moduleOverloadBit(t, sig.AllowTo) != (i == 2) ||
			param.Default.IsValid() != (i == 2) || param.Variadic != (i == 3) || allowTo != (i == 2) {
			t.Fatal("original default/variadic/allow_to signature differs from source")
		}
		if i == 2 {
			span := dep.Builder.Exprs.Get(param.Default).Span
			if string(content[span.Start:span.End]) != "1" {
				t.Fatal("original default expression changed")
			}
		}
		info, known := res.Sema.TypeInterner.FnInfo(sym.Type)
		if !known || info == nil || len(info.Params) != 1 || info.Result != expected {
			t.Fatal("original function lost its typed physical signature")
		}
		if i == 3 {
			if elem, ok := res.Sema.TypeInterner.ArrayInfo(info.Params[0]); !ok || elem != info.Result {
				t.Fatal("variadic physical parameter is not Array<int>")
			}
		} else if info.Params[0] != info.Result {
			t.Fatal("original scalar parameter/result disagree")
		}
		matched := 0
		exported := dep.Exports.Lookup(dep.Builder.StringsInterner.MustLookup(fn.Name))
		for j := range exported {
			if exported[j].Span == sym.Span && exported[j].Type == sym.Type && exported[j].Signature == sig {
				matched++
			}
		}
		if matched != 1 || len(exported) != []int{2, 2, 1, 1}[i] {
			t.Fatal("PRECONDITION: actual export set lost physical declaration")
		}
		logReturnOriginCallEvidence(t, map[string]any{"stage": "module_overload_physical", "item": item, "symbol_id": ids[0], "symbol": sym, "fn": fn, "fn_info": info, "exported": exported})
		out = append(out, sym)
	}
	if len(out) != 4 {
		t.Fatal("PRECONDITION: missing original source declarations")
	}
	return out
}

func moduleOverloadBit(t *testing.T, bits []bool) bool {
	t.Helper()
	if len(bits) > 1 {
		t.Fatal("signature metadata exceeds its one physical slot")
	}
	return len(bits) == 1 && bits[0]
}

func checkModuleOverloadSymbol(t *testing.T, got, physical *symbols.Symbol) {
	t.Helper()
	if got == nil || got.Kind != symbols.SymbolFunction || got.Span != physical.Span || got.Type != physical.Type ||
		got.Flags&symbols.SymbolFlagPublic == 0 || got.Flags&symbols.SymbolFlagMethod != 0 || got.ReceiverKey != "" || got.Signature == nil {
		t.Fatal("selected symbol does not identify the expected physical free function")
	}
	a, b := got.Signature, physical.Signature
	if !a.HasBody || a.HasSelf || !slices.Equal(a.Params, b.Params) || !slices.Equal(a.ParamNames, b.ParamNames) || a.Result != b.Result ||
		moduleOverloadBit(t, a.Defaults) != moduleOverloadBit(t, b.Defaults) || moduleOverloadBit(t, a.Variadic) != moduleOverloadBit(t, b.Variadic) ||
		moduleOverloadBit(t, a.AllowTo) != moduleOverloadBit(t, b.AllowTo) || !a.ReturnSourceSyntax.Sources().IsAllInputs() {
		t.Fatal("module-selected signature lost original parameter metadata")
	}
}
