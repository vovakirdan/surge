package hir

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// lowerPlaceExpr preserves addressable syntax while lowering the operand of
// `&` or `&mut`. In particular, an index selected by sema normally becomes an
// exact `__index` call, whose reference result carries the selected element
// place. Physical projections remain reserved for Array/ArrayFixed storage.
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
		if l.indexUsesPhysicalArrayPlace(exprID, index.Target) {
			return &Expr{
				Kind: ExprIndex,
				Type: l.semaRes.ExprTypes[exprID],
				Span: expr.Span,
				Data: IndexData{
					Object: l.lowerPlaceExpr(index.Target),
					Index:  l.lowerExpr(index.Index),
				},
			}
		}

		// A custom index remains the exact selected callable. Its reference
		// result is a carrier for the element place, so expose the pointee to
		// the outer `&`/`&mut` as `*(call __index(...))` rather than projecting
		// into storage whose representation the compiler does not own.
		call := l.lowerExpr(exprID)
		if call == nil {
			return nil
		}
		carrierType := resolveAliasHIR(l.semaRes.TypeInterner, call.Type)
		elem, ok, _ := l.referenceInfo(carrierType)
		if !ok {
			return call
		}
		return &Expr{
			Kind: ExprUnaryOp,
			Type: elem,
			Span: expr.Span,
			Data: UnaryOpData{
				Op:      ast.ExprUnaryDeref,
				Operand: call,
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

func (l *lowerer) indexUsesPhysicalArrayPlace(exprID, target ast.ExprID) bool {
	if l == nil || l.semaRes == nil || l.semaRes.TypeInterner == nil || !target.IsValid() {
		return false
	}
	typeID := resolveAliasHIR(l.semaRes.TypeInterner, l.semaRes.ExprTypes[target])
	if tt, ok := l.semaRes.TypeInterner.Lookup(typeID); ok && tt.Kind == types.KindReference {
		typeID = tt.Elem
	}
	if !l.isArrayType(typeID) {
		return false
	}
	symID, selected := l.semaRes.IndexSymbols[exprID]
	return !selected || l.isBuiltinSymbol(symID)
}
