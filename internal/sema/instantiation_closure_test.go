package sema

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestInstantiationClosureDirectRoot(t *testing.T) {
	in := types.NewInterner()
	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(10, in.Builtins().Int, 7))

	closure := requireInstantiationClosure(t, &graph, in, 64)
	if len(closure.Instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(closure.Instances))
	}
	instance := closure.Instances[0]
	if instance.Template != 10 || !reflect.DeepEqual(instance.TemplateArgs, []types.TypeID{in.Builtins().Int}) {
		t.Fatalf("unexpected direct instance: %+v", instance)
	}
	if instance.Witness.Root != instance.Key || instance.Witness.Site.Start != 7 {
		t.Fatalf("root witness was not frozen: %+v", instance.Witness)
	}
}

func TestInstantiationClosureTransitiveSubstitution(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f, g, h := symbols.SymbolID(10), symbols.SymbolID(20), symbols.SymbolID(30)
	fParam := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(f), 0, false, types.NoTypeID)
	gParam := in.RegisterTypeParam(in.Strings.Intern("U"), uint32(g), 0, false, types.NoTypeID)
	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(f, in.Builtins().Int, 1))
	graph.recordEdge(testInstantiationEdge(f, g, fParam, 2))
	graph.recordEdge(testInstantiationEdge(g, h, gParam, 3))

	closure := requireInstantiationClosure(t, &graph, in, 64)
	if len(closure.Instances) != 3 {
		t.Fatalf("instances = %d, want F/G/H; %+v", len(closure.Instances), closure.Instances)
	}
	for _, template := range []symbols.SymbolID{f, g, h} {
		instance := requireClosureTemplate(t, closure, template)
		if !reflect.DeepEqual(instance.TemplateArgs, []types.TypeID{in.Builtins().Int}) {
			t.Fatalf("symbol %d args = %v, want int", template, instance.TemplateArgs)
		}
	}
	if got := len(requireClosureTemplate(t, closure, h).Witness.Steps); got != 2 {
		t.Fatalf("H witness steps = %d, want 2", got)
	}
}

func TestInstantiationClosureSubstitutesNestedArray(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f, g := symbols.SymbolID(10), symbols.SymbolID(20)
	param := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(f), 0, false, types.NoTypeID)
	nested := in.Intern(types.MakeArray(param, 4))
	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(f, in.Builtins().Int32, 1))
	graph.recordEdge(testInstantiationEdge(f, g, nested, 2))

	closure := requireInstantiationClosure(t, &graph, in, 64)
	instance := requireClosureTemplate(t, closure, g)
	if len(instance.TemplateArgs) != 1 {
		t.Fatalf("nested args = %v", instance.TemplateArgs)
	}
	array, ok := in.Lookup(instance.TemplateArgs[0])
	if !ok || array.Kind != types.KindArray || array.Count != 4 || array.Elem != in.Builtins().Int32 {
		t.Fatalf("nested substitution = %+v, want int32[4]", array)
	}
}

func TestInstantiationClosureSubstitutesRecursiveGenericNominal(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	caller, callee := symbols.SymbolID(10), symbols.SymbolID(20)
	param := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(caller), 0, false, types.NoTypeID)
	name := in.Strings.Intern("Node")
	decl := source.Span{File: 1, Start: 100, End: 110}
	nodeOfT := in.RegisterStructInstance(name, decl, []types.TypeID{param})
	next := in.Intern(types.MakePointer(nodeOfT))
	in.SetStructFields(nodeOfT, []types.StructField{{Name: in.Strings.Intern("next"), Type: next}})

	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(caller, in.Builtins().Int, 1))
	graph.recordEdge(testInstantiationEdge(caller, callee, nodeOfT, 2))
	closure := requireInstantiationClosure(t, &graph, in, 64)
	concrete := requireClosureTemplate(t, closure, callee).TemplateArgs[0]
	info, ok := in.StructInfo(concrete)
	if !ok || info == nil || !reflect.DeepEqual(info.TypeArgs, []types.TypeID{in.Builtins().Int}) {
		t.Fatalf("recursive Node<T> did not become Node<int>: %+v", info)
	}
	fields := in.StructFields(concrete)
	if len(fields) != 1 {
		t.Fatalf("Node<int> fields = %+v", fields)
	}
	pointer, ok := in.Lookup(fields[0].Type)
	if !ok || pointer.Kind != types.KindPointer || pointer.Elem != concrete {
		t.Fatalf("recursive field = %+v, want *Node<int>#%d", pointer, concrete)
	}
}

