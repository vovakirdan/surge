package sema

import (
	"sort"

	"surge/internal/symbols"
)

func reachableInstantiationGraph(
	graph *InstantiationGraph,
	callEdges map[symbols.SymbolID]map[symbols.SymbolID]struct{},
	seeds map[symbols.SymbolID]struct{},
) (InstantiationGraph, []symbols.SymbolID) {
	if graph == nil {
		return InstantiationGraph{}, sortedCallableSet(seeds)
	}
	roots := graph.Roots()
	edges := graph.Edges()
	genericTemplates := make(map[symbols.SymbolID]struct{}, len(roots)+len(edges)*2)
	rootsByCaller := make(map[symbols.SymbolID][]InstantiationRoot)
	edgesByCaller := make(map[symbols.SymbolID][]InstantiationEdge)
	for i := range roots {
		root := roots[i]
		genericTemplates[root.Template] = struct{}{}
		rootsByCaller[root.Witness.Caller] = append(rootsByCaller[root.Witness.Caller], root)
	}
	for i := range edges {
		edge := edges[i]
		genericTemplates[edge.Caller] = struct{}{}
		genericTemplates[edge.Callee] = struct{}{}
		edgesByCaller[edge.Caller] = append(edgesByCaller[edge.Caller], edge)
	}

	liveCallables := make(map[symbols.SymbolID]struct{}, len(seeds))
	liveTemplates := make(map[symbols.SymbolID]struct{})
	for seed := range seeds {
		if !seed.IsValid() {
			continue
		}
		if _, generic := genericTemplates[seed]; generic {
			liveTemplates[seed] = struct{}{}
		} else {
			liveCallables[seed] = struct{}{}
		}
	}

	changed := true
	for changed {
		changed = false
		for caller := range liveCallables {
			for callee := range callEdges[caller] {
				if _, generic := genericTemplates[callee]; generic {
					continue
				}
				if _, found := liveCallables[callee]; !found && callee.IsValid() {
					liveCallables[callee] = struct{}{}
					changed = true
				}
			}
			for i := range rootsByCaller[caller] {
				template := rootsByCaller[caller][i].Template
				if _, found := liveTemplates[template]; !found {
					liveTemplates[template] = struct{}{}
					changed = true
				}
			}
		}
		for caller := range liveTemplates {
			for callee := range callEdges[caller] {
				if _, generic := genericTemplates[callee]; generic {
					continue
				}
				if _, found := liveCallables[callee]; !found && callee.IsValid() {
					liveCallables[callee] = struct{}{}
					changed = true
				}
			}
			for i := range edgesByCaller[caller] {
				template := edgesByCaller[caller][i].Callee
				if _, found := liveTemplates[template]; !found {
					liveTemplates[template] = struct{}{}
					changed = true
				}
			}
		}
	}

	var filtered InstantiationGraph
	for i := range roots {
		root := &roots[i]
		if _, live := liveCallables[root.Witness.Caller]; live {
			filtered.recordRoot(root)
		}
	}
	for i := range edges {
		filtered.recordEdge(&edges[i])
	}
	return filtered, sortedCallableSet(liveCallables)
}

func sortedCallableSet(set map[symbols.SymbolID]struct{}) []symbols.SymbolID {
	if len(set) == 0 {
		return nil
	}
	out := make([]symbols.SymbolID, 0, len(set))
	for id := range set {
		if id.IsValid() {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
