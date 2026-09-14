package driver

import (
	"crypto/sha256"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type returnOriginArgumentCase struct {
	name, body, want, call, callee, rhs string
	slot                                int
	callback                            bool
	invoke, probe                       []uint32
}

// Real single-unit typing precedes private origin analysis. Only the two
// block-exit witnesses retain the existing SEM3139 bag as private evidence.
func TestAnalyzeTypedReturnOriginCallableArguments(t *testing.T) {
	const prefix = `fn first(a: &string, b: &string) -> &string { return a; }
fn second(a: &string, b: &string) -> &string { return b; }
type Narrow = fn(@return_source &string, &string) -> &string;
type Wide = fn(&string, &string) -> &string;
fn take(cb: Narrow) -> int { return 1; }
`
	for _, tc := range []returnOriginArgumentCase{
		{"known_narrow_argument", `fn probe() -> int { let f = first; let copied = f; return take(copied); }
`, "clean", "take(copied)", "take", "", 0, false, nil, nil},
		{"known_wrong_argument", `fn probe() -> int { return take(second); }
`, "mismatch", "take(second)", "take", "second", 0, false, nil, nil},
		{"wide_copy_reassignment_argument", `fn probe() -> int {
    let wide: Wide = first; let mut copied = wide; copied = first;
    return take(copied);
}
`, "mismatch", "take(copied)", "take", "copied", 0, false, nil, nil},
		{"incoming_narrow_argument", `fn probe(cb: Narrow) -> int { let copied = cb; return take(copied); }
`, "clean", "take(copied)", "take", "", 0, false, nil, nil},
		{"named_two_callable_slots", `fn ordered(a: Narrow, b: Wide, ignored: int = 1) -> int { return 1; }
fn probe() -> int { return ordered(b: second, a: first); }
`, "clean", "ordered(b: second, a: first)", "ordered", "", 0, false, nil, nil},
		{"named_wrong_slot", `fn ordered(a: Narrow, b: Wide, ignored: int = 1) -> int { return 1; }
fn probe() -> int { return ordered(b: first, a: second); }
`, "mismatch", "ordered(b: first, a: second)", "ordered", "second", 0, false, nil, nil},
		{"invoked_narrow_result", `fn invoke(cb: Narrow, a: &string, b: &string) -> &string { return cb(a, b); }
fn probe(outside: &string) -> int {
    let escaped: &string = {
        let owned: string = "local";
        ret invoke(first, outside, &owned);
    };
    return 1;
}
`, "clean", "invoke(first, outside, &owned)", "invoke", "", 0, false, []uint32{1}, nil},
		{"invoked_wide_result", `fn invoke(cb: Wide, a: &string, b: &string) -> &string { return cb(a, b); }
fn probe(outside: &string) -> int {
    let escaped: &string = {
        let owned: string = "local";
        ret invoke(first, outside, &owned);
    };
    return 1;
}
`, "escape", "invoke(first, outside, &owned)", "invoke", "", 0, false, []uint32{1, 2}, nil},
		{"method_self_slot", `type Receiver = { n: int };
extern<Receiver> {
    fn take(self: &Receiver, cb: Narrow) -> int { return 1; }
}
fn probe(r: &Receiver) -> int { let cb: Narrow = first; return r.take(cb); }
`, "clean", "r.take(cb)", "take", "", 1, false, nil, nil},
		{"opaque_bodyless_effect_pending", `fn opaque(cb: Narrow) -> int;
fn probe() -> int { return opaque(first); }
`, "pending", "opaque(first)", "opaque", "", 0, false, nil, nil},
		{"opaque_indirect_effect_pending", `fn probe(consumer: fn(Narrow) -> int) -> int { return consumer(first); }
`, "pending", "consumer(first)", "consumer", "", 0, true, nil, nil},
		{"direct_incoming_alias", `fn probe(cb: Narrow, a: &string, b: &string) -> &string { return cb(a, b); }
`, "clean", "cb(a, b)", "cb", "", 0, true, nil, []uint32{1}},
		{"direct_incoming_inline", `fn probe(cb: fn(@return_source &string, &string) -> &string, a: &string, b: &string) -> &string { return cb(a, b); }
`, "clean", "cb(a, b)", "cb", "", 0, true, nil, []uint32{1}},
		{"direct_incoming_all_alias", `fn probe(cb: Wide, a: &string, b: &string) -> &string { return cb(a, b); }
`, "clean", "cb(a, b)", "cb", "", 0, true, nil, []uint32{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := prefix + tc.body
			t.Logf("RETURN_ORIGIN_ARGUMENT_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(src)), src)
			res := returnOriginTypedFixtureWithEscapeEvidence(t, src, len(tc.invoke) != 0)
			inputs, err := collectReturnOriginUnits(res)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "stage": "typed", "source": src,
				"diagnostics": res.Bag.Items(), "expr_types": res.Sema.ExprTypes, "expr_symbols": res.Symbols.ExprSymbols,
				"bindings": returnOriginValueBindings(res), "unit_count": len(inputs.units), "error": errorReturnOriginCallText(err)})
			if err != nil || len(inputs.units) != 1 {
				t.Fatalf("PRECONDITION: expected the actual single owning unit: units=%d error=%v", len(inputs.units), err)
			}
			checkReturnOriginArgumentCall(t, res, inputs.units[0], tc, src)
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "stage": "analysis", "analysis": analysis,
				"error": errorReturnOriginCallText(err), "retained_diagnostics": res.Bag.Items()})
			if err != nil || analysis == nil {
				t.Fatalf("PRECONDITION: private source traversal failed: %v", err)
			}
			for _, body := range []struct {
				name  string
				slots []uint32
			}{{"first", []uint32{0}}, {"second", []uint32{1}}, {"probe", tc.probe}} {
				summary := requireReturnOriginSummary(t, analysis, body.name)
				if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, body.slots) {
					t.Fatalf("callable argument changed actual body/result roots: %+v", summary)
				}
			}
			if len(tc.invoke) != 0 {
				summary := requireReturnOriginSummary(t, analysis, "invoke")
				if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, tc.invoke) {
					t.Fatalf("invoke must use its declared callback slots, excluding callback content slot zero: %+v", summary)
				}
			}
			if tc.want == "pending" {
				found := false
				for _, p := range analysis.Pending {
					found = found || p.Reason == "opaque call may change reference-bearing or callable contents"
				}
				if analysis.Complete() || !found || len(analysis.Diagnostics) != 0 {
					t.Fatalf("unproved opaque callable effects became safe/refused: %+v", analysis)
				}
				return
			}
			if !analysis.Complete() {
				t.Fatalf("callable argument or direct alias promise remains unfinished: %+v", analysis.Pending)
			}
			requireReturnOriginArgumentOutcome(t, res, analysis, tc, src)
		})
	}
}