func TestInstantiationClosureSubstitutesGenericStructBase(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	caller, callee := symbols.SymbolID(10), symbols.SymbolID(20)
	param := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(caller), 0, false, types.NoTypeID)
	baseDecl := source.Span{File: 1, Start: 100, End: 110}
	derivedDecl := source.Span{File: 1, Start: 120, End: 130}
	baseOfT := in.RegisterStructInstance(in.Strings.Intern("Base"), baseDecl, []types.TypeID{param})
	derivedOfT := in.RegisterStructInstance(in.Strings.Intern("Derived"), derivedDecl, []types.TypeID{param})
	in.SetStructBase(derivedOfT, baseOfT)

	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(caller, in.Builtins().Int, 1))
	graph.recordEdge(testInstantiationEdge(caller, callee, derivedOfT, 2))
	closure := requireInstantiationClosure(t, &graph, in, 64)
	derivedConcrete := requireClosureTemplate(t, closure, callee).TemplateArgs[0]
	baseConcrete, ok := in.StructBase(derivedConcrete)
	if !ok {
		t.Fatalf("Derived<int> lost its substituted base")
	}
	baseInfo, ok := in.StructInfo(baseConcrete)
	if !ok || baseInfo == nil || baseInfo.Name != in.Strings.Intern("Base") ||
		!reflect.DeepEqual(baseInfo.TypeArgs, []types.TypeID{in.Builtins().Int}) {
		t.Fatalf("Derived<int> base = %+v, want Base<int>", baseInfo)
	}
}

func TestInstantiationClosureSubstitutesMutuallyRecursiveGenericNominals(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	caller, callee := symbols.SymbolID(10), symbols.SymbolID(20)
	param := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(caller), 0, false, types.NoTypeID)
	aName, bName := in.Strings.Intern("A"), in.Strings.Intern("B")
	aDecl := source.Span{File: 1, Start: 100, End: 110}
	bDecl := source.Span{File: 1, Start: 120, End: 130}
	aOfT := in.RegisterStructInstance(aName, aDecl, []types.TypeID{param})
	bOfT := in.RegisterStructInstance(bName, bDecl, []types.TypeID{param})
	in.SetStructFields(aOfT, []types.StructField{{Name: in.Strings.Intern("b"), Type: in.Intern(types.MakePointer(bOfT))}})
	in.SetStructFields(bOfT, []types.StructField{{Name: in.Strings.Intern("a"), Type: in.Intern(types.MakePointer(aOfT))}})

	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(caller, in.Builtins().String, 1))
	graph.recordEdge(testInstantiationEdge(caller, callee, aOfT, 2))
	closure := requireInstantiationClosure(t, &graph, in, 64)
	aConcrete := requireClosureTemplate(t, closure, callee).TemplateArgs[0]
	aFields := in.StructFields(aConcrete)
	if len(aFields) != 1 {
		t.Fatalf("A<string> fields = %+v", aFields)
	}
	bPointer, ok := in.Lookup(aFields[0].Type)
	if !ok || bPointer.Kind != types.KindPointer {
		t.Fatalf("A<string>.b = %+v", bPointer)
	}
	bConcrete := bPointer.Elem
	bInfo, _ := in.StructInfo(bConcrete)
	if bInfo == nil || !reflect.DeepEqual(bInfo.TypeArgs, []types.TypeID{in.Builtins().String}) {
		t.Fatalf("mutual B<T> did not become B<string>: %+v", bInfo)
	}
	bFields := in.StructFields(bConcrete)
	if len(bFields) != 1 {
		t.Fatalf("B<string> fields = %+v", bFields)
	}
	aPointer, ok := in.Lookup(bFields[0].Type)
	if !ok || aPointer.Kind != types.KindPointer || aPointer.Elem != aConcrete {
		t.Fatalf("B<string>.a = %+v, want *A<string>#%d", aPointer, aConcrete)
	}
}

