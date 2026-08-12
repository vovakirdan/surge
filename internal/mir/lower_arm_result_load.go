package mir

import (
	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

// lowerValueExpr lowers an expression the consumer wants as a VALUE, loading
// through a reference when the expression produces one. It lives beside
// loadInsideDropCarryingBlock because the two are one rule: WHERE that load
// goes decides whether it reads live storage or freed storage.
func (l *funcLowerer) lowerValueExpr(e *hir.Expr, consume bool) (Operand, error) {
	if e == nil {
		return l.constNothing(types.NoTypeID), nil
	}
	if l.types != nil && e.Type != types.NoTypeID {
		if tt, ok := l.types.Lookup(resolveAlias(l.types, e.Type)); ok && tt.Kind == types.KindReference {
			// A block that frees on its way out must be LOADED THROUGH BEFORE
			// it frees, not after. Wrapping the deref around such a block reads
			// the referent once the block has exited — and a compare arm exits
			// by dropping the payload the reference points into.
			if loaded := loadInsideDropCarryingBlock(e, tt.Elem); loaded != nil {
				return l.lowerExpr(loaded, consume)
			}
			deref := &hir.Expr{
				Kind: hir.ExprUnaryOp,
				Type: tt.Elem,
				Span: e.Span,
				Data: hir.UnaryOpData{
					Op:      ast.ExprUnaryDeref,
					Operand: e,
				},
			}
			return l.lowerExpr(deref, consume)
		}
	}
	return l.lowerExpr(e, consume)
}

// loadInsideDropCarryingBlock rebuilds a drop-carrying block so its value is
// LOADED before the drops run, and returns nil for anything else.
//
// `compare values.pop() { Some(inner) => inner[0]; ... }` lowers the arm to a
// block whose `ret` carries the payload's drop, which is the right order for
// the VALUE — `wrapArmDrops` moved the drops after it for exactly that reason.
// It is not enough when the value is a REFERENCE. An indexed read stays a call
// returning `&T`, so the block yields a pointer, the payload frees on the way
// out, and the load the consumer wraps around the block then reads storage
// `rt_array_free_elems` has already returned. Measured natively as an
// `Invalid read of size 8` against a block freed by `array_free_base_storage`,
// printing freed memory and exiting 0.
//
// Only the load moves. No drop is added, removed or reordered, and ownership is
// untouched: the payload still belongs to the arm and is still freed by it. The
// instruction that used to run after the free now runs before it.
//
// The load stays a plain deref deliberately. Where the pointee itself owned
// heap storage this would be a shallow copy of something the drop then frees —
// but no such shape is admitted today (`string[][]` with an arm of `inner[0]`
// is refused earlier, "cannot assign &string to string"), and the status quo
// there is the strictly worse version of the same read. Whoever admits that
// shape owes this helper an owning read.
func loadInsideDropCarryingBlock(e *hir.Expr, elem types.TypeID) *hir.Expr {
	if e == nil || e.Kind != hir.ExprBlock {
		return nil
	}
	data, ok := e.Data.(hir.BlockExprData)
	if !ok || data.Block == nil || len(data.Block.Stmts) == 0 {
		return nil
	}
	last := len(data.Block.Stmts) - 1
	ret := data.Block.Stmts[last]
	if ret.Kind != hir.StmtRet {
		return nil
	}
	retData, ok := ret.Data.(hir.RetData)
	if !ok || retData.Value == nil || len(retData.DropsAfterValue) == 0 {
		return nil
	}
	// Only the block whose own value is the reference. A block yielding
	// something else that merely contains one is not this shape.
	if retData.Value.Type != e.Type {
		return nil
	}
	// HIR nodes are shared, so the rewrite happens on copies.
	stmts := make([]hir.Stmt, len(data.Block.Stmts))
	copy(stmts, data.Block.Stmts)
	retData.Value = &hir.Expr{
		Kind: hir.ExprUnaryOp,
		Type: elem,
		Span: retData.Value.Span,
		Data: hir.UnaryOpData{
			Op:      ast.ExprUnaryDeref,
			Operand: retData.Value,
		},
	}
	ret.Data = retData
	stmts[last] = ret
	return &hir.Expr{
		Kind: hir.ExprBlock,
		Type: elem,
		Span: e.Span,
		Data: hir.BlockExprData{Block: &hir.Block{Stmts: stmts, Span: data.Block.Span}},
	}
}
