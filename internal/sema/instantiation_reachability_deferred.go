package sema

import (
	"fmt"
	"slices"
	"sort"

	"surge/internal/symbols"
	"surge/internal/types"
)

type reachableClosureBuilder struct {
	graph          *InstantiationGraph
	callEdges      map[symbols.SymbolID]map[symbols.SymbolID]struct{}
	candidates     []CallableCandidate
	clones         *cloneCanonicalSelector
	templateParams map[symbols.SymbolID][]types.TypeID
	identity       InstantiationIdentity
	limits         instantiationClosureLimits

	rootsByCaller    map[symbols.SymbolID][]InstantiationRoot
	edgesByCaller    map[symbols.SymbolID][]InstantiationEdge
	deferredByCaller map[symbols.SymbolID][]DeferredCallableEdge
	generic          map[symbols.SymbolID]struct{}

	liveCallables map[symbols.SymbolID]struct{}
	pendingCalls  []symbols.SymbolID
	discovered    map[InstanceKey]struct{}
	pending       []InstantiationInstance
	instances     []InstantiationInstance
	uses          []ConcreteInstantiationUse
	resolved      []ResolvedDeferredCall
}

func buildReachableInstantiationClosureWithDeferred(
	graph *InstantiationGraph,
	callEdges map[symbols.SymbolID]map[symbols.SymbolID]struct{},
	seeds map[symbols.SymbolID]struct{},
	candidates []CallableCandidate,
	templateParams map[symbols.SymbolID][]types.TypeID,
	identity InstantiationIdentity,
	limits instantiationClosureLimits,
) (InstantiationClosure, error) {
	b := &reachableClosureBuilder{
		graph: graph, callEdges: callEdges, candidates: candidates, templateParams: templateParams,
		clones:   newCloneCanonicalSelector(candidates, identity.Types.Types),
		identity: identity, limits: limits,
		rootsByCaller:    make(map[symbols.SymbolID][]InstantiationRoot),
		edgesByCaller:    make(map[symbols.SymbolID][]InstantiationEdge),
		deferredByCaller: make(map[symbols.SymbolID][]DeferredCallableEdge),
		generic:          make(map[symbols.SymbolID]struct{}), liveCallables: make(map[symbols.SymbolID]struct{}),
		discovered: make(map[InstanceKey]struct{}),
	}
	if b.limits.maxDepth <= 0 {
		b.limits.maxDepth = 64
	}
	if b.limits.maxInstances <= 0 {
		b.limits.maxInstances = DefaultInstantiationClosureInstanceLimit
	}
	if err := b.index(); err != nil {
		return InstantiationClosure{}, err
	}
	for seed := range seeds {
		if _, isGeneric := b.generic[seed]; !isGeneric {
			b.enqueueCallable(seed)
		}
	}
	for len(b.pendingCalls) > 0 || len(b.pending) > 0 {
		if len(b.pendingCalls) > 0 {
			callable, err := b.popCallable()
			if err != nil {
				return InstantiationClosure{}, err
			}
			if err := b.processCallable(callable); err != nil {
				return InstantiationClosure{}, err
			}
			continue
		}
		sortClosureInstances(b.pending)
		current := b.pending[0]
		b.pending = b.pending[1:]
		b.instances = append(b.instances, current)
		if err := b.processInstance(&current); err != nil {
			return InstantiationClosure{}, err
		}
	}

	live, err := b.sortedLiveCallables()
	if err != nil {
		return InstantiationClosure{}, err
	}
	sortClosureInstances(b.instances)
	b.uses = sortAndCompactConcreteUses(b.uses)
	sortResolvedDeferredCalls(b.resolved)
	resolved, err := compactResolvedDeferredCalls(b.resolved)
	if err != nil {
		return InstantiationClosure{}, err
	}
	return InstantiationClosure{
		LiveCallables: live, Instances: b.instances, UseSites: b.uses, ResolvedDeferredCalls: resolved,
	}, nil
}

