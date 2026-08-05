package sema

import (
	"fmt"
	"slices"
	"sort"
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

func buildInstantiationClosure(graph *InstantiationGraph, identity InstantiationIdentity, limits instantiationClosureLimits) (InstantiationClosure, error) {
	if identity.Types.Types == nil {
		return InstantiationClosure{}, fmt.Errorf("instantiation closure: missing type interner")
	}
	if limits.maxDepth <= 0 {
		limits.maxDepth = 64
	}
	if limits.maxInstances <= 0 {
		limits.maxInstances = DefaultInstantiationClosureInstanceLimit
	}

	pending, uses, err := closureRoots(graph, identity)
	if err != nil {
		return InstantiationClosure{}, err
	}
	edges, err := closureEdges(graph, identity)
	if err != nil {
		return InstantiationClosure{}, err
	}
	discovered := make(map[InstanceKey]struct{}, len(pending))
	for i := range pending {
		root := &pending[i]
		if len(discovered) >= limits.maxInstances {
			return InstantiationClosure{}, &InstantiationBudgetError{Limit: limits.maxInstances, Instance: root.Key, Witness: root.Witness}
		}
		discovered[root.Key] = struct{}{}
	}
	instances := make([]InstantiationInstance, 0, len(pending))

	for len(pending) > 0 {
		sortClosureInstances(pending)
		current := pending[0]
		pending = pending[1:]
		instances = append(instances, current)

		currentEdges := edges[current.Template]
		for i := range currentEdges {
			edge := &currentEdges[i]
			if int(edge.CallerTemplateArity) != len(current.TemplateArgs) {
				return InstantiationClosure{}, fmt.Errorf("instantiation closure edge %d -> %d: caller instance has %d argument(s), want %d", edge.Caller, edge.Callee, len(current.TemplateArgs), edge.CallerTemplateArity)
			}
			subst, substErr := newInstantiationSubstitution(identity.Types.Types, edge.CallerBindings, current.TemplateArgs)
			if substErr != nil {
				return InstantiationClosure{}, fmt.Errorf("instantiation closure edge %d -> %d: %w", edge.Caller, edge.Callee, substErr)
			}
			args := make([]types.TypeID, len(edge.CalleeTemplateArgs))
			for i, arg := range edge.CalleeTemplateArgs {
				args[i], err = subst.typeID(arg)
				if err != nil {
					return InstantiationClosure{}, fmt.Errorf("instantiation closure edge %d -> %d: %w", edge.Caller, edge.Callee, err)
				}
				if types.ContainsGenericParam(identity.Types.Types, args[i]) {
					return InstantiationClosure{}, fmt.Errorf("instantiation closure edge %d -> %d left unresolved generic argument %d", edge.Caller, edge.Callee, i)
				}
			}
			key, keyErr := NewInstanceKey(identity, edge.Callee, args)
			if keyErr != nil {
				return InstantiationClosure{}, keyErr
			}
			witness := cloneInstantiationWitness(&current.Witness)
			witness.Steps = append(witness.Steps, InstantiationStep{
				Caller:    current.Key,
				Callee:    key,
				Site:      edge.Witness.Site,
				SourceKey: edge.Witness.SourceKey,
				Reason:    edge.Witness.Reason,
			})
			uses = append(uses, ConcreteInstantiationUse{
				Caller:             current.Key,
				CallerTemplate:     current.Template,
				CallerTemplateArgs: slices.Clone(current.TemplateArgs),
				Callee:             key,
				CalleeTemplate:     edge.Callee,
				Kind:               edge.Kind,
				TemplateArgs:       slices.Clone(args),
				Site:               edge.Witness.Site,
				SourceKey:          edge.Witness.SourceKey,
				Reason:             edge.Witness.Reason,
			})
			if _, exists := discovered[key]; exists {
				continue
			}
			depth := current.Depth + 1
			if depth > limits.maxDepth {
				return InstantiationClosure{}, &InstantiationExpansionError{Limit: limits.maxDepth, Instance: key, Witness: witness}
			}
			if len(discovered) >= limits.maxInstances {
				return InstantiationClosure{}, &InstantiationBudgetError{Limit: limits.maxInstances, Instance: key, Witness: witness}
			}
			discovered[key] = struct{}{}
			pending = append(pending, InstantiationInstance{
				Key:          key,
				Template:     edge.Callee,
				Kind:         edge.Kind,
				TemplateArgs: args,
				Witness:      witness,
				Depth:        depth,
			})
		}
	}

	sortClosureInstances(instances)
	uses = sortAndCompactConcreteUses(uses)
	return InstantiationClosure{Instances: instances, UseSites: uses}, nil
}

func closureRoots(graph *InstantiationGraph, identity InstantiationIdentity) ([]InstantiationInstance, []ConcreteInstantiationUse, error) {
	if graph == nil {
		return nil, nil, nil
	}
	roots := graph.Roots()
	instances := make([]InstantiationInstance, 0, len(roots))
	uses := make([]ConcreteInstantiationUse, 0, len(roots))
	byKey := make(map[InstanceKey]int, len(roots))
	for rootIndex := range roots {
		root := &roots[rootIndex]
		for i, arg := range root.TemplateArgs {
			if types.ContainsGenericParam(identity.Types.Types, arg) {
				return nil, nil, fmt.Errorf("instantiation root %d has unresolved generic argument %d", root.Template, i)
			}
		}
		key, err := NewInstanceKey(identity, root.Template, root.TemplateArgs)
		if err != nil {
			return nil, nil, err
		}
		witness, err := canonicalInstantiationWitness(&root.Witness, identity)
		if err != nil {
			return nil, nil, err
		}
		witness.Root = key
		uses = append(uses, ConcreteInstantiationUse{
			CallerTemplate: root.Witness.Caller,
			Callee:         key,
			CalleeTemplate: root.Template,
			Kind:           root.Kind,
			TemplateArgs:   slices.Clone(root.TemplateArgs),
			Site:           root.Witness.Site,
			SourceKey:      witness.SourceKey,
			Reason:         root.Witness.Reason,
		})
		candidate := InstantiationInstance{Key: key, Template: root.Template, Kind: root.Kind, TemplateArgs: slices.Clone(root.TemplateArgs), Witness: witness}
		if index, exists := byKey[key]; exists {
			if compareInstantiationWitness(&candidate.Witness, &instances[index].Witness) < 0 {
				instances[index] = candidate
			}
			continue
		}
		byKey[key] = len(instances)
		instances = append(instances, candidate)
	}
	sortClosureInstances(instances)
	return instances, sortAndCompactConcreteUses(uses), nil
}

func closureEdges(graph *InstantiationGraph, identity InstantiationIdentity) (map[symbols.SymbolID][]InstantiationEdge, error) {
	out := make(map[symbols.SymbolID][]InstantiationEdge)
	if graph == nil {
		return out, nil
	}
	graphEdges := graph.Edges()
	for i := range graphEdges {
		edge := &graphEdges[i]
		out[edge.Caller] = append(out[edge.Caller], *edge)
	}
	for caller := range out {
		edges := out[caller]
		type keyedEdge struct {
			edge        InstantiationEdge
			templateKey string
			argsKey     string
		}
		keyed := make([]keyedEdge, len(edges))
		for i := range edges {
			edge := &edges[i]
			if err := validateInstantiationBindings(edge); err != nil {
				return nil, fmt.Errorf("instantiation edge %d -> %d at %s (%s): %w", edge.Caller, edge.Callee, witnessSiteLabel(&edge.Witness), edge.Witness.Reason, err)
			}
			argsKey, err := canonicalEdgeArgsKey(identity.Types, edge)
			if err != nil {
				return nil, fmt.Errorf("instantiation edge %d -> %d at %s (%s): %w", edge.Caller, edge.Callee, witnessSiteLabel(&edge.Witness), edge.Witness.Reason, err)
			}
			templateKey, err := identity.ResolveTemplate(edge.Callee)
			if err != nil {
				return nil, fmt.Errorf("instantiation edge %d -> %d template: %w", edge.Caller, edge.Callee, err)
			}
			edge.Witness, err = canonicalInstantiationWitness(&edge.Witness, identity)
			if err != nil {
				return nil, err
			}
			keyed[i] = keyedEdge{edge: *edge, templateKey: templateKey, argsKey: argsKey}
		}
		sort.SliceStable(keyed, func(i, j int) bool {
			if keyed[i].templateKey != keyed[j].templateKey {
				return keyed[i].templateKey < keyed[j].templateKey
			}
			if keyed[i].edge.Kind != keyed[j].edge.Kind {
				return keyed[i].edge.Kind < keyed[j].edge.Kind
			}
			if keyed[i].argsKey != keyed[j].argsKey {
				return keyed[i].argsKey < keyed[j].argsKey
			}
			return compareInstantiationWitness(&keyed[i].edge.Witness, &keyed[j].edge.Witness) < 0
		})
		for i := range keyed {
			edges[i] = keyed[i].edge
		}
		out[caller] = edges
	}
	return out, nil
}

func validateInstantiationBindings(edge *InstantiationEdge) error {
	arity := int(edge.CallerTemplateArity)
	if len(edge.CallerBindings) != arity {
		return fmt.Errorf("caller binding environment has %d entries, want exactly %d; note: receiver parameters precede method parameters in the caller argument vector", len(edge.CallerBindings), arity)
	}
	seenArgs := make([]bool, arity)
	seenExact := make(map[types.TypeID]struct{}, arity)
	seenParams := make(map[instantiationParamRef]struct{}, arity)
	for _, binding := range edge.CallerBindings {
		if int(binding.ArgIndex) >= arity {
			return fmt.Errorf("binding owner %d parameter %d maps to caller argument %d outside arity %d", binding.Owner, binding.ParamIndex, binding.ArgIndex, arity)
		}
		if seenArgs[binding.ArgIndex] {
			return fmt.Errorf("caller argument %d has more than one parameter binding", binding.ArgIndex)
		}
		seenArgs[binding.ArgIndex] = true
		if binding.Param != types.NoTypeID {
			if _, duplicate := seenExact[binding.Param]; duplicate {
				return fmt.Errorf("type parameter type#%d is bound more than once", binding.Param)
			}
			seenExact[binding.Param] = struct{}{}
		}
		ref := instantiationParamRef{owner: uint32(binding.Owner), index: binding.ParamIndex}
		if _, duplicate := seenParams[ref]; duplicate {
			return fmt.Errorf("type parameter owner %d index %d is bound more than once", binding.Owner, binding.ParamIndex)
		}
		seenParams[ref] = struct{}{}
	}
	return nil
}

func canonicalEdgeArgsKey(typeKeys types.CanonicalKeyContext, edge *InstantiationEdge) (string, error) {
	exact := make(map[types.TypeID]uint32, len(edge.CallerBindings))
	bindings := make(map[instantiationParamRef]uint32, len(edge.CallerBindings))
	for _, binding := range edge.CallerBindings {
		if binding.Param != types.NoTypeID {
			if previous, found := exact[binding.Param]; found && previous != binding.ArgIndex {
				return "", fmt.Errorf("type parameter type#%d has conflicting caller arguments %d and %d", binding.Param, previous, binding.ArgIndex)
			}
			exact[binding.Param] = binding.ArgIndex
		}
		ref := instantiationParamRef{owner: uint32(binding.Owner), index: binding.ParamIndex}
		if previous, found := bindings[ref]; found && previous != binding.ArgIndex {
			return "", fmt.Errorf("type parameter owner %d index %d has conflicting caller arguments %d and %d", binding.Owner, binding.ParamIndex, previous, binding.ArgIndex)
		}
		bindings[ref] = binding.ArgIndex
	}
	typeKeys.ResolveTypeParam = func(id types.TypeID, info types.TypeParamInfo) (string, error) {
		argIndex, ok := exact[id]
		if !ok {
			argIndex, ok = bindings[instantiationParamRef{owner: info.Owner, index: info.Index}]
		}
		if !ok {
			return "", fmt.Errorf("type parameter owner %d index %d is not bound by the caller instance; note: callee arguments inside a generic caller may reference only declared receiver or method parameters", info.Owner, info.Index)
		}
		return fmt.Sprintf("caller-arg/%d", argIndex), nil
	}
	return typeKeys.TypeArgsKey(edge.CalleeTemplateArgs)
}

func sortClosureInstances(instances []InstantiationInstance) {
	sort.SliceStable(instances, func(i, j int) bool {
		return compareInstanceKey(instances[i].Key, instances[j].Key) < 0
	})
}

func compareInstanceKey(left, right InstanceKey) int {
	if left.TemplateKey != right.TemplateKey {
		return strings.Compare(left.TemplateKey, right.TemplateKey)
	}
	return strings.Compare(left.ArgsKey, right.ArgsKey)
}

func canonicalInstantiationWitness(original *InstantiationWitness, identity InstantiationIdentity) (InstantiationWitness, error) {
	witness := cloneInstantiationWitness(original)
	if witness.Caller.IsValid() && witness.CallerKey == "" {
		if identity.ResolveTemplate == nil {
			return InstantiationWitness{}, fmt.Errorf("instantiation witness: missing canonical template resolver")
		}
		callerKey, err := identity.ResolveTemplate(witness.Caller)
		if err != nil {
			return InstantiationWitness{}, fmt.Errorf("instantiation witness caller %d: %w", witness.Caller, err)
		}
		witness.CallerKey = callerKey
	}
	if witness.SourceKey == "" && identity.ResolveSource != nil {
		sourceKey, err := identity.ResolveSource(witness.Site.File)
		if err != nil {
			return InstantiationWitness{}, fmt.Errorf("instantiation witness source %d: %w", witness.Site.File, err)
		}
		witness.SourceKey = sourceKey
	}
	return witness, nil
}

func witnessSiteLabel(witness *InstantiationWitness) string {
	if witness.SourceKey == "" {
		return witness.Site.String()
	}
	return fmt.Sprintf("%s:%d-%d", witness.SourceKey, witness.Site.Start, witness.Site.End)
}

func stepSiteLabel(step *InstantiationStep) string {
	if step.SourceKey != "" {
		return fmt.Sprintf("%s:%d-%d", step.SourceKey, step.Site.Start, step.Site.End)
	}
	return step.Site.String()
}

func cloneInstantiationInstance(instance *InstantiationInstance) InstantiationInstance {
	cloned := *instance
	cloned.TemplateArgs = slices.Clone(instance.TemplateArgs)
	cloned.Witness = cloneInstantiationWitness(&instance.Witness)
	return cloned
}

func sortAndCompactConcreteUses(uses []ConcreteInstantiationUse) []ConcreteInstantiationUse {
	sort.SliceStable(uses, func(i, j int) bool {
		left, right := uses[i], uses[j]
		if cmp := compareInstanceKey(left.Caller, right.Caller); cmp != 0 {
			return cmp < 0
		}
		if cmp := compareInstanceKey(left.Callee, right.Callee); cmp != 0 {
			return cmp < 0
		}
		if left.SourceKey != right.SourceKey {
			return left.SourceKey < right.SourceKey
		}
		if cmp := compareSpanOffsets(left.Site, right.Site); cmp != 0 {
			return cmp < 0
		}
		return left.Reason < right.Reason
	})
	out := uses[:0]
	for i := range uses {
		use := &uses[i]
		if len(out) > 0 && concreteUsesEqual(&out[len(out)-1], use) {
			continue
		}
		cloned := *use
		cloned.TemplateArgs = slices.Clone(use.TemplateArgs)
		cloned.CallerTemplateArgs = slices.Clone(use.CallerTemplateArgs)
		out = append(out, cloned)
	}
	return out
}

func concreteUsesEqual(left, right *ConcreteInstantiationUse) bool {
	return left.Caller == right.Caller &&
		left.Callee == right.Callee &&
		left.Kind == right.Kind &&
		compareSpanOffsets(left.Site, right.Site) == 0 &&
		left.SourceKey == right.SourceKey &&
		left.Reason == right.Reason &&
		left.Caller.ArgsKey == right.Caller.ArgsKey &&
		left.Callee.ArgsKey == right.Callee.ArgsKey
}
