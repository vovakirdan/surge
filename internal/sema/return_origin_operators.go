package sema

import (
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
		returnOriginContainerFormal(in, fn.info.Params) || returnOriginCallHasUnprovedEffects(in, fn.info.Params) {
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
