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
	if fn.candidate == nil || !fn.item.Body.IsValid() || !slices.Contains(fn.candidate.TemplateParams, id) {
		return false
	}
	typ, ok := fn.unit.Sema.TypeInterner.Lookup(id)
	return ok && typ.Kind == types.KindGenericParam
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
	authority := a.units[0].authority
	if authority.InstantiationIdentity == nil || authority.InstantiationClosure == nil {
		return "generic use lacks finalized instance authority"
	}
	instance, found := authority.InstantiationClosure.Lookup(key)
	if !found {
		return "generic use lacks its finalized callee instance"
	}
	want, err := NewInstanceKey(*authority.InstantiationIdentity, template, args)
	if err != nil || want != key || instance.Template != template || instance.Kind != InstantiationFunction || !slices.Equal(instance.TemplateArgs, args) {
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
		return nil, nil, ast.NoExprID, "generic caller requires its exact type-dependent use transfer"
	}
	if use.Kind != InstantiationFunction || len(caller.candidate.TemplateParams) != 0 || len(use.CallerTemplateArgs) != 0 ||
		!slices.Contains(authority.InstantiationClosure.LiveCallables, use.CallerTemplate) || caller.unit.SourceKey != use.SourceKey ||
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
	if matched != 1 {
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

func (a *returnOriginAnalyzer) genericUseInfo(use ConcreteInstantiationUse) (*returnOriginFunction, *types.FnInfo, string) {
	fn, caller, expression, reason := a.genericUseContext(use)
	if reason != "" {
		return nil, nil, reason
	}
	info, reason := a.genericCallUseInfo(fn, caller, expression, use)
	return fn, info, reason
}

// The call view substitutes exact direct parameters without interning or
// changing FnInfo. Index primitives validate their element/length separately.
func (a *returnOriginAnalyzer) genericCallUseInfo(fn, caller *returnOriginFunction, expression ast.ExprID, use ConcreteInstantiationUse) (*types.FnInfo, string) {
	authority := caller.unit.authority
	bind := func(id types.TypeID) types.TypeID {
		if slot := slices.Index(fn.candidate.TemplateParams, id); slot >= 0 {
			id = use.TemplateArgs[slot]
		}
		if _, ok := authority.TypeInterner.Lookup(id); !ok || types.ContainsGenericParam(authority.TypeInterner, id) {
			return types.NoTypeID
		}
		return id
	}
	view := *fn.info
	view.Params = slices.Clone(fn.info.Params)
	view.Result = bind(fn.info.Result)
	for i, param := range view.Params {
		view.Params[i] = bind(param)
	}
	if view.Result == types.NoTypeID || slices.Contains(view.Params, types.NoTypeID) {
		return nil, "generic use requires a type-dependent signature substitution"
	}
	if caller.unit.Sema.ExprTypes[expression] != view.Result {
		return nil, "generic use disagrees with its original typed call"
	}
	call, ok := caller.unit.Builder.Exprs.Call(expression)
	identity, err := caller.unit.callableIdentity(caller.unit.Symbols.ExprSymbols[expression], "")
	if !ok || call == nil || err != nil || identity.BodyKey != fn.key || identity.SourceKey != fn.canonicalSourceKey {
		return nil, "generic use lacks its original selected call"
	}
	var receiver ast.ExprID
	sym := caller.unit.Symbols.Table.Symbols.Get(caller.unit.Symbols.ExprSymbols[expression])
	if sym != nil && sym.Signature != nil && sym.Signature.HasSelf {
		if member, ok := caller.unit.Builder.Exprs.Member(call.Target); ok && member != nil {
			receiver = member.Target
		}
	}
	if sym == nil {
		return nil, "generic call lacks original formal metadata"
	}
	slots, err := mapReturnOriginArguments(sym.Signature, call, receiver)
	if err != nil || len(slots) != len(view.Params) {
		return nil, "generic call lacks exact physical argument slots"
	}
	for i, slot := range slots {
		if slot.defaulted || len(slot.exprs) != 1 || caller.unit.Sema.ExprTypes[slot.exprs[0]] != view.Params[i] {
			return nil, "generic call needs its exact default, variadic or implicit argument transfer"
		}
	}
	return &view, ""
}

// Body flow may not visit a retained use. Check the finalized uses and their
// producing roots independently; summaries do not stand in for this coverage.
func (a *returnOriginAnalyzer) checkGenericUses() error {
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
		if use.Kind != InstantiationFunction {
			pending(use, "generic constructor authority needs its owning payload transfer")
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
		fn, caller, expression, reason := a.genericUseContext(use)
		if reason == "" {
			if caller.unit.Builder.Exprs.Get(expression).Kind == ast.ExprIndex {
				reason = a.checkIndexUse(fn, caller, expression, use)
			} else {
				var info *types.FnInfo
				info, reason = a.genericCallUseInfo(fn, caller, expression, use)
				if reason == "" {
					reason = a.checkGenericPromise(fn, info, use)
				}
			}
		}
		if reason != "" {
			pending(use, reason)
		}
	}
	for _, root := range authority.InstantiationGraph.Roots() {
		if root.Kind != InstantiationFunction || (root.Witness.Caller.IsValid() && !slices.Contains(closure.LiveCallables, root.Witness.Caller)) {
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
				actual.Kind == root.Kind && actual.SourceKey == use.SourceKey && actual.Site == use.Site {
				matched++
			}
		}
		if matched != 1 {
			pending(use, "generic call lacks its finalized concrete use")
		}
	}
	for i, instance := range closure.Instances {
		if instance.Kind != InstantiationFunction {
			continue
		}
		use := ConcreteInstantiationUse{SourceKey: instance.Witness.SourceKey, Site: instance.Witness.Site}
		if reason := a.genericInstance(instance.Key, instance.Template, instance.TemplateArgs); reason != "" {
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
