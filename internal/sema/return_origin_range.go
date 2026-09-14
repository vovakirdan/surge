package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *returnOriginBody) rangeLiteral(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	data, ok := u.Builder.Exprs.RangeLit(id)
	if !ok || data == nil {
		return b.unknownExpr(env, u.Builder.Exprs.Get(id).Span, "range literal lacks its original bounds"), nil
	}
	flow := returnOriginFlow{normal: env}
	for _, bound := range [...]ast.ExprID{data.Start, data.End} {
		if !bound.IsValid() {
			continue
		}
		var err error
		flow, err = flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
			out, stepErr := b.expr(bound, next, targets)
			return out.flow, stepErr
		})
		if err != nil {
			return returnOriginExprResult{}, err
		}
	}
	out := returnOriginExprResult{flow: flow}
	if !flow.normal.reachable {
		return out, nil
	}
	// The int descriptor owns its bounds, but carries no language borrow.
	// Discard only its value facts after both bounds' effects and exits survive.
	out.value = returnOriginValueOf()
	if reason := b.rangeLiteralOperation(id, data); reason != "" {
		b.pending(u.Builder.Exprs.Get(id).Span, reason)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return out, nil
}

func (b *returnOriginBody) rangeLiteralOperation(id ast.ExprID, data *ast.ExprRangeLitData) string {
	u := b.function.unit
	in := u.Sema.TypeInterner
	for _, expr := range [...]ast.ExprID{id, data.Start, data.End} {
		if !expr.IsValid() {
			continue
		}
		if _, converted := u.Sema.ImplicitConversions[expr]; converted {
			return "range literal requires its implicit conversion effect transfer"
		}
		if expr != id && u.Sema.ExprTypes[expr] != in.Builtins().Int {
			return "range literal requires original int bounds"
		}
	}
	local := u.Symbols.ExprSymbols[id]
	// Imported selected symbols need not be caller LocalCallables. This
	// existing reader normalizes any symbol through the same unique publication
	// relation; parameter-owner validation belongs to its other consumer.
	canonical, mapped := u.canonicalParameterOwner(local)
	fn := b.analyzer.functionForTemplate(canonical)
	sym := u.Symbols.Table.Symbols.Get(local)
	if !mapped || fn == nil || sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
		return "range literal lacks its published constructor authority"
	}
	info := returnOriginFnInfo(in, sym.Type)
	if info == nil || sym.Span != fn.candidate.Source || sym.Signature.HasSelf != fn.candidate.HasSelf ||
		sym.Signature.HasBody != fn.candidate.HasBody || !sym.Signature.ReturnSourceSyntax.Sources().Equal(fn.info.ReturnSources()) ||
		!slices.Equal(info.Params, fn.info.Params) || info.Result != fn.info.Result || !info.ReturnSources().Equal(fn.info.ReturnSources()) {
		return "range literal disagrees with its original typed constructor"
	}
	name := "rt_range_int_full"
	switch {
	case data.Start.IsValid() && data.End.IsValid():
		name = "rt_range_int_new"
	case data.Start.IsValid():
		name = "rt_range_int_from_start"
	case data.End.IsValid():
		name = "rt_range_int_to_end"
	}
	if fn.name != name {
		return "range literal disagrees with its original constructor"
	}
	if !returnOriginRangeConstructor(fn) {
		return "range literal lacks its original builtin constructor certificate"
	}
	rangeType, reason := b.analyzer.intRangeType()
	if reason != "" {
		return reason
	}
	if fn.info.Result != rangeType || u.Sema.ExprTypes[id] != rangeType {
		return "range literal disagrees with its original result type"
	}
	return ""
}

// Names select a primitive contract only after its original physical AST and
// canonical publication establish that this is the retained core declaration.
func returnOriginRangeBuiltin(fn *returnOriginFunction, name string) bool {
	if fn == nil || fn.info == nil || fn.item == nil || fn.candidate == nil {
		return false
	}
	c, u := fn.candidate, fn.unit
	if !c.Builtin || !c.Intrinsic || c.HasBody || c.Async || fn.item.Body.IsValid() || c.Name != name || fn.name != name ||
		c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" || u.SourceKey != "core/intrinsics.sg" ||
		len(c.TemplateParams) != 0 || c.ReceiverTemplateArity != 0 || len(c.Defaults) != len(fn.info.Params) ||
		len(c.Variadic) != len(fn.info.Params) || slices.Contains(c.Defaults, true) || slices.Contains(c.Variadic, true) {
		return false
	}
	identity, err := u.owningCallableIdentity(fn.item, fn.symbol, fn.info)
	return err == nil && identity.BodyKey == fn.key && identity.SourceKey == fn.canonicalSourceKey
}

