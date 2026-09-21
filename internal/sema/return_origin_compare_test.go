package sema

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestReturnOriginCompareFlow(t *testing.T) {
	for _, fixture := range returnOriginCompareFixtures()[:10] {
		t.Run(fixture.name, func(t *testing.T) { checkReturnOriginCompare(t, fixture) })
	}
}

func TestReturnOriginCompareBoundaries(t *testing.T) {
	for _, fixture := range returnOriginCompareFixtures()[10:] {
		t.Run(fixture.name, func(t *testing.T) { checkReturnOriginCompare(t, fixture) })
	}
}

func checkReturnOriginCompare(t *testing.T, fixture returnOriginCompareFixture) {
	t.Helper()
	record := map[string]any{"fixture": fixture.name, "source": fixture.text, "sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(fixture.text)))}
	defer func() {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("COMPARE_ORIGIN_CAPTURE %s", data)
	}()
	if record["sha256"] != fixture.digest || len(returnOriginCompareFixtures()) != 14 {
		t.Fatal("PRECONDITION: frozen source inventory or roster changed")
	}
	b, file, parseBag := parseSnippet(t, fixture.text)
	record["parse_diagnostics"] = parseBag.Items()
	if parseBag.HasErrors() {
		t.Fatalf("PRECONDITION: source parse failed: %s", diagnosticsSummary(parseBag))
	}
	resolveBag := diag.NewBag(64)
	resolved := symbols.ResolveFile(b, file, &symbols.ResolveOptions{Reporter: &diag.BagReporter{Bag: resolveBag}})
	record["resolve_diagnostics"] = resolveBag.Items()
	if resolveBag.HasErrors() {
		t.Fatalf("PRECONDITION: symbol resolution failed: %s", diagnosticsSummary(resolveBag))
	}
	bag := diag.NewBag(64)
	checked := Check(t.Context(), b, file, Options{Symbols: &resolved, Reporter: &diag.BagReporter{Bag: bag}})
	record["sema_diagnostics"] = bag.Items()
	checkReturnOriginCompareOriginalDiagnostics(t, fixture, b.Files.Get(file).Span.File, bag.Items())
	const key = "compare-origin.sg"
	if err := CanonicalizeInstantiationGraphSources(&checked, func(source.FileID) (string, error) { return key, nil }); err != nil {
		t.Fatal(err)
	}
	publication := FinalizationPublication{SourceKey: key}
	for _, candidate := range checked.CallableCandidates {
		publication.LocalCallables = append(publication.LocalCallables, FinalizationCallableIdentity{Symbol: candidate.Symbol, BodyKey: candidate.BodyKey, SourceKey: candidate.SourceKey})
	}
	unit := ReturnOriginUnit{Builder: b, FileID: file, Sema: &checked, Symbols: &resolved, SourceKey: key, Publication: publication}
	index, err := indexReturnOriginUnit(unit, &checked)
	if err != nil {
		t.Fatalf("PRECONDITION: original unit index: %v", err)
	}
	probe := lookupSymbolByName(&resolved, b.StringsInterner.Intern("probe"))
	fn := index.functions[probe]
	if fn == nil || !fn.item.Body.IsValid() || fn.info == nil {
		t.Fatal("PRECONDITION: original probe has no typed body")
	}
	for _, slot := range fixture.slots {
		if int(slot) >= len(fn.params) || !fn.params[slot].IsValid() || returnOriginTypeShape(checked.TypeInterner, fn.info.Params[slot], nil) != returnOriginCarriesRef {
			t.Fatal("PRECONDITION: expected source slot is not an original reference-bearing parameter")
		}
	}
	record["probe"] = map[string]any{"symbol": probe, "scope": fn.scope, "params": fn.params, "fn_info": fn.info, "source": fn.item.Span, "body_key": fn.key}
	record["publication"], record["candidates"] = publication, checked.CallableCandidates
	record["symbols"], record["scopes"] = resolved.Table.Symbols.Data(), resolved.Table.Scopes.Data()
	record["expr_types"], record["binding_types"], record["is_operands"] = checked.ExprTypes, checked.BindingTypes, checked.IsOperands
	record["function_effects"], record["implicit_conversions"], record["borrows"] = checked.FunctionEffects, checked.ImplicitConversions, checked.Borrows
	nodes, descriptors := []map[string]any{}, map[types.TypeID]any{}
	for raw := uint32(1); raw <= b.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		typ, typed := checked.ExprTypes[id]
		n := map[string]any{"id": id, "node": b.Exprs.Get(id), "type": typ, "type_present": typed, "symbol": resolved.ExprSymbols[id]}
		switch b.Exprs.Get(id).Kind {
		case ast.ExprBinary:
			n["payload"], _ = b.Exprs.Binary(id)
		case ast.ExprUnary:
			n["payload"], _ = b.Exprs.Unary(id)
		case ast.ExprBlock:
			n["payload"], _ = b.Exprs.Block(id)
		}
		nodes = append(nodes, n)
		if typ != types.NoTypeID {
			desc, _ := checked.TypeInterner.Lookup(typ)
			union, _ := checked.TypeInterner.UnionInfo(typ)
			descriptors[typ] = map[string]any{"type": desc, "union": union}
		}
	}
	record["nodes"], record["type_descriptors"], record["statements"] = nodes, descriptors, b.Stmts.Arena.Slice()
	compares, calls, _ := captureReturnOriginCompareMetadata(t, unit, fixture.name)
	record["compares"], record["calls"] = compares, calls
	if t.Failed() {
		t.Fatal("PRECONDITION: actual typed compare metadata is incomplete")
	}
	analysis, analysisErr := AnalyzeReturnOrigins(t.Context(), &checked, []ReturnOriginUnit{unit})
	record["analysis"], record["analysis_error"] = analysis, fmt.Sprint(analysisErr)
	if analysisErr != nil || analysis == nil {
		t.Fatalf("origin analysis unexpectedly aborted: %v", analysisErr)
	}
	var summary *ReturnOriginSummary
	for i := range analysis.Summaries {
		if analysis.Summaries[i].BodyKey == fn.key {
			if summary != nil {
				t.Fatal("duplicate original probe summary")
			}
			summary = &analysis.Summaries[i]
		}
	}
	if summary == nil {
		t.Fatal("original probe summary is absent")
	}
	if analysis.Complete() != fixture.complete || summary.Unknown != fixture.unknown || summary.NoNormalReturn {
		t.Errorf("completion/result state changed: complete=%t summary=%+v", analysis.Complete(), *summary)
	}
	if fixture.checkSlots && !slices.Equal(summary.ParamSlots, fixture.slots) {
		t.Errorf("actual parameter sources=%v, want exact %v", summary.ParamSlots, fixture.slots)
	}
	for _, pending := range analysis.Pending {
		if pending.Reason == fmt.Sprintf("expression kind %d needs an origin transfer", ast.ExprCompare) {
			t.Errorf("compare still lacks its own transfer: %+v", pending)
		}
	}
	if strings.HasSuffix(fixture.name, "panic_fallthrough") {
		found := false
		for _, pending := range analysis.Pending {
			found = found || pending.SourceKey == key && pending.Span == fn.item.ReturnSpan && pending.Reason == "reachable function fallthrough has no proven value for its declared result"
		}
		if !found {
			t.Error("missing actual-value fallthrough refusal at the original result span")
		}
	}
	checkReturnOriginCompareEscape(t, fixture, unit, fn, analysis)
}

