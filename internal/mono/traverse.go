package mono

import (
	"surge/internal/hir"
)

type callRewriteFunc func(call *hir.Expr, data *hir.CallData) error
type varRefRewriteFunc func(expr *hir.Expr, data *hir.VarRefData) error

func rewriteCallsInBlock(b *hir.Block, f callRewriteFunc) error {
	if b == nil || f == nil {
		return nil
	}
	for i := range b.Stmts {
		if err := rewriteCallsInStmt(&b.Stmts[i], f); err != nil {
			return err
		}
	}
	return nil
}

func rewriteCallsInStmt(st *hir.Stmt, f callRewriteFunc) error {
	if st == nil || f == nil {
		return nil
	}
	switch st.Kind {
	case hir.StmtLet:
		data, ok := st.Data.(hir.LetData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		if err := rewriteCallsInExpr(data.Pattern, f); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtExpr:
		data, ok := st.Data.(hir.ExprStmtData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Expr, f); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtAssign:
		data, ok := st.Data.(hir.AssignData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Target, f); err != nil {
			return err
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtReturn:
		data, ok := st.Data.(hir.ReturnData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtIf:
		data, ok := st.Data.(hir.IfStmtData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Cond, f); err != nil {
			return err
		}
		if err := rewriteCallsInBlock(data.Then, f); err != nil {
			return err
		}
		if err := rewriteCallsInBlock(data.Else, f); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtWhile:
		data, ok := st.Data.(hir.WhileData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Cond, f); err != nil {
			return err
		}
		if err := rewriteCallsInBlock(data.Body, f); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtFor:
		data, ok := st.Data.(hir.ForData)
		if !ok {
			return nil
		}
		if data.Init != nil {
			if err := rewriteCallsInStmt(data.Init, f); err != nil {
				return err
			}
		}
		if err := rewriteCallsInExpr(data.Cond, f); err != nil {
			return err
		}
		if err := rewriteCallsInExpr(data.Post, f); err != nil {
			return err
		}
		if err := rewriteCallsInExpr(data.Iterable, f); err != nil {
			return err
		}
		if err := rewriteCallsInBlock(data.Body, f); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtBlock:
		data, ok := st.Data.(hir.BlockStmtData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInBlock(data.Block, f); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtDrop:
		data, ok := st.Data.(hir.DropData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		st.Data = data
	default:
	}
	return nil
}

func rewriteCallsInExpr(e *hir.Expr, f callRewriteFunc) error {
	if e == nil || f == nil {
		return nil
	}
	switch e.Kind {
	case hir.ExprUnaryOp:
		data, ok := e.Data.(hir.UnaryOpData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Operand, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprBinaryOp:
		data, ok := e.Data.(hir.BinaryOpData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Left, f); err != nil {
			return err
		}
		if err := rewriteCallsInExpr(data.Right, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprCall:
		data, ok := e.Data.(hir.CallData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Callee, f); err != nil {
			return err
		}
		for i := range data.Args {
			if err := rewriteCallsInExpr(data.Args[i], f); err != nil {
				return err
			}
		}
		if err := f(e, &data); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprFieldAccess:
		data, ok := e.Data.(hir.FieldAccessData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Object, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprIndex:
		data, ok := e.Data.(hir.IndexData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Object, f); err != nil {
			return err
		}
		if err := rewriteCallsInExpr(data.Index, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprStructLit:
		data, ok := e.Data.(hir.StructLitData)
		if !ok {
			return nil
		}
		for i := range data.Fields {
			if err := rewriteCallsInExpr(data.Fields[i].Value, f); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprArrayLit:
		data, ok := e.Data.(hir.ArrayLitData)
		if !ok {
			return nil
		}
		for i := range data.Elements {
			if err := rewriteCallsInExpr(data.Elements[i], f); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprMapLit:
		data, ok := e.Data.(hir.MapLitData)
		if !ok {
			return nil
		}
		for i := range data.Entries {
			if err := rewriteCallsInExpr(data.Entries[i].Key, f); err != nil {
				return err
			}
			if err := rewriteCallsInExpr(data.Entries[i].Value, f); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprTupleLit:
		data, ok := e.Data.(hir.TupleLitData)
		if !ok {
			return nil
		}
		for i := range data.Elements {
			if err := rewriteCallsInExpr(data.Elements[i], f); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprCompare:
		data, ok := e.Data.(hir.CompareData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		for i := range data.Arms {
			if err := rewriteCallsInExpr(data.Arms[i].Pattern, f); err != nil {
				return err
			}
			if err := rewriteCallsInExpr(data.Arms[i].Guard, f); err != nil {
				return err
			}
			if err := rewriteCallsInExpr(data.Arms[i].Result, f); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprTagTest:
		data, ok := e.Data.(hir.TagTestData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprTagPayload:
		data, ok := e.Data.(hir.TagPayloadData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprIterInit:
		data, ok := e.Data.(hir.IterInitData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Iterable, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprIterNext:
		data, ok := e.Data.(hir.IterNextData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Iter, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprIf:
		data, ok := e.Data.(hir.IfData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Cond, f); err != nil {
			return err
		}
		if err := rewriteCallsInExpr(data.Then, f); err != nil {
			return err
		}
		if err := rewriteCallsInExpr(data.Else, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprAwait:
		data, ok := e.Data.(hir.AwaitData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprTask:
		data, ok := e.Data.(hir.TaskData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprSpawn:
		data, ok := e.Data.(hir.SpawnData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprAsync:
		data, ok := e.Data.(hir.AsyncData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInBlock(data.Body, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprBlocking:
		data, ok := e.Data.(hir.BlockingData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInBlock(data.Body, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprCast:
		data, ok := e.Data.(hir.CastData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInExpr(data.Value, f); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprBlock:
		data, ok := e.Data.(hir.BlockExprData)
		if !ok {
			return nil
		}
		if err := rewriteCallsInBlock(data.Block, f); err != nil {
			return err
		}
		e.Data = data
	default:
	}
	return nil
}
