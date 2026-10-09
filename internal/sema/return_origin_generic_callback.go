package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// genericCallbackConfined records the conditions under which an incoming
// declared function cannot move borrowed state through a generic helper. Surge
// function values have no captures; shared inputs cannot be mutated, and every
// input/result payload must become NoBorrowedState at each concrete use.
func (b *returnOriginBody) genericCallbackConfined(info *types.FnInfo, target ast.ExprID, span source.Span) bool {
	fn := b.function
	if info == nil || fn == nil || fn.candidate == nil || len(fn.candidate.TemplateParams) == 0 || !fn.item.Body.IsValid() {
		return false
	}
	symID := fn.unit.Symbols.ExprSymbols[target]
	sym := fn.unit.Symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.Kind != symbols.SymbolParam || !slices.Contains(fn.params, symID) || returnOriginFnInfo(fn.unit.Sema.TypeInterner, sym.Type) == nil {
		return false
	}
	view := returnOriginView(fn)
	for _, id := range info.Params {
		typ, present := fn.unit.Sema.TypeInterner.Lookup(returnOriginResolveAlias(fn.unit.Sema.TypeInterner, id))
		if present && typ.Kind == types.KindReference {
			if typ.Mutable {
				return false
			}
			id = typ.Elem
		}
		if len(b.requireOpaqueState(view, id, span).roots) != 0 {
			return false
		}
	}
	return len(b.requireOpaqueState(view, info.Result, span).roots) == 0
}