func checkReturnOriginArgumentCall(t *testing.T, res *DiagnoseResult, unit sema.ReturnOriginUnit, tc returnOriginArgumentCase, src string) {
	t.Helper()
	start := strings.LastIndex(src, tc.call)
	var id ast.ExprID
	for expr := range res.Sema.ExprTypes {
		node := res.Builder.Exprs.Get(expr)
		if node != nil && node.Kind == ast.ExprCall && int(node.Span.Start) == start && int(node.Span.End) == start+len(tc.call) {
			if id.IsValid() {
				t.Fatal("PRECONDITION: ambiguous source call")
			}
			id = expr
		}
	}
	call, ok := res.Builder.Exprs.Call(id)
	if !ok || call == nil || res.Sema.ExprTypes[id] == types.NoTypeID {
		t.Fatal("PRECONDITION: exact source call has no real result type")
	}
	selected := res.Symbols.ExprSymbols[id]
	if tc.callback {
		selected = res.Symbols.ExprSymbols[call.Target]
	}
	sym := res.Symbols.Table.Symbols.Get(selected)
	logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_symbol", "call_id": id, "call": call, "selected_id": selected,
		"symbol": sym, "source_key": unit.SourceKey, "publication": unit.Publication, "candidates": res.Sema.CallableCandidates})
	if sym == nil || res.Builder.StringsInterner.MustLookup(sym.Name) != tc.callee || sym.Decl.ASTFile != res.FileID || sym.Decl.SourceFile != res.File.ID {
		t.Fatal("PRECONDITION: selected call has no original source symbol")
	}
	var info *types.FnInfo
	var syntax symbols.ReturnSourceSyntax
	if tc.callback {
		if sym.Kind != symbols.SymbolParam {
			t.Fatal("PRECONDITION: callback is not the actual incoming parameter")
		}
		scope := res.Symbols.Table.Scopes.Get(sym.Scope)
		logReturnOriginCallEvidence(t, map[string]any{"stage": "incoming_parameter_scope", "scope_id": sym.Scope, "scope": scope})
		if scope == nil || scope.Kind != symbols.ScopeFunction || scope.Owner.ASTFile != res.FileID ||
			scope.Owner.SourceFile != res.File.ID || !slices.Contains(scope.NameIndex[sym.Name], selected) {
			t.Fatal("PRECONDITION: callback has no exact owning function scope")
		}
		fn, found := res.Builder.Items.Fn(scope.Owner.Item)
		if !found || fn == nil {
			t.Fatal("PRECONDITION: callback scope has no original function")
		}
		var expr ast.TypeID
		for _, pid := range res.Builder.Items.GetFnParamIDs(fn) {
			param := res.Builder.Items.FnParam(pid)
			if param.Name == sym.Name {
				if expr.IsValid() {
					t.Fatal("PRECONDITION: callback parameter is ambiguous")
				}
				expr = param.Type
			}
		}
		info, syntax = returnOriginArgumentFnType(t, res, sym.Type, expr)
	} else {
		if sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
			t.Fatal("PRECONDITION: selected declaration is not a function")
		}
		info, ok = res.Sema.TypeInterner.FnInfo(sym.Type)
		syntax = sym.Signature.ReturnSourceSyntax
		if !ok || info == nil || sym.Signature.HasSelf != (tc.slot == 1) || syntax.Span().File != res.File.ID {
			t.Fatal("PRECONDITION: selected function lost its physical signature")
		}
		matched := 0
		for _, local := range unit.Publication.LocalCallables {
			if local.Symbol != selected {
				continue
			}
			for _, candidate := range res.Sema.CallableCandidates {
				if candidate.Symbol != selected || candidate.BodyKey != local.BodyKey || candidate.SourceKey != local.SourceKey {
					continue
				}
				logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_authority", "identity": local, "candidate": candidate})
				if !slices.Equal(candidate.ParamTypes, info.Params) || candidate.ResultType != info.Result ||
					!candidate.ReturnSources.Equal(info.ReturnSources()) || len(candidate.TemplateParams) != 0 || local.SourceKey != unit.SourceKey ||
					candidate.HasSelf != sym.Signature.HasSelf || candidate.HasBody != (tc.callee != "opaque") || candidate.Source != sym.Span ||
					candidate.Source.File != syntax.Span().File || candidate.Source.Start < syntax.Span().Start || candidate.Source.End > syntax.Span().End {
					t.Fatal("PRECONDITION: selected canonical body/declaration disagrees with its original signature")
				}
				matched++
			}
		}
		if matched != 1 {
			t.Fatalf("PRECONDITION: expected one selected publication/candidate identity, got %d", matched)
		}
	}
	if len(syntax.Params()) != len(info.Params) || !syntax.Sources().Equal(info.ReturnSources()) {
		t.Fatal("PRECONDITION: original callable syntax does not match the typed ABI")
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_call", "call_id": id, "call": call,
		"source_key": unit.SourceKey, "publication": unit.Publication,
		"selected_id": selected, "symbol": sym, "function_type": sym.Type, "function": info,
		"syntax_span": syntax.Span(), "syntax_params": syntax.Params(), "syntax_result": syntax.Result(),
		"all_inputs": info.ReturnSources().IsAllInputs(), "slots": info.ReturnSources().Slots()})
	if tc.callee == "cb" {
		wantAll := tc.name == "direct_incoming_all_alias"
		if len(call.Args) != 2 || info.ReturnSources().IsAllInputs() != wantAll ||
			(!wantAll && !slices.Equal(info.ReturnSources().Slots(), []uint32{0})) {
			t.Fatal("PRECONDITION: direct incoming callback lost its frozen original promise")
		}
		return
	}
	slot, arg := tc.slot, 0
	if tc.callee == "ordered" {
		sig := sym.Signature
		if len(sig.ParamNames) != 3 || len(call.Args) != 2 || !slices.Equal(sig.Defaults, []bool{false, false, true}) ||
			res.Builder.StringsInterner.MustLookup(sig.ParamNames[0]) != "a" || res.Builder.StringsInterner.MustLookup(sig.ParamNames[1]) != "b" ||
			call.Args[0].Name != sig.ParamNames[1] || call.Args[1].Name != sig.ParamNames[0] {
			t.Fatal("PRECONDITION: reversed named arguments/trailing default lost actual physical slot metadata")
		}
		wide, _ := returnOriginArgumentFnType(t, res, info.Params[1], syntax.Params()[1])
		if !wide.ReturnSources().IsAllInputs() {
			t.Fatal("PRECONDITION: physical slot one is not Wide")
		}
		arg = 1
	}
	if slot >= len(info.Params) || arg >= len(call.Args) {
		t.Fatal("PRECONDITION: missing actual/formal argument")
	}
	formal, original := returnOriginArgumentFnType(t, res, info.Params[slot], syntax.Params()[slot])
	wantAll := tc.name == "invoked_wide_result"
	if formal.ReturnSources().IsAllInputs() != wantAll || (!wantAll && !slices.Equal(formal.ReturnSources().Slots(), []uint32{0})) {
		t.Fatal("PRECONDITION: selected physical argument destination lost its declared promise")
	}
	actualID := call.Args[arg].Value
	actualType, present := res.Sema.ExprTypes[actualID]
	actual := returnOriginArgumentFnInfo(res.Sema.TypeInterner, actualType)
	if !present || actual == nil {
		t.Fatal("PRECONDITION: actual callable argument is not typed")
	}
	if tc.name == "wide_copy_reassignment_argument" && !actual.ReturnSources().IsAllInputs() {
		t.Fatal("PRECONDITION: copied wide binding was narrowed before origin analysis")
	}
	if tc.slot == 1 {
		member, found := res.Builder.Exprs.Member(call.Target)
		if !found || member == nil || res.Sema.ExprTypes[member.Target] == types.NoTypeID || len(info.Params) != 2 ||
			actualType != info.Params[1] || actual.ReturnSources().IsAllInputs() || !slices.Equal(actual.ReturnSources().Slots(), []uint32{0}) {
			t.Fatal("PRECONDITION: selected method lacks typed receiver/self slot zero or exact Narrow argument in physical slot one")
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "argument_destination", "physical_slot": slot, "actual_id": actualID,
		"actual_type": actualType, "actual_function": actual, "actual_all_inputs": actual.ReturnSources().IsAllInputs(), "actual_slots": actual.ReturnSources().Slots(),
		"formal_type": info.Params[slot], "formal_function": formal, "formal_all_inputs": formal.ReturnSources().IsAllInputs(), "formal_slots": formal.ReturnSources().Slots(),
		"original_type_span": original.Span(), "original_params": original.Params(), "original_result": original.Result()})
}

