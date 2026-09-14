package sema

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const externHeaderPrelude = "pragma no_std;\ntag Some<T>(T); type Option<T> = Some(T) | nothing;\ntype Cursor<T> = { value: T };\n"

func TestExternHeaderDeclarationOrder(t *testing.T) {
	for _, name := range []string{"ordinary_before", "ordinary_after", "intrinsic_before", "intrinsic_after"} {
		t.Run(name, func(t *testing.T) { checkExternHeaderOrder(t, name) })
	}
}

func TestExternHeaderContextAndReuse(t *testing.T) {
	for _, name := range []string{"caller_shadow", "later_owner_walk"} {
		t.Run(name, func(t *testing.T) { checkExternHeaderOrder(t, name) })
	}
}

func TestExternHeaderMetadataAndRefusal(t *testing.T) {
	for _, name := range []string{"return_sources", "unknown_result", "body_once"} {
		t.Run(name, func(t *testing.T) { checkExternHeaderOrder(t, name) })
	}
}

// The fixture follows diagnose_modules: DeclareOnly for all files, then one
// file's ReuseDecls resolution and Check at a time. No signature is installed.
func checkExternHeaderOrder(t *testing.T, name string) {
	t.Helper()
	intrinsic, marked := strings.HasPrefix(name, "intrinsic_"), name == "return_sources"
	split := intrinsic || name == "later_owner_walk" || marked
	ref, result, generics, attr, tail := "&mut ", "Option<T>", "U", "", ";"
	if intrinsic {
		attr = "@intrinsic pub "
	}
	if name == "caller_shadow" || name == "unknown_result" {
		generics = "T, U, V"
	}
	if name == "unknown_result" {
		result = "V" // Isolate the undeclared name from Option<NoTypeID> recovery.
	}
	if name == "body_once" {
		tail = " { let invalid: &int; return nothing; }"
	}
	marker, extra := "", ""
	if marked {
		ref, result, marker = "&", "Option<&T>", "@return_source "
		extra = " fn callback(self: &Cursor<T>, cb: fn(@return_source &T) -> &T) -> nothing;"
	}
	decl := fmt.Sprintf("extern<Cursor<T>> { %sfn next(%sself: %sCursor<T>) -> %s%s%s }\n", attr, marker, ref, result, tail, extra)
	probe := fmt.Sprintf("fn probe<%s>(iter: %sCursor<U>) { let result = iter.next(); }\n", generics, ref)
	sources := []string{externHeaderPrelude + probe + decl}
	order, owner := []int{0}, 0
	if name == "ordinary_before" || name == "body_once" {
		sources[0] = externHeaderPrelude + decl + probe
	}
	if split {
		sources, order, owner = []string{externHeaderPrelude + probe, "pragma no_std;\n" + decl}, []int{0, 1}, 1
		if name == "intrinsic_before" {
			order = []int{1, 0}
		}
	}
	fs, b, in := source.NewFileSet(), ast.NewBuilder(ast.Hints{}, source.NewInterner()), types.NewInterner()
	in.Strings = b.StringsInterner
	files, paths := make([]ast.FileID, len(sources)), []string{"/extern-header/core/array.sg", "/extern-header/core/intrinsics.sg"}
	modules := []string{"core/array", "core/intrinsics"}
	moduleFiles := make(map[ast.FileID]struct{})
	bag := diag.NewBag(128)
	reporter := &diag.BagReporter{Bag: bag}
	for i, text := range sources {
		file := fs.Get(fs.AddVirtual(paths[i], []byte(text)))
		parsed := parser.ParseFile(context.Background(), fs, lexer.New(file, lexer.Options{Reporter: reporter}), b, parser.Options{Reporter: reporter})
		files[i], moduleFiles[parsed.File] = parsed.File, struct{}{}
	}
	if bag.HasErrors() {
		t.Fatalf("setup parse: %s", diagnosticsSummary(bag))
	}
	table := symbols.NewTable(symbols.Hints{}, b.StringsInterner)
	root := table.ModuleRoot("core", b.Files.Get(files[0]).Span)
	resolve := func(i int, declare bool) symbols.Result {
		return symbols.ResolveFile(b, files[i], &symbols.ResolveOptions{Table: table, Reporter: reporter, ModulePath: modules[i], FilePath: paths[i], BaseDir: "/extern-header", ModuleScope: root, NoStd: true, DeclareOnly: declare, ReuseDecls: !declare})
	}
	for i := range files {
		resolve(i, true)
	}
	var method symbols.SymbolID
	for i, sym := range table.Symbols.Data() {
		if sym.Kind == symbols.SymbolFunction && b.StringsInterner.MustLookup(sym.Name) == "next" {
			method = symbols.SymbolID(i + 1)
		}
	}
	sym := table.Symbols.Get(method)
	if sym == nil || sym.Type != types.NoTypeID || sym.Signature == nil || sym.Decl.ASTFile != files[owner] {
		t.Fatal("setup lacks an original untyped extern declaration")
	}
	member := b.Items.ExternMember(ast.ExternMemberID(sym.Decl.Expr))
	if member == nil || member.Kind != ast.ExternMemberFn || b.Items.FnByPayload(member.Fn) == nil {
		t.Fatal("setup declaration does not identify its original extern member")
	}
	index := &typeChecker{builder: b, fileID: files[0], types: in, symbols: &symbols.Result{Table: table}, reporter: reporter}
	index.buildMagicIndex()
	if candidates := index.magic[canonicalTypeKey(sym.ReceiverKey)]["next"]; len(candidates) != 1 || candidates[0] != sym.Signature || index.magicSymbolForSignature(sym.Signature).IsValid() == intrinsic {
		t.Fatal("setup did not retain exact signature pointer and expected builtin magic omission")
	}
	if bag.HasErrors() {
		t.Fatalf("setup declarations/index: %s", diagnosticsSummary(bag))
	}
	results := make([]*Result, len(files))
	var firstHeaders, firstRequests string
	externHeaderCapture(t, "declared", b, table, in, results, sym.Decl.ASTFile, method, sources, order, bag)
	for step, i := range order {
		syms := resolve(i, false)
		if bag.HasErrors() && name != "unknown_result" && name != "body_once" {
			t.Fatalf("setup full resolution: %s", diagnosticsSummary(bag))
		}
		syms.ModuleFiles = moduleFiles
		checked := Check(context.Background(), b, files[i], Options{Symbols: &syms, Types: in, Reporter: reporter, ModulePath: b.StringsInterner.Intern(modules[i])})
		results[i] = &checked
		sym = table.Symbols.Get(method)
		headers, requests := externHeaderCapture(t, fmt.Sprintf("checked_%d", i), b, table, in, results, sym.Decl.ASTFile, method, sources, order, bag)
		if split && step == 0 && i != owner {
			firstHeaders, firstRequests = headers, requests
			if _, ok := in.FnInfo(sym.Type); !ok {
				t.Error("forward header absent before owning-file walk")
			}
		}
		if i == 0 {
			externHeaderAssertCall(t, b, &checked, &syms, sources[0], sym.Signature, marked, name == "unknown_result")
		}
		if split && step == 1 && firstHeaders != "" {
			if headers != firstHeaders {
				t.Error("owning-file walk replaced original FnInfo or generic descriptors")
			}
			if marked && requests != firstRequests {
				t.Error("owning-file walk mutated requests retained by the earlier Result")
			}
		}
	}
	if name == "unknown_result" {
		start := uint32(strings.LastIndex(sources[owner], "-> V") + len("-> "))
		if bag.Len() != 1 || bag.Items()[0].Code != diag.SemaUnresolvedSymbol || bag.Items()[0].Message != "unknown type V" || bag.Items()[0].Primary != (source.Span{File: b.Files.Get(files[owner]).Span.File, Start: start, End: start + 1}) {
			t.Errorf("unknown declaration result must report once at its original source: %s", diagnosticsSummary(bag))
		}
		return
	}
	if name == "body_once" {
		start := uint32(strings.Index(sources[owner], "let invalid: &int;") + len("let invalid: "))
		if bag.Len() != 1 || bag.Items()[0].Code != diag.SemaTypeMismatch || !strings.HasPrefix(bag.Items()[0].Message, "default is not defined for &int") || bag.Items()[0].Primary != (source.Span{File: b.Files.Get(files[owner]).Span.File, Start: start, End: start + 4}) {
			t.Errorf("body must report once at its original source: %s", diagnosticsSummary(bag))
		}
		return
	}
	if bag.HasErrors() {
		t.Errorf("sema: %s", diagnosticsSummary(bag))
	}
	if name == "later_owner_walk" || marked {
		fn, ok := in.FnInfo(sym.Type)
		if ok && len(fn.Params) > 0 {
			shape, _ := in.StructInfo((&typeChecker{types: in}).valueType(fn.Params[0]))
			if shape == nil || !slices.Equal(results[owner].InstantiationTemplateParams[method], shape.TypeArgs) {
				t.Error("owner template record lost exact published receiver descriptors")
			}
		}
	}
	if marked {
		fn, _ := in.FnInfo(sym.Type)
		for file, res := range results {
			functionRequests, nestedRequests := 0, 0
			for _, req := range res.ReturnSourceDeclarations {
				if req.Syntax.Span().File != b.Files.Get(files[owner]).Span.File {
					continue
				}
				scope := table.Scopes.Get(req.Scope)
				if scope == nil || (scope.Owner.ASTFile.IsValid() && scope.Owner.ASTFile != files[owner]) || req.TypeParamEnv == 0 {
					t.Error("original request lost its declaration scope or generic environment")
				}
				if req.TypeExpr.IsValid() {
					nestedRequests++
					params := req.Params()
					if len(params) != 1 || params[0] != req.Result() || !slices.Equal(req.Syntax.Sources().Slots(), []uint32{0}) {
						t.Error("nested callback request lost exact typed roots or marked slot")
					}
				} else {
					functionRequests++
					if fn == nil || req.Owner != method || !slices.Equal(req.Params(), fn.Params) || req.Result() != fn.Result {
						t.Error("function request roots differ from original callable descriptor")
					}
				}
			}
			if functionRequests > 1 || nestedRequests > 1 || (file == owner && (functionRequests != 1 || nestedRequests != 1)) {
				t.Errorf("Result[%d] original function/nested requests=%d/%d; owner wants 1/1, no Result permits duplicates", file, functionRequests, nestedRequests)
			}
		}
	}
}

