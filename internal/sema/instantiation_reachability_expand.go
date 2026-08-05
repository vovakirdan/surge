package sema

import (
	"fmt"
	"slices"
	"sort"

	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *reachableClosureBuilder) addRoot(root *InstantiationRoot) error {
	for i, arg := range root.TemplateArgs {
		if types.ContainsGenericParam(b.identity.Types.Types, arg) {
			return fmt.Errorf("instantiation root %d has unresolved generic argument %d", root.Template, i)
		}
	}
	key, err := NewInstanceKey(b.identity, root.Template, root.TemplateArgs)
	if err != nil {
		return err
	}
	witness, err := canonicalInstantiationWitness(&root.Witness, b.identity)
	if err != nil {
		return err
	}
	witness.Root = key
	b.uses = append(b.uses, ConcreteInstantiationUse{
		CallerTemplate: root.Witness.Caller, Callee: key, CalleeTemplate: root.Template,
		Kind: root.Kind, TemplateArgs: slices.Clone(root.TemplateArgs), Site: root.Witness.Site,
		SourceKey: witness.SourceKey, Reason: root.Witness.Reason,
	})
	return b.addInstance(InstantiationInstance{
		Key: key, Template: root.Template, Kind: root.Kind, TemplateArgs: slices.Clone(root.TemplateArgs), Witness: witness,
	})
}

func (b *reachableClosureBuilder) expandEdge(current *InstantiationInstance, edge *InstantiationEdge) error {
	if int(edge.CallerTemplateArity) != len(current.TemplateArgs) {
		return fmt.Errorf("instantiation closure edge %d -> %d: caller instance has %d argument(s), want %d", edge.Caller, edge.Callee, len(current.TemplateArgs), edge.CallerTemplateArity)
	}
	subst, err := newInstantiationSubstitution(b.identity.Types.Types, edge.CallerBindings, current.TemplateArgs)
	if err != nil {
		return fmt.Errorf("instantiation closure edge %d -> %d: %w", edge.Caller, edge.Callee, err)
	}
	args, err := substituteConcreteTypes(subst, edge.CalleeTemplateArgs, b.identity.Types.Types)
	if err != nil {
		return fmt.Errorf("instantiation closure edge %d -> %d: %w", edge.Caller, edge.Callee, err)
	}
	key, err := NewInstanceKey(b.identity, edge.Callee, args)
	if err != nil {
		return err
	}
	witness := cloneInstantiationWitness(&current.Witness)
	witness.Steps = append(witness.Steps, InstantiationStep{
		Caller: current.Key, Callee: key, Site: edge.Witness.Site, SourceKey: edge.Witness.SourceKey, Reason: edge.Witness.Reason,
	})
	b.uses = append(b.uses, ConcreteInstantiationUse{
		Caller: current.Key, CallerTemplate: current.Template, CallerTemplateArgs: slices.Clone(current.TemplateArgs),
		Callee: key, CalleeTemplate: edge.Callee, Kind: edge.Kind, TemplateArgs: slices.Clone(args),
		Site: edge.Witness.Site, SourceKey: edge.Witness.SourceKey, Reason: edge.Witness.Reason,
	})
	return b.addInstance(InstantiationInstance{
		Key: key, Template: edge.Callee, Kind: edge.Kind, TemplateArgs: args, Witness: witness, Depth: current.Depth + 1,
	})
}

