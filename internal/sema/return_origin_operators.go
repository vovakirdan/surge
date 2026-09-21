package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// An operator or conversion SEMA resolved to a magic method is a call HIR spells
// and the AST does not. The selection is admitted only as a body-less, non-generic,
// synchronous declaration named for the operation and certified by the reader
// calls use, with no container formal, whose formals pass the opaque-call effect
// rule and whose declared result holds no borrowed state. Then no operand borrow
// or storage loan can leave through the call. The callee's own view equals the
// caller's for a non-generic declaration.
func (b *returnOriginBody) selectedOperation(selections map[ast.ExprID]symbols.SymbolID, id ast.ExprID, name string, arity int, operands ...ast.ExprID) bool {
	u := b.function.unit
	selected, present := selections[id]
	if !present || !selected.IsValid() || name == "" {
		return false
	}
	for _, operand := range operands {
		if _, converted := u.Sema.ImplicitConversions[operand]; converted {
			return false
		}
	}
	fn, reason := b.analyzer.selectedCallableFunction(u, selected)
	if reason != "" {
		return false
	}
	c, in := fn.candidate, u.Sema.TypeInterner
	if c.Name != name || len(fn.info.Params) != arity || c.HasBody || fn.item.Body.IsValid() || c.Async || len(c.TemplateParams) != 0 ||
		returnOriginContainerFormal(in, fn.info.Params) || returnOriginCallHasUnprovedEffects(in, fn.info.Params) || b.loanSinkEffects(fn.info.Params, fn.info.Params) {
		return false
	}
	required := returnOriginView(fn).requirement(returnOriginNoBorrowedState, fn.info.Result)
	return !required.failed() && len(required.atoms) == 0
}

// A cast needs no transfer when it has no selection (a native cast) or its own
// selection is certified. Recording an implicit __to conversion on the same node
// overwrites that selection, so such a node proves nothing.
func (b *returnOriginBody) castProven(id, value ast.ExprID) bool {
	u := b.function.unit
	if _, selected := u.Sema.ToSymbols[id]; !selected {
		return true
	}
	if conv, converted := u.Sema.ImplicitConversions[id]; converted && conv.Kind == ImplicitConversionTo {
		return false
	}
	return b.selectedOperation(u.Sema.ToSymbols, id, "__to", 2, value)
}

// A container formal, directly or behind one reference or own, can keep a storage
// loan that the effect rule reads as ref-free by its payload.
func returnOriginContainerFormal(in *types.Interner, params []types.TypeID) bool {
	for _, param := range params {
		for layer := 0; layer < 2; layer++ {
			if target, ok := in.AliasTarget(param); ok {
				param = target
			}
			typ, ok := in.Lookup(param)
			if !ok || typ.Kind == types.KindArray {
				return true
			}
			if info, isStruct := in.StructInfo(param); isStruct && info != nil {
				if _, nominal := returnOriginNominalShape(in, param, info, nil); nominal {
					return true
				}
			}
			if layer != 0 || (typ.Kind != types.KindReference && typ.Kind != types.KindOwn) {
				break
			}
			param = typ.Elem
		}
	}
	return false
}

// A selection proves nothing on the borrow-free path unless this analysis can NAME
// the declaration it chose. `borrowFree` is a statement about SHAPES, and a carrier
// is RefFree-shaped: a body can hand back a view of what a reference formal points
// at, and so can an OPAQUE body-less declaration, whose implementation is elsewhere
// and which cannot even announce the fact -- `@return_source` is refused on a result
// that is not reference-bearing. Only the two core concatenation intrinsics are
// exempt, and they are certified by identity, not by the absence of a body. Any other
// result leaves the path only for a selection that can keep a loan with no body to
// refuse it (return_origin_effect_sinks.go), or one this analysis cannot name.
func (b *returnOriginBody) selectedCarrierResult(selections map[ast.ExprID]symbols.SymbolID, id ast.ExprID) bool {
	u := b.function.unit
	selected, present := selections[id]
	if !present {
		return false
	}
	if b.analyzer.loanCarrier(u.Sema.ExprTypes[id]) {
		fn, reason := b.analyzer.selectedCallableFunction(u, selected)
		return reason != "" || !b.analyzer.coreArrayConcat(fn)
	}
	sym := u.Symbols.Table.Symbols.Get(selected)
	if sym == nil || !b.operationLoanSink(returnOriginFnInfo(u.Sema.TypeInterner, sym.Type), u.Sema.ExprTypes[id]) {
		return false
	}
	fn, reason := b.analyzer.selectedCallableFunction(u, selected)
	return reason != "" || !fn.candidate.HasBody && !fn.item.Body.IsValid()
}

// coreArrayConcat certifies `Array<T> + Array<T>` and `ArrayFixed<T, N> + ArrayFixed<T, N>`
// the way coreArrayIntrinsic certifies the retained array declarations: by the original
// body-less builtin declaration, its owning source, its template arity and its exact
// structural descriptors. A name alone selects nothing, and neither does a missing body.
func (a *returnOriginAnalyzer) coreArrayConcat(fn *returnOriginFunction) bool {
	if fn == nil || fn.info == nil || fn.item == nil || fn.candidate == nil {
		return false
	}
	c, u := fn.candidate, fn.unit
	in := u.Sema.TypeInterner
	if !c.Builtin || !c.Intrinsic || c.HasBody || c.Async || fn.item.Body.IsValid() ||
		c.Name != "__add" || c.Name != fn.name || !c.HasSelf ||
		c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" || u.SourceKey != "core/intrinsics.sg" ||
		len(c.TemplateParams) == 0 || c.ReceiverTemplateArity != len(c.TemplateParams) ||
		len(fn.info.Params) != 2 || len(c.Defaults) != 2 || len(c.Variadic) != 2 ||
		slices.Contains(c.Defaults, true) || slices.Contains(c.Variadic, true) ||
		!fn.info.ReturnSources().IsAllInputs() {
		return false
	}
	identity, err := u.owningCallableIdentity(fn.item, fn.symbol, fn.info)
	if err != nil || identity.BodyKey != fn.key || identity.SourceKey != fn.canonicalSourceKey {
		return false
	}
	// `elem` is T in both families. ArrayFixed carries a second template parameter,
	// the const length N, and it must be the SAME N in both formals and in the result:
	// the model checks exactly this, and without it a concat whose lengths disagreed
	// would certify. The formals are shared references; the result is owned.
	elem := c.TemplateParams[0]
	shaped := func(id types.TypeID, wantReference bool) bool {
		ct, ok := returnOriginContainer(in, id)
		if !ok || ct.element != elem || ct.reference != wantReference {
			return false
		}
		if wantReference {
			if mutable, reference := returnOriginBackingDescriptor(in, id); !reference || mutable {
				return false
			}
		}
		info, present := in.StructInfo(ct.container)
		if !present || info == nil {
			return false
		}
		switch ct.family {
		case in.ArrayFixedNominalType():
			return len(c.TemplateParams) == 2 && len(info.TypeArgs) == 2 && info.TypeArgs[1] == c.TemplateParams[1]
		case in.ArrayNominalType():
			return len(c.TemplateParams) == 1
		default:
			return false
		}
	}
	if !shaped(fn.info.Params[0], true) || !shaped(fn.info.Params[1], true) || !shaped(fn.info.Result, false) {
		return false
	}
	left, _ := returnOriginContainer(in, fn.info.Params[0])
	right, _ := returnOriginContainer(in, fn.info.Params[1])
	out, _ := returnOriginContainer(in, fn.info.Result)
	return left.family == right.family && left.family == out.family
}
