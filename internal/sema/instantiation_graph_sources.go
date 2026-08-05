package sema

import (
	"fmt"
	"sort"

	"surge/internal/source"
)

// CanonicalizeInstantiationGraphSources stamps source-path identities before
// file/module graphs are merged. This keeps witness selection independent of
// FileID registration order.
func CanonicalizeInstantiationGraphSources(result *Result, resolve func(source.FileID) (string, error)) error {
	if result == nil {
		return fmt.Errorf("instantiation graph sources: missing semantic result")
	}
	if resolve == nil {
		return fmt.Errorf("instantiation graph sources: missing source resolver")
	}
	var rebuilt InstantiationGraph
	roots := result.InstantiationGraph.Roots()
	for i := range roots {
		root := &roots[i]
		key, err := resolve(root.Witness.Site.File)
		if err != nil {
			return fmt.Errorf("instantiation root %d source: %w", root.Template, err)
		}
		if key == "" {
			return fmt.Errorf("instantiation root %d source: empty canonical identity", root.Template)
		}
		root.Witness.SourceKey = key
		rebuilt.recordRoot(root)
	}
	edges := result.InstantiationGraph.Edges()
	for i := range edges {
		edge := &edges[i]
		key, err := resolve(edge.Witness.Site.File)
		if err != nil {
			return fmt.Errorf("instantiation edge %d -> %d source: %w", edge.Caller, edge.Callee, err)
		}
		if key == "" {
			return fmt.Errorf("instantiation edge %d -> %d source: empty canonical identity", edge.Caller, edge.Callee)
		}
		edge.Witness.SourceKey = key
		rebuilt.recordEdge(edge)
	}
	deferred := result.InstantiationGraph.DeferredCallables()
	useIDRemap := make(map[DeferredUseID]DeferredUseID, len(deferred))
	for i := range deferred {
		edge := &deferred[i]
		key, err := resolve(edge.Witness.Site.File)
		if err != nil {
			return fmt.Errorf("deferred callable edge %d %s source: %w", edge.Caller, edge.Method, err)
		}
		if key == "" {
			return fmt.Errorf("deferred callable edge %d %s source: empty canonical identity", edge.Caller, edge.Method)
		}
		edge.Witness.SourceKey = key
		canonicalUseID := canonicalDeferredUseID(key, edge.Kind, edge.Witness.Site, edge.UseOrdinal)
		useIDRemap[edge.UseID] = canonicalUseID
		edge.UseID = canonicalUseID
		rebuilt.recordDeferredCallable(edge)
	}
	for ref, id := range result.DeferredCallableUses {
		if canonical, ok := useIDRemap[id]; ok {
			result.DeferredCallableUses[ref] = canonical
		}
	}
	for i := range result.CallableCandidates {
		candidate := &result.CallableCandidates[i]
		if candidate.Builtin {
			candidate.SourceKey = "builtin"
		} else {
			key, err := resolve(candidate.Source.File)
			if err != nil {
				return fmt.Errorf("callable candidate %s source: %w", candidate.Name, err)
			}
			if key == "" {
				return fmt.Errorf("callable candidate %s source: empty canonical identity", candidate.Name)
			}
			candidate.SourceKey = key
		}
		candidate.BodyKey = canonicalCallableBodyKey(candidate)
	}
	sort.SliceStable(result.CallableCandidates, func(i, j int) bool {
		return result.CallableCandidates[i].BodyKey < result.CallableCandidates[j].BodyKey
	})
	if err := canonicalizeFinalizationRequestSources(result, resolve); err != nil {
		return err
	}
	result.InstantiationGraph = rebuilt
	result.InstantiationClosure = nil
	result.rebuildFunctionInstantiations()
	return nil
}
