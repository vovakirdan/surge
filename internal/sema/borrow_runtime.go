package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type placeDescriptor struct {
	Base     symbols.SymbolID
	Segments []PlaceSegment
}

func (tc *typeChecker) canonicalPlace(desc placeDescriptor) Place {
	if tc.borrow == nil || !desc.Base.IsValid() {
		return Place{}
	}
	return tc.borrow.CanonicalPlace(desc.Base, desc.Segments)
}

func (tc *typeChecker) expandPlaceDescriptor(desc placeDescriptor) (placeDescriptor, BorrowID) {
	if tc == nil || tc.borrow == nil {
		return desc, NoBorrowID
	}
	if tc.bindingBorrow == nil {
		return desc, NoBorrowID
	}
	visited := make(map[symbols.SymbolID]struct{})
	var parent BorrowID
	for {
		if !desc.Base.IsValid() {
			return desc, parent
		}
		if _, ok := visited[desc.Base]; ok {
			return desc, parent
		}
		visited[desc.Base] = struct{}{}
		bid := tc.bindingBorrow[desc.Base]
		if bid == NoBorrowID {
			return desc, parent
		}
		info := tc.borrow.Info(bid)
		if info == nil {
			return desc, parent
		}
		if parent == NoBorrowID {
			parent = bid
		}
		baseSegs := tc.borrow.placeSegments(info.Place)
		desc = placeDescriptor{
			Base:     info.Place.Base,
			Segments: append(baseSegs, desc.Segments...),
		}
	}
}

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

func (tc *typeChecker) observeMove(expr ast.ExprID, span source.Span) {
	if !expr.IsValid() || tc.borrow == nil {
		return
	}

	// Skip move tracking for Copy types - they can be implicitly copied
	// and the original value remains valid after the "copy".
	exprType := tc.result.ExprTypes[expr]
	if tc.isCopyType(exprType) {
		return
	}
	if tc.isSharedRefDeref(expr) {
		return
	}
	if tc.isRefReborrow(expr) {
		return
	}

	desc, ok := tc.resolvePlace(expr)
	if !ok {
		return
	}
	desc, _ = tc.expandPlaceDescriptor(desc)
	place := tc.canonicalPlace(desc)
	if !place.IsValid() {
		return
	}
	issue := tc.borrow.MoveAllowed(place)
	evSpan := span
	if evSpan == (source.Span{}) {
		evSpan = tc.exprSpan(expr)
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:        BorrowEvMove,
		Place:       place,
		Span:        evSpan,
		Scope:       tc.currentScope(),
		Issue:       issue.Kind,
		IssueBorrow: issue.Borrow,
	})
	if issue.Kind != BorrowIssueNone {
		if span == (source.Span{}) {
			span = evSpan
		}
		tc.reportBorrowMove(place, span, issue)
	}
}

func (tc *typeChecker) isSharedRefDeref(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil || tc.types == nil || tc.result == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil || node.Kind != ast.ExprUnary {
		return false
	}
	unary, ok := tc.builder.Exprs.Unary(expr)
	if !ok || unary == nil || unary.Op != ast.ExprUnaryDeref {
		return false
	}
	operandType := tc.result.ExprTypes[unary.Operand]
	if operandType == types.NoTypeID {
		return false
	}
	operandType = tc.resolveAlias(operandType)
	tt, ok := tc.types.Lookup(operandType)
	if !ok || tt.Kind != types.KindReference {
		return false
	}
	return !tt.Mutable
}

func (tc *typeChecker) isRefReborrow(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil || tc.types == nil || tc.result == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil || node.Kind != ast.ExprUnary {
		return false
	}
	unary, ok := tc.builder.Exprs.Unary(expr)
	if !ok || unary == nil {
		return false
	}
	if unary.Op != ast.ExprUnaryRef && unary.Op != ast.ExprUnaryRefMut {
		return false
	}
	innerNode := tc.builder.Exprs.Get(unary.Operand)
	if innerNode == nil || innerNode.Kind != ast.ExprUnary {
		return false
	}
	innerUnary, ok := tc.builder.Exprs.Unary(unary.Operand)
	if !ok || innerUnary == nil || innerUnary.Op != ast.ExprUnaryDeref {
		return false
	}
	operandType := tc.result.ExprTypes[innerUnary.Operand]
	if operandType == types.NoTypeID {
		return false
	}
	operandType = tc.resolveAlias(operandType)
	tt, ok := tc.types.Lookup(operandType)
	if !ok || tt.Kind != types.KindReference {
		return false
	}
	return true
}