func returnOriginArgumentFnInfo(in *types.Interner, typ types.TypeID) *types.FnInfo {
	seen := make(map[types.TypeID]bool)
	for !seen[typ] {
		seen[typ] = true
		if alias, ok := in.AliasInfo(typ); ok {
			typ = alias.Target
			continue
		}
		info, _ := in.FnInfo(typ)
		return info
	}
	return nil
}

func returnOriginArgumentFnType(t *testing.T, res *DiagnoseResult, typ types.TypeID, expr ast.TypeID) (*types.FnInfo, symbols.ReturnSourceSyntax) {
	t.Helper()
	if alias, ok := res.Sema.TypeInterner.AliasInfo(typ); ok {
		var target ast.TypeID
		for _, itemID := range res.Builder.Files.Get(res.FileID).Items {
			item, found := res.Builder.Items.Type(itemID)
			if !found || item.Span != alias.Decl {
				continue
			}
			ids := res.Symbols.ItemSymbols[itemID]
			decl := res.Builder.Items.TypeAlias(item)
			if len(ids) != 1 || decl == nil || target.IsValid() || len(alias.TypeArgs) != 0 || alias.Target == typ {
				t.Fatal("PRECONDITION: alias has no unique original nongeneric declaration")
			}
			owner := res.Symbols.Table.Symbols.Get(ids[0])
			if owner == nil || owner.Type != typ || owner.Decl.ASTFile != res.FileID || owner.Decl.Item != itemID || owner.Decl.SourceFile != alias.Decl.File {
				t.Fatal("PRECONDITION: alias original AST and symbol disagree")
			}
			logReturnOriginCallEvidence(t, map[string]any{"stage": "original_alias", "type": typ, "alias": alias, "owner_id": ids[0], "owner": owner, "target_expr": decl.Target})
			target = decl.Target
		}
		if !target.IsValid() || alias.Decl.File != res.File.ID {
			t.Fatal("PRECONDITION: alias lacks its actual retained source declaration")
		}
		return returnOriginArgumentFnType(t, res, alias.Target, target)
	}
	info, ok := res.Sema.TypeInterner.FnInfo(typ)
	node := res.Builder.Types.Get(expr)
	if !ok || info == nil || node == nil || node.Kind != ast.TypeExprFn || node.Span.File != res.File.ID {
		t.Fatal("PRECONDITION: original function-type syntax/type is absent")
	}
	syntax := symbols.FunctionTypeReturnSourceSyntax(res.Builder, expr)
	if len(syntax.Params()) != len(info.Params) || !syntax.Sources().Equal(info.ReturnSources()) {
		t.Fatal("PRECONDITION: original function-type syntax and typed promise disagree")
	}
	matches := 0
	for _, request := range res.Sema.ReturnSourceDeclarations {
		logReturnOriginCallEvidence(t, map[string]any{"stage": "original_request", "request": request,
			"params": request.Params(), "result": request.Result(), "syntax_params": request.Syntax.Params(),
			"syntax_result": request.Syntax.Result(), "syntax_span": request.Syntax.Span(), "markers": request.Syntax.Markers()})
		if request.Owner == symbols.NoSymbolID && request.TypeExpr == expr && request.Syntax.Span() == syntax.Span() {
			if !slices.Equal(request.Params(), info.Params) || request.Result() != info.Result ||
				!request.Syntax.Sources().Equal(info.ReturnSources()) || res.Symbols.Table.Scopes.Get(request.Scope) == nil {
				t.Fatal("PRECONDITION: original typed source request differs from the selected promise")
			}
			matches++
		}
	}
	if !syntax.Sources().IsAllInputs() && matches != 1 {
		t.Fatalf("PRECONDITION: explicit original promise needs one typed request, got %d", matches)
	}
	return info, syntax
}

