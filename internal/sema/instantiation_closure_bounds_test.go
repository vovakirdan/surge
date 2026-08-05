package sema

import (
	"errors"
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