func (tc *typeChecker) exprSpan(id ast.ExprID) source.Span {
	if !id.IsValid() || tc.builder == nil || tc.builder.Exprs == nil {
		return source.Span{}
	}
	expr := tc.builder.Exprs.Get(id)
	if expr == nil {
		return source.Span{}
	}
	return expr.Span
}

func (tc *typeChecker) resolvePlace(expr ast.ExprID) (placeDescriptor, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return placeDescriptor{}, false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return placeDescriptor{}, false
	}
	switch node.Kind {
	case ast.ExprIdent:
		symID := tc.symbolForExpr(expr)
		if !symID.IsValid() {
			return placeDescriptor{}, false
		}
		sym := tc.symbolFromID(symID)
		if sym == nil {
			return placeDescriptor{}, false
		}
		if sym.Kind != symbols.SymbolLet && sym.Kind != symbols.SymbolParam {
			return placeDescriptor{}, false
		}
		return placeDescriptor{Base: symID}, true
	case ast.ExprMember:
		member, ok := tc.builder.Exprs.Member(expr)
		if !ok || member == nil {
			return placeDescriptor{}, false
		}
		desc, ok := tc.resolvePlace(member.Target)
		if !ok {
			return placeDescriptor{}, false
		}
		desc.Segments = append(desc.Segments, PlaceSegment{
			Kind: PlaceSegmentField,
			Name: member.Field,
		})
		return desc, true
	case ast.ExprIndex:
		index, ok := tc.builder.Exprs.Index(expr)
		if !ok || index == nil {
			return placeDescriptor{}, false
		}
		desc, ok := tc.resolvePlace(index.Target)
		if !ok {
			return placeDescriptor{}, false
		}
		desc.Segments = append(desc.Segments, PlaceSegment{Kind: PlaceSegmentIndex})
		return desc, true
	case ast.ExprGroup:
		group, ok := tc.builder.Exprs.Group(expr)
		if !ok || group == nil {
			return placeDescriptor{}, false
		}
		return tc.resolvePlace(group.Inner)
	case ast.ExprUnary:
		unary, ok := tc.builder.Exprs.Unary(expr)
		if !ok || unary == nil {
			return placeDescriptor{}, false
		}
		if unary.Op != ast.ExprUnaryDeref {
			return placeDescriptor{}, false
		}
		desc, ok := tc.resolvePlace(unary.Operand)
		if !ok {
			return placeDescriptor{}, false
		}
		desc.Segments = append(desc.Segments, PlaceSegment{Kind: PlaceSegmentDeref})
		return desc, true
	default:
		return placeDescriptor{}, false
	}
}

func (tc *typeChecker) symbolForExpr(id ast.ExprID) symbols.SymbolID {
	if tc.symbols == nil || tc.symbols.ExprSymbols == nil {
		return symbols.NoSymbolID
	}
	if sym, ok := tc.symbols.ExprSymbols[id]; ok {
		return sym
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) ensureMutablePlace(place Place, span source.Span) bool {
	if !place.IsValid() {
		return false
	}
	sym := tc.symbolFromID(place.Base)
	if sym == nil {
		return false
	}
	if sym.Flags&symbols.SymbolFlagMutable == 0 {
		tc.report(diag.SemaBorrowImmutable, span, "cannot take mutable borrow of %s", tc.placeLabel(place))
		return false
	}
	return true
}

func (tc *typeChecker) handleBorrow(exprID ast.ExprID, span source.Span, op ast.ExprUnaryOp, operand ast.ExprID) {
	if tc.borrow == nil {
		return
	}
	desc, ok := tc.resolvePlace(operand)
	if !ok {
		tc.report(diag.SemaBorrowNonAddressable, span, "expression is not addressable")
		return
	}
	desc, parent := tc.expandPlaceDescriptor(desc)
	place := tc.canonicalPlace(desc)
	if !place.IsValid() {
		return
	}
	scope := tc.currentScope()
	if !scope.IsValid() {
		return
	}
	kind := BorrowShared
	if op == ast.ExprUnaryRefMut {
		if !tc.ensureMutablePlace(place, span) {
			return
		}
		kind = BorrowMut
	}
	bid, issue := tc.borrow.BeginBorrow(exprID, span, kind, place, scope, parent)
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:        BorrowEvBorrowStart,
		Borrow:      bid,
		BorrowKind:  kind,
		Place:       place,
		Span:        span,
		Scope:       scope,
		Issue:       issue.Kind,
		IssueBorrow: issue.Borrow,
	})
	if issue.Kind != BorrowIssueNone {
		tc.reportBorrowConflict(place, span, issue, kind)
	}
}

