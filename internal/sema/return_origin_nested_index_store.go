package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
)

// nestedRefFreeIndexStore closes N-STORE only where no reference content can
// change: an exact nested member/index selects a reference to a canonical
// container whose final element and stored expression are reference-free and
// the element is not itself a storage-loan carrier. The evaluated store still
// runs through indexStoreOperation before this helper; only backing identity is
// waived because storeBackingContents would have no origin fact to update.
func (b *returnOriginBody) nestedRefFreeIndexStore(lhs ast.ExprID, container returnOriginIndexType, rhs returnOriginValue,
	rhsExpr ast.ExprID, span source.Span,
) bool {
	u := b.function.unit
	data, ok := u.Builder.Exprs.Index(lhs)
	if !ok || data == nil || b.shape(rhsExpr) != returnOriginRefFree ||
		returnOriginTypeShape(u.Sema.TypeInterner, container.element, nil) != returnOriginRefFree ||
		b.analyzer.loanCarrier(container.element) {
		return false
	}
	if _, converted := u.Sema.ImplicitConversions[rhsExpr]; converted {
		return false
	}
	target := b.ungroup(data.Target)
	node := u.Builder.Exprs.Get(target)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprIndex:
		if _, reason := b.analyzer.indexOperation(b.function, target); !container.reference || reason != "" {
			return false
		}
	case ast.ExprMember:
		member, ok := u.Builder.Exprs.Member(target)
		if _, converted := u.Sema.ImplicitConversions[target]; !ok || member == nil || converted {
			return false
		}
	default:
		return false
	}
	b.discardLoans(rhs, span)
	return true
}
