package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

func (tc *typeChecker) enforceSpawn(expr ast.ExprID, allowNosend bool) {
	seen := make(map[symbols.SymbolID]struct{})
	tc.scanSpawn(expr, seen, allowNosend)
}

// noteSpawnOperandBorrow records a borrow taken while the spawn operand is being
// typed. It has to be recorded HERE, at creation, and not read back afterwards
// from the expression: a call-argument borrow is dropped when the call ends, and
// DropBorrow deletes the expression's entry, so by the time the operand has been
// typed the table no longer remembers that `spawn worker(&v)` borrowed anything.
// That deletion is why the inline form was invisible while the binding form was
// refused — the binding outlives the call and the temporary does not.
// Being inside the operand is necessary and NOT sufficient. In
// `spawn worker(peek(&v))` the reference is consumed by a synchronous call and
// the child receives an `int`, so nothing of the parent's storage travels with
// it; pinning `v` there refuses a sound program. spawnReachingBorrowExprs names
// the positions a reference can actually reach the child from.
func (tc *typeChecker) noteSpawnOperandBorrow(exprID ast.ExprID, borrow BorrowID, span source.Span) {
	if !tc.spawnOperand.IsValid() || !tc.spawnBorrowReaches(exprID) {
		return
	}
	tc.noteSpawnBorrowCapture(borrow, span)
}

// spawnBorrowReaches reports whether a borrow taken at exprID travels into the
// child. A nil set means "every borrow in this operand does", which is the right
// answer for an `async { ... }` body: the body IS the child, so a borrow taken
// anywhere inside it is the child's own.
func (tc *typeChecker) spawnBorrowReaches(exprID ast.ExprID) bool {
	if tc.spawnReachingExprs == nil {
		return true
	}
	if !exprID.IsValid() {
		return false
	}
	if _, ok := tc.spawnReachingExprs[exprID]; ok {
		return true
	}
	_, ok := tc.spawnReachingExprs[tc.unwrapGroupExpr(exprID)]
	return ok
}

// spawnReachingBorrowExprs names the expressions of a spawn operand whose borrow
// is handed to the child: the spawned call's own arguments, and its receiver
// when the call is a method. Returns nil when the operand is not a call, which
// means every borrow inside it counts.
func (tc *typeChecker) spawnReachingBorrowExprs(value ast.ExprID) map[ast.ExprID]struct{} {
	if tc.builder == nil {
		return nil
	}
	call, ok := tc.builder.Exprs.Call(tc.unwrapGroupExpr(value))
	if !ok || call == nil {
		return nil
	}
	out := make(map[ast.ExprID]struct{}, len(call.Args)+1)
	add := func(id ast.ExprID) {
		if !id.IsValid() {
			return
		}
		out[id] = struct{}{}
		out[tc.unwrapGroupExpr(id)] = struct{}{}
	}
	for _, arg := range call.Args {
		add(arg.Value)
	}
	// `spawn c.tick()` hands the receiver over exactly as an argument does.
	if member, ok := tc.builder.Exprs.Member(tc.unwrapGroupExpr(call.Target)); ok && member != nil {
		add(member.Target)
	}
	return out
}

// noteSpawnBorrowCapture records one borrowed place the spawn operand carries
// into the child. Both routes into a child are collected here because they are
// the same capture written two ways, and only one of them was ever visible: a
// borrow behind a NAMED BINDING reaches the ident branch below, while a borrow
// taken inline as `&v` in an argument is a temporary no binding holds, so it is
// found by asking the borrow table what this expression borrowed.
func (tc *typeChecker) noteSpawnBorrowCapture(borrow BorrowID, span source.Span) {
	if borrow == NoBorrowID || tc.borrow == nil {
		return
	}
	info := tc.borrow.Info(borrow)
	if info == nil || !info.Place.IsValid() {
		return
	}
	if span == (source.Span{}) {
		span = info.Span
	}
	for _, existing := range tc.spawnBorrowCaptures {
		if existing.Place == info.Place && existing.Borrow == borrow {
			return
		}
	}
	tc.spawnBorrowCaptures = append(tc.spawnBorrowCaptures, spawnBorrowCapture{
		Place:  info.Place,
		Borrow: borrow,
		Kind:   info.Kind,
		Span:   span,
	})
}

