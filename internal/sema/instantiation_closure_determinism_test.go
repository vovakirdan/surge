package sema

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
