package mono

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/sema"
)

// The observer and cleanup filter share this inventory of expressions evaluated
// in the current activation. Defaults and nested activation bodies are excluded.
// prune is used only on a private clone; an observing walk never writes the HIR.
type borrowedBodyWalker struct {
	prune bool
	expr  func(*hir.Expr) error
	stmt  func(*hir.Stmt) (bool, error)
}

func (w *borrowedBodyWalker) block(block *hir.Block) error {
	if block == nil {
		return nil
	}
	n := 0
	for i := range block.Stmts {
		keep, err := w.statement(&block.Stmts[i])
		if err != nil {
			return err
		}
		if keep {
			if w.prune {
				block.Stmts[n] = block.Stmts[i]
			}
			n++
		}
	}
	if w.prune {
		clear(block.Stmts[n:])
		block.Stmts = block.Stmts[:n]
	}
	return nil
}

func (w *borrowedBodyWalker) statement(st *hir.Stmt) (bool, error) {
	if st == nil {
		return true, nil
	}
	var kind hir.StmtKind
	var exprs []*hir.Expr
	var blocks []*hir.Block
	switch data := st.Data.(type) {
	case hir.LetData:
		kind, exprs = hir.StmtLet, []*hir.Expr{data.Value, data.Pattern}
	case hir.ExprStmtData:
		kind, exprs = hir.StmtExpr, []*hir.Expr{data.Expr}
	case hir.AssignData:
		kind, exprs = hir.StmtAssign, []*hir.Expr{data.Target, data.Value}
		if data.Target == nil || data.Value == nil {
			return false, fmt.Errorf("assignment statement has a missing operand")
		}
	case hir.ReturnData:
		kind, exprs = hir.StmtReturn, []*hir.Expr{data.Value}
	case hir.RetData:
		kind, exprs = hir.StmtRet, []*hir.Expr{data.Value}
	case hir.BreakData:
		kind = hir.StmtBreak
	case hir.ContinueData:
		kind = hir.StmtContinue
	case hir.IfStmtData:
		kind, exprs, blocks = hir.StmtIf, []*hir.Expr{data.Cond}, []*hir.Block{data.Then, data.Else}
	case hir.WhileData:
		kind, exprs, blocks = hir.StmtWhile, []*hir.Expr{data.Cond, data.Post}, []*hir.Block{data.Body}
	case hir.ForData:
		kind, blocks = hir.StmtFor, []*hir.Block{data.Body}
		switch data.Kind {
		case hir.ForClassic:
			exprs = []*hir.Expr{data.Cond, data.Post}
			keep, err := w.statement(data.Init)
			if err != nil {
				return false, err
			}
			if w.prune && !keep {
				data.Init = nil
				st.Data = data
			}
		case hir.ForIn:
			exprs = []*hir.Expr{data.Iterable}
		default:
			return false, fmt.Errorf("unknown for kind %d", data.Kind)
		}
	case hir.BlockStmtData:
		kind, blocks = hir.StmtBlock, []*hir.Block{data.Block}
	case hir.DropData:
		kind, exprs = hir.StmtDrop, []*hir.Expr{data.Value}
		if data.Value == nil {
			return false, fmt.Errorf("drop statement has no value")
		}
	case hir.EnvelopeReleaseData:
		kind, exprs = hir.StmtEnvelopeRelease, []*hir.Expr{data.Value}
	default:
		return false, fmt.Errorf("unsupported statement kind %d payload %T", st.Kind, st.Data)
	}
	if kind != st.Kind {
		return false, fmt.Errorf("statement kind %d disagrees with payload %T", st.Kind, st.Data)
	}
	if err := w.children(exprs, blocks); err != nil {
		return false, err
	}
	if w.stmt != nil {
		return w.stmt(st)
	}
	return true, nil
}

