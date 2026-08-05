package sema

import (
	"fmt"
	"slices"
	"sort"

	"surge/internal/symbols"
	"surge/internal/types"
)

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