func TestInstantiationClosureUsesExactReceiverAndMethodBindings(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	caller, receiver, callee := symbols.SymbolID(10), symbols.SymbolID(11), symbols.SymbolID(20)
	receiverFirst := in.RegisterTypeParam(in.Strings.Intern("R"), uint32(receiver), 0, false, types.NoTypeID)
	receiverSecond := in.RegisterTypeParam(in.Strings.Intern("S"), uint32(receiver), 1, false, types.NoTypeID)
	methodParam := in.RegisterTypeParam(in.Strings.Intern("M"), uint32(caller), 0, false, types.NoTypeID)

	graph := InstantiationGraph{}
	graph.recordRoot(&InstantiationRoot{
		Kind:         InstantiationFunction,
		Template:     caller,
		TemplateArgs: []types.TypeID{in.Builtins().Int, in.Builtins().String, in.Builtins().Bool},
		Witness:      InstantiationWitness{Site: source.Span{File: 1, Start: 1, End: 2}, Caller: 1, Reason: "call"},
	})
	graph.recordEdge(&InstantiationEdge{
		Kind:                InstantiationFunction,
		Caller:              caller,
		CallerTemplateArity: 3,
		CallerBindings: []InstantiationParamBinding{
			{Owner: receiver, ParamIndex: 0, ArgIndex: 0},
			{Owner: receiver, ParamIndex: 1, ArgIndex: 1},
			{Owner: caller, ParamIndex: 0, ArgIndex: 2},
		},
		Callee:             callee,
		CalleeTemplateArgs: []types.TypeID{receiverSecond, methodParam, receiverFirst},
		Witness:            InstantiationWitness{Site: source.Span{File: 1, Start: 2, End: 3}, Caller: caller, Reason: "call"},
	})

	closure := requireInstantiationClosure(t, &graph, in, 64)
	instance := requireClosureTemplate(t, closure, callee)
	want := []types.TypeID{in.Builtins().String, in.Builtins().Bool, in.Builtins().Int}
	if !reflect.DeepEqual(instance.TemplateArgs, want) {
		t.Fatalf("mixed receiver/method args = %v, want %v", instance.TemplateArgs, want)
	}
}

func TestInstantiationClosureRejectsForeignNestedOwner(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	caller, callee, foreign := symbols.SymbolID(10), symbols.SymbolID(20), symbols.SymbolID(99)
	callerParam := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(caller), 0, false, types.NoTypeID)
	foreignParam := in.RegisterTypeParam(in.Strings.Intern("U"), uint32(foreign), 0, false, types.NoTypeID)
	nested := in.Intern(types.MakeArray(foreignParam, 2))

	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(caller, in.Builtins().Int, 1))
	graph.recordEdge(&InstantiationEdge{
		Kind:                InstantiationFunction,
		Caller:              caller,
		CallerTemplateArity: 1,
		CallerBindings:      []InstantiationParamBinding{{Owner: caller, ParamIndex: 0, ArgIndex: 0}},
		Callee:              callee,
		CalleeTemplateArgs:  []types.TypeID{callerParam, nested},
		Witness:             InstantiationWitness{Site: source.Span{File: 1, Start: 2, End: 3}, Caller: caller, Reason: "call"},
	})

	_, err := BuildInstantiationClosure(&graph, testInstantiationIdentity(in), 64)
	if err == nil || !strings.Contains(err.Error(), "edge 10 -> 20") || !strings.Contains(err.Error(), "type parameter owner 99 index 0 is not bound") || !strings.Contains(err.Error(), "note: callee arguments") {
		t.Fatalf("foreign owner error is not actionable: %v", err)
	}
}

func TestInstantiationClosureExactSCCConverges(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f, g := symbols.SymbolID(10), symbols.SymbolID(20)
	fParam := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(f), 0, false, types.NoTypeID)
	gParam := in.RegisterTypeParam(in.Strings.Intern("U"), uint32(g), 0, false, types.NoTypeID)
	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(f, in.Builtins().Int, 1))
	graph.recordEdge(testInstantiationEdge(f, g, fParam, 2))
	graph.recordEdge(testInstantiationEdge(g, f, gParam, 3))

	closure := requireInstantiationClosure(t, &graph, in, 2)
	if len(closure.Instances) != 2 {
		t.Fatalf("exact SCC did not converge: %+v", closure.Instances)
	}
}

