package sema

import (
	"strings"

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
	bid := tc.bindingBorrowForExpr(symID, expr)
	tc.bindingBorrow[symID] = bid
	if bid != NoBorrowID && tc.borrowBindings != nil {
		if _, exists := tc.borrowBindings[bid]; !exists {
			tc.borrowBindings[bid] = symID
		}
	}
}

func (tc *typeChecker) bindingBorrowForExpr(symID symbols.SymbolID, expr ast.ExprID) BorrowID {
	if tc.borrow == nil || !expr.IsValid() {
		return NoBorrowID
	}
	if bid := tc.borrow.ExprBorrow(expr); bid != NoBorrowID {
		return bid
	}

	boundType := tc.bindingType(symID)
	if boundType == types.NoTypeID && tc.result != nil && tc.result.ExprTypes != nil {
		boundType = tc.result.ExprTypes[expr]
	}
	if !tc.isReferenceType(boundType) {
		return NoBorrowID
	}

	return tc.inheritedBorrowForExpr(expr)
}

func (tc *typeChecker) inheritedBorrowForExpr(expr ast.ExprID) BorrowID {
	expr = tc.unwrapGroupExpr(expr)
	if tc.borrow == nil || !expr.IsValid() {
		return NoBorrowID
	}
	if bid := tc.borrow.ExprBorrow(expr); bid != NoBorrowID {
		return bid
	}
	if tc.builder == nil {
		return NoBorrowID
	}

	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return NoBorrowID
	}

	switch node.Kind {
	case ast.ExprIdent:
		symID := tc.symbolForExpr(expr)
		if !symID.IsValid() || tc.bindingBorrow == nil {
			return NoBorrowID
		}
		return tc.bindingBorrow[symID]
	case ast.ExprUnary:
		unary, ok := tc.builder.Exprs.Unary(expr)
		if !ok || unary == nil {
			return NoBorrowID
		}
		switch unary.Op {
		case ast.ExprUnaryDeref, ast.ExprUnaryOwn:
			return tc.inheritedBorrowForExpr(unary.Operand)
		}
	case ast.ExprCall:
		return tc.inheritedBorrowForCall(expr)
	}

	return NoBorrowID
}

func (tc *typeChecker) inheritedBorrowForCall(expr ast.ExprID) BorrowID {
	if tc.builder == nil || tc.result == nil || !expr.IsValid() {
		return NoBorrowID
	}
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		return NoBorrowID
	}
	if _, carries := tc.carriedReferenceType(tc.result.ExprTypes[expr]); !carries {
		return NoBorrowID
	}

	symID := tc.symbolForExpr(expr)
	if !symID.IsValid() {
		return NoBorrowID
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Signature == nil || len(sym.Signature.Params) == 0 {
		return NoBorrowID
	}

	candidates := make([]BorrowID, 0, 1)
	seen := make(map[BorrowID]struct{}, 1)
	addCandidate := func(param symbols.TypeKey, argExpr ast.ExprID) {
		if !tc.refResultCanAliasParam(tc.result.ExprTypes[expr], param) {
			return
		}
		bid := tc.inheritedBorrowForExpr(argExpr)
		if bid == NoBorrowID {
			return
		}
		if _, exists := seen[bid]; exists {
			return
		}
		seen[bid] = struct{}{}
		candidates = append(candidates, bid)
	}

	offset := 0
	if sym.Signature.HasSelf {
		offset = 1
		if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
			addCandidate(sym.Signature.Params[0], member.Target)
		}
	}
	for i, arg := range call.Args {
		paramIndex := i + offset
		if paramIndex >= len(sym.Signature.Params) {
			break
		}
		addCandidate(sym.Signature.Params[paramIndex], arg.Value)
	}

	if len(candidates) == 1 {
		return candidates[0]
	}
	return NoBorrowID
}

func (tc *typeChecker) refResultCanAliasParam(resultType types.TypeID, param symbols.TypeKey) bool {
	carried, carries := tc.carriedReferenceType(resultType)
	if !carries {
		return false
	}
	paramStr := strings.TrimSpace(string(param))
	if !strings.HasPrefix(paramStr, "&") {
		return false
	}
	if !tc.isMutRefType(carried) {
		return true
	}
	return strings.HasPrefix(paramStr, "&mut ")
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

// carriedReferenceType answers the reference a value of this type hands out.
//
// A borrow that a call returns has to be tracked against the receiver it came
// from, and the question that decides it is not "is the RESULT spelled as a
// reference" — it is "does the result CARRY one". `Map::get_mut` returns
// `Option<&mut V>`, so the spelling says union and the value says borrow, and
// asking the spelling let a caller hold a live element borrow while removing
// the entry under it. Both lanes then wrote into whichever entry the removal
// swapped into that slot, silently and with no diagnostic.
//
// This is the same shape as a member alignment taken from the member's own
// type: the answer belongs to what the value carries, not to how it is
// written. The walk is recursive because `Option<Option<&T>>` is expressible,
// and guarded because a union may name itself.
func (tc *typeChecker) carriedReferenceType(id types.TypeID) (types.TypeID, bool) {
	return tc.carriedReferenceTypeSeen(id, nil)
}

func (tc *typeChecker) carriedReferenceTypeSeen(id types.TypeID, seen map[types.TypeID]struct{}) (types.TypeID, bool) {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID, false
	}
	resolved := tc.resolveAlias(id)
	if tc.isReferenceType(resolved) {
		return resolved, true
	}
	if _, visited := seen[resolved]; visited {
		return types.NoTypeID, false
	}
	info, ok := tc.types.UnionInfo(resolved)
	if !ok || info == nil {
		return types.NoTypeID, false
	}
	if seen == nil {
		seen = make(map[types.TypeID]struct{}, 2)
	}
	seen[resolved] = struct{}{}
	for i := range info.Members {
		member := &info.Members[i]
		if inner, found := tc.carriedReferenceTypeSeen(member.Type, seen); found {
			return inner, true
		}
		for _, arg := range member.TagArgs {
			if inner, found := tc.carriedReferenceTypeSeen(arg, seen); found {
				return inner, true
			}
		}
	}
	return types.NoTypeID, false
}