func (w *borrowedBodyWalker) expression(expr *hir.Expr) error {
	if expr == nil {
		return nil
	}
	var kind hir.ExprKind
	var exprs []*hir.Expr
	var blocks []*hir.Block
	switch data := expr.Data.(type) {
	case hir.LiteralData:
		kind = hir.ExprLiteral
	case hir.VarRefData:
		kind = hir.ExprVarRef
	case hir.UnaryOpData:
		kind, exprs = hir.ExprUnaryOp, []*hir.Expr{data.Operand}
		if data.Operand == nil {
			return fmt.Errorf("unary expression has no operand")
		}
	case hir.BinaryOpData:
		kind, exprs = hir.ExprBinaryOp, []*hir.Expr{data.Left, data.Right}
		if data.Left == nil || data.Right == nil {
			return fmt.Errorf("binary expression has a missing operand")
		}
	case hir.CallData:
		kind = hir.ExprCall
		exprs = append([]*hir.Expr{data.Callee}, data.Args...)
	case hir.FieldAccessData:
		kind, exprs = hir.ExprFieldAccess, []*hir.Expr{data.Object}
	case hir.IndexData:
		kind, exprs = hir.ExprIndex, []*hir.Expr{data.Object, data.Index}
	case hir.StructLitData:
		kind = hir.ExprStructLit
		for _, field := range data.Fields {
			exprs = append(exprs, field.Value)
		}
	case hir.ArrayLitData:
		kind, exprs = hir.ExprArrayLit, data.Elements
	case hir.MapLitData:
		kind = hir.ExprMapLit
		for _, entry := range data.Entries {
			exprs = append(exprs, entry.Key, entry.Value)
		}
	case hir.TupleLitData:
		kind, exprs = hir.ExprTupleLit, data.Elements
	case hir.CompareData:
		kind, exprs = hir.ExprCompare, []*hir.Expr{data.Value}
		for _, arm := range data.Arms {
			exprs = append(exprs, arm.Pattern, arm.Guard, arm.Result)
		}
	case hir.SelectData:
		kind = hir.ExprSelect
		if expr.Kind == hir.ExprRace {
			kind = hir.ExprRace
		}
		if data.Crossing != nil {
			if data.Crossing.Kind != sema.CrossingLoweringChannelSelect {
				return fmt.Errorf("remote select has crossing kind %d", data.Crossing.Kind)
			}
			var err error
			exprs, err = borrowedCrossingOperands(*data.Crossing)
			if err != nil {
				return err
			}
		}
		for _, arm := range data.Arms {
			if data.Crossing == nil {
				exprs = append(exprs, arm.Await)
			}
			exprs = append(exprs, arm.Result)
		}
	case hir.TagTestData:
		kind, exprs = hir.ExprTagTest, []*hir.Expr{data.Value}
	case hir.TagPayloadData:
		kind, exprs = hir.ExprTagPayload, []*hir.Expr{data.Value}
	case hir.IterInitData:
		kind, exprs = hir.ExprIterInit, []*hir.Expr{data.Iterable}
	case hir.IterNextData:
		kind, exprs = hir.ExprIterNext, []*hir.Expr{data.Iter}
	case hir.IfData:
		kind, exprs = hir.ExprIf, []*hir.Expr{data.Cond, data.Then, data.Else}
	case hir.AwaitData:
		kind, exprs = hir.ExprAwait, []*hir.Expr{data.Value}
	case hir.TaskData:
		kind, exprs = hir.ExprTask, []*hir.Expr{data.Value}
	case hir.SpawnData:
		kind, exprs = hir.ExprSpawn, []*hir.Expr{data.Value}
	case hir.CrossingData:
		kind = hir.ExprCrossing
		var err error
		exprs, err = borrowedCrossingOperands(data)
		if err != nil {
			return err
		}
	case hir.AsyncData:
		kind = hir.ExprAsync // Body and captured bindings belong to another activation.
	case hir.BlockingData:
		kind = hir.ExprBlocking
	case hir.CastData:
		kind, exprs = hir.ExprCast, []*hir.Expr{data.Value}
	case hir.BlockExprData:
		kind, blocks = hir.ExprBlock, []*hir.Block{data.Block}
	case hir.OwnedTempData:
		kind, exprs = hir.ExprOwnedTemp, []*hir.Expr{data.Inner}
	case hir.RaiseReleaseGuardData:
		kind, exprs = hir.ExprRaiseReleaseGuard, []*hir.Expr{data.Inner}
	default:
		return fmt.Errorf("unsupported expression kind %d payload %T", expr.Kind, expr.Data)
	}
	if kind != expr.Kind {
		return fmt.Errorf("expression kind %d disagrees with payload %T", expr.Kind, expr.Data)
	}
	if err := w.children(exprs, blocks); err != nil {
		return err
	}
	if w.expr != nil {
		return w.expr(expr)
	}
	return nil
}

func (w *borrowedBodyWalker) children(exprs []*hir.Expr, blocks []*hir.Block) error {
	for _, expr := range exprs {
		if err := w.expression(expr); err != nil {
			return err
		}
	}
	for _, block := range blocks {
		if err := w.block(block); err != nil {
			return err
		}
	}
	return nil
}

func borrowedCrossingOperands(data hir.CrossingData) ([]*hir.Expr, error) {
	switch data.Kind {
	case sema.CrossingLoweringChannelSelect:
		// lowerRemoteSelect evaluates these in the caller. Its arm headers
		// are represented here, not in SelectArm.Await (hir.lowerSelectExpr).
		var exprs []*hir.Expr
		for _, op := range data.RemoteOps {
			exprs = append(exprs, op.Receiver, op.Value)
		}
		return exprs, nil
	case sema.CrossingLoweringOnPlacement, sema.CrossingLoweringOnFarHandle,
		sema.CrossingLoweringSpawnOn, sema.CrossingLoweringFarTaskAwait,
		sema.CrossingLoweringFarTaskCancel, sema.CrossingLoweringChannelCreate,
		sema.CrossingLoweringChannelShare:
		exprs := []*hir.Expr{data.Destination.Value, data.Receiver}
		for _, capture := range data.Captures {
			exprs = append(exprs, capture.Value)
		}
		// In particular, on/spawn-on Body and RemoteOps run in the child.
		return exprs, nil
	default:
		return nil, fmt.Errorf("unknown crossing kind %d", data.Kind)
	}
}
