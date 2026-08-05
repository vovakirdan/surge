package sema

import (
	"slices"
	"sort"
	"strings"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// MergeInstantiationGraphs merges one file/module graph into the canonical
// result and remaps every symbol-bearing field. Type-param metadata itself is
// remapped by the driver on the shared interner before closure finalization.
func MergeInstantiationGraphs(dst, src *Result, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if dst == nil || src == nil {
		return
	}
	roots := src.InstantiationGraph.Roots()
	for i := range roots {
		root := &roots[i]
		root.Template = remapInstantiationSymbol(root.Template, mapping)
		root.Witness.Caller = remapInstantiationSymbol(root.Witness.Caller, mapping)
		dst.InstantiationGraph.recordRoot(root)
	}
	edges := src.InstantiationGraph.Edges()
	for i := range edges {
		edge := &edges[i]
		edge.Caller = remapInstantiationSymbol(edge.Caller, mapping)
		edge.Callee = remapInstantiationSymbol(edge.Callee, mapping)
		edge.Witness.Caller = remapInstantiationSymbol(edge.Witness.Caller, mapping)
		for i := range edge.CallerBindings {
			edge.CallerBindings[i].Owner = remapInstantiationSymbol(edge.CallerBindings[i].Owner, mapping)
		}
		sort.Slice(edge.CallerBindings, func(i, j int) bool {
			return compareInstantiationBinding(edge.CallerBindings[i], edge.CallerBindings[j]) < 0
		})
		edge.CallerBindings = slices.Compact(edge.CallerBindings)
		dst.InstantiationGraph.recordEdge(edge)
	}
	deferred := src.InstantiationGraph.DeferredCallables()
	for i := range deferred {
		edge := &deferred[i]
		edge.Caller = remapInstantiationSymbol(edge.Caller, mapping)
		edge.Witness.Caller = remapInstantiationSymbol(edge.Witness.Caller, mapping)
		for contractIndex := range edge.Requirement.Contracts {
			edge.Requirement.Contracts[contractIndex] = remapInstantiationSymbol(edge.Requirement.Contracts[contractIndex], mapping)
		}
		sort.Slice(edge.Requirement.Contracts, func(i, j int) bool {
			return edge.Requirement.Contracts[i] < edge.Requirement.Contracts[j]
		})
		edge.Requirement.Contracts = slices.Compact(edge.Requirement.Contracts)
		for i := range edge.CallerBindings {
			edge.CallerBindings[i].Owner = remapInstantiationSymbol(edge.CallerBindings[i].Owner, mapping)
		}
		sort.Slice(edge.CallerBindings, func(i, j int) bool {
			return compareInstantiationBinding(edge.CallerBindings[i], edge.CallerBindings[j]) < 0
		})
		edge.CallerBindings = slices.Compact(edge.CallerBindings)
		dst.InstantiationGraph.recordDeferredCallable(edge)
	}
	for i := range src.CallableCandidates {
		candidate := cloneCallableCandidate(&src.CallableCandidates[i])
		candidate.Symbol = remapInstantiationSymbol(candidate.Symbol, mapping)
		duplicate := false
		for j := range dst.CallableCandidates {
			existing := &dst.CallableCandidates[j]
			if existing.BodyKey == candidate.BodyKey && existing.Symbol == candidate.Symbol {
				duplicate = true
				break
			}
		}
		if !duplicate {
			dst.CallableCandidates = append(dst.CallableCandidates, candidate)
		}
	}
	sort.SliceStable(dst.CallableCandidates, func(i, j int) bool {
		return dst.CallableCandidates[i].BodyKey < dst.CallableCandidates[j].BodyKey
	})
	mergeFinalizationRequests(dst, src, mapping)
	dst.InstantiationClosure = nil
	dst.rebuildFunctionInstantiations()
}

func (r *Result) rebuildFunctionInstantiations() {
	if r == nil {
		return
	}
	type compatibilityEntry struct {
		template symbols.SymbolID
		args     []types.TypeID
		witness  InstantiationWitness
	}
	entries := make([]compatibilityEntry, 0, len(r.InstantiationGraph.roots))
	finalized := r.InstantiationClosure != nil
	if r.InstantiationClosure != nil {
		entries = make([]compatibilityEntry, 0, len(r.InstantiationClosure.Instances))
		for i := range r.InstantiationClosure.Instances {
			instance := &r.InstantiationClosure.Instances[i]
			entries = append(entries, compatibilityEntry{template: instance.Template, args: instance.TemplateArgs, witness: instance.Witness})
		}
	} else {
		// Before the post-merge closure exists, expose concrete roots only. Raw
		// edge arguments are expressed in a generic caller vocabulary and must
		// never masquerade as concrete compatibility instances.
		for i := range r.InstantiationGraph.roots {
			root := &r.InstantiationGraph.roots[i]
			entries = append(entries, compatibilityEntry{template: root.Template, args: root.TemplateArgs, witness: root.Witness})
		}
	}
	if !finalized {
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].template != entries[j].template {
				return entries[i].template < entries[j].template
			}
			if cmp := slices.Compare(entries[i].args, entries[j].args); cmp != 0 {
				return cmp < 0
			}
			return compareInstantiationWitness(&entries[i].witness, &entries[j].witness) < 0
		})
	}
	r.FunctionInstantiations = make(map[symbols.SymbolID][][]types.TypeID)
	r.FunctionInstantiationSites = make(map[symbols.SymbolID][]source.Span)
	for i := range entries {
		entry := &entries[i]
		instances := r.FunctionInstantiations[entry.template]
		if len(instances) > 0 && slices.Equal(instances[len(instances)-1], entry.args) {
			continue
		}
		r.FunctionInstantiations[entry.template] = append(instances, slices.Clone(entry.args))
		r.FunctionInstantiationSites[entry.template] = append(r.FunctionInstantiationSites[entry.template], entry.witness.Site)
	}
}