func (tc *typeChecker) handleAssignment(op ast.ExprBinaryOp, left, right ast.ExprID, span source.Span) {
	// Check @readonly attribute before allowing assignment
	if tc.checkReadonlyFieldWrite(left, span) {
		return // @readonly violation reported
	}

	desc, ok := tc.resolvePlace(left)
	if !ok {
		return
	}

	// Check if this is a write through a mutable reference binding (*r = value).
	// In this case, we should NOT expand through the borrow because writing
	// through &mut is allowed - that's the whole point of exclusive borrows.
	writeThroughMutRef := tc.isWriteThroughMutRef(desc)

	if !writeThroughMutRef {
		desc, _ = tc.expandPlaceDescriptor(desc)
	}
	place := tc.canonicalPlace(desc)
	if !place.IsValid() {
		return
	}
	var issue BorrowIssue
	if tc.borrow != nil && !writeThroughMutRef {
		// Only check for mutation conflicts if not writing through a &mut reference.
		// Writes through &mut references are allowed by design.
		issue = tc.borrow.MutationAllowed(place)
		tc.recordBorrowEvent(&BorrowEvent{
			Kind:        BorrowEvWrite,
			Place:       place,
			Span:        span,
			Scope:       tc.currentScope(),
			Issue:       issue.Kind,
			IssueBorrow: issue.Borrow,
		})
		if issue.Kind != BorrowIssueNone {
			tc.reportBorrowMutation(place, span, issue)
		}
	} else if tc.borrow != nil {
		// Still record the write event for diagnostics/debugging
		tc.recordBorrowEvent(&BorrowEvent{
			Kind:  BorrowEvWrite,
			Place: place,
			Span:  span,
			Scope: tc.currentScope(),
			Note:  "write_through_mut_ref",
		})
	}
	if op == ast.ExprBinaryAssign {
		tc.observeMove(right, tc.exprSpan(right))
		if !writeThroughMutRef {
			tc.updateBindingValue(place.Base, right)
		}
		return
	}
	if tc.bindingBorrow != nil && !writeThroughMutRef {
		tc.bindingBorrow[place.Base] = NoBorrowID
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
	// Check if the first segment is a deref (i.e., *base)
	if desc.Segments[0].Kind != PlaceSegmentDeref {
		return false
	}
	// Check if the base binding has a mutable reference type
	sym := tc.symbolFromID(desc.Base)
	if sym == nil {
		return false
	}
	ty := tc.result.BindingTypes[desc.Base]
	if ty == types.NoTypeID {
		ty = sym.Type
	}
	if ty == types.NoTypeID {
		return false
	}
	return tc.isMutRefType(ty)
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

func (tc *typeChecker) enforceSpawn(expr ast.ExprID) {
	if len(tc.bindingBorrow) == 0 {
		return
	}
	seen := make(map[symbols.SymbolID]struct{})
	tc.scanSpawn(expr, seen)
}

func (tc *typeChecker) scanSpawn(expr ast.ExprID, seen map[symbols.SymbolID]struct{}) {
	if !expr.IsValid() || tc.builder == nil {
		return
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return
	}
	if node.Kind == ast.ExprIdent {
		symID := tc.symbolForExpr(expr)
		if !symID.IsValid() {
			return
		}
		if seen != nil {
			if _, ok := seen[symID]; ok {
				return
			}
		}
		bid := tc.bindingBorrow[symID]
		if bid != NoBorrowID {
			if seen != nil {
				seen[symID] = struct{}{}
			}
			var place Place
			if tc.borrow != nil {
				if info := tc.borrow.Info(bid); info != nil {
					place = info.Place
				}
			}
			tc.recordBorrowEvent(&BorrowEvent{
				Kind:    BorrowEvSpawnEscape,
				Borrow:  bid,
				Place:   place,
				Binding: symID,
				Span:    node.Span,
				Scope:   tc.currentScope(),
			})
			tc.reportSpawnThreadEscape(symID, node.Span, bid)
		}
		// Check @nosend attribute
		tc.checkSpawnSendability(symID, node.Span)
		return
	}
	switch node.Kind {
	case ast.ExprBinary:
		if data, _ := tc.builder.Exprs.Binary(expr); data != nil {
			tc.scanSpawn(data.Left, seen)
			tc.scanSpawn(data.Right, seen)
		}
	case ast.ExprUnary:
		if data, _ := tc.builder.Exprs.Unary(expr); data != nil {
			tc.scanSpawn(data.Operand, seen)
		}
	case ast.ExprGroup:
		if data, _ := tc.builder.Exprs.Group(expr); data != nil {
			tc.scanSpawn(data.Inner, seen)
		}
	case ast.ExprCall:
		if data, _ := tc.builder.Exprs.Call(expr); data != nil {
			tc.scanSpawn(data.Target, seen)
			for _, arg := range data.Args {
				tc.scanSpawn(arg.Value, seen)
			}
		}
	case ast.ExprTuple:
		if data, _ := tc.builder.Exprs.Tuple(expr); data != nil {
			for _, elem := range data.Elements {
				tc.scanSpawn(elem, seen)
			}
		}
	case ast.ExprArray:
		if data, _ := tc.builder.Exprs.Array(expr); data != nil {
			for _, elem := range data.Elements {
				tc.scanSpawn(elem, seen)
			}
		}
	case ast.ExprRangeLit:
		if data, _ := tc.builder.Exprs.RangeLit(expr); data != nil {
			tc.scanSpawn(data.Start, seen)
			tc.scanSpawn(data.End, seen)
		}
	case ast.ExprIndex:
		if data, _ := tc.builder.Exprs.Index(expr); data != nil {
			tc.scanSpawn(data.Target, seen)
			tc.scanSpawn(data.Index, seen)
		}
	case ast.ExprMember:
		if data, _ := tc.builder.Exprs.Member(expr); data != nil {
			tc.scanSpawn(data.Target, seen)
		}
	case ast.ExprAwait:
		if data, _ := tc.builder.Exprs.Await(expr); data != nil {
			tc.scanSpawn(data.Value, seen)
		}
	case ast.ExprSpread:
		if data, _ := tc.builder.Exprs.Spread(expr); data != nil {
			tc.scanSpawn(data.Value, seen)
		}
	case ast.ExprParallel:
		if data, _ := tc.builder.Exprs.Parallel(expr); data != nil {
			tc.scanSpawn(data.Iterable, seen)
			tc.scanSpawn(data.Init, seen)
			for _, arg := range data.Args {
				tc.scanSpawn(arg, seen)
			}
			tc.scanSpawn(data.Body, seen)
		}
	case ast.ExprCompare:
		if data, _ := tc.builder.Exprs.Compare(expr); data != nil {
			tc.scanSpawn(data.Value, seen)
			for _, arm := range data.Arms {
				tc.scanSpawn(arm.Pattern, seen)
				tc.scanSpawn(arm.Guard, seen)
				tc.scanSpawn(arm.Result, seen)
			}
		}
	case ast.ExprSpawn:
		if data, _ := tc.builder.Exprs.Spawn(expr); data != nil {
			tc.scanSpawn(data.Value, seen)
		}
	case ast.ExprAsync:
		if data, _ := tc.builder.Exprs.Async(expr); data != nil {
			// Scan async block body for captured @nosend variables
			tc.scanSpawnStmt(data.Body, seen)
		}
	}
}

// scanSpawnStmt recursively scans statements for @nosend captures
func (tc *typeChecker) scanSpawnStmt(stmtID ast.StmtID, seen map[symbols.SymbolID]struct{}) {
	if !stmtID.IsValid() || tc.builder == nil {
		return
	}
	stmt := tc.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return
	}
	switch stmt.Kind {
	case ast.StmtBlock:
		if data := tc.builder.Stmts.Block(stmtID); data != nil {
			for _, child := range data.Stmts {
				tc.scanSpawnStmt(child, seen)
			}
		}
	case ast.StmtExpr:
		if data := tc.builder.Stmts.Expr(stmtID); data != nil {
			tc.scanSpawn(data.Expr, seen)
		}
	case ast.StmtLet:
		if data := tc.builder.Stmts.Let(stmtID); data != nil {
			tc.scanSpawn(data.Value, seen)
		}
	case ast.StmtConst:
		if data := tc.builder.Stmts.Const(stmtID); data != nil {
			tc.scanSpawn(data.Value, seen)
		}
	case ast.StmtReturn:
		if data := tc.builder.Stmts.Return(stmtID); data != nil {
			tc.scanSpawn(data.Expr, seen)
		}
	case ast.StmtSignal:
		if data := tc.builder.Stmts.Signal(stmtID); data != nil {
			tc.scanSpawn(data.Value, seen)
		}
	case ast.StmtDrop:
		if data := tc.builder.Stmts.Drop(stmtID); data != nil {
			tc.scanSpawn(data.Expr, seen)
		}
	case ast.StmtIf:
		if data := tc.builder.Stmts.If(stmtID); data != nil {
			tc.scanSpawn(data.Cond, seen)
			tc.scanSpawnStmt(data.Then, seen)
			tc.scanSpawnStmt(data.Else, seen)
		}
	case ast.StmtWhile:
		if data := tc.builder.Stmts.While(stmtID); data != nil {
			tc.scanSpawn(data.Cond, seen)
			tc.scanSpawnStmt(data.Body, seen)
		}
	case ast.StmtForIn:
		if data := tc.builder.Stmts.ForIn(stmtID); data != nil {
			tc.scanSpawn(data.Iterable, seen)
			tc.scanSpawnStmt(data.Body, seen)
		}
	case ast.StmtForClassic:
		if data := tc.builder.Stmts.ForClassic(stmtID); data != nil {
			tc.scanSpawnStmt(data.Init, seen)
			tc.scanSpawn(data.Cond, seen)
			tc.scanSpawn(data.Post, seen)
			tc.scanSpawnStmt(data.Body, seen)
		}
	}
}

func (tc *typeChecker) handleDrop(expr ast.ExprID, span source.Span) {
	tc.typeExpr(expr)
	symID := tc.symbolForExpr(expr)
	if !symID.IsValid() {
		tc.report(diag.SemaBorrowNonAddressable, span, "drop target must be a binding")
		return
	}
	bid := tc.bindingBorrow[symID]
	if bid == NoBorrowID {
		tc.recordBorrowEvent(&BorrowEvent{
			Kind:    BorrowEvDrop,
			Binding: symID,
			Span:    span,
			Scope:   tc.currentScope(),
			Note:    "drop",
		})
		return
	}
	var place Place
	if tc.borrow != nil {
		if info := tc.borrow.Info(bid); info != nil {
			place = info.Place
		}
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:    BorrowEvDrop,
		Borrow:  bid,
		Place:   place,
		Binding: symID,
		Span:    span,
		Scope:   tc.currentScope(),
	})
	if tc.borrow != nil {
		tc.borrow.DropBorrow(bid)
	}
	tc.bindingBorrow[symID] = NoBorrowID
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:    BorrowEvBorrowEnd,
		Borrow:  bid,
		Place:   place,
		Binding: symID,
		Span:    span,
		Scope:   tc.currentScope(),
		Note:    "drop",
	})
}

func (tc *typeChecker) recordBorrowEvent(ev *BorrowEvent) {
	if tc == nil || ev == nil {
		return
	}
	tc.borrowEvents = append(tc.borrowEvents, *ev)
}

func (tc *typeChecker) symbolFromID(id symbols.SymbolID) *symbols.Symbol {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return nil
	}
	return tc.symbols.Table.Symbols.Get(id)
}

func (tc *typeChecker) lookupName(id source.StringID) string {
	if id == source.NoStringID {
		return ""
	}
	if tc.builder != nil && tc.builder.StringsInterner != nil {
		return tc.builder.StringsInterner.MustLookup(id)
	}
	if tc.symbols != nil && tc.symbols.Table != nil && tc.symbols.Table.Strings != nil {
		return tc.symbols.Table.Strings.MustLookup(id)
	}
	return ""
}
