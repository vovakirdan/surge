package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

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
		if tc.isTaskContainerType(tc.bindingType(symID)) {
			tc.reportTaskContainerEscape(expr, node.Span)
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
	case ast.ExprSelect:
		if data, _ := tc.builder.Exprs.Select(expr); data != nil {
			for _, arm := range data.Arms {
				tc.scanSpawn(arm.Await, seen)
				tc.scanSpawn(arm.Result, seen)
			}
		}
	case ast.ExprRace:
		if data, _ := tc.builder.Exprs.Race(expr); data != nil {
			for _, arm := range data.Arms {
				tc.scanSpawn(arm.Await, seen)
				tc.scanSpawn(arm.Result, seen)
			}
		}
	case ast.ExprTask:
		if data, _ := tc.builder.Exprs.Task(expr); data != nil {
			tc.scanSpawn(data.Value, seen)
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