func (b *reachableClosureBuilder) resolveDeferred(current *InstantiationInstance, edge *DeferredCallableEdge) error {
	if int(edge.CallerTemplateArity) != len(current.TemplateArgs) {
		return fmt.Errorf("deferred callable %s: caller instance has %d argument(s), want %d", edge.UseID, len(current.TemplateArgs), edge.CallerTemplateArity)
	}
	subst, err := newInstantiationSubstitution(b.identity.Types.Types, edge.CallerBindings, current.TemplateArgs)
	if err != nil {
		return fmt.Errorf("deferred callable %s: %w", edge.UseID, err)
	}
	receiver, err := substituteConcreteType(subst, edge.Receiver, b.identity.Types.Types)
	if err != nil {
		return fmt.Errorf("deferred callable %s receiver: %w", edge.UseID, err)
	}
	args, err := substituteConcreteTypes(subst, edge.Args, b.identity.Types.Types)
	if err != nil {
		return fmt.Errorf("deferred callable %s arguments: %w", edge.UseID, err)
	}
	explicit, err := substituteConcreteTypes(subst, edge.ExplicitTypeArgs, b.identity.Types.Types)
	if err != nil {
		return fmt.Errorf("deferred callable %s explicit arguments: %w", edge.UseID, err)
	}
	result, err := substituteConcreteType(subst, edge.ExpectedResult, b.identity.Types.Types)
	if err != nil {
		return fmt.Errorf("deferred callable %s result: %w", edge.UseID, err)
	}
	requirement := cloneDeferredCallableRequirement(edge.Requirement)
	requirement.Params, err = substituteConcreteTypes(subst, requirement.Params, b.identity.Types.Types)
	if err != nil {
		return fmt.Errorf("deferred callable %s contract parameters: %w", edge.UseID, err)
	}
	if requirement.Result != types.NoTypeID {
		requirement.Result, err = substituteConcreteType(subst, requirement.Result, b.identity.Types.Types)
		if err != nil {
			return fmt.Errorf("deferred callable %s contract result: %w", edge.UseID, err)
		}
	}
	request := DeferredCallableRequest{
		Kind: edge.Kind, Receiver: receiver, Method: edge.Method, Args: args, ExplicitTypeArgs: explicit,
		ExpectedResult: result, StaticReceiver: edge.StaticReceiver, AccessModule: edge.AccessModule,
		SourceKey: edge.Witness.SourceKey, Requirement: requirement,
	}
	resolution, err := resolveDeferredCallable(edge.UseID, request, b.candidates, b.identity.Types.Types)
	if err != nil {
		return err
	}
	b.resolved = append(b.resolved, ResolvedDeferredCall{
		UseID: edge.UseID, Caller: current.Key, CallerTemplate: current.Template,
		CallerTemplateArgs: slices.Clone(current.TemplateArgs), Kind: edge.Kind, Outcome: resolution.Outcome,
		Callee: resolution.Callee, CalleeKey: resolution.CalleeKey,
		CalleeTemplateArgs: slices.Clone(resolution.TemplateArgs), CalleeParamTypes: slices.Clone(resolution.ParamTypes),
		CalleeResultType: resolution.ResultType, Receiver: receiver, Args: slices.Clone(args),
		ExpectedResult: result, StaticReceiver: edge.StaticReceiver, Site: edge.Witness.Site,
		SourceKey: edge.Witness.SourceKey, Reason: edge.Witness.Reason,
	})
	if resolution.Outcome == DeferredCallableBuiltinCopy {
		return nil
	}
	if !resolution.Callee.IsValid() {
		return fmt.Errorf("deferred callable %s resolved without a callable symbol", edge.UseID)
	}
	if len(resolution.TemplateArgs) == 0 {
		if _, generic := b.generic[resolution.Callee]; generic {
			return fmt.Errorf("deferred callable %s selected generic %s without concrete template arguments", edge.UseID, resolution.CalleeKey)
		}
		b.enqueueCallable(resolution.Callee)
		return nil
	}
	key, err := NewInstanceKey(b.identity, resolution.Callee, resolution.TemplateArgs)
	if err != nil {
		return err
	}
	witness := cloneInstantiationWitness(&current.Witness)
	witness.Steps = append(witness.Steps, InstantiationStep{
		Caller: current.Key, Callee: key, Site: edge.Witness.Site, SourceKey: edge.Witness.SourceKey,
		Reason: edge.Witness.Reason + " -> " + resolution.CalleeKey,
	})
	b.uses = append(b.uses, ConcreteInstantiationUse{
		Caller: current.Key, CallerTemplate: current.Template, CallerTemplateArgs: slices.Clone(current.TemplateArgs),
		Callee: key, CalleeTemplate: resolution.Callee, Kind: InstantiationFunction,
		TemplateArgs: slices.Clone(resolution.TemplateArgs), Site: edge.Witness.Site,
		SourceKey: edge.Witness.SourceKey, Reason: edge.Witness.Reason,
	})
	return b.addInstance(InstantiationInstance{
		Key: key, Template: resolution.Callee, Kind: InstantiationFunction,
		TemplateArgs: slices.Clone(resolution.TemplateArgs), Witness: witness, Depth: current.Depth + 1,
	})
}

func (b *reachableClosureBuilder) addInstance(instance InstantiationInstance) error {
	if _, exists := b.discovered[instance.Key]; exists {
		b.preferInstanceWitness(&instance)
		return nil
	}
	if instance.Depth > b.limits.maxDepth {
		return &InstantiationExpansionError{Limit: b.limits.maxDepth, Instance: instance.Key, Witness: instance.Witness}
	}
	if len(b.discovered) >= b.limits.maxInstances {
		return &InstantiationBudgetError{Limit: b.limits.maxInstances, Instance: instance.Key, Witness: instance.Witness}
	}
	b.discovered[instance.Key] = struct{}{}
	b.pending = append(b.pending, instance)
	return nil
}

func (b *reachableClosureBuilder) preferInstanceWitness(candidate *InstantiationInstance) {
	for i := range b.pending {
		if b.pending[i].Key == candidate.Key && compareInstantiationWitness(&candidate.Witness, &b.pending[i].Witness) < 0 {
			b.pending[i].Witness = cloneInstantiationWitness(&candidate.Witness)
			b.pending[i].Depth = candidate.Depth
			return
		}
	}
	for i := range b.instances {
		if b.instances[i].Key == candidate.Key && compareInstantiationWitness(&candidate.Witness, &b.instances[i].Witness) < 0 {
			b.instances[i].Witness = cloneInstantiationWitness(&candidate.Witness)
			return
		}
	}
}

func substituteConcreteTypes(subst *instantiationSubstitution, input []types.TypeID, typesIn *types.Interner) ([]types.TypeID, error) {
	out := make([]types.TypeID, len(input))
	for i := range input {
		var err error
		out[i], err = substituteConcreteType(subst, input[i], typesIn)
		if err != nil {
			return nil, fmt.Errorf("type argument %d: %w", i, err)
		}
	}
	return out, nil
}

func substituteConcreteType(subst *instantiationSubstitution, input types.TypeID, typesIn *types.Interner) (types.TypeID, error) {
	if input == types.NoTypeID {
		return types.NoTypeID, fmt.Errorf("missing type")
	}
	out, err := subst.typeID(input)
	if err != nil {
		return types.NoTypeID, err
	}
	if types.ContainsGenericParam(typesIn, out) {
		return types.NoTypeID, fmt.Errorf("substitution left unresolved generic type#%d", out)
	}
	return out, nil
}

func sortedCallees(set map[symbols.SymbolID]struct{}) []symbols.SymbolID {
	out := make([]symbols.SymbolID, 0, len(set))
	for id := range set {
		if id.IsValid() {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