func externHeaderAssertCall(t *testing.T, b *ast.Builder, res *Result, syms *symbols.Result, text string, signature *symbols.FunctionSignature, borrowed, refused bool) {
	t.Helper()
	start, found := strings.Index(text, "iter.next()"), 0
	for id, got := range res.ExprTypes {
		e := b.Exprs.Get(id)
		if e == nil || e.Kind != ast.ExprCall || int(e.Span.Start) != start || int(e.Span.End) != start+len("iter.next()") || e.Span.File != b.Files.Get(syms.File).Span.File {
			continue
		}
		found++
		call, _ := b.Exprs.Call(id)
		member, _ := b.Exprs.Member(call.Target)
		if member == nil {
			t.Fatal("call receiver is absent")
		}
		recv := res.ExprTypes[member.Target]
		tc := &typeChecker{builder: b, fileID: syms.File, types: res.TypeInterner, symbols: syms, result: res, typeParamNames: make(map[types.TypeID]source.StringID)}
		shape, ok := res.TypeInterner.StructInfo(tc.valueType(recv))
		if !ok || shape == nil || len(shape.TypeArgs) != 1 {
			t.Fatal("actual receiver has no exact generic argument")
		}
		want := shape.TypeArgs[0]
		param, ok := res.TypeInterner.TypeParamInfo(want)
		if !ok || param == nil {
			t.Fatal("actual receiver payload is not a caller generic descriptor")
		}
		caller := lookupSymbolByName(syms, b.StringsInterner.Intern("probe"))
		if param.Owner != uint32(caller) {
			t.Error("receiver payload lost its original caller owner")
		}
		tc.typeParamNames[want] = param.Name
		tc.buildMagicIndex()
		selected, _, _, _, matched := tc.matchMethodSignature("next", recv, ast.NoExprID, nil, nil, false)
		if !matched || selected != signature {
			t.Fatal("actual receiver does not select the original signature pointer")
		}
		for _, callee := range syms.Table.Symbols.Data() {
			if callee.Signature == selected {
				if fn, ok := res.TypeInterner.FnInfo(callee.Type); ok && len(fn.Params) > 0 {
					formal, _ := res.TypeInterner.StructInfo(tc.valueType(fn.Params[0]))
					if formal == nil || len(formal.TypeArgs) != 1 || formal.TypeArgs[0] == want {
						t.Error("original declaration formal was replaced by the caller descriptor")
					}
				}
			}
		}
		t.Logf("call expr=%d type=%d selected_symbol=%d magic=%d payload=%d owner=%d index=%d", id, got, syms.ExprSymbols[id], tc.magicSymbolForSignature(selected), want, param.Owner, param.Index)
		if refused {
			if got != types.NoTypeID {
				t.Errorf("early call result=%d, want unresolved declaration result to remain Type0", got)
			}
			continue
		}
		union, ok := res.TypeInterner.UnionInfo(got)
		if !ok || union == nil || len(union.TypeArgs) != 1 || len(union.Members) != 2 || len(union.Members[0].TagArgs) != 1 || union.Members[0].TagArgs[0] != union.TypeArgs[0] {
			t.Errorf("call result=%d lacks exact Option payload %d", got, want)
			continue
		}
		payload := union.TypeArgs[0]
		if borrowed {
			ref, exists := res.TypeInterner.Lookup(payload)
			if !exists || ref.Kind != types.KindReference || ref.Mutable {
				t.Error("marked result lost shared reference")
				continue
			}
			payload = ref.Elem
		}
		if payload != want {
			t.Errorf("call payload=%d, want receiver's exact %d", payload, want)
		}
	}
	if found != 1 {
		t.Errorf("typed call entries=%d, want exactly one", found)
	}
}

