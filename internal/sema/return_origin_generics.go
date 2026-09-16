package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Only the original template's direct parameter has a universal content slot.
// An arbitrary unknown shape is not a symbolic input, and storage stays Local.
func (fn *returnOriginFunction) directTemplateParam(id types.TypeID) bool {
	_, valid := fn.templateSlot(id)
	return valid && fn.item.Body.IsValid()
}

func (a *returnOriginAnalyzer) functionForTemplate(id symbols.SymbolID) *returnOriginFunction {
	var found *returnOriginFunction
	for _, unit := range a.units {
		for _, fn := range unit.functions {
			if fn.candidate != nil && fn.candidate.Symbol == id {
				if found != nil {
					return nil
				}
				found = fn
			}
		}
	}
	return found
}

func (a *returnOriginAnalyzer) genericInstance(key InstanceKey, template symbols.SymbolID, args []types.TypeID) string {
	return a.genericInstanceKind(key, template, args, InstantiationFunction)
}

func (a *returnOriginAnalyzer) genericInstanceKind(key InstanceKey, template symbols.SymbolID, args []types.TypeID, kind InstantiationTemplateKind) string {
	authority := a.units[0].authority
	if authority.InstantiationIdentity == nil || authority.InstantiationClosure == nil {
		return "generic use lacks finalized instance authority"
	}
	instance, found := authority.InstantiationClosure.Lookup(key)
	if !found {
		return "generic use lacks its finalized callee instance"
	}
	want, err := NewInstanceKey(*authority.InstantiationIdentity, template, args)
	if err != nil || want != key || instance.Template != template || instance.Kind != kind || !slices.Equal(instance.TemplateArgs, args) {
		return "generic use disagrees with its finalized callee instance"
	}
	return ""
}

// Both calls and selected index operations must retain the same exact current
// instance, original producing root and owning typed expression.
func (a *returnOriginAnalyzer) genericUseContext(use ConcreteInstantiationUse) (*returnOriginFunction, *returnOriginFunction, ast.ExprID, string) {
	if reason := a.genericInstance(use.Callee, use.CalleeTemplate, use.TemplateArgs); reason != "" {
		return nil, nil, ast.NoExprID, reason
	}
	fn := a.functionForTemplate(use.CalleeTemplate)
	caller := a.functionForTemplate(use.CallerTemplate)
	if fn == nil || caller == nil || len(fn.candidate.TemplateParams) != len(use.TemplateArgs) || len(use.TemplateArgs) == 0 {
		return nil, nil, ast.NoExprID, "generic use lacks its exact original callable declarations"
	}
	authority := caller.unit.authority
	if use.Caller != (InstanceKey{}) {
		if reason := a.genericInstance(use.Caller, use.CallerTemplate, use.CallerTemplateArgs); reason != "" {
			return nil, nil, ast.NoExprID, "generic caller: " + reason
		}
		if len(caller.candidate.TemplateParams) != len(use.CallerTemplateArgs) || !returnOriginConcreteArgs(authority.TypeInterner, use.CallerTemplateArgs) {
			return nil, nil, ast.NoExprID, "generic caller lacks its concrete original bindings"
		}
	} else if len(caller.candidate.TemplateParams) != 0 || len(use.CallerTemplateArgs) != 0 ||
		!slices.Contains(authority.InstantiationClosure.LiveCallables, use.CallerTemplate) {
		return nil, nil, ast.NoExprID, "generic use disagrees with its owning caller"
	}
	if use.Kind != InstantiationFunction || !returnOriginConcreteArgs(authority.TypeInterner, use.TemplateArgs) || caller.unit.SourceKey != use.SourceKey ||
		use.Site.File != caller.item.Span.File || use.Site.Start < caller.item.Span.Start || use.Site.End > caller.item.Span.End {
		return nil, nil, ast.NoExprID, "generic use disagrees with its owning caller"
	}
	matched := 0
	for _, root := range authority.InstantiationGraph.Roots() {
		witness, err := canonicalInstantiationWitness(&root.Witness, *authority.InstantiationIdentity)
		if err == nil && root.Kind == use.Kind && root.Template == use.CalleeTemplate && root.Witness.Caller == use.CallerTemplate &&
			witness.SourceKey == use.SourceKey && root.Witness.Site == use.Site && slices.Equal(root.TemplateArgs, use.TemplateArgs) {
			matched++
		}
	}
	if use.Caller == (InstanceKey{}) && matched != 1 {
		return nil, nil, ast.NoExprID, "generic use lacks its unique original concrete root"
	}
	var expression ast.ExprID
	for id, typ := range caller.unit.Sema.ExprTypes {
		if node := caller.unit.Builder.Exprs.Get(id); node != nil && node.Span == use.Site {
			if expression.IsValid() || typ == types.NoTypeID || (node.Kind != ast.ExprCall && node.Kind != ast.ExprIndex) {
				return nil, nil, ast.NoExprID, "generic use disagrees with its original typed operation"
			}
			expression = id
		}
	}
	if !expression.IsValid() {
		return nil, nil, ast.NoExprID, "generic use lacks its original typed operation"
	}
	return fn, caller, expression, ""
}

// Current uses reuse the original signature reader after their independent
// instance/root/caller checks. An original-body proof cannot supply a missing
// finalized use, and a concrete use cannot replace the source request.
func (a *returnOriginAnalyzer) genericCallUseInfo(fn, caller *returnOriginFunction, expression ast.ExprID, use ConcreteInstantiationUse) (*returnOriginSignature, string) {
	if use.Caller != (InstanceKey{}) {
		return a.currentCallBinding(fn, caller, expression, use)
	}
	args, reason := caller.originalInstantiation(expression, InstantiationFunction, fn.candidate.Symbol)
	if reason != "" {
		return nil, reason
	}
	if !slices.Equal(args, use.TemplateArgs) {
		return nil, "generic use disagrees with its original call arguments"
	}
	view, reason := fn.originalSignature(caller, expression, args)
	if reason == "" {
		binding := returnOriginBoundView(fn, nil, use.TemplateArgs)
		view.binding = &binding
	}
	return view, reason
}