func TestInstantiationClosureExpandingRecursionIsBoundedAndTraceable(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f := symbols.SymbolID(10)
	param := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(f), 0, false, types.NoTypeID)
	expanded := in.Intern(types.MakeArray(param, 1))
	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(f, in.Builtins().Int, 1))
	graph.recordEdge(testInstantiationEdge(f, f, expanded, 2))

	_, err := BuildInstantiationClosure(&graph, testInstantiationIdentity(in), 4)
	var expansion *InstantiationExpansionError
	if !errors.As(err, &expansion) {
		t.Fatalf("expected expansion error, got %v", err)
	}
	if expansion.Limit != 4 || len(expansion.Witness.Steps) != 5 {
		t.Fatalf("unexpected expansion evidence: %+v", expansion)
	}
	message := expansion.Error()
	if !strings.Contains(message, "depth 4") || !strings.Contains(message, "root sym/10") || !strings.Contains(message, "(call)") {
		t.Fatalf("expansion error is not traceable: %s", message)
	}
}

func TestInstantiationClosureBranchingExpansionHitsTotalBudget(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f := symbols.SymbolID(10)
	param := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(f), 0, false, types.NoTypeID)
	arrayBranch := in.Intern(types.MakeArray(param, 1))
	pointerBranch := in.Intern(types.MakePointer(param))
	graph := InstantiationGraph{}
	graph.recordRoot(testInstantiationRoot(f, in.Builtins().Int, 1))
	graph.recordEdge(testInstantiationEdge(f, f, arrayBranch, 2))
	graph.recordEdge(testInstantiationEdge(f, f, pointerBranch, 3))

	_, err := buildInstantiationClosure(&graph, testInstantiationIdentity(in), instantiationClosureLimits{maxDepth: 64, maxInstances: 4})
	var budget *InstantiationBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("expected concrete-instance budget error, got %v", err)
	}
	if budget.Limit != 4 || len(budget.Witness.Steps) == 0 {
		t.Fatalf("budget evidence = %+v", budget)
	}
	message := budget.Error()
	if !strings.Contains(message, "budget 4") || !strings.Contains(message, "root sym/10") || !strings.Contains(message, "must converge") {
		t.Fatalf("budget error is not actionable: %s", message)
	}
}

func TestInstantiationClosureLeavesUnusedGenericDeferred(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f, g := symbols.SymbolID(10), symbols.SymbolID(20)
	param := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(f), 0, false, types.NoTypeID)
	graph := InstantiationGraph{}
	graph.recordEdge(testInstantiationEdge(f, g, param, 2))

	closure := requireInstantiationClosure(t, &graph, in, 64)
	if len(closure.Instances) != 0 {
		t.Fatalf("unused generic became reachable: %+v", closure.Instances)
	}
}

