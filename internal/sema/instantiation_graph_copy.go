package sema

import (
	"slices"

	"surge/internal/symbols"
	"surge/internal/types"
)

// CopyInstantiationAuthority shares one finalized module-level authority with
// another per-file semantic result while preserving that result's expression
// and borrow analysis maps.
func CopyInstantiationAuthority(dst, src *Result) {
	if dst == nil || src == nil {
		return
	}
	var graph InstantiationGraph
	roots := src.InstantiationGraph.Roots()
	for i := range roots {
		graph.recordRoot(&roots[i])
	}
	edges := src.InstantiationGraph.Edges()
	for i := range edges {
		graph.recordEdge(&edges[i])
	}
	deferred := src.InstantiationGraph.DeferredCallables()
	for i := range deferred {
		graph.recordDeferredCallable(&deferred[i])
	}
	dst.CallableCandidates = make([]CallableCandidate, len(src.CallableCandidates))
	for i := range src.CallableCandidates {
		dst.CallableCandidates[i] = cloneCallableCandidate(&src.CallableCandidates[i])
	}
	copyFinalizationRequests(dst, src)
	dst.InstantiationGraph = graph
	dst.InstantiationIdentity = src.InstantiationIdentity
	dst.InstantiationClosure = nil
	if src.InstantiationClosure != nil {
		closure := InstantiationClosure{
			LiveCallables:         slices.Clone(src.InstantiationClosure.LiveCallables),
			Instances:             make([]InstantiationInstance, len(src.InstantiationClosure.Instances)),
			UseSites:              make([]ConcreteInstantiationUse, len(src.InstantiationClosure.UseSites)),
			ResolvedDeferredCalls: make([]ResolvedDeferredCall, len(src.InstantiationClosure.ResolvedDeferredCalls)),
		}
		for i := range src.InstantiationClosure.Instances {
			closure.Instances[i] = cloneInstantiationInstance(&src.InstantiationClosure.Instances[i])
		}
		for i := range src.InstantiationClosure.UseSites {
			use := src.InstantiationClosure.UseSites[i]
			use.CallerTemplateArgs = slices.Clone(use.CallerTemplateArgs)
			use.TemplateArgs = slices.Clone(use.TemplateArgs)
			closure.UseSites[i] = use
		}
		for i := range src.InstantiationClosure.ResolvedDeferredCalls {
			closure.ResolvedDeferredCalls[i] = cloneResolvedDeferredCall(&src.InstantiationClosure.ResolvedDeferredCalls[i])
		}
		dst.InstantiationClosure = &closure
	}
	dst.FunctionCallEdges = cloneFunctionCallEdges(src.FunctionCallEdges)
	dst.InstantiationCallableSeeds = cloneCallableSet(src.InstantiationCallableSeeds)
	dst.InstantiationTemplateParams = cloneTemplateParams(src.InstantiationTemplateParams)
	dst.rebuildFunctionInstantiations()
}

func cloneTemplateParams(src map[symbols.SymbolID][]types.TypeID) map[symbols.SymbolID][]types.TypeID {
	if src == nil {
		return nil
	}
	out := make(map[symbols.SymbolID][]types.TypeID, len(src))
	for template, params := range src {
		out[template] = slices.Clone(params)
	}
	return out
}

func cloneFunctionCallEdges(src map[symbols.SymbolID]map[symbols.SymbolID]struct{}) map[symbols.SymbolID]map[symbols.SymbolID]struct{} {
	if src == nil {
		return nil
	}
	out := make(map[symbols.SymbolID]map[symbols.SymbolID]struct{}, len(src))
	for caller, callees := range src {
		out[caller] = cloneCallableSet(callees)
	}
	return out
}

func cloneCallableSet(src map[symbols.SymbolID]struct{}) map[symbols.SymbolID]struct{} {
	if src == nil {
		return nil
	}
	out := make(map[symbols.SymbolID]struct{}, len(src))
	for id := range src {
		out[id] = struct{}{}
	}
	return out
}

func cloneInstantiationRoot(root *InstantiationRoot) InstantiationRoot {
	cloned := *root
	cloned.TemplateArgs = slices.Clone(root.TemplateArgs)
	cloned.Witness = cloneInstantiationWitness(&root.Witness)
	return cloned
}

func cloneInstantiationEdge(edge *InstantiationEdge) InstantiationEdge {
	cloned := *edge
	cloned.CallerBindings = slices.Clone(edge.CallerBindings)
	cloned.CalleeTemplateArgs = slices.Clone(edge.CalleeTemplateArgs)
	cloned.Witness = cloneInstantiationWitness(&edge.Witness)
	return cloned
}

func cloneDeferredCallableEdge(edge *DeferredCallableEdge) DeferredCallableEdge {
	cloned := *edge
	cloned.CallerBindings = slices.Clone(edge.CallerBindings)
	cloned.Args = slices.Clone(edge.Args)
	cloned.ExplicitTypeArgs = slices.Clone(edge.ExplicitTypeArgs)
	cloned.Requirement = cloneDeferredCallableRequirement(&edge.Requirement)
	cloned.Witness = cloneInstantiationWitness(&edge.Witness)
	return cloned
}

func cloneInstantiationWitness(witness *InstantiationWitness) InstantiationWitness {
	cloned := *witness
	cloned.Steps = slices.Clone(witness.Steps)
	return cloned
}
