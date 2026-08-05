package hir

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// lowerRangeLitExpr lowers a range literal expression.
func (l *lowerer) lowerRangeLitExpr(exprID ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
	rangeData := l.builder.Exprs.RangeLits.Get(uint32(expr.Payload))
	if rangeData == nil {
		return nil
	}

	var (
		name string
		args []*Expr
	)

	switch {
	case rangeData.Start.IsValid() && rangeData.End.IsValid():
		name = "rt_range_int_new"
		args = append(args, l.lowerExpr(rangeData.Start), l.lowerExpr(rangeData.End), l.boolLiteralExpr(expr.Span, rangeData.Inclusive))
	case rangeData.Start.IsValid():
		name = "rt_range_int_from_start"
		args = append(args, l.lowerExpr(rangeData.Start), l.boolLiteralExpr(expr.Span, rangeData.Inclusive))
	case rangeData.End.IsValid():
		name = "rt_range_int_to_end"
		args = append(args, l.lowerExpr(rangeData.End), l.boolLiteralExpr(expr.Span, rangeData.Inclusive))
	default:
		name = "rt_range_int_full"
		args = append(args, l.boolLiteralExpr(expr.Span, rangeData.Inclusive))
	}

	callee, symID := l.intrinsicCallee(name, expr.Span)
	if l.symRes != nil {
		if exact := l.symRes.ExprSymbols[exprID]; exact.IsValid() {
			if exactCallee := l.varRefForSymbol(exact, expr.Span); exactCallee != nil {
				callee, symID = exactCallee, exact
			}
		}
	}
	return &Expr{
		Kind: ExprCall,
		Type: ty,
		Span: expr.Span,
		Data: CallData{
			Callee:   callee,
			Args:     args,
			SymbolID: symID,
		},
	}
}
