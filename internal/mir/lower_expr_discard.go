package mir

import (
	"surge/internal/hir"
	"surge/internal/types"
)

func (l *funcLowerer) discardResultExpr(e *hir.Expr) *hir.Expr {
	if l == nil || e == nil || l.types == nil {
		return nil
	}
	if e.Type == types.NoTypeID || l.isNothingType(e.Type) {
		return nil
	}
	switch e.Kind {
	case hir.ExprBlock, hir.ExprIf, hir.ExprSelect, hir.ExprRace:
		clone := *e
		clone.Type = l.types.Builtins().Nothing
		return &clone
	default:
		return nil
	}
}
