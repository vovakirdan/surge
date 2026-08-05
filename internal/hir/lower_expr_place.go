package hir

import "surge/internal/ast"

// lowerPlaceExpr preserves addressable syntax while lowering the operand of
// `&` or `&mut`. In particular, an index selected by sema normally becomes an
// exact `__index` call, but that call only returns `&T`; wrapping its temporary
// in `&mut` would mutate the reference slot instead of the array element.
func (l *lowerer) lowerPlaceExpr(exprID ast.ExprID) *Expr {
	if l == nil || !exprID.IsValid() {
		return nil
	}
	expr := l.builder.Exprs.Arena.Get(uint32(exprID))
	if expr == nil {
		return nil
	}

	switch expr.Kind {
	case ast.ExprGroup:
		group := l.builder.Exprs.Groups.Get(uint32(expr.Payload))
		if group != nil {
			return l.lowerPlaceExpr(group.Inner)
		}

	case ast.ExprIndex:
		index := l.builder.Exprs.Indices.Get(uint32(expr.Payload))
		if index == nil {
			return nil
		}
		return &Expr{
			Kind: ExprIndex,
			Type: l.semaRes.ExprTypes[exprID],
			Span: expr.Span,
			Data: IndexData{
				Object: l.lowerPlaceExpr(index.Target),
				Index:  l.lowerExpr(index.Index),
			},
		}

	case ast.ExprMember:
		member := l.builder.Exprs.Members.Get(uint32(expr.Payload))
		result := l.lowerMemberExpr(exprID, expr, l.semaRes.ExprTypes[exprID])
		if member == nil || result == nil || result.Kind != ExprFieldAccess {
			return result
		}
		data := result.Data.(FieldAccessData)
		data.Object = l.lowerPlaceExpr(member.Target)
		result.Data = data
		return result

	case ast.ExprTupleIndex:
		tupleIndex := l.builder.Exprs.TupleIndices.Get(uint32(expr.Payload))
		result := l.lowerTupleIndexExpr(exprID, expr, l.semaRes.ExprTypes[exprID])
		if tupleIndex == nil || result == nil || result.Kind != ExprFieldAccess {
			return result
		}
		data := result.Data.(FieldAccessData)
		data.Object = l.lowerPlaceExpr(tupleIndex.Target)
		result.Data = data
		return result
	}

	return l.lowerExpr(exprID)
}
