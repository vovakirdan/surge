package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Original Invalid never becomes legal through substitution. A deferred
// declaration may have a ref-free result; that instance's promise is vacuous.
func (v returnOriginTypeView) validatePromise(request ReturnSourceDeclarationRequest) ReturnSourceValidation {
	original := ValidateDeclaredReturnSources(v.owner.unit.Sema.TypeInterner, request)
	if original.Status == ReturnSourcesInvalid || request.Syntax.Sources().IsAllInputs() {
		return original
	}
	deferred := ReturnSourceValidation{Status: ReturnSourcesDeferred, Span: request.Syntax.Span()}
	result, known := v.bearing(request.Result(), nil)
	if !known {
		return deferred
	}
	if result == ReturnSourcesInvalid {
		if original.Status == ReturnSourcesDeferred {
			return ReturnSourceValidation{Status: ReturnSourcesValid}
		}
		return invalidReturnSource(request.Syntax.Markers()[0], "@return_source requires a reference-bearing result")
	}
	for _, marker := range request.Syntax.Markers() {
		params := request.Params()
		if int64(marker.Slot) >= int64(len(params)) {
			return invalidReturnSource(marker, "@return_source parameter is missing from the function signature")
		}
		state, valid := v.bearing(params[marker.Slot], nil)
		if !valid {
			return deferred
		}
		if state == ReturnSourcesInvalid {
			return invalidReturnSource(marker, "@return_source requires a reference-bearing parameter")
		}
	}
	return ReturnSourceValidation{Status: ReturnSourcesValid}
}

func (a *returnOriginAnalyzer) useRequirements(fn *returnOriginFunction, use ConcreteInstantiationUse) returnOriginRequirements {
	view := returnOriginBoundView(fn, nil, use.TemplateArgs)
	required := a.summaries[fn.key].required.rebase(view)
	for _, reason := range []struct {
		failed bool
		text   string
	}{
		{required.refuted, "opaque result type may carry borrowed state"},
		{required.unsupported, "opaque result borrowed-state classification is unsupported"},
	} {
		item := ReturnOriginPending{SourceKey: use.SourceKey, Span: use.Site, Reason: reason.text}
		if reason.failed && !slices.Contains(a.report.Pending, item) {
			a.report.Pending = append(a.report.Pending, item)
		}
	}
	return required
}

// Every retained source edge must have its concrete use for every current
// caller instance. In particular deleting both an inner use and its callee
// instance cannot evade the old orphan-instance check.
func (a *returnOriginAnalyzer) checkCurrentConditionEdges(pending func(ConcreteInstantiationUse, string)) error {
	authority := a.units[0].authority
	closure := authority.InstantiationClosure
	for _, instance := range closure.Instances {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		if instance.Kind != InstantiationFunction {
			continue
		}
		caller := a.functionForTemplate(instance.Template)
		if caller == nil || len(caller.candidate.TemplateParams) != len(instance.TemplateArgs) {
			continue
		}
		for _, edge := range authority.InstantiationGraph.Edges() {
			if edge.Caller != instance.Template {
				continue
			}
			witness, err := canonicalInstantiationWitness(&edge.Witness, *authority.InstantiationIdentity)
			if err != nil {
				return err
			}
			want := ConcreteInstantiationUse{Caller: instance.Key, CallerTemplate: instance.Template,
				CalleeTemplate: edge.Callee, Kind: edge.Kind, SourceKey: witness.SourceKey, Site: edge.Witness.Site}
			matched := 0
			for _, use := range closure.UseSites {
				if use.Caller != want.Caller || use.CallerTemplate != want.CallerTemplate || use.CalleeTemplate != want.CalleeTemplate ||
					use.Kind != want.Kind || use.SourceKey != want.SourceKey || use.Site != want.Site {
					continue
				}
				matched++
			}
			if matched != 1 {
				pending(want, "generic call lacks its finalized concrete use")
			}
		}
	}
	return nil
}

func (a *returnOriginAnalyzer) currentCallBinding(fn, caller *returnOriginFunction, expression ast.ExprID, use ConcreteInstantiationUse) (*returnOriginSignature, string) {
	args, reason := caller.originalInstantiation(expression, InstantiationFunction, fn.candidate.Symbol)
	if reason != "" {
		return nil, reason
	}
	if len(args) != len(use.TemplateArgs) {
		return nil, "generic use disagrees with its original call arguments"
	}
	for i, original := range args {
		if matchReturnOriginSourceType(caller.unit.Sema.TypeInterner, original, use.TemplateArgs[i],
			caller.candidate.TemplateParams, use.CallerTemplateArgs) != "" {
			return nil, "generic use disagrees with its original call arguments"
		}
	}
	view, reason := fn.originalSignature(caller, expression, args)
	if reason == "" {
		binding := returnOriginBoundView(fn, nil, use.TemplateArgs)
		view.binding = &binding
	}
	return view, reason
}

func returnOriginConcreteArgs(in *types.Interner, args []types.TypeID) bool {
	for _, arg := range args {
		if _, ok := in.Lookup(arg); !ok || types.ContainsGenericParam(in, arg) {
			return false
		}
	}
	return true
}

func (fn *returnOriginFunction) templateParameterAuthority(slot int, id types.TypeID, info *types.TypeParamInfo) bool {
	u, candidate := fn.unit, fn.candidate
	selected := u.Symbols.Table.Symbols.Get(fn.symbol)
	if !slices.Equal(u.authority.InstantiationTemplateParams[candidate.Symbol], candidate.TemplateParams) ||
		slices.Index(candidate.TemplateParams, id) != slot || slices.Contains(candidate.TemplateParams[slot+1:], id) ||
		selected == nil || len(selected.TypeParams) != len(candidate.TemplateParams) || selected.TypeParams[slot] != info.Name {
		return false
	}
	local := symbols.SymbolID(info.Owner)
	canonical, ok := u.canonicalParameterOwner(local)
	if !ok {
		return false
	}
	prefix := candidate.ReceiverTemplateArity
	if prefix < 0 || prefix > len(candidate.TemplateParams) {
		return false
	}
	if slot >= prefix {
		return canonical == candidate.Symbol && local == fn.symbol && uint64(info.Index) == uint64(slot-prefix)
	}
	// pushTypeParams uses the selected receiver symbol in the method's scope;
	// a prelude/import binding may have no Decl.Item. Its exact nominal target
	// and the retained receiver vector certify it, without finding an owner by name.
	owner := u.Symbols.Table.Symbols.Get(local)
	if owner == nil || owner.Kind != symbols.SymbolType {
		return false
	}
	original, found := u.Sema.TypeInterner.StructInfo(owner.Type)
	receiver, hasReceiver := u.Sema.TypeInterner.StructInfo(candidate.ReceiverType)
	if !found || original == nil || !hasReceiver || receiver == nil || original.Name != receiver.Name || original.Decl != receiver.Decl ||
		!slices.Equal(receiver.TypeArgs, candidate.TemplateParams[:prefix]) || int64(info.Index) >= int64(len(original.TypeParams)) {
		return false
	}
	declared, hasDeclared := u.Sema.TypeInterner.TypeParamInfo(original.TypeParams[info.Index])
	return hasDeclared && declared != nil && uint64(info.Index) == uint64(slot) &&
		info.IsConst == declared.IsConst && info.ConstType == declared.ConstType
}