func (tc *typeChecker) scanSpawn(expr ast.ExprID, seen map[symbols.SymbolID]struct{}, allowNosend bool) {
	if !expr.IsValid() || tc.builder == nil {
		return
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return
	}
	if tc.borrow != nil {
		tc.noteSpawnBorrowCapture(tc.borrow.ExprBorrow(expr), node.Span)
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
			// Same reaching test as the inline form: `spawn worker(peek(r))`
			// hands the child a value, not r's referent.
			if tc.spawnBorrowReaches(expr) {
				tc.noteSpawnBorrowCapture(bid, node.Span)
			}
			tc.reportSpawnThreadEscape(symID, node.Span, bid)
		}
		if tc.isTaskContainerType(tc.bindingType(symID)) {
			tc.reportTaskContainerEscape(expr, node.Span)
		}
		// Check @nosend attribute
		if !allowNosend {
			tc.checkSpawnSendability(symID, node.Span)
		}
		return
	}
	switch node.Kind {
	case ast.ExprBinary:
		if data, _ := tc.builder.Exprs.Binary(expr); data != nil {
			tc.scanSpawn(data.Left, seen, allowNosend)
			tc.scanSpawn(data.Right, seen, allowNosend)
		}
	case ast.ExprUnary:
		if data, _ := tc.builder.Exprs.Unary(expr); data != nil {
			tc.scanSpawn(data.Operand, seen, allowNosend)
		}
	case ast.ExprGroup:
		if data, _ := tc.builder.Exprs.Group(expr); data != nil {
			tc.scanSpawn(data.Inner, seen, allowNosend)
		}
	case ast.ExprCall:
		if data, _ := tc.builder.Exprs.Call(expr); data != nil {
			tc.scanSpawn(data.Target, seen, allowNosend)
			for _, arg := range data.Args {
				tc.scanSpawn(arg.Value, seen, allowNosend)
			}
		}
	case ast.ExprTuple:
		if data, _ := tc.builder.Exprs.Tuple(expr); data != nil {
			for _, elem := range data.Elements {
				tc.scanSpawn(elem, seen, allowNosend)
			}
		}
	case ast.ExprArray:
		if data, _ := tc.builder.Exprs.Array(expr); data != nil {
			for _, elem := range data.Elements {
				tc.scanSpawn(elem, seen, allowNosend)
			}
		}
	case ast.ExprRangeLit:
		if data, _ := tc.builder.Exprs.RangeLit(expr); data != nil {
			tc.scanSpawn(data.Start, seen, allowNosend)
			tc.scanSpawn(data.End, seen, allowNosend)
		}
	case ast.ExprIndex:
		if data, _ := tc.builder.Exprs.Index(expr); data != nil {
			tc.scanSpawn(data.Target, seen, allowNosend)
			tc.scanSpawn(data.Index, seen, allowNosend)
		}
	case ast.ExprMember:
		if data, _ := tc.builder.Exprs.Member(expr); data != nil {
			tc.scanSpawn(data.Target, seen, allowNosend)
		}
	case ast.ExprAwait:
		if data, _ := tc.builder.Exprs.Await(expr); data != nil {
			tc.scanSpawn(data.Value, seen, allowNosend)
		}
	case ast.ExprSpread:
		if data, _ := tc.builder.Exprs.Spread(expr); data != nil {
			tc.scanSpawn(data.Value, seen, allowNosend)
		}
	case ast.ExprParallel:
		if data, _ := tc.builder.Exprs.Parallel(expr); data != nil {
			tc.scanSpawn(data.Iterable, seen, allowNosend)
			tc.scanSpawn(data.Init, seen, allowNosend)
			for _, arg := range data.Args {
				tc.scanSpawn(arg, seen, allowNosend)
			}
			tc.scanSpawn(data.Body, seen, allowNosend)
		}
	case ast.ExprCompare:
		if data, _ := tc.builder.Exprs.Compare(expr); data != nil {
			tc.scanSpawn(data.Value, seen, allowNosend)
			for _, arm := range data.Arms {
				tc.scanSpawn(arm.Pattern, seen, allowNosend)
				tc.scanSpawn(arm.Guard, seen, allowNosend)
				tc.scanSpawn(arm.Result, seen, allowNosend)
			}
		}
	case ast.ExprSelect:
		if data, _ := tc.builder.Exprs.Select(expr); data != nil {
			for _, arm := range data.Arms {
				tc.scanSpawn(arm.Await, seen, allowNosend)
				tc.scanSpawn(arm.Result, seen, allowNosend)
			}
		}
	case ast.ExprRace:
		if data, _ := tc.builder.Exprs.Race(expr); data != nil {
			for _, arm := range data.Arms {
				tc.scanSpawn(arm.Await, seen, allowNosend)
				tc.scanSpawn(arm.Result, seen, allowNosend)
			}
		}
	case ast.ExprTask:
		if data, _ := tc.builder.Exprs.Task(expr); data != nil {
			tc.scanSpawn(data.Value, seen, allowNosend)
		}
	case ast.ExprSpawn:
		if data, _ := tc.builder.Exprs.Spawn(expr); data != nil {
			tc.scanSpawn(data.Value, seen, allowNosend)
		}
	case ast.ExprAsync:
		if data, _ := tc.builder.Exprs.Async(expr); data != nil {
			// Scan async block body for captured @nosend variables
			tc.scanSpawnStmt(data.Body, seen, allowNosend)
		}
	}
}