func returnOriginRangeConstructor(fn *returnOriginFunction) bool {
	if fn == nil || !returnOriginRangeBuiltin(fn, fn.name) {
		return false
	}
	bounds := 0
	switch fn.name {
	case "rt_range_int_new":
		bounds = 2
	case "rt_range_int_from_start", "rt_range_int_to_end":
		bounds = 1
	case "rt_range_int_full":
	default:
		return false
	}
	in := fn.unit.Sema.TypeInterner
	if fn.candidate.HasSelf || fn.candidate.ReceiverType != types.NoTypeID || len(fn.info.Params) != bounds+1 ||
		fn.info.Params[bounds] != in.Builtins().Bool || !fn.info.ReturnSources().IsAllInputs() {
		return false
	}
	for _, param := range fn.info.Params[:bounds] {
		if param != in.Builtins().Int {
			return false
		}
	}
	result, typed := in.StructInfo(fn.info.Result)
	return typed && result != nil && result.Decl.File == fn.item.NameSpan.File &&
		slices.Equal(result.TypeArgs, []types.TypeID{in.Builtins().Int}) && returnOriginTypeShape(in, fn.info.Result, nil) == returnOriginRefFree
}

// Parameter bounds have no literal ExprSymbol. Their accepted TypeID comes
// from the same original zero-bound constructor, never a nominal name lookup.
func (a *returnOriginAnalyzer) intRangeType() (types.TypeID, string) {
	var selected *returnOriginFunction
	for _, fn := range a.declarations {
		if fn.name != "rt_range_int_full" || fn.canonicalSourceKey != "builtin" || fn.unit.SourceKey != "core/intrinsics.sg" {
			continue
		}
		if selected != nil {
			return types.NoTypeID, "range type has ambiguous original constructor authority"
		}
		selected = fn
	}
	if !returnOriginRangeConstructor(selected) {
		return types.NoTypeID, "range type lacks its original builtin constructor authority"
	}
	return selected.info.Result, ""
}

// The scalar reader already checked operand presence/types and conversions.
// Only its non-scalar refusal enters here; Array ranges remain a separate view.
func (a *returnOriginAnalyzer) stringRangeIndex(caller *returnOriginFunction, id ast.ExprID) (returnOriginIndexType, string) {
	u := caller.unit
	data, _ := u.Builder.Exprs.Index(id)
	in := u.Sema.TypeInterner
	primitive, valid := returnOriginIndexContainer(in, u.Sema.ExprTypes[data.Target])
	if !valid || primitive.family != in.Builtins().String {
		return returnOriginIndexType{}, "index requires a non-scalar index transfer"
	}
	rangeType, reason := a.intRangeType()
	if reason != "" {
		return primitive, reason
	}
	if u.Sema.ExprTypes[data.Index] != rangeType {
		return primitive, "index requires a non-scalar index transfer"
	}
	result, _, typed := returnOriginIndexResolve(in, u.Sema.ExprTypes[id])
	if !typed || result != in.Builtins().String {
		return primitive, "string range index has an inconsistent result type"
	}
	if _, selected := u.Sema.IndexSymbols[id]; !selected {
		return primitive, ""
	}
	fn, reason := a.selectedIndexFunction(u, id)
	if reason != "" {
		return primitive, reason
	}
	if !returnOriginRangeBuiltin(fn, "__index") || !fn.candidate.HasSelf || len(fn.info.Params) != 2 {
		return primitive, "string range index lacks its original builtin declaration certificate"
	}
	_, self, typed := returnOriginIndexResolve(in, fn.info.Params[0])
	if !typed || self.Kind != types.KindReference || self.Mutable || self.Elem != in.Builtins().String || fn.candidate.ReceiverType != in.Builtins().String ||
		fn.info.Params[1] != rangeType || fn.info.Result != in.Builtins().String || !fn.info.ReturnSources().IsAllInputs() {
		return primitive, "selected string range index disagrees with its original signature"
	}
	return primitive, ""
}
