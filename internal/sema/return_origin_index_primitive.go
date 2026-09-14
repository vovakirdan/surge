package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// IndexSymbols stays in its owning unit's vocabulary after authority merge.
// A synthetic selected symbol need not itself be an original LocalCallable.
func (a *returnOriginAnalyzer) selectedIndexFunction(u *returnOriginUnitIndex, id ast.ExprID) (*returnOriginFunction, string) {
	selected, present := u.Sema.IndexSymbols[id]
	if !present || !selected.IsValid() {
		return nil, "selected index lacks a valid original symbol"
	}
	var candidate *CallableCandidate
	for i := range u.authority.CallableCandidates {
		c := &u.authority.CallableCandidates[i]
		mapped := c.Symbol == selected
		if len(u.Publication.RootToLocalSymbols) != 0 {
			mapped = slices.Contains(u.Publication.LocalSymbols(c.Symbol), selected)
		}
		if mapped {
			if candidate != nil {
				return nil, "selected index has ambiguous canonical authority"
			}
			candidate = c
		}
	}
	sym := u.Symbols.Table.Symbols.Get(selected)
	if candidate == nil || sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
		return nil, "selected index lacks its published callable authority"
	}
	info := returnOriginFnInfo(u.Sema.TypeInterner, sym.Type)
	if info == nil || sym.Span != candidate.Source || !sym.Signature.HasSelf ||
		!sym.Signature.ReturnSourceSyntax.Sources().Equal(candidate.ReturnSources) ||
		!slices.Equal(info.Params, candidate.ParamTypes) || info.Result != candidate.ResultType ||
		!info.ReturnSources().Equal(candidate.ReturnSources) {
		return nil, "selected index disagrees with its original typed signature"
	}
	fn := a.functionForTemplate(candidate.Symbol)
	if fn == nil || fn.key != candidate.BodyKey || fn.canonicalSourceKey != candidate.SourceKey {
		return nil, "selected index lacks its indexed physical declaration"
	}
	return fn, ""
}

func (a *returnOriginAnalyzer) indexDeclaration(fn *returnOriginFunction, actual returnOriginIndexType, span source.Span) string {
	c := fn.candidate
	u := fn.unit
	in := u.Sema.TypeInterner
	if c == nil || !c.Builtin || !c.Intrinsic || !c.HasSelf || c.HasBody || c.Async || fn.item.Body.IsValid() ||
		c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" || u.SourceKey != "core/intrinsics.sg" ||
		c.Name != "__index" || len(fn.info.Params) != 2 || len(c.Defaults) != 2 || len(c.Variadic) != 2 ||
		slices.Contains(c.Defaults, true) || slices.Contains(c.Variadic, true) {
		return "selected index requires its nonprimitive effect transfer"
	}
	identity, err := u.owningCallableIdentity(fn.item, fn.symbol, fn.info)
	if err != nil || identity.BodyKey != fn.key || identity.SourceKey != fn.canonicalSourceKey {
		return "selected index lost its original builtin declaration certificate"
	}
	_, self, typed := returnOriginIndexResolve(in, fn.info.Params[0])
	original, family := returnOriginIndexContainer(in, fn.info.Params[0])
	if !typed || self.Kind != types.KindReference || self.Mutable || !family || original.family != actual.family ||
		c.ReceiverType != original.container || fn.info.Params[1] != in.Builtins().Int {
		return "selected scalar index disagrees with its original receiver or index type"
	}
	if actual.family == in.Builtins().String {
		if len(c.TemplateParams) != 0 || c.ReceiverTemplateArity != 0 || fn.info.Result != in.Builtins().Uint32 || !fn.info.ReturnSources().IsAllInputs() {
			return "scalar string index lost its original signature"
		}
	} else {
		arity := 1
		if actual.family == in.ArrayFixedNominalType() {
			arity = 2
		}
		_, result, typed := returnOriginIndexResolve(in, fn.info.Result)
		if len(c.TemplateParams) != arity || c.ReceiverTemplateArity != arity || original.element != c.TemplateParams[0] ||
			!typed || result.Kind != types.KindReference || result.Mutable || result.Elem != original.element ||
			fn.info.ReturnSources().IsAllInputs() || !slices.Equal(fn.info.ReturnSources().Slots(), []uint32{0}) {
			return "scalar array index lost its original element or owner promise"
		}
		if arity == 2 {
			info, _ := in.StructInfo(original.container)
			if len(info.TypeArgs) != 2 || info.TypeArgs[1] != c.TemplateParams[1] {
				return "fixed array index lost its original length parameter"
			}
		}
	}
	body := &returnOriginBody{analyzer: a, function: fn}
	allowed, valid := body.declaredFunctionSources(fn, span)
	if !valid || (actual.family != in.Builtins().String && !slices.Equal(allowed, []uint32{0})) {
		return "scalar index has an unresolved original return-source declaration"
	}
	return ""
}