// scanSpawnStmt recursively scans statements for @nosend captures
func (tc *typeChecker) scanSpawnStmt(stmtID ast.StmtID, seen map[symbols.SymbolID]struct{}, allowNosend bool) {
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
				tc.scanSpawnStmt(child, seen, allowNosend)
			}
		}
	case ast.StmtExpr:
		if data := tc.builder.Stmts.Expr(stmtID); data != nil {
			tc.scanSpawn(data.Expr, seen, allowNosend)
		}
	case ast.StmtLet:
		if data := tc.builder.Stmts.Let(stmtID); data != nil {
			tc.scanSpawn(data.Value, seen, allowNosend)
		}
	case ast.StmtConst:
		if data := tc.builder.Stmts.Const(stmtID); data != nil {
			tc.scanSpawn(data.Value, seen, allowNosend)
		}
	case ast.StmtReturn:
		if data := tc.builder.Stmts.Return(stmtID); data != nil {
			tc.scanSpawn(data.Expr, seen, allowNosend)
		}
	case ast.StmtRet:
		if data := tc.builder.Stmts.Ret(stmtID); data != nil {
			tc.scanSpawn(data.Expr, seen, allowNosend)
		}
	case ast.StmtSignal:
		if data := tc.builder.Stmts.Signal(stmtID); data != nil {
			tc.scanSpawn(data.Value, seen, allowNosend)
		}
	case ast.StmtDrop:
		if data := tc.builder.Stmts.Drop(stmtID); data != nil {
			tc.scanSpawn(data.Expr, seen, allowNosend)
		}
	case ast.StmtIf:
		if data := tc.builder.Stmts.If(stmtID); data != nil {
			tc.scanSpawn(data.Cond, seen, allowNosend)
			tc.scanSpawnStmt(data.Then, seen, allowNosend)
			tc.scanSpawnStmt(data.Else, seen, allowNosend)
		}
	case ast.StmtWhile:
		if data := tc.builder.Stmts.While(stmtID); data != nil {
			tc.scanSpawn(data.Cond, seen, allowNosend)
			tc.scanSpawnStmt(data.Body, seen, allowNosend)
		}
	case ast.StmtForIn:
		if data := tc.builder.Stmts.ForIn(stmtID); data != nil {
			tc.scanSpawn(data.Iterable, seen, allowNosend)
			tc.scanSpawnStmt(data.Body, seen, allowNosend)
		}
	case ast.StmtForClassic:
		if data := tc.builder.Stmts.ForClassic(stmtID); data != nil {
			tc.scanSpawnStmt(data.Init, seen, allowNosend)
			tc.scanSpawn(data.Cond, seen, allowNosend)
			tc.scanSpawn(data.Post, seen, allowNosend)
			tc.scanSpawnStmt(data.Body, seen, allowNosend)
		}
	}
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
