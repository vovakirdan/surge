package sema

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestReturnOriginConditionMaskLattice(t *testing.T) {
	a := returnOriginRequirements{atoms: []returnOriginAtom{{kind: returnOriginNoBorrowedState, slot: 0}}}
	b := returnOriginRequirements{atoms: []returnOriginAtom{{kind: returnOriginDefaultable, slot: 0}, {kind: returnOriginNoBorrowedState, slot: 1}}}
	c := returnOriginRequirements{refuted: true, unsupported: true}
	joined := a.join(b).join(c)
	if !joined.equal(c.join(b.join(a))) || !joined.equal(joined.join(joined)) || len(joined.atoms) != 3 ||
		!joined.refuted || !joined.unsupported || !a.join(returnOriginRequirements{}).equal(a) {
		t.Fatalf("requirements lost finite union/flags/bottom laws: %+v", joined)
	}
	for range 64 {
		joined = joined.join(a).join(b).join(c)
	}
	if len(joined.atoms) != 3 {
		t.Fatal("recursive joins grew the original formal alphabet")
	}
	joined.atoms[0].slot = 7
	if a.atoms[0].slot != 0 || b.atoms[0].slot != 0 {
		t.Fatal("mask join aliases an input vector")
	}
}

func returnOriginConditionFixture(t *testing.T, text string) *returnOriginAnalyzer {
	t.Helper()
	a := returnOriginConditionAnalyzer(t, text)
	if len(a.report.Pending) != 0 || len(a.report.Diagnostics) != 0 {
		t.Fatalf("real typed condition fixture is incomplete: pending=%+v diagnostics=%+v", a.report.Pending, a.report.Diagnostics)
	}
	return a
}

func returnOriginConditionAnalyzer(t *testing.T, text string, prepare ...func(*Result, ReturnOriginUnit)) *returnOriginAnalyzer {
	t.Helper()
	t.Logf("RETURN_ORIGIN_CONDITION_SOURCE sha256=%x source=%q", sha256.Sum256([]byte(text)), text)
	result, unit := returnOriginPublicationFixture(t, text, false)
	finalizeReturnOriginConditionFixture(t, result, unit)
	for _, mutate := range prepare {
		mutate(result, unit)
	}
	index, err := indexReturnOriginUnit(unit, result)
	if err != nil {
		t.Fatal(err)
	}
	a := &returnOriginAnalyzer{ctx: t.Context(), units: []*returnOriginUnitIndex{index},
		bodies: make(map[string]*returnOriginFunction), declarations: make(map[string]*returnOriginFunction),
		summaries: make(map[string]returnOriginSummaryFact), report: &ReturnOriginAnalysis{Pending: slices.Clone(index.pending)}}
	for _, fn := range index.functions {
		if fn.item.Body.IsValid() {
			a.functions = append(a.functions, fn)
			a.bodies[fn.key] = fn
		} else {
			a.declarations[fn.key] = fn
		}
	}
	slices.SortFunc(a.functions, func(a, b *returnOriginFunction) int {
		if a.key < b.key {
			return -1
		}
		if a.key > b.key {
			return 1
		}
		return 0
	})
	if err := a.solveBodies(); err != nil {
		t.Fatal(err)
	}
	return a
}

func finalizeReturnOriginConditionFixture(t *testing.T, result *Result, unit ReturnOriginUnit) {
	t.Helper()
	file := unit.Builder.Files.Get(unit.FileID)
	if file == nil || unit.SourceKey == "" || unit.Sema != result {
		t.Fatal("PRECONDITION: missing original single-unit source authority")
	}
	var seeds []symbols.SymbolID
	for _, itemID := range file.Items {
		fn, ok := unit.Builder.Items.Fn(itemID)
		if !ok || fn == nil || !fn.Body.IsValid() {
			continue
		}
		for _, id := range unit.Symbols.ItemSymbols[itemID] {
			sym := unit.Symbols.Table.Symbols.Get(id)
			if sym != nil && sym.Kind == symbols.SymbolFunction && len(sym.TypeParams) == 0 {
				seeds = append(seeds, id)
			}
		}
	}
	slices.Sort(seeds)
	seeds = slices.Compact(seeds)
	if len(seeds) == 0 {
		t.Fatal("PRECONDITION: source fixture has no ordinary root body")
	}
	AddInstantiationCallableSeeds(result, seeds)
	identity, err := NewInstantiationKeyContext(result.TypeInterner, unit.Symbols, func(id source.FileID) (string, error) {
		if id != file.Span.File {
			return "", fmt.Errorf("unknown condition fixture source file %d", id)
		}
		return unit.SourceKey, nil
	})
	if err != nil {
		t.Fatalf("PRECONDITION: source instantiation identity: %v", err)
	}
	result.InstantiationIdentity = &identity
	for _, stage := range []struct {
		name string
		run  func() error
	}{
		{"entrypoint callables", result.FinalizeEntrypointCallables},
		{"direct clone bindings", result.FinalizeDirectCloneBindings},
		{"clone obligations", result.FinalizeCloneObligations},
		{"instantiation closure", func() error { return result.FinalizeInstantiationClosure(identity, 64) }},
	} {
		if err := stage.run(); err != nil {
			t.Fatalf("PRECONDITION: %s: %v", stage.name, err)
		}
	}
	if result.InstantiationIdentity == nil || result.InstantiationClosure == nil {
		t.Fatal("PRECONDITION: finalization did not publish actual identity and closure")
	}
	t.Logf("CONDITION_FINALIZATION source=%s file=%d seeds=%v roots=%d edges=%d instances=%d uses=%d",
		unit.SourceKey, file.Span.File, seeds, len(result.InstantiationGraph.Roots()), len(result.InstantiationGraph.Edges()),
		len(result.InstantiationClosure.Instances), len(result.InstantiationClosure.UseSites))
}