func externHeaderCapture(t *testing.T, phase string, b *ast.Builder, table *symbols.Table, in *types.Interner, results []*Result, owner ast.FileID, method symbols.SymbolID, sources []string, order []int, bag *diag.Bag) (string, string) {
	t.Helper()
	headers, requests := []map[string]any{}, []map[string]any{}
	earlierRequests := []map[string]any{}
	for i, sym := range table.Symbols.Data() {
		if sym.Kind != symbols.SymbolFunction || sym.Decl.ASTFile != owner || !sym.Decl.Expr.IsValid() {
			continue
		}
		entry := map[string]any{"symbol": i + 1, "name": b.StringsInterner.MustLookup(sym.Name), "decl": sym.Decl, "scope": sym.Scope, "type": sym.Type}
		if fn, ok := in.FnInfo(sym.Type); ok {
			entry["params"], entry["result"], entry["sources"] = fn.Params, fn.Result, fn.ReturnSources().Slots()
			entry["all_inputs"] = fn.ReturnSources().IsAllInputs()
			if len(fn.Params) > 0 {
				shape, _ := in.StructInfo((&typeChecker{types: in}).valueType(fn.Params[0]))
				entry["receiver"] = shape
				if shape != nil && len(shape.TypeArgs) == 1 {
					param, _ := in.TypeParamInfo(shape.TypeArgs[0])
					entry["formal_param"] = param
				}
			}
			if len(fn.Params) == 2 {
				if nested, ok := in.FnInfo(fn.Params[1]); ok {
					entry["nested"] = map[string]any{"params": nested.Params, "result": nested.Result, "slots": nested.ReturnSources().Slots(), "all_inputs": nested.ReturnSources().IsAllInputs()}
					if !slices.Equal(nested.ReturnSources().Slots(), []uint32{0}) {
						t.Error("nested callback lost its declared return-source slot")
					}
				}
			}
		}
		headers = append(headers, entry)
	}
	for file, res := range results {
		if res == nil {
			continue
		}
		for _, req := range res.ReturnSourceDeclarations {
			if req.Syntax.Span().File != b.Files.Get(owner).Span.File {
				continue
			}
			requests = append(requests, map[string]any{"result_file": file, "owner": req.Owner, "type_expr": req.TypeExpr, "scope": req.Scope, "env": req.TypeParamEnv, "params": req.Params(), "result": req.Result(), "span": req.Syntax.Span(), "slots": req.Syntax.Sources().Slots()})
			if file == 0 {
				earlierRequests = append(earlierRequests, requests[len(requests)-1])
			}
			if !req.TypeExpr.IsValid() && req.Owner != method {
				t.Error("original function request acquired a different owner")
			}
		}
	}
	encode := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	h, r := encode(headers), encode(requests)
	scopes := []map[string]any{}
	for i, scope := range table.Scopes.Data() {
		if scope.Owner.ASTFile == owner && scope.Owner.Extern.IsValid() {
			scopes = append(scopes, map[string]any{"id": i + 1, "scope": scope})
		}
	}
	t.Logf("EXTERN_HEADER_CAPTURE %s", encode(map[string]any{"phase": phase, "sources": sources, "order": order, "headers": json.RawMessage(h), "requests": json.RawMessage(r), "owner_scopes": scopes, "diagnostics": bag.Items()}))
	return h, encode(earlierRequests)
}
