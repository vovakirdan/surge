package sema

import (
	"slices"
	"testing"

	"surge/internal/symbols"
	"surge/internal/types"
)

// TestInjectRequiredValueOpRootsSendsEachShapeThroughItsOwnDoor pins the split
// the two root inputs exist for. A generic implementation is an instantiation
// demand the graph can hold; a non-generic one is not, and recordRoot silently
// drops an empty argument vector, so routing both the same way would lose the
// second without saying so.
func TestInjectRequiredValueOpRootsSendsEachShapeThroughItsOwnDoor(t *testing.T) {
	result := &Result{}
	result.InjectRequiredValueOpRoots([]RequiredValueOp{
		{
			Receiver: types.TypeID(7), Template: symbols.SymbolID(11),
			TemplateArgs: []types.TypeID{types.TypeID(3)},
			Reason:       requiredCloneOpReason,
			Witness:      InstantiationWitness{Reason: requiredValueOpWitnessReason},
		},
		{
			Receiver: types.TypeID(9), Template: symbols.SymbolID(12),
			Reason:  requiredCloneOpReason,
			Witness: InstantiationWitness{Reason: requiredValueOpWitnessReason},
		},
	})

	roots := result.InstantiationGraph.Roots()
	if len(roots) != 1 {
		t.Fatalf("instantiation roots = %+v, want exactly the generic one", roots)
	}
	if roots[0].Template != symbols.SymbolID(11) || roots[0].Witness.Reason != "required value operation" {
		t.Fatalf("generic root = %+v, want template 11 witnessed as a required value operation", roots[0])
	}
	if _, named := result.RequiredValueOpRoots[symbols.SymbolID(12)]; !named {
		t.Fatalf("non-generic root inputs = %v, want symbol 12", result.RequiredValueOpRoots)
	}
	if _, leaked := result.RequiredValueOpRoots[symbols.SymbolID(11)]; leaked {
		t.Fatalf("generic implementation also landed in the non-generic root input: %v", result.RequiredValueOpRoots)
	}
}

func TestInjectRequiredValueOpRootsIsIdempotent(t *testing.T) {
	op := RequiredValueOp{
		Receiver: types.TypeID(7), Template: symbols.SymbolID(11),
		TemplateArgs: []types.TypeID{types.TypeID(3)},
		Witness:      InstantiationWitness{Reason: requiredValueOpWitnessReason},
	}
	result := &Result{}
	result.InjectRequiredValueOpRoots([]RequiredValueOp{op})
	result.InjectRequiredValueOpRoots([]RequiredValueOp{op})
	if roots := result.InstantiationGraph.Roots(); len(roots) != 1 {
		t.Fatalf("re-injecting one operation recorded %d roots, want one: %+v", len(roots), roots)
	}
}

// TestRequiredValueOpRootReachesACallableNoSeedNames is the reachability half
// of the contract: the new input must make a body live on its own, without the
// root-module seed policy and without any call edge naming it.
func TestRequiredValueOpRootReachesACallableNoSeedNames(t *testing.T) {
	const seeded, required = symbols.SymbolID(1), symbols.SymbolID(2)
	live := requiredValueOpLiveCallables(t,
		map[symbols.SymbolID]struct{}{seeded: {}},
		map[symbols.SymbolID]struct{}{required: {}},
	)
	if !slices.Contains(live, required) {
		t.Fatalf("live callables = %v, want the required implementation %d", live, required)
	}
	if !slices.Contains(live, seeded) {
		t.Fatalf("live callables = %v, want the seeded callable %d kept", live, seeded)
	}

	withoutTheRoot := requiredValueOpLiveCallables(t, map[symbols.SymbolID]struct{}{seeded: {}}, nil)
	if slices.Contains(withoutTheRoot, required) {
		t.Fatalf("callable %d was live without its required-operation root: %v", required, withoutTheRoot)
	}
}

func requiredValueOpLiveCallables(
	t *testing.T,
	seeds map[symbols.SymbolID]struct{},
	requiredRoots map[symbols.SymbolID]struct{},
) []symbols.SymbolID {
	t.Helper()
	var graph InstantiationGraph
	_, live := reachableInstantiationGraph(&graph, nil, seeds, requiredRoots)
	return live
}

// TestDeriveRequiredValueOpsNeedsAClassifier keeps the derivation from silently
// answering "nothing required" when the authority it depends on is missing.
func TestDeriveRequiredValueOpsNeedsAClassifier(t *testing.T) {
	result := &Result{}
	if _, err := result.DeriveRequiredValueOps(nil); err == nil {
		t.Fatal("derivation accepted a missing capability classifier")
	}
}