func (b *reachableClosureBuilder) index() error {
	if b.identity.Types.Types == nil {
		return fmt.Errorf("instantiation closure: missing type interner")
	}
	for template, params := range b.templateParams {
		if len(params) > 0 {
			b.generic[template] = struct{}{}
		}
	}
	if b.graph == nil {
		return nil
	}
	for _, root := range b.graph.Roots() {
		b.generic[root.Template] = struct{}{}
		b.rootsByCaller[root.Witness.Caller] = append(b.rootsByCaller[root.Witness.Caller], root)
	}
	edges, err := closureEdges(b.graph, b.identity)
	if err != nil {
		return err
	}
	for caller, callerEdges := range edges {
		b.generic[caller] = struct{}{}
		b.edgesByCaller[caller] = callerEdges
		for i := range callerEdges {
			b.generic[callerEdges[i].Callee] = struct{}{}
		}
	}
	for _, edge := range b.graph.DeferredCallables() {
		if err := validateDeferredCallableEdge(&edge); err != nil {
			return err
		}
		edge.Witness, err = canonicalInstantiationWitness(&edge.Witness, b.identity)
		if err != nil {
			return err
		}
		b.generic[edge.Caller] = struct{}{}
		b.deferredByCaller[edge.Caller] = append(b.deferredByCaller[edge.Caller], edge)
	}
	for caller := range b.deferredByCaller {
		sort.SliceStable(b.deferredByCaller[caller], func(i, j int) bool {
			return b.deferredByCaller[caller][i].UseID < b.deferredByCaller[caller][j].UseID
		})
	}
	return nil
}

func (b *reachableClosureBuilder) processCallable(caller symbols.SymbolID) error {
	for _, callee := range sortedCallees(b.callEdges[caller]) {
		if _, generic := b.generic[callee]; !generic {
			b.enqueueCallable(callee)
		}
	}
	roots := slices.Clone(b.rootsByCaller[caller])
	sort.SliceStable(roots, func(i, j int) bool { return compareInstantiationWitness(&roots[i].Witness, &roots[j].Witness) < 0 })
	for i := range roots {
		if err := b.addRoot(&roots[i]); err != nil {
			return err
		}
	}
	return nil
}

func (b *reachableClosureBuilder) processInstance(current *InstantiationInstance) error {
	for _, callee := range sortedCallees(b.callEdges[current.Template]) {
		if _, generic := b.generic[callee]; !generic {
			b.enqueueCallable(callee)
		}
	}
	for i := range b.edgesByCaller[current.Template] {
		if err := b.expandEdge(current, &b.edgesByCaller[current.Template][i]); err != nil {
			return err
		}
	}
	for i := range b.deferredByCaller[current.Template] {
		if err := b.resolveDeferred(current, &b.deferredByCaller[current.Template][i]); err != nil {
			return err
		}
	}
	return nil
}

func (b *reachableClosureBuilder) enqueueCallable(id symbols.SymbolID) {
	if !id.IsValid() {
		return
	}
	if _, exists := b.liveCallables[id]; exists {
		return
	}
	b.liveCallables[id] = struct{}{}
	b.pendingCalls = append(b.pendingCalls, id)
}

func (b *reachableClosureBuilder) popCallable() (symbols.SymbolID, error) {
	type keyed struct {
		id  symbols.SymbolID
		key string
	}
	items := make([]keyed, len(b.pendingCalls))
	for i, id := range b.pendingCalls {
		key, err := b.identity.ResolveTemplate(id)
		if err != nil {
			return symbols.NoSymbolID, fmt.Errorf("reachable callable %d: %w", id, err)
		}
		items[i] = keyed{id: id, key: key}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].key < items[j].key })
	selected := items[0].id
	for i, id := range b.pendingCalls {
		if id == selected {
			b.pendingCalls = append(b.pendingCalls[:i], b.pendingCalls[i+1:]...)
			break
		}
	}
	return selected, nil
}

func (b *reachableClosureBuilder) sortedLiveCallables() ([]symbols.SymbolID, error) {
	out := make([]symbols.SymbolID, 0, len(b.liveCallables))
	for id := range b.liveCallables {
		out = append(out, id)
	}
	var sortErr error
	sort.SliceStable(out, func(i, j int) bool {
		left, err := b.identity.ResolveTemplate(out[i])
		if err != nil {
			sortErr = err
			return out[i] < out[j]
		}
		right, err := b.identity.ResolveTemplate(out[j])
		if err != nil {
			sortErr = err
			return out[i] < out[j]
		}
		return left < right
	})
	return out, sortErr
}