// Body flow may not visit a retained use. Check the finalized uses and their
// producing roots independently; summaries do not stand in for this coverage.
func (a *returnOriginAnalyzer) checkGenericUses() error {
	if err := a.checkDeferredMethods(); err != nil {
		return err
	}
	authority := a.units[0].authority
	pending := func(use ConcreteInstantiationUse, reason string) {
		item := ReturnOriginPending{SourceKey: use.SourceKey, Span: use.Site, Reason: reason}
		if !slices.Contains(a.report.Pending, item) {
			a.report.Pending = append(a.report.Pending, item)
		}
	}
	closure := authority.InstantiationClosure
	if closure == nil || authority.InstantiationIdentity == nil {
		if !authority.InstantiationGraph.IsEmpty() {
			pending(ConcreteInstantiationUse{SourceKey: a.units[0].SourceKey}, "generic graph lacks finalized instance authority")
		}
		return nil
	}
	for _, use := range closure.UseSites {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		if use.Kind != InstantiationFunction && use.Kind != InstantiationTag {
			pending(use, "generic use has an unknown template kind")
			continue
		}
		duplicates := 0
		for _, other := range closure.UseSites {
			if other.Caller == use.Caller && other.CallerTemplate == use.CallerTemplate && other.SourceKey == use.SourceKey && other.Site == use.Site {
				duplicates++
			}
		}
		if duplicates != 1 {
			pending(use, "generic use has duplicate or contradictory finalized authority")
			continue
		}
		if use.Kind == InstantiationTag {
			if reason := a.checkTagUse(use); reason != "" {
				pending(use, reason)
			}
			continue
		}
		fn, caller, expression, reason := a.genericUseContext(use)
		if reason == "" {
			if _, store := caller.unit.Sema.IndexSetSymbols[expression]; store && caller.unit.Builder.Exprs.Get(expression).Kind == ast.ExprIndex {
				reason = a.checkIndexStoreUse(fn, caller, expression, use)
			} else if caller.unit.Builder.Exprs.Get(expression).Kind == ast.ExprIndex {
				reason = a.checkIndexUse(fn, caller, expression, use)
				if handled, view := a.checkArrayRangeIndexUse(fn, caller, expression, use); handled {
					reason = view
				}
			} else {
				var info *returnOriginSignature
				info, reason = a.genericCallUseInfo(fn, caller, expression, use)
				if handled, intrinsic := a.checkBackingIntrinsicUse(fn, use); reason == "" && handled {
					reason = intrinsic
				} else if reason == "" {
					reason = a.checkGenericPromise(fn, info, use)
				}
			}
		}
		if reason != "" {
			pending(use, reason)
		}
	}
	for _, root := range authority.InstantiationGraph.Roots() {
		if root.Kind != InstantiationFunction && root.Kind != InstantiationTag {
			pending(ConcreteInstantiationUse{SourceKey: root.Witness.SourceKey, Site: root.Witness.Site}, "generic root has an unknown template kind")
			continue
		}
		if root.Witness.Caller.IsValid() && !slices.Contains(closure.LiveCallables, root.Witness.Caller) {
			continue
		}
		witness, err := canonicalInstantiationWitness(&root.Witness, *authority.InstantiationIdentity)
		if err != nil {
			return err
		}
		use := ConcreteInstantiationUse{CallerTemplate: root.Witness.Caller, CalleeTemplate: root.Template, SourceKey: witness.SourceKey, Site: root.Witness.Site}
		matched := 0
		for _, actual := range closure.UseSites {
			if actual.Caller == (InstanceKey{}) && actual.CallerTemplate == use.CallerTemplate && actual.CalleeTemplate == use.CalleeTemplate &&
				actual.Kind == root.Kind && actual.SourceKey == use.SourceKey && actual.Site == use.Site &&
				(root.Kind != InstantiationTag || slices.Equal(actual.TemplateArgs, root.TemplateArgs)) {
				matched++
			}
		}
		if matched != 1 {
			pending(use, "generic call lacks its finalized concrete use")
		}
	}
	if err := a.checkCurrentConditionEdges(pending); err != nil {
		return err
	}
	for i, instance := range closure.Instances {
		if instance.Kind != InstantiationFunction && instance.Kind != InstantiationTag {
			pending(ConcreteInstantiationUse{SourceKey: instance.Witness.SourceKey, Site: instance.Witness.Site}, "generic instance has an unknown template kind")
			continue
		}
		use := ConcreteInstantiationUse{SourceKey: instance.Witness.SourceKey, Site: instance.Witness.Site}
		if reason := a.genericInstanceKind(instance.Key, instance.Template, instance.TemplateArgs, instance.Kind); reason != "" {
			pending(use, reason)
		}
		if i > 0 && compareInstanceKey(closure.Instances[i-1].Key, instance.Key) >= 0 {
			pending(use, "generic instances have duplicate or noncanonical ordering")
		}
		if !slices.ContainsFunc(closure.UseSites, func(use ConcreteInstantiationUse) bool {
			return use.Kind == instance.Kind && use.Callee == instance.Key
		}) {
			pending(use, "generic instance lacks a finalized producing use")
		}
	}
	return nil
}