func TestInstantiationClosureIsDeterministicUnderDuplicateShuffledRecords(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f, g, h := symbols.SymbolID(10), symbols.SymbolID(20), symbols.SymbolID(30)
	param := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(f), 0, false, types.NoTypeID)

	type graphOp struct {
		root *InstantiationRoot
		edge *InstantiationEdge
	}
	root20 := testInstantiationRoot(f, in.Builtins().Int, 20)
	root10 := testInstantiationRoot(f, in.Builtins().Int, 10)
	edgeH50 := testInstantiationEdge(f, h, param, 50)
	edgeG40 := testInstantiationEdge(f, g, param, 40)
	edgeG30 := testInstantiationEdge(f, g, param, 30)
	ops := []graphOp{
		{root: root20},
		{root: root10},
		{edge: edgeH50},
		{edge: edgeG40},
		{edge: edgeG30},
		{root: root10}, // exact duplicates exercise recorder compaction too
		{edge: edgeG30},
	}
	permutations := [][]int{
		{0, 1, 2, 3, 4, 5, 6},
		{6, 5, 4, 3, 2, 1, 0},
		{0, 2, 1, 3, 5, 4, 6},
		{2, 0, 3, 1, 4, 6, 5},
		{4, 3, 2, 1, 0, 6, 5},
		{5, 1, 6, 4, 0, 2, 3},
		{3, 6, 0, 4, 1, 5, 2},
		{1, 4, 2, 5, 3, 0, 6},
	}
	build := func(order []int) InstantiationGraph {
		var graph InstantiationGraph
		for _, index := range order {
			op := ops[index]
			if op.root != nil {
				graph.recordRoot(op.root)
			} else {
				graph.recordEdge(op.edge)
			}
		}
		return graph
	}

	baseline := requireInstantiationClosure(t, ptrGraph(build(permutations[0])), in, 64)
	for i, permutation := range permutations[1:] {
		candidate := requireInstantiationClosure(t, ptrGraph(build(permutation)), in, 64)
		if !reflect.DeepEqual(baseline, candidate) {
			t.Fatalf("permutation %d changed closure/use-sites/witnesses:\nbaseline %+v\ncandidate %+v", i+1, baseline, candidate)
		}
	}
	if got := requireClosureTemplate(t, baseline, f).Witness.Site.Start; got != 10 {
		t.Fatalf("root witness start = %d, want stable minimum 10", got)
	}
	if got := requireClosureTemplate(t, baseline, g).Witness.Steps[0].Site.Start; got != 30 {
		t.Fatalf("edge witness start = %d, want stable minimum 30", got)
	}
	if got := len(baseline.UseSites); got != 5 {
		t.Fatalf("concrete use-sites = %d, want both roots and all three distinct edge callsites", got)
	}
}

func TestInstantiationClosureCanonicalSnapshotIgnoresRawIDsAndAllocationOrder(t *testing.T) {
	type variant struct {
		closure  InstantiationClosure
		identity InstantiationIdentity
	}
	build := func(f, g, h symbols.SymbolID, file source.FileID, reverse, addNoise bool) variant {
		in := types.NewInterner()
		in.Strings = source.NewInterner()
		if addNoise {
			_ = in.Intern(types.MakePointer(in.Builtins().String))
		}
		fParam := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(f), 0, false, types.NoTypeID)
		gParam := in.RegisterTypeParam(in.Strings.Intern("U"), uint32(g), 0, false, types.NoTypeID)
		arrayOfF := in.Intern(types.MakeArray(fParam, 4))
		pointerToG := in.Intern(types.MakePointer(gParam))
		root := testInstantiationRoot(f, in.Builtins().Int, 10)
		root.Witness.Site.File = file
		root.Witness.SourceKey = "src/main.sg"
		root.Witness.CallerKey = "fn/main"
		edgeFG := testInstantiationEdge(f, g, arrayOfF, 20)
		edgeFG.Witness.Site.File = file
		edgeFG.Witness.SourceKey = "src/lib.sg"
		edgeFG.Witness.CallerKey = "fn/f"
		edgeGH := testInstantiationEdge(g, h, pointerToG, 30)
		edgeGH.Witness.Site.File = file
		edgeGH.Witness.SourceKey = "src/lib.sg"
		edgeGH.Witness.CallerKey = "fn/g"

		var graph InstantiationGraph
		if reverse {
			graph.recordEdge(edgeGH)
			graph.recordEdge(edgeFG)
			graph.recordRoot(root)
		} else {
			graph.recordRoot(root)
			graph.recordEdge(edgeFG)
			graph.recordEdge(edgeGH)
		}
		names := map[symbols.SymbolID]string{f: "fn/f", g: "fn/g", h: "fn/h"}
		identity := InstantiationIdentity{
			Types: types.CanonicalKeyContext{Types: in},
			ResolveTemplate: func(id symbols.SymbolID) (string, error) {
				if name := names[id]; name != "" {
					return name, nil
				}
				return "", fmt.Errorf("unknown template %d", id)
			},
			ResolveSource: func(source.FileID) (string, error) { return "unused", nil },
		}
		closure, err := BuildInstantiationClosure(&graph, identity, 64)
		if err != nil {
			t.Fatalf("build variant closure: %v", err)
		}
		return variant{closure: closure, identity: identity}
	}

	left := build(10, 20, 30, 1, false, false)
	right := build(101, 77, 205, 9, true, true)
	leftSnapshot := semanticClosureSnapshot(t, left.closure, left.identity)
	rightSnapshot := semanticClosureSnapshot(t, right.closure, right.identity)
	if leftSnapshot != rightSnapshot {
		t.Fatalf("canonical closure changed with raw IDs/allocation order:\nleft:\n%s\nright:\n%s", leftSnapshot, rightSnapshot)
	}
}

