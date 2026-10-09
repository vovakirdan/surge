package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
)

// projectedLoad resolves the bounded content cells the analysis models: a
// certified container element, the exact marked borrowed view, or an admitted
// external cell. Other reference-through-reference loads stay fail-closed.
func (b *returnOriginBody) projectedLoad(operand, result ast.ExprID, storage returnOriginValue, env returnOriginEnv, span source.Span) (returnOriginValue, bool) {
	if loaded, handled := b.indexElementContents(operand, storage, env); handled {
		return loaded, true
	}
	u := b.function.unit
	if returnOriginBorrowedViewLoad(u.Sema.TypeInterner, u.Sema.ExprTypes[operand], u.Sema.ExprTypes[result]) {
		return storage.clone(), true
	}
	return b.loadExternalCells(u.Sema.ExprTypes[operand], storage, env, span)
}