func captureReturnOriginCompareMetadata(t *testing.T, unit ReturnOriginUnit, name string) ([]map[string]any, []map[string]any, ast.ExprID) {
	t.Helper()
	b, res := unit.Builder, unit.Sema
	compares, calls := []map[string]any{}, []map[string]any{}
	var isRHS ast.ExprID
	for raw := uint32(1); raw <= b.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		node := b.Exprs.Get(id)
		if call, ok := b.Exprs.Call(id); ok && call != nil {
			selected := unit.Symbols.ExprSymbols[id]
			var info *types.FnInfo
			if sym := unit.Symbols.Table.Symbols.Get(selected); sym != nil {
				info, _ = res.TypeInterner.FnInfo(sym.Type)
			}
			calls = append(calls, map[string]any{"id": id, "span": node.Span, "call": call, "selected": selected, "target_symbol": unit.Symbols.ExprSymbols[call.Target], "fn_info": info})
			if strings.Contains(name, "panic") {
				panicID := lookupSymbolByName(unit.Symbols, b.StringsInterner.Intern("panic"))
				sym := unit.Symbols.Table.Symbols.Get(selected)
				if sym == nil || info == nil || sym.Signature == nil || selected != panicID || sym.Flags&symbols.SymbolFlagBuiltin != 0 || !sym.Signature.HasBody || info.Result != res.TypeInterner.Builtins().Nothing {
					t.Error("PRECONDITION: selected panic is not the original normal Nothing body")
				}
			}
		}
		cmp, ok := b.Exprs.Compare(id)
		if !ok || cmp == nil {
			continue
		}
		arms := []map[string]any{}
		for _, arm := range cmp.Arms {
			var scopeID symbols.ScopeID
			for i, scope := range unit.Symbols.Table.Scopes.Data() {
				if scope.Owner.ASTFile == unit.FileID && scope.Owner.SourceFile == node.Span.File && scope.Owner.Kind == symbols.ScopeOwnerExpr && scope.Owner.Expr == id && scope.Span == arm.PatternSpan {
					if scopeID.IsValid() {
						t.Error("PRECONDITION: original arm scope is ambiguous")
					}
					scopeID = symbols.ScopeID(i + 1)
				}
			}
			if !scopeID.IsValid() {
				t.Error("PRECONDITION: original arm scope is absent")
			}
			bindings := []map[string]any{}
			for raw := uint32(1); raw <= b.Exprs.Arena.Len(); raw++ {
				expr := ast.ExprID(raw)
				symID := unit.Symbols.ExprSymbols[expr]
				sym, pattern := unit.Symbols.Table.Symbols.Get(symID), b.Exprs.Get(expr)
				if sym == nil || pattern == nil || sym.Kind != symbols.SymbolLet || sym.Scope != scopeID || pattern.Span.File != arm.PatternSpan.File || pattern.Span.Start < arm.PatternSpan.Start || pattern.Span.End > arm.PatternSpan.End {
					continue
				}
				typ, present := res.BindingTypes[symID]
				if !present || typ == types.NoTypeID || sym.Type != typ {
					t.Errorf("PRECONDITION: pattern binding %d has no exact original type", symID)
				}
				bindings = append(bindings, map[string]any{"expr": expr, "symbol": symID, "symbol_record": sym, "type": typ, "type_present": present, "span": pattern.Span})
			}
			for _, runtimeID := range []ast.ExprID{id, cmp.Value, arm.Guard, arm.Result} {
				if runtimeID.IsValid() && res.ExprTypes[runtimeID] == types.NoTypeID {
					t.Errorf("PRECONDITION: runtime node %d is untyped", runtimeID)
				}
			}
			arms = append(arms, map[string]any{"arm": arm, "scope": scopeID, "bindings": bindings, "result_type": res.ExprTypes[arm.Result], "guard_type": res.ExprTypes[arm.Guard]})
			if name == "guard_is_type_operand" && arm.Guard.IsValid() {
				binary, present := b.Exprs.Binary(arm.Guard)
				if !present || binary == nil || binary.Op != ast.ExprBinaryIs {
					t.Fatal("PRECONDITION: guard lost its actual binary-is shape")
				}
				isRHS = binary.Right
				operand, hasOperand := res.IsOperands[arm.Guard]
				_, typed := res.ExprTypes[isRHS]
				rhsSymbol := unit.Symbols.Table.Symbols.Get(unit.Symbols.ExprSymbols[isRHS])
				arms[len(arms)-1]["is"] = map[string]any{"binary": binary, "operand": operand, "operand_present": hasOperand, "lhs_type": res.ExprTypes[binary.Left], "rhs": isRHS, "rhs_span": b.Exprs.Get(isRHS).Span, "rhs_type_present": typed, "rhs_symbol": rhsSymbol}
				// The original builtin SymbolType has Type0; this fixture preserves that reader fact.
				if !hasOperand || operand != (IsOperand{Kind: IsOperandType}) || typed || res.ExprTypes[binary.Left] != res.TypeInterner.Builtins().Int || rhsSymbol == nil || rhsSymbol.Kind != symbols.SymbolType || rhsSymbol.Flags&symbols.SymbolFlagBuiltin == 0 || rhsSymbol.Type != types.NoTypeID {
					t.Error("PRECONDITION: is selector/runtime operand distinction changed")
				}
			}
		}
		compares = append(compares, map[string]any{"id": id, "span": node.Span, "type": res.ExprTypes[id], "subject": cmp.Value, "subject_type": res.ExprTypes[cmp.Value], "arms": arms})
	}
	want := 1
	if name == "direct_panic_fallthrough" {
		want = 0
	}
	if len(compares) != want || name == "guard_is_type_operand" && !isRHS.IsValid() {
		t.Error("PRECONDITION: frozen compare/guard census changed")
	}
	return compares, calls, isRHS
}

