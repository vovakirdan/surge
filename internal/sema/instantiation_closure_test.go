package sema

import (
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