func remapInstantiationSymbol(id symbols.SymbolID, mapping map[symbols.SymbolID]symbols.SymbolID) symbols.SymbolID {
	if mapped, ok := mapping[id]; ok && mapped.IsValid() {
		return mapped
	}
	return id
}

func compareInstantiationBinding(left, right InstantiationParamBinding) int {
	if left.ArgIndex != right.ArgIndex {
		if left.ArgIndex < right.ArgIndex {
			return -1
		}
		return 1
	}
	if left.Owner != right.Owner {
		if left.Owner < right.Owner {
			return -1
		}
		return 1
	}
	if left.Param != right.Param {
		if left.Param < right.Param {
			return -1
		}
		return 1
	}
	if left.ParamIndex < right.ParamIndex {
		return -1
	}
	if left.ParamIndex > right.ParamIndex {
		return 1
	}
	return 0
}

func compareInstantiationWitness(left, right *InstantiationWitness) int {
	if cmp := compareWitnessSite(left, right); cmp != 0 {
		return cmp
	}
	if left.CallerKey != right.CallerKey {
		return strings.Compare(left.CallerKey, right.CallerKey)
	}
	if left.CallerKey == "" && left.Caller != right.Caller {
		if left.Caller < right.Caller {
			return -1
		}
		return 1
	}
	if left.Reason < right.Reason {
		return -1
	}
	if left.Reason > right.Reason {
		return 1
	}
	return 0
}

func compareWitnessSite(left, right *InstantiationWitness) int {
	if left.SourceKey != right.SourceKey {
		return strings.Compare(left.SourceKey, right.SourceKey)
	}
	if left.SourceKey != "" {
		return compareSpanOffsets(left.Site, right.Site)
	}
	return compareSpan(left.Site, right.Site)
}

func compareSpanOffsets(left, right source.Span) int {
	if left.Start != right.Start {
		if left.Start < right.Start {
			return -1
		}
		return 1
	}
	if left.End < right.End {
		return -1
	}
	if left.End > right.End {
		return 1
	}
	return 0
}

func compareSpan(left, right source.Span) int {
	if left.File != right.File {
		if left.File < right.File {
			return -1
		}
		return 1
	}
	if left.Start != right.Start {
		if left.Start < right.Start {
			return -1
		}
		return 1
	}
	if left.End < right.End {
		return -1
	}
	if left.End > right.End {
		return 1
	}
	return 0
}