func TestReturnOriginConditionsSurviveExecutedOperations(t *testing.T) {
	for _, tc := range []struct {
		name, source, producer, wrapper string
		conditions                      int
	}{
		{"discarded", `fn use<T>(g: fn() -> T, flag: bool) -> nothing {
    if flag { let _ = g(); }
    return nothing;
}
fn probe(g: fn() -> string, flag: bool) -> nothing { use::<string>(g, flag); }
`, "use", "", 1},
		{"recursive", `fn run<T>(g: fn() -> T) -> T { return g(); }
fn relay<U>(g: fn() -> U, again: bool) -> U {
    if again { return relay::<U>(g, false); }
    return run::<U>(g);
}
fn make() -> string { return "x"; }
fn probe() -> string { return relay::<string>(make, false); }
`, "run", "relay", 1},
		{"copy_only", `fn copy_only<T>(g: fn() -> T) -> nothing {
    let copied = g;
    return nothing;
}
fn probe(g: fn() -> &string) -> nothing { copy_only::<&string>(g); }
`, "copy_only", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := returnOriginConditionFixture(t, tc.source)
			found := 0
			for _, fn := range a.functions {
				fact := a.summaries[fn.key]
				t.Logf("CONDITION_FACT body=%s name=%s normal=%v required=%+v locals=%d", fn.key, fn.name, fact.value.normal, fact.required, len(fact.conditions))
				if fn.name == tc.producer {
					found++
					if len(fact.conditions) != tc.conditions || len(fact.required.atoms) != tc.conditions || fact.required.failed() {
						t.Fatalf("executed/discarded condition differs from pure copy: %+v", fact)
					}
					for _, condition := range fact.conditions {
						if condition.body != fn.key || condition.kind != returnOriginNoBorrowedState || condition.subject != fn.candidate.TemplateParams[0] ||
							condition.site.File != fn.item.Span.File || condition.site.Start < fn.item.Span.Start || condition.site.End > fn.item.Span.End {
							t.Fatalf("condition lost its exact original operation: %+v", condition)
						}
					}
				}
				if fn.name == tc.wrapper {
					found++
					if len(fact.conditions) != 0 || len(fact.required.atoms) != 1 || fact.required.atoms[0] != (returnOriginAtom{kind: returnOriginNoBorrowedState, slot: 0}) {
						t.Fatalf("recursive edge did not retain only its finite derived mask: %+v", fact)
					}
				}
				if fn.name == "probe" && (fact.required.failed() || len(fact.required.atoms) != 0) {
					t.Fatal("concrete caller retained an undischarged mask")
				}
			}
			want := 1
			if tc.wrapper != "" {
				want++
			}
			if found != want {
				t.Fatal("source bodies were omitted")
			}
		})
	}
}

func TestReturnOriginCallableViewsRemainDistinct(t *testing.T) {
	a := returnOriginConditionFixture(t, `fn keep_callback<T>(g: fn() -> T) -> nothing { let copied = g; }
fn text() -> string { return "x"; }
fn number() -> int64 { return 1; }
fn probe() -> nothing { keep_callback::<string>(text); keep_callback::<int64>(number); }
`)
	var callee, caller *returnOriginFunction
	for _, fn := range a.functions {
		if fn.name == "keep_callback" {
			callee = fn
		}
		if fn.name == "probe" {
			caller = fn
		}
	}
	if callee == nil || caller == nil || len(callee.candidate.TemplateParams) != 1 {
		t.Fatal("PRECONDITION: real generic source missing")
	}
	var alternatives []returnOriginCallable
	for id := range caller.unit.Sema.ExprTypes {
		node := caller.unit.Builder.Exprs.Get(id)
		if node == nil || node.Kind != ast.ExprCall || node.Span.Start < caller.item.Span.Start || node.Span.End > caller.item.Span.End {
			continue
		}
		args, reason := caller.originalInstantiation(id, InstantiationFunction, callee.candidate.Symbol)
		if reason != "" {
			t.Fatal(reason)
		}
		if _, reason = callee.originalSignature(caller, id, args); reason != "" {
			t.Fatal(reason)
		}
		alternatives = append(alternatives, returnOriginCallable{typ: callee.info.Params[0], promise: callee.item.ParamsSpan,
			view: returnOriginBoundView(callee, caller, args)})
	}
	if len(alternatives) != 2 || slices.Equal(alternatives[0].view.args, alternatives[1].view.args) {
		t.Fatal("PRECONDITION: two distinct admitted concrete bindings are missing")
	}
	left, right := returnOriginValueOf(), returnOriginValueOf()
	left.callables, right.callables = alternatives[:1], alternatives[1:]
	joined := left.join(right)
	if len(joined.callables) != 2 || !joined.equal(right.join(left)) || !joined.equal(joined.join(joined)) {
		t.Fatal("binding views collapsed at callable join")
	}
	copy := joined.clone()
	copy.callables[0].view.args[0] = types.NoTypeID
	copy.callables[0].view.params[0] = types.NoTypeID
	if joined.equal(copy) || joined.callables[0].view.args[0] == types.NoTypeID || joined.callables[0].view.params[0] == types.NoTypeID {
		t.Fatal("callable view identity/clone shares or ignores its immutable vectors")
	}
}

