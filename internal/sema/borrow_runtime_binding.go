package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) updateStmtBinding(stmtID ast.StmtID, expr ast.ExprID) {
	if !expr.IsValid() {
		return
	}
	symID := tc.symbolForStmt(stmtID)
	tc.updateBindingValue(symID, expr)
}

func (tc *typeChecker) updateItemBinding(itemID ast.ItemID, expr ast.ExprID) {
	if tc.symbols == nil || tc.symbols.ItemSymbols == nil {
		return
	}
	syms := tc.symbols.ItemSymbols[itemID]
	if len(syms) == 0 {
		return
	}
	tc.updateBindingValue(syms[0], expr)
}

func (tc *typeChecker) updateBindingValue(symID symbols.SymbolID, expr ast.ExprID) {
	if !symID.IsValid() || tc.bindingBorrow == nil {
		return
	}
	if tc.borrow == nil {
		tc.bindingBorrow[symID] = NoBorrowID
		return
	}
	bid := tc.borrow.ExprBorrow(expr)
	tc.bindingBorrow[symID] = bid
	if bid != NoBorrowID && tc.borrowBindings != nil {
		if _, exists := tc.borrowBindings[bid]; !exists {
			tc.borrowBindings[bid] = symID
		}
	}
}

// isWriteThroughMutRef checks if the place descriptor represents a write through
// a mutable reference binding (i.e., *r = value where r: &mut T).
// This is allowed even when the underlying value has an active exclusive borrow,
// because the reference IS that borrow.
func (tc *typeChecker) isWriteThroughMutRef(desc placeDescriptor) bool {
	if !desc.Base.IsValid() || len(desc.Segments) == 0 {
		return false
	}
	// Check if the base binding has a mutable reference type.
	ty := tc.bindingType(desc.Base)
	if ty == types.NoTypeID || !tc.isMutRefType(ty) {
		return false
	}
	// If the first segment is a deref (i.e., *base), it's a direct write-through.
	if desc.Segments[0].Kind == PlaceSegmentDeref {
		return true
	}
	// For field/index access on &mut bindings, treat it as implicit deref.
	return true
}

// isMutRefType checks if a type is &mut T.
func (tc *typeChecker) isMutRefType(ty types.TypeID) bool {
	if tc.types == nil || ty == types.NoTypeID {
		return false
	}
	tt, ok := tc.types.Lookup(ty)
	if !ok {
		return false
	}
	return tt.Kind == types.KindReference && tt.Mutable
}

func (tc *typeChecker) ensureMutablePlace(place Place, span source.Span) bool {
	if !place.IsValid() {
		return false
	}
	if !tc.isMutableBinding(place.Base) {
		tc.report(diag.SemaBorrowImmutable, span, "cannot take mutable borrow of %s", tc.placeLabel(place))
		return false
	}
	return true
}