func checkReturnOriginCompareOriginalDiagnostics(t *testing.T, fixture returnOriginCompareFixture, file source.FileID, items []*diag.Diagnostic) {
	t.Helper()
	if fixture.name != "binding_storage_escape" {
		for _, d := range items {
			if d.Severity >= diag.SevError && (fixture.owner == "" || d.Code != diag.SemaBorrowEscapesReturn) {
				t.Fatalf("PRECONDITION: unrelated original SEMA refusal: %+v", *d)
			}
		}
		return
	}
	start := strings.Index(fixture.text, "&value")
	span := source.Span{File: file, Start: uint32(start), End: uint32(start + len("&value"))}
	wantNotes := []diag.Note{
		{Span: span, Msg: "the compare owns the value it matched, so `value` is this arm's to release - the reference would point at freed storage before it is ever read"},
		{Span: span, Msg: "a reference into a payload stays legal when the compare only BORROWS its subject, because then the owner keeps `value` alive"},
		{Span: span, Msg: "hint: answer with the value instead of a reference to it, or match on a borrow of the subject so `value` outlives the compare"},
	}
	if len(items) != 1 {
		t.Fatalf("PRECONDITION: expected one original SEM3200, got %+v", items)
	}
	d := items[0]
	if start != 88 || d.Code != diag.SemaArmReferenceIntoFreedPayload || d.Severity != diag.SevError || d.Primary != span || d.Message != "cannot answer with a reference into `value`: this arm frees it when it ends" || !slices.Equal(d.Notes, wantNotes) || len(d.Help) != 0 || len(d.Fixes) != 0 {
		t.Fatalf("PRECONDITION: original exact SEM3200 changed: %+v", *d)
	}
}

