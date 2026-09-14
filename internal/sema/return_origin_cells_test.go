package sema

import (
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// This is the admitted driver P08 source, including its external destination.
const returnOriginExternalCellSource = `fn read_old(dst: &mut &string, replacement: &string) -> &string {
    let old: &string = *dst;
    *dst = replacement;
    return old;
}
fn read_new(dst: &mut &string, replacement: &string) -> &string {
    *dst = replacement;
    return *dst;
}
fn returned_alias(dst: &mut &string, replacement: &string) -> &mut &string {
    *dst = replacement;
    return dst;
}
fn probe(dst: &mut &string, value: &string, replacement: &string) -> &string {
    let alias: &mut &string = returned_alias(dst, replacement);
    *alias = value;
    return *alias;
}
`

func TestReturnOriginExternalCellSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		slot uint32
	}{{"read_old", 0}, {"read_new", 1}, {"returned_alias", 0}, {"probe", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			// The non-strict helper retains actual Pending after real finalization.
			a := returnOriginConditionAnalyzer(t, returnOriginExternalCellSource)
			bodies := checkReturnOriginCellAuthority(t, a)
			fn := bodies[tc.name]
			fact, present := a.summaries[fn.key]
			var pending []ReturnOriginPending
			for _, item := range a.report.Pending {
				if item.SourceKey == fn.unit.SourceKey && item.Span.File == fn.item.Span.File &&
					item.Span.Start >= fn.item.Span.Start && item.Span.End <= fn.item.Span.End {
					pending = append(pending, item)
				}
			}
			t.Logf("CELL_FACT source=%s body=%s name=%s span=%+v present=%v normal=%v roots=%+v callables=%d required=%+v conditions=%+v pending=%+v diagnostics=%+v",
				fn.unit.SourceKey, fn.key, fn.name, fn.item.Span, present, fact.value.normal, fact.value.roots,
				len(fact.value.callables), fact.required, fact.conditions, pending, a.report.Diagnostics)
			if !present || !fact.value.normal || len(fact.value.callables) != 0 {
				t.Error("external cell result lacks its normal, non-callable summary")
			}
			var slots []uint32
			for _, root := range fact.value.roots {
				if root.kind != returnOriginParam || root.expired || int(root.param) >= len(fn.params) {
					t.Errorf("external cell result retains an unproved or expired source: %+v", root)
					continue
				}
				slots = append(slots, root.param)
			}
			slices.Sort(slots)
			slots = slices.Compact(slots)
			if !slices.Equal(slots, []uint32{tc.slot}) {
				t.Errorf("external cell result slots=%v, want [%d]", slots, tc.slot)
			}
			if len(pending) != 0 || len(a.report.Diagnostics) != 0 {
				t.Error("external cell body retains unresolved transfers or origin diagnostics")
			}
		})
	}
}

func checkReturnOriginCellAuthority(t *testing.T, a *returnOriginAnalyzer) map[string]*returnOriginFunction {
	t.Helper()
	if len(a.units) != 1 || len(a.functions) != 4 || len(a.bodies) != 4 || len(a.declarations) != 0 {
		t.Fatal("PRECONDITION: four original same-unit bodies are required")
	}
	u := a.units[0]
	res := u.Sema
	if res.InstantiationIdentity == nil || res.InstantiationClosure == nil || len(res.CallableCandidates) != 4 {
		t.Fatal("PRECONDITION: real identity, closure, and four original candidates are required")
	}
	closure := res.InstantiationClosure
	t.Logf("CELL_FINALIZATION source=%s roots=%+v edges=%+v instances=%+v uses=%+v live=%v seeds=%v ordinary_edges=%v",
		u.SourceKey, res.InstantiationGraph.Roots(), res.InstantiationGraph.Edges(), closure.Instances,
		closure.UseSites, closure.LiveCallables, res.InstantiationCallableSeeds, res.FunctionCallEdges)
	if len(res.InstantiationGraph.Roots()) != 0 || len(res.InstantiationGraph.Edges()) != 0 ||
		len(closure.Instances) != 0 || len(closure.UseSites) != 0 {
		t.Fatal("PRECONDITION: this original source has no generic instantiations")
	}
	bodies := make(map[string]*returnOriginFunction, 4)
	var seeds []symbols.SymbolID
	for _, fn := range a.functions {
		if fn.candidate == nil || !fn.candidate.HasBody || !fn.item.Body.IsValid() ||
			fn.candidate.Symbol != fn.symbol || len(fn.candidate.TemplateParams) != 0 || len(fn.candidate.TypeParams) != 0 ||
			fn.candidate.BodyKey != fn.key || fn.candidate.SourceKey != u.SourceKey || fn.unit != u || bodies[fn.name] != nil {
			t.Fatal("PRECONDITION: body lacks its exact original nongeneric candidate")
		}
		bodies[fn.name] = fn
		seeds = append(seeds, fn.symbol)
		wantParams := 2
		if fn.name == "probe" {
			wantParams = 3
		}
		if fn.info == nil || len(fn.info.Params) != wantParams || len(fn.params) != wantParams ||
			!slices.Equal(fn.info.Params, fn.candidate.ParamTypes) || fn.info.Result != fn.candidate.ResultType {
			t.Fatal("PRECONDITION: exact formal descriptors are missing")
		}
		dst, ok := res.TypeInterner.Lookup(fn.info.Params[0])
		if !ok || dst.Kind != types.KindReference || !dst.Mutable {
			t.Fatal("PRECONDITION: destination is not an external mutable reference")
		}
		inner, ok := res.TypeInterner.Lookup(dst.Elem)
		if !ok || inner.Kind != types.KindReference || inner.Mutable || inner.Elem != res.TypeInterner.Builtins().String {
			t.Fatal("PRECONDITION: destination does not contain a shared string reference")
		}
		wantResult := dst.Elem
		if fn.name == "returned_alias" {
			wantResult = fn.info.Params[0]
		}
		if fn.info.Result != wantResult {
			t.Fatal("PRECONDITION: original result type differs from the source")
		}
		for slot, id := range fn.params {
			sym := u.Symbols.Table.Symbols.Get(id)
			if sym == nil || sym.Kind != symbols.SymbolParam || sym.Scope != fn.scope || sym.Type != fn.info.Params[slot] ||
				res.BindingTypes[id] != sym.Type || slot > 0 && sym.Type != dst.Elem {
				t.Fatal("PRECONDITION: original parameter symbol, type, and scope disagree")
			}
			t.Logf("CELL_FORMAL body=%s slot=%d symbol=%d kind=%d scope=%d type=%d span=%+v", fn.key, slot, id, sym.Kind, sym.Scope, sym.Type, sym.Span)
		}
		t.Logf("CELL_BODY source=%s body=%s name=%s symbol=%d scope=%d span=%+v params=%v result=%d dst=%+v inner=%+v",
			u.SourceKey, fn.key, fn.name, fn.symbol, fn.scope, fn.item.Span, fn.info.Params, fn.info.Result, dst, inner)
	}
	for _, name := range []string{"read_old", "read_new", "returned_alias", "probe"} {
		if bodies[name] == nil {
			t.Fatalf("PRECONDITION: original body %s is absent", name)
		}
	}
	for _, seed := range seeds {
		_, seeded := res.InstantiationCallableSeeds[seed]
		if !seeded || !slices.Contains(closure.LiveCallables, seed) {
			t.Fatal("PRECONDITION: real finalization omitted an original body seed")
		}
	}
	checkReturnOriginCellOperations(t, bodies)
	return bodies
}