func requireReturnOriginArgumentOutcome(t *testing.T, res *DiagnoseResult, analysis *sema.ReturnOriginAnalysis, tc returnOriginArgumentCase, src string) {
	t.Helper()
	if tc.want == "clean" {
		if len(analysis.Diagnostics) != 0 {
			t.Fatalf("valid callable argument refused: %+v", analysis.Diagnostics)
		}
		return
	}
	if len(analysis.Diagnostics) == 0 || (tc.want == "mismatch" && len(analysis.Diagnostics) != 1) {
		t.Fatalf("missing/extraneous frozen source diagnostic: %+v", analysis.Diagnostics)
	}
	for _, d := range analysis.Diagnostics {
		code, message := diag.SemaError, returnOriginCallableMismatch
		start := strings.LastIndex(src, tc.call) + strings.LastIndex(tc.call, tc.rhs)
		end := start + len(tc.rhs)
		noteText := "fn(@return_source &string, &string) -> &string"
		if tc.want == "escape" {
			code, message = diag.SemaBorrowEscapesReturn, "borrow of 'owned' outlives its owner when this scope exits"
			statement := "ret " + tc.call + ";"
			start, end = strings.Index(src, statement), strings.Index(src, statement)+len(statement)
			noteText = `let owned: string = "local";`
		}
		if d.Code != code || d.Severity != diag.SevError || d.Message != message || len(d.Help) == 0 ||
			d.Primary != (source.Span{File: res.File.ID, Start: uint32(start), End: uint32(end)}) {
			t.Fatalf("wrong callable argument/normal-exit diagnostic: %+v", d)
		}
		noteStart, found := strings.Index(src, noteText), false
		for _, note := range d.Notes {
			found = found || (note.Span.File == res.File.ID && int(note.Span.Start) == noteStart && int(note.Span.End) == noteStart+len(noteText))
		}
		if !found {
			t.Fatal("diagnostic lost the exact destination promise or local owner note")
		}
	}
}