func checkReturnOriginCompareEscape(t *testing.T, fixture returnOriginCompareFixture, unit ReturnOriginUnit, fn *returnOriginFunction, analysis *ReturnOriginAnalysis) {
	t.Helper()
	if fixture.owner == "" {
		if len(analysis.Diagnostics) != 0 {
			t.Errorf("unexpected origin diagnostics: %+v", analysis.Diagnostics)
		}
		return
	}
	var owner *symbols.Symbol
	for i := range unit.Symbols.Table.Symbols.Data() {
		sym := unit.Symbols.Table.Symbols.Get(symbols.SymbolID(i + 1))
		name, _ := unit.Builder.StringsInterner.Lookup(sym.Name)
		if sym.Kind == symbols.SymbolLet && name == fixture.owner {
			if owner != nil {
				t.Fatal("PRECONDITION: escape owner is ambiguous")
			}
			owner = sym
		}
	}
	if owner == nil || !(&returnOriginBody{function: fn}).within(owner.Scope, fn.scope) {
		t.Fatal("PRECONDITION: escape owner lacks its original function scope")
	}
	needle := "ret &owned;"
	if fixture.name == "binding_storage_escape" {
		needle = "&value"
	} else if fixture.name == "guard_local_escape" {
		needle = "ret flag;"
	}
	start := strings.Index(fixture.text, needle)
	span := source.Span{File: fn.item.Span.File, Start: uint32(start), End: uint32(start + len(needle))}
	found := false
	for _, d := range analysis.Diagnostics {
		if d.Code != diag.SemaBorrowEscapesReturn || d.Message != fmt.Sprintf("borrow of '%s' outlives its owner when this scope exits", fixture.owner) {
			t.Errorf("unrelated origin diagnostic: %+v", d)
		}
		for _, note := range d.Notes {
			found = found || d.Code == diag.SemaBorrowEscapesReturn && d.Primary == span && note.Span == owner.Span && note.Msg == fmt.Sprintf("'%s' owns storage that ends in this scope", fixture.owner)
		}
	}
	if !found {
		t.Errorf("exact owner escape diagnostic at %v with owner note %v is absent", span, owner.Span)
	}
}