func checkReturnOriginCellOperations(t *testing.T, bodies map[string]*returnOriginFunction) {
	t.Helper()
	caller, callee := bodies["probe"], bodies["returned_alias"]
	u := caller.unit
	ids := make([]ast.ExprID, 0, len(u.Sema.ExprTypes))
	for id := range u.Sema.ExprTypes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	calls, stores, derefs := 0, 0, 0
	for _, id := range ids {
		node := u.Builder.Exprs.Get(id)
		if node == nil || node.Span.File != caller.item.Span.File || node.Span.End > uint32(len(returnOriginExternalCellSource)) {
			continue
		}
		if node.Kind != ast.ExprCall && node.Kind != ast.ExprUnary && node.Kind != ast.ExprBinary {
			continue
		}
		t.Logf("CELL_OPERATION expr=%d kind=%d type=%d span=%+v selected=%d source=%q", id, node.Kind,
			u.Sema.ExprTypes[id], node.Span, u.Symbols.ExprSymbols[id], returnOriginExternalCellSource[node.Span.Start:node.Span.End])
		if u.Sema.ExprTypes[id] == types.NoTypeID {
			t.Fatal("PRECONDITION: source cell operation lacks its actual type")
		}
		switch node.Kind {
		case ast.ExprUnary:
			unary, _ := u.Builder.Exprs.Unary(id)
			if unary.Op == ast.ExprUnaryDeref {
				derefs++
			}
		case ast.ExprBinary:
			binary, _ := u.Builder.Exprs.Binary(id)
			if binary.Op == ast.ExprBinaryAssign {
				stores++
			}
		case ast.ExprCall:
			calls++
			call, _ := u.Builder.Exprs.Call(id)
			if len(call.Args) != 2 || call.HasNamedArgs() || len(call.TypeArgs) != 0 ||
				u.Symbols.ExprSymbols[id] != callee.symbol || u.Symbols.ExprSymbols[call.Target] != callee.symbol ||
				u.Sema.ExprTypes[id] != callee.info.Result || node.Span.Start < caller.item.Span.Start || node.Span.End > caller.item.Span.End {
				t.Fatal("PRECONDITION: original probe call does not select returned_alias")
			}
			for i, slot := range []int{0, 2} {
				arg := call.Args[i].Value
				t.Logf("CELL_CALL_ARG call=%d slot=%d expr=%d symbol=%d type=%d span=%+v", id, i, arg,
					u.Symbols.ExprSymbols[arg], u.Sema.ExprTypes[arg], u.Builder.Exprs.Get(arg).Span)
				if u.Symbols.ExprSymbols[arg] != caller.params[slot] || u.Sema.ExprTypes[arg] != caller.info.Params[slot] ||
					u.Sema.ExprTypes[arg] != callee.info.Params[i] {
					t.Fatal("PRECONDITION: selected call lost its actual external destination or replacement")
				}
			}
		}
	}
	_, edge := u.Sema.FunctionCallEdges[caller.symbol][callee.symbol]
	if calls != 1 || stores != 4 || derefs != 7 || !edge {
		t.Fatalf("PRECONDITION: original operations/ordinary edge differ: calls=%d stores=%d derefs=%d edge=%v", calls, stores, derefs, edge)
	}
}
