package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

type blockingCapture struct {
	symID  symbols.SymbolID
	exprID ast.ExprID
	span   source.Span
}

func (tc *typeChecker) collectBlockingCaptures(stmtID ast.StmtID) []blockingCapture {
	if tc == nil || tc.builder == nil || !stmtID.IsValid() {
		return nil
	}
	scopeSet := make(map[symbols.ScopeID]struct{}, len(tc.scopeStack))
	for _, scope := range tc.scopeStack {
		scopeSet[scope] = struct{}{}
	}
	seen := make(map[symbols.SymbolID]struct{})
	var captures []blockingCapture

	var scanExpr func(ast.ExprID)
	var scanStmt func(ast.StmtID)

	scanExpr = func(exprID ast.ExprID) {
		if !exprID.IsValid() {
			return
		}
		expr := tc.builder.Exprs.Get(exprID)
		if expr == nil {
			return
		}
		switch expr.Kind {
		case ast.ExprIdent:
			symID := tc.symbolForExpr(exprID)
			if !symID.IsValid() {
				return
			}
			if _, ok := seen[symID]; ok {
				return
			}
			sym := tc.symbolFromID(symID)
			if sym == nil {
				return
			}
			switch sym.Kind {
			case symbols.SymbolLet, symbols.SymbolConst, symbols.SymbolParam:
			default:
				return
			}
			if _, ok := scopeSet[sym.Scope]; !ok {
				return
			}
			seen[symID] = struct{}{}
			captures = append(captures, blockingCapture{
				symID:  symID,
				exprID: exprID,
				span:   expr.Span,
			})
			return
		case ast.ExprBinary:
			if data, ok := tc.builder.Exprs.Binary(exprID); ok && data != nil {
				scanExpr(data.Left)
				scanExpr(data.Right)
			}
		case ast.ExprUnary:
			if data, ok := tc.builder.Exprs.Unary(exprID); ok && data != nil {
				scanExpr(data.Operand)
			}
		case ast.ExprGroup:
			if data, ok := tc.builder.Exprs.Group(exprID); ok && data != nil {
				scanExpr(data.Inner)
			}
		case ast.ExprCall:
			if data, ok := tc.builder.Exprs.Call(exprID); ok && data != nil {
				scanExpr(data.Target)
				for _, arg := range data.Args {
					scanExpr(arg.Value)
				}
			}
		case ast.ExprTuple:
			if data, ok := tc.builder.Exprs.Tuple(exprID); ok && data != nil {
				for _, elem := range data.Elements {
					scanExpr(elem)
				}
			}
		case ast.ExprArray:
			if data, ok := tc.builder.Exprs.Array(exprID); ok && data != nil {
				for _, elem := range data.Elements {
					scanExpr(elem)
				}
			}
		case ast.ExprMap:
			if data, ok := tc.builder.Exprs.Map(exprID); ok && data != nil {
				for _, entry := range data.Entries {
					scanExpr(entry.Key)
					scanExpr(entry.Value)
				}
			}
		case ast.ExprRangeLit:
			if data, ok := tc.builder.Exprs.RangeLit(exprID); ok && data != nil {
				scanExpr(data.Start)
				scanExpr(data.End)
			}
		case ast.ExprIndex:
			if data, ok := tc.builder.Exprs.Index(exprID); ok && data != nil {
				scanExpr(data.Target)
				scanExpr(data.Index)
			}
		case ast.ExprMember:
			if data, ok := tc.builder.Exprs.Member(exprID); ok && data != nil {
				scanExpr(data.Target)
			}
		case ast.ExprAwait:
			if data, ok := tc.builder.Exprs.Await(exprID); ok && data != nil {
				scanExpr(data.Value)
			}
		case ast.ExprSpread:
			if data, ok := tc.builder.Exprs.Spread(exprID); ok && data != nil {
				scanExpr(data.Value)
			}
		case ast.ExprParallel:
			if data, ok := tc.builder.Exprs.Parallel(exprID); ok && data != nil {
				scanExpr(data.Iterable)
				scanExpr(data.Init)
				for _, arg := range data.Args {
					scanExpr(arg)
				}
				scanExpr(data.Body)
			}
		case ast.ExprCompare:
			if data, ok := tc.builder.Exprs.Compare(exprID); ok && data != nil {
				scanExpr(data.Value)
				for _, arm := range data.Arms {
					scanExpr(arm.Pattern)
					scanExpr(arm.Guard)
					scanExpr(arm.Result)
				}
			}
		case ast.ExprSelect:
			if data, ok := tc.builder.Exprs.Select(exprID); ok && data != nil {
				for _, arm := range data.Arms {
					scanExpr(arm.Await)
					scanExpr(arm.Result)
				}
			}
		case ast.ExprRace:
			if data, ok := tc.builder.Exprs.Race(exprID); ok && data != nil {
				for _, arm := range data.Arms {
					scanExpr(arm.Await)
					scanExpr(arm.Result)
				}
			}
		case ast.ExprTask:
			if data, ok := tc.builder.Exprs.Task(exprID); ok && data != nil {
				scanExpr(data.Value)
			}
		case ast.ExprSpawn:
			if data, ok := tc.builder.Exprs.Spawn(exprID); ok && data != nil {
				scanExpr(data.Value)
			}
		case ast.ExprAsync:
			if data, ok := tc.builder.Exprs.Async(exprID); ok && data != nil {
				scanStmt(data.Body)
			}
		case ast.ExprBlocking:
			if data, ok := tc.builder.Exprs.Blocking(exprID); ok && data != nil {
				scanStmt(data.Body)
			}
		case ast.ExprCast:
			if data, ok := tc.builder.Exprs.Cast(exprID); ok && data != nil {
				scanExpr(data.Value)
			}
		case ast.ExprTernary:
			if data, ok := tc.builder.Exprs.Ternary(exprID); ok && data != nil {
				scanExpr(data.Cond)
				scanExpr(data.TrueExpr)
				scanExpr(data.FalseExpr)
			}
		case ast.ExprStruct:
			if data, ok := tc.builder.Exprs.Struct(exprID); ok && data != nil {
				for _, field := range data.Fields {
					scanExpr(field.Value)
				}
			}
		case ast.ExprBlock:
			if data, ok := tc.builder.Exprs.Block(exprID); ok && data != nil {
				for _, stmt := range data.Stmts {
					scanStmt(stmt)
				}
			}
		}
	}

	scanStmt = func(id ast.StmtID) {
		if !id.IsValid() {
			return
		}
		stmt := tc.builder.Stmts.Get(id)
		if stmt == nil {
			return
		}
		switch stmt.Kind {
		case ast.StmtBlock:
			if data := tc.builder.Stmts.Block(id); data != nil {
				for _, child := range data.Stmts {
					scanStmt(child)
				}
			}
		case ast.StmtExpr:
			if data := tc.builder.Stmts.Expr(id); data != nil {
				scanExpr(data.Expr)
			}
		case ast.StmtLet:
			if data := tc.builder.Stmts.Let(id); data != nil {
				scanExpr(data.Pattern)
				scanExpr(data.Value)
			}
		case ast.StmtConst:
			if data := tc.builder.Stmts.Const(id); data != nil {
				scanExpr(data.Value)
			}
		case ast.StmtReturn:
			if data := tc.builder.Stmts.Return(id); data != nil {
				scanExpr(data.Expr)
			}
		case ast.StmtSignal:
			if data := tc.builder.Stmts.Signal(id); data != nil {
				scanExpr(data.Value)
			}
		case ast.StmtDrop:
			if data := tc.builder.Stmts.Drop(id); data != nil {
				scanExpr(data.Expr)
			}
		case ast.StmtIf:
			if data := tc.builder.Stmts.If(id); data != nil {
				scanExpr(data.Cond)
				scanStmt(data.Then)
				scanStmt(data.Else)
			}
		case ast.StmtWhile:
			if data := tc.builder.Stmts.While(id); data != nil {
				scanExpr(data.Cond)
				scanStmt(data.Body)
			}
		case ast.StmtForIn:
			if data := tc.builder.Stmts.ForIn(id); data != nil {
				scanExpr(data.Iterable)
				scanStmt(data.Body)
			}
		case ast.StmtForClassic:
			if data := tc.builder.Stmts.ForClassic(id); data != nil {
				scanStmt(data.Init)
				scanExpr(data.Cond)
				scanExpr(data.Post)
				scanStmt(data.Body)
			}
		}
	}

	scanStmt(stmtID)
	return captures
}

func (tc *typeChecker) recordBlockingCaptures(exprID ast.ExprID, captures []blockingCapture) {
	if tc == nil || tc.result == nil || tc.result.BlockingCaptures == nil || !exprID.IsValid() {
		return
	}
	if len(captures) == 0 {
		return
	}
	ids := make([]symbols.SymbolID, 0, len(captures))
	for _, cap := range captures {
		ids = append(ids, cap.symID)
	}
	tc.result.BlockingCaptures[exprID] = ids
}
