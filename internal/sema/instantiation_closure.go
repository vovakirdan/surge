package sema

import (
	"fmt"
	"slices"
	"strings"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// DefaultInstantiationClosureInstanceLimit bounds the total worklist even
// when a shallow generic graph branches exponentially.
const DefaultInstantiationClosureInstanceLimit = 65_536

type instantiationClosureLimits struct {
	maxDepth     int
	maxInstances int
}

// InstantiationInstance is one concrete, reachable template instance.
type InstantiationInstance struct {
	Key          InstanceKey
	Template     symbols.SymbolID
	Kind         InstantiationTemplateKind
	TemplateArgs []types.TypeID
	Witness      InstantiationWitness
	Depth        int
}

// ConcreteInstantiationUse is one finalized call/function-value use-site.
// Generic callers are keyed by their concrete canonical instance; non-generic
// roots have a zero Caller key.
type ConcreteInstantiationUse struct {
	Caller             InstanceKey
	CallerTemplate     symbols.SymbolID
	CallerTemplateArgs []types.TypeID
	Callee             InstanceKey
	CalleeTemplate     symbols.SymbolID
	Kind               InstantiationTemplateKind
	TemplateArgs       []types.TypeID
	Site               source.Span
	SourceKey          string
	Reason             string
}

// InstantiationClosure is the deterministic concrete-substitution fixpoint of
// the always-on graph. Instances are sorted by canonical InstanceKey.
type InstantiationClosure struct {
	// LiveCallables are the reachable non-generic callable bodies. Generic
	// bodies are represented by their concrete Instances below.
	LiveCallables []symbols.SymbolID
	Instances     []InstantiationInstance
	UseSites      []ConcreteInstantiationUse
	// ResolvedDeferredCalls is the exact concrete callable map keyed by
	// (caller instance, DeferredUseID) and consumed directly by mono.
	ResolvedDeferredCalls []ResolvedDeferredCall
}

// Lookup returns a detached concrete instance by canonical key.
func (c *InstantiationClosure) Lookup(key InstanceKey) (InstantiationInstance, bool) {
	if c == nil {
		return InstantiationInstance{}, false
	}
	idx, found := slices.BinarySearchFunc(c.Instances, key, func(instance InstantiationInstance, target InstanceKey) int {
		return compareInstanceKey(instance.Key, target)
	})
	if !found {
		return InstantiationInstance{}, false
	}
	return cloneInstantiationInstance(&c.Instances[idx]), true
}

// InstantiationExpansionError reports a generic recursion whose concrete
// argument shape keeps expanding beyond the shared monomorphization bound.
type InstantiationExpansionError struct {
	Limit    int
	Instance InstanceKey
	Witness  InstantiationWitness
}

// InstantiationBudgetError reports a branching graph that exceeded the total
// concrete-instance budget before exhausting compiler memory.
type InstantiationBudgetError struct {
	Limit    int
	Instance InstanceKey
	Witness  InstantiationWitness
}

func (e *InstantiationBudgetError) Error() string {
	if e == nil {
		return "instantiation closure exceeded its concrete-instance budget"
	}
	return fmt.Sprintf(
		"instantiation closure exceeded concrete-instance budget %d at %s args=%s; root %s at %s; note: generic expansion must converge to a finite set of concrete arguments",
		e.Limit,
		e.Instance.TemplateKey,
		e.Instance.ArgsKey,
		e.Witness.Root.TemplateKey,
		witnessSiteLabel(&e.Witness),
	)
}

func (e *InstantiationExpansionError) Error() string {
	if e == nil {
		return "instantiation expansion exceeded its depth limit"
	}
	var out strings.Builder
	fmt.Fprintf(&out, "instantiation expansion exceeded depth %d at %s args=%s", e.Limit, e.Instance.TemplateKey, e.Instance.ArgsKey)
	if e.Witness.Root.TemplateKey != "" {
		fmt.Fprintf(&out, "; root %s at %s", e.Witness.Root.TemplateKey, witnessSiteLabel(&e.Witness))
	}
	for i := range e.Witness.Steps {
		step := &e.Witness.Steps[i]
		fmt.Fprintf(&out, " -> %s at %s", step.Callee.TemplateKey, stepSiteLabel(step))
		if step.Reason != "" {
			fmt.Fprintf(&out, " (%s)", step.Reason)
		}
	}
	return out.String()
}

// FinalizeInstantiationClosure computes and stores the authoritative closure.
func (r *Result) FinalizeInstantiationClosure(identity InstantiationIdentity, maxDepth int) error {
	if r == nil {
		return fmt.Errorf("instantiation closure: missing semantic result")
	}
	var closure InstantiationClosure
	var err error
	if r.InstantiationCallableSeeds == nil && r.FunctionCallEdges == nil {
		// Compatibility for low-level embedders that construct Result values by
		// hand. Driver-created results always install an explicit root policy.
		closure, err = BuildInstantiationClosure(&r.InstantiationGraph, identity, maxDepth)
	} else {
		closure, err = buildReachableInstantiationClosureWithDeferred(
			&r.InstantiationGraph,
			r.FunctionCallEdges,
			r.InstantiationCallableSeeds,
			r.CallableCandidates,
			r.InstantiationTemplateParams,
			identity,
			instantiationClosureLimits{maxDepth: maxDepth, maxInstances: DefaultInstantiationClosureInstanceLimit},
		)
	}
	if err != nil {
		return err
	}
	r.InstantiationClosure = &closure
	r.rebuildFunctionInstantiations()
	return nil
}

// BuildInstantiationClosure computes a sorted worklist/fixpoint. Exact SCCs
// converge through InstanceKey dedupe; expanding recursion fails with a source
// trace once maxDepth is crossed.
func BuildInstantiationClosure(graph *InstantiationGraph, identity InstantiationIdentity, maxDepth int) (InstantiationClosure, error) {
	return buildInstantiationClosure(graph, identity, instantiationClosureLimits{
		maxDepth:     maxDepth,
		maxInstances: DefaultInstantiationClosureInstanceLimit,
	})
}

// BuildReachableInstantiationClosure applies the root-module callable policy
// before computing concrete generic substitution. Dependency functions become
// live only through sema-resolved calls from an already-live callable or
// generic template.
func BuildReachableInstantiationClosure(
	graph *InstantiationGraph,
	callEdges map[symbols.SymbolID]map[symbols.SymbolID]struct{},
	seeds map[symbols.SymbolID]struct{},
	identity InstantiationIdentity,
	maxDepth int,
) (InstantiationClosure, error) {
	filtered, live := reachableInstantiationGraph(graph, callEdges, seeds)
	closure, err := buildInstantiationClosure(&filtered, identity, instantiationClosureLimits{
		maxDepth:     maxDepth,
		maxInstances: DefaultInstantiationClosureInstanceLimit,
	})
	if err != nil {
		return InstantiationClosure{}, err
	}
	closure.LiveCallables = live
	return closure, nil
}
