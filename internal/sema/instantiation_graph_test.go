package sema

import (
	"context"
	"reflect"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestInstantiationGraphBuildsDeclarativeReceiverAndMethodBindings(t *testing.T) {
	src := `
type Duo<A, B> = { first: A, second: B };

fn sink<X, Y, Z>() -> nothing { return nothing; }

extern<Duo<R, S>> {
    fn relay<M>(value: M) -> nothing {
        sink::<S, M, R>();
        return nothing;
    }
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(32)
	result := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	if bag.HasErrors() {
		t.Fatalf("sema diagnostics: %s", diagnosticsSummary(bag))
	}

	edges := result.InstantiationGraph.Edges()
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want relay -> sink: %+v", len(edges), edges)
	}
	edge := edges[0]
	caller := syms.Table.Symbols.Get(edge.Caller)
	callee := syms.Table.Symbols.Get(edge.Callee)
	if caller == nil || callee == nil || builder.StringsInterner.MustLookup(caller.Name) != "relay" || builder.StringsInterner.MustLookup(callee.Name) != "sink" {
		t.Fatalf("unexpected edge endpoints: caller=%+v callee=%+v", caller, callee)
	}
	if edge.CallerTemplateArity != 3 {
		t.Fatalf("caller arity = %d, want receiver(2) + method(1)", edge.CallerTemplateArity)
	}
	if len(edge.CallerBindings) != 3 {
		t.Fatalf("bindings = %+v, want three exact bindings", edge.CallerBindings)
	}
	receiverOwner := edge.CallerBindings[0].Owner
	want := []InstantiationParamBinding{
		{Param: edge.CallerBindings[0].Param, Owner: receiverOwner, ParamIndex: 0, ArgIndex: 0},
		{Param: edge.CallerBindings[1].Param, Owner: receiverOwner, ParamIndex: 1, ArgIndex: 1},
		{Param: edge.CallerBindings[2].Param, Owner: edge.Caller, ParamIndex: 0, ArgIndex: 2},
	}
	if !receiverOwner.IsValid() || receiverOwner == edge.Caller || !reflect.DeepEqual(edge.CallerBindings, want) {
		t.Fatalf("receiver/method binding offsets are wrong:\n got %+v\nwant %+v", edge.CallerBindings, want)
	}
	if caller.Kind != symbols.SymbolFunction || callee.Kind != symbols.SymbolFunction {
		t.Fatalf("edge kinds are not functions: caller=%s callee=%s", caller.Kind, callee.Kind)
	}
}

func TestMergeInstantiationGraphsRemapsEverySymbolBearingField(t *testing.T) {
	src := &Result{}
	src.InstantiationGraph.recordRoot(&InstantiationRoot{
		Kind:         InstantiationFunction,
		Template:     10,
		TemplateArgs: []types.TypeID{1},
		Witness: InstantiationWitness{
			Site:      source.Span{File: 3, Start: 1, End: 2},
			SourceKey: "lib/root.sg",
			Caller:    11,
			Reason:    "call",
		},
	})
	src.InstantiationGraph.recordEdge(&InstantiationEdge{
		Kind:                InstantiationFunction,
		Caller:              20,
		CallerTemplateArity: 2,
		CallerBindings: []InstantiationParamBinding{
			{Owner: 21, ParamIndex: 0, ArgIndex: 1},
			{Owner: 20, ParamIndex: 0, ArgIndex: 0},
		},
		Callee:             30,
		CalleeTemplateArgs: []types.TypeID{2},
		Witness: InstantiationWitness{
			Site:      source.Span{File: 4, Start: 2, End: 3},
			SourceKey: "lib/edge.sg",
			Caller:    20,
			Reason:    "call",
		},
	})
	mapping := map[symbols.SymbolID]symbols.SymbolID{
		10: 110,
		11: 111,
		20: 120,
		21: 121,
		30: 130,
	}
	dst := &Result{}
	MergeInstantiationGraphs(dst, src, mapping)
	roots, edges := dst.InstantiationGraph.Roots(), dst.InstantiationGraph.Edges()
	if len(roots) != 1 || roots[0].Template != 110 || roots[0].Witness.Caller != 111 || roots[0].Witness.SourceKey != "lib/root.sg" {
		t.Fatalf("remapped root = %+v", roots)
	}
	wantBindings := []InstantiationParamBinding{
		{Owner: 120, ParamIndex: 0, ArgIndex: 0},
		{Owner: 121, ParamIndex: 0, ArgIndex: 1},
	}
	if len(edges) != 1 || edges[0].Caller != 120 || edges[0].Callee != 130 || edges[0].Witness.Caller != 120 || !reflect.DeepEqual(edges[0].CallerBindings, wantBindings) || edges[0].Witness.SourceKey != "lib/edge.sg" {
		t.Fatalf("remapped edge = %+v, want bindings %+v", edges, wantBindings)
	}
	if original := src.InstantiationGraph.Edges()[0]; original.Caller != 20 || original.CallerBindings[0].Owner != 21 {
		t.Fatalf("merge mutated source graph: %+v", original)
	}
}

func TestInstantiationGraphRecordAndSnapshotsAreDetached(t *testing.T) {
	rootInput := &InstantiationRoot{
		Kind:         InstantiationFunction,
		Template:     10,
		TemplateArgs: []types.TypeID{1, 2},
		Witness: InstantiationWitness{
			SourceKey: "root.sg",
			Steps: []InstantiationStep{{
				Caller:    InstanceKey{TemplateKey: "caller", ArgsKey: "int"},
				Callee:    InstanceKey{TemplateKey: "callee", ArgsKey: "text"},
				SourceKey: "root-step.sg",
				Reason:    "root step",
			}},
		},
	}
	edgeInput := &InstantiationEdge{
		Kind:                InstantiationFunction,
		Caller:              20,
		CallerTemplateArity: 1,
		CallerBindings: []InstantiationParamBinding{{
			Owner:      20,
			ParamIndex: 0,
			ArgIndex:   0,
		}},
		Callee:             30,
		CalleeTemplateArgs: []types.TypeID{3},
		Witness: InstantiationWitness{
			SourceKey: "edge.sg",
			Steps: []InstantiationStep{{
				Caller:    InstanceKey{TemplateKey: "edge-caller", ArgsKey: "int"},
				Callee:    InstanceKey{TemplateKey: "edge-callee", ArgsKey: "bool"},
				SourceKey: "edge-step.sg",
				Reason:    "edge step",
			}},
		},
	}

	var graph InstantiationGraph
	graph.recordRoot(rootInput)
	graph.recordEdge(edgeInput)

	rootInput.TemplateArgs[0] = 99
	rootInput.Witness.Steps[0].SourceKey = "mutated-input-root.sg"
	edgeInput.CallerBindings[0].Owner = 99
	edgeInput.CalleeTemplateArgs[0] = 99
	edgeInput.Witness.Steps[0].Reason = "mutated input edge"

	roots, edges := graph.Roots(), graph.Edges()
	if len(roots) != 1 || roots[0].TemplateArgs[0] != 1 || roots[0].Witness.Steps[0].SourceKey != "root-step.sg" {
		t.Fatalf("recordRoot retained caller-owned data: %+v", roots)
	}
	if len(edges) != 1 || edges[0].CallerBindings[0].Owner != 20 || edges[0].CalleeTemplateArgs[0] != 3 || edges[0].Witness.Steps[0].Reason != "edge step" {
		t.Fatalf("recordEdge retained caller-owned data: %+v", edges)
	}

	roots[0].TemplateArgs[0] = 88
	roots[0].Witness.Steps[0].SourceKey = "mutated-snapshot-root.sg"
	edges[0].CallerBindings[0].Owner = 88
	edges[0].CalleeTemplateArgs[0] = 88
	edges[0].Witness.Steps[0].Reason = "mutated snapshot edge"

	roots, edges = graph.Roots(), graph.Edges()
	if roots[0].TemplateArgs[0] != 1 || roots[0].Witness.Steps[0].SourceKey != "root-step.sg" {
		t.Fatalf("Roots returned graph-owned data: %+v", roots)
	}
	if edges[0].CallerBindings[0].Owner != 20 || edges[0].CalleeTemplateArgs[0] != 3 || edges[0].Witness.Steps[0].Reason != "edge step" {
		t.Fatalf("Edges returned graph-owned data: %+v", edges)
	}
}

func TestDeferredInstantiationAuthorityCopiesAreDetached(t *testing.T) {
	deferredInput := &DeferredCallableEdge{
		UseID:               "main.sg/10:20/1/0",
		Kind:                DeferredMethodCall,
		Caller:              20,
		CallerTemplateArity: 1,
		CallerBindings: []InstantiationParamBinding{{
			Param: 1, Owner: 20, ParamIndex: 0, ArgIndex: 0,
		}},
		Receiver:         1,
		Method:           "Pick",
		Args:             []types.TypeID{2},
		ExplicitTypeArgs: []types.TypeID{3},
		ExpectedResult:   4,
		Requirement: DeferredCallableRequirement{
			Contracts: []symbols.SymbolID{30},
			Name:      "Pick",
			Params:    []types.TypeID{1, 2},
			Result:    4,
			Attrs:     []string{"hot"},
		},
		Witness: InstantiationWitness{
			SourceKey: "main.sg",
			Steps: []InstantiationStep{{
				Caller: InstanceKey{TemplateKey: "caller", ArgsKey: "int"},
				Callee: InstanceKey{TemplateKey: "callee", ArgsKey: "int"},
			}},
		},
	}
	var graph InstantiationGraph
	graph.recordDeferredCallable(deferredInput)
	deferredInput.CallerBindings[0].Owner = 99
	deferredInput.Args[0] = 99
	deferredInput.ExplicitTypeArgs[0] = 99
	deferredInput.Requirement.Contracts[0] = 99
	deferredInput.Requirement.Params[0] = 99
	deferredInput.Requirement.Attrs[0] = "mutated"
	deferredInput.Witness.Steps[0].Caller.TemplateKey = "mutated"

	snapshot := graph.DeferredCallables()
	if len(snapshot) != 1 || snapshot[0].CallerBindings[0].Owner != 20 || snapshot[0].Args[0] != 2 ||
		snapshot[0].ExplicitTypeArgs[0] != 3 || snapshot[0].Requirement.Contracts[0] != 30 ||
		snapshot[0].Requirement.Params[0] != 1 || snapshot[0].Requirement.Attrs[0] != "hot" ||
		snapshot[0].Witness.Steps[0].Caller.TemplateKey != "caller" {
		t.Fatalf("recordDeferredCallable retained caller-owned data: %+v", snapshot)
	}
	snapshot[0].Args[0] = 88
	snapshot[0].Requirement.Contracts[0] = 88
	snapshot[0].Witness.Steps[0].Caller.TemplateKey = "snapshot"
	snapshot = graph.DeferredCallables()
	if snapshot[0].Args[0] != 2 || snapshot[0].Requirement.Contracts[0] != 30 || snapshot[0].Witness.Steps[0].Caller.TemplateKey != "caller" {
		t.Fatalf("DeferredCallables returned graph-owned data: %+v", snapshot)
	}

	resolved := ResolvedDeferredCall{
		UseID: "use", CallerTemplateArgs: []types.TypeID{1}, CalleeTemplateArgs: []types.TypeID{2},
		CalleeParamTypes: []types.TypeID{3}, Args: []types.TypeID{4},
	}
	consumer := CloneResolvedDeferredCallForConsumer(&resolved)
	consumer.CallerTemplateArgs[0] = 90
	consumer.CalleeTemplateArgs[0] = 91
	consumer.CalleeParamTypes[0] = 92
	consumer.Args[0] = 93
	if resolved.CallerTemplateArgs[0] != 1 || resolved.CalleeTemplateArgs[0] != 2 ||
		resolved.CalleeParamTypes[0] != 3 || resolved.Args[0] != 4 {
		t.Fatalf("resolved deferred consumer alias leaked to authority: %+v", resolved)
	}

	src := &Result{
		CallableCandidates: []CallableCandidate{{
			Symbol: 40, Params: []symbols.TypeKey{"T"}, ParamTypes: []types.TypeID{1},
			TemplateParams: []types.TypeID{2}, TypeParams: []string{"T"}, Defaults: []bool{false},
			Variadic: []bool{false}, Attrs: []string{"hot"},
		}},
		EntrypointCallableRequests: []EntrypointCallableRequest{{Args: []types.TypeID{1}}},
		EntrypointCallableBindings: []EntrypointCallableBinding{{TemplateArgs: []types.TypeID{2}, ParamTypes: []types.TypeID{3}}},
		InstantiationClosure:       &InstantiationClosure{ResolvedDeferredCalls: []ResolvedDeferredCall{resolved}},
	}
	src.InstantiationGraph = graph
	dst := &Result{}
	CopyInstantiationAuthority(dst, src)
	dst.CallableCandidates[0].ParamTypes[0] = 70
	dst.CallableCandidates[0].Attrs[0] = "cold"
	dst.EntrypointCallableRequests[0].Args[0] = 71
	dst.EntrypointCallableBindings[0].TemplateArgs[0] = 72
	dst.InstantiationClosure.ResolvedDeferredCalls[0].CalleeParamTypes[0] = 73
	dst.InstantiationClosure.ResolvedDeferredCalls[0].Args[0] = 73
	if src.CallableCandidates[0].ParamTypes[0] != 1 || src.CallableCandidates[0].Attrs[0] != "hot" ||
		src.EntrypointCallableRequests[0].Args[0] != 1 || src.EntrypointCallableBindings[0].TemplateArgs[0] != 2 ||
		src.InstantiationClosure.ResolvedDeferredCalls[0].CalleeParamTypes[0] != 3 ||
		src.InstantiationClosure.ResolvedDeferredCalls[0].Args[0] != 4 {
		t.Fatalf("CopyInstantiationAuthority retained source aliases: src=%+v dst=%+v", src, dst)
	}
}