func semanticClosureSnapshot(t *testing.T, closure InstantiationClosure, identity InstantiationIdentity) string {
	t.Helper()
	var out strings.Builder
	for _, instance := range closure.Instances {
		fmt.Fprintf(&out, "instance %s|%s at %s:%d-%d", instance.Key.TemplateKey, instance.Key.ArgsKey, instance.Witness.SourceKey, instance.Witness.Site.Start, instance.Witness.Site.End)
		for _, step := range instance.Witness.Steps {
			fmt.Fprintf(&out, " -> %s|%s at %s:%d-%d", step.Callee.TemplateKey, step.Callee.ArgsKey, step.SourceKey, step.Site.Start, step.Site.End)
		}
		out.WriteByte('\n')
	}
	for _, use := range closure.UseSites {
		callerArgs, err := identity.Types.TypeArgsKey(use.CallerTemplateArgs)
		if err != nil {
			t.Fatalf("canonical caller args: %v", err)
		}
		calleeArgs, err := identity.Types.TypeArgsKey(use.TemplateArgs)
		if err != nil {
			t.Fatalf("canonical callee args: %v", err)
		}
		fmt.Fprintf(&out, "use %s|%s => %s|%s at %s:%d-%d %s\n", use.Caller.TemplateKey, callerArgs, use.Callee.TemplateKey, calleeArgs, use.SourceKey, use.Site.Start, use.Site.End, use.Reason)
	}
	return out.String()
}

func testInstantiationRoot(template symbols.SymbolID, arg types.TypeID, start uint32) *InstantiationRoot {
	return &InstantiationRoot{
		Kind:         InstantiationFunction,
		Template:     template,
		TemplateArgs: []types.TypeID{arg},
		Witness: InstantiationWitness{
			Site:   source.Span{File: 1, Start: start, End: start + 1},
			Caller: 1,
			Reason: "call",
		},
	}
}

func testInstantiationEdge(caller, callee symbols.SymbolID, arg types.TypeID, start uint32) *InstantiationEdge {
	return &InstantiationEdge{
		Kind:                InstantiationFunction,
		Caller:              caller,
		CallerTemplateArity: 1,
		CallerBindings:      []InstantiationParamBinding{{Owner: caller, ParamIndex: 0, ArgIndex: 0}},
		Callee:              callee,
		CalleeTemplateArgs:  []types.TypeID{arg},
		Witness: InstantiationWitness{
			Site:   source.Span{File: 1, Start: start, End: start + 1},
			Caller: caller,
			Reason: "call",
		},
	}
}

func requireInstantiationClosure(t *testing.T, graph *InstantiationGraph, in *types.Interner, maxDepth int) InstantiationClosure {
	t.Helper()
	closure, err := BuildInstantiationClosure(graph, testInstantiationIdentity(in), maxDepth)
	if err != nil {
		t.Fatalf("build closure: %v", err)
	}
	return closure
}

func requireClosureTemplate(t *testing.T, closure InstantiationClosure, template symbols.SymbolID) InstantiationInstance {
	t.Helper()
	for _, instance := range closure.Instances {
		if instance.Template == template {
			return instance
		}
	}
	t.Fatalf("closure has no symbol %d: %+v", template, closure.Instances)
	return InstantiationInstance{}
}

func testInstantiationIdentity(in *types.Interner) InstantiationIdentity {
	return InstantiationIdentity{
		Types: types.CanonicalKeyContext{
			Types: in,
			ResolveNominal: func(kind types.Kind, name string, decl source.Span) (string, error) {
				return fmt.Sprintf("%s/%s/%d/%d", kind, name, decl.Start, decl.End), nil
			},
		},
		ResolveTemplate: func(id symbols.SymbolID) (string, error) {
			return fmt.Sprintf("sym/%d", id), nil
		},
		ResolveSource: func(id source.FileID) (string, error) {
			return fmt.Sprintf("file/%d", id), nil
		},
	}
}

func ptrGraph(graph InstantiationGraph) *InstantiationGraph { return &graph }