func (a *returnOriginAnalyzer) indexOperation(caller *returnOriginFunction, id ast.ExprID) (returnOriginIndexType, string) {
	u := caller.unit
	primitive, reason := returnOriginTypedIndex(u, id)
	if reason == "index requires a non-scalar index transfer" {
		return a.stringRangeIndex(caller, id)
	}
	if reason != "" {
		return primitive, reason
	}
	// Absent selection is the native HIR ExprIndex operation. An invalid
	// present entry never acquires that meaning by falling back to this path.
	if _, selected := u.Sema.IndexSymbols[id]; !selected {
		return primitive, ""
	}
	fn, reason := a.selectedIndexFunction(u, id)
	if reason != "" {
		return primitive, reason
	}
	span := u.Builder.Exprs.Get(id).Span
	if reason = a.indexDeclaration(fn, primitive, span); reason != "" {
		return primitive, reason
	}
	if len(fn.candidate.TemplateParams) == 0 {
		return primitive, ""
	}
	closure := u.authority.InstantiationClosure
	if closure == nil {
		return primitive, "generic index lacks its finalized concrete use"
	}
	var found *ConcreteInstantiationUse
	for i := range closure.UseSites {
		use := &closure.UseSites[i]
		if use.SourceKey == u.SourceKey && use.Site == span {
			if found != nil {
				return primitive, "generic index has ambiguous finalized concrete uses"
			}
			found = use
		}
	}
	if found == nil {
		return primitive, "generic index lacks its finalized concrete use"
	}
	callee, owner, expr, reason := a.genericUseContext(*found)
	if reason != "" {
		return primitive, reason
	}
	if callee != fn || owner != caller || expr != id {
		return primitive, "generic index disagrees with its selected caller and expression"
	}
	return primitive, a.checkIndexUse(fn, caller, id, *found)
}

// This certifies the existing scalar primitive, including its effect and
// borrowed result. It does not discharge arbitrary bodyless generic calls.
func (a *returnOriginAnalyzer) checkIndexUse(fn, caller *returnOriginFunction, id ast.ExprID, use ConcreteInstantiationUse) string {
	u := caller.unit
	actual, reason := returnOriginTypedIndex(u, id)
	if reason != "" {
		return reason
	}
	selected, reason := a.selectedIndexFunction(u, id)
	if reason != "" {
		return reason
	}
	if selected != fn || use.CalleeTemplate != fn.candidate.Symbol {
		return "generic index differs from its current selected declaration"
	}
	if reason = a.indexDeclaration(fn, actual, use.Site); reason != "" {
		return reason
	}
	in := u.Sema.TypeInterner
	if actual.family == in.Builtins().String || len(use.TemplateArgs) != len(fn.candidate.TemplateParams) ||
		len(use.TemplateArgs) == 0 || use.TemplateArgs[0] != actual.element {
		return "generic index differs from its concrete element arguments"
	}
	for _, arg := range use.TemplateArgs {
		if _, ok := in.Lookup(arg); !ok || types.ContainsGenericParam(in, arg) {
			return "generic index requires its concrete element or length"
		}
	}
	if actual.family == in.ArrayFixedNominalType() {
		_, length, valid := in.ArrayFixedInfo(actual.container)
		arg, typed := in.Lookup(use.TemplateArgs[1])
		if !valid || !typed || arg.Kind != types.KindConst || arg.Count != length {
			return "generic fixed index differs from its concrete length argument"
		}
	}
	return ""
}