func TestReturnOriginIndirectFailedConditionPreservesNoNormalReturn(t *testing.T) {
	const src = `fn stop(g: fn() -> &string) -> nothing {
    let _ = g();
    while true {} return nothing;
}
fn probe(g: fn() -> &string) -> nothing {
    let selected = stop;
    selected(g);
    return nothing;
}
`
	a := returnOriginConditionAnalyzer(t, src)
	var stopped, caller *returnOriginFunction
	for _, fn := range a.functions {
		if fn.name == "stop" {
			stopped = fn
		}
		if fn.name == "probe" {
			caller = fn
		}
	}
	if stopped == nil || caller == nil || len(a.units) != 1 || len(a.report.Diagnostics) != 0 {
		t.Fatalf("PRECONDITION: missing admitted bodies or unexpected diagnostics: %+v", a.report)
	}
	u := caller.unit
	calls := make(map[string]ast.ExprID)
	for id, typ := range u.Sema.ExprTypes {
		node := u.Builder.Exprs.Get(id)
		if node == nil || node.Kind != ast.ExprCall || typ == types.NoTypeID || node.Span.End > uint32(len(src)) {
			continue
		}
		calls[src[node.Span.Start:node.Span.End]] = id
	}
	opaque, indirect := calls["g()"], calls["selected(g)"]
	if !opaque.IsValid() || !indirect.IsValid() || len(calls) != 2 {
		t.Fatal("PRECONDITION: exact original and indirect typed calls are missing")
	}
	call, ok := u.Builder.Exprs.Call(indirect)
	if !ok || call == nil || len(call.Args) != 1 {
		t.Fatal("PRECONDITION: indirect call lost its physical argument")
	}
	target := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[call.Target])
	selected := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[indirect])
	if target == nil || target.Kind == symbols.SymbolFunction || !target.Decl.Stmt.IsValid() ||
		target.Decl.ASTFile != u.FileID || selected != nil && selected.Kind == symbols.SymbolFunction {
		t.Fatal("PRECONDITION: expected inferred local callback route, not a direct selected function")
	}
	decl := u.Builder.Stmts.Let(target.Decl.Stmt)
	if decl == nil || decl.Type.IsValid() || u.Symbols.ExprSymbols[decl.Value] != stopped.symbol ||
		u.Sema.BindingTypes[u.Symbols.ExprSymbols[call.Target]] != u.Symbols.Table.Symbols.Get(stopped.symbol).Type {
		t.Fatal("PRECONDITION: inferred callable local lost its actual original function")
	}
	stopFact, callerFact := a.summaries[stopped.key], a.summaries[caller.key]
	t.Logf("INDIRECT_CONDITION stop=%+v probe=%+v pending=%+v", stopFact, callerFact, a.report.Pending)
	if stopFact.value.normal || callerFact.value.normal || !stopFact.required.refuted || !callerFact.required.refuted ||
		stopFact.required.unsupported || callerFact.required.unsupported || len(stopFact.conditions) != 1 || len(callerFact.conditions) != 0 {
		t.Fatal("failed invocation requirement fabricated a normal continuation or lost its dependency")
	}
	opaqueSpan, callSpan := u.Builder.Exprs.Get(opaque).Span, u.Builder.Exprs.Get(indirect).Span
	argSpan := u.Builder.Exprs.Get(call.Args[0].Value).Span
	want := []ReturnOriginPending{
		{SourceKey: u.SourceKey, Span: opaqueSpan, Reason: "opaque result type may carry borrowed state"},
		{SourceKey: u.SourceKey, Span: opaqueSpan, Reason: "callee returned an unproved source"},
		{SourceKey: u.SourceKey, Span: callSpan, Reason: "opaque result type may carry borrowed state"},
		{SourceKey: u.SourceKey, Span: argSpan, Reason: "callable argument conversion needs its selected destination promise"},
		{SourceKey: u.SourceKey, Span: callSpan, Reason: "indirect call may change reference-bearing or callable contents"},
	}
	if len(a.report.Pending) != len(want) {
		t.Fatalf("unexpected additional or missing Pending: %+v", a.report.Pending)
	}
	for _, item := range want {
		if !slices.Contains(a.report.Pending, item) {
			t.Fatalf("exact source refusal missing: %+v", item)
		}
	}
}
