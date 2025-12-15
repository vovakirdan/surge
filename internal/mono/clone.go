package mono

import (
	"slices"

	"surge/internal/hir"
)

func cloneFunc(fn *hir.Func) *hir.Func {
	if fn == nil {
		return nil
	}
	out := *fn

	if len(fn.GenericParams) > 0 {
		out.GenericParams = make([]hir.GenericParam, len(fn.GenericParams))
		for i, gp := range fn.GenericParams {
			out.GenericParams[i] = gp
			if len(gp.Bounds) > 0 {
				out.GenericParams[i].Bounds = slices.Clone(gp.Bounds)
			}
		}
	}

	if len(fn.Params) > 0 {
		out.Params = make([]hir.Param, len(fn.Params))
		for i, p := range fn.Params {
			out.Params[i] = p
			if p.Default != nil {
				out.Params[i].Default = cloneExpr(p.Default)
			}
		}
	}

	if fn.Body != nil {
		out.Body = cloneBlock(fn.Body)
	}

	out.Borrow = nil
	out.MovePlan = nil
	return &out
}

func cloneBlock(b *hir.Block) *hir.Block {
	if b == nil {
		return nil
	}
	out := &hir.Block{Span: b.Span}
	if len(b.Stmts) == 0 {
		return out
	}
	out.Stmts = make([]hir.Stmt, len(b.Stmts))
	for i := range b.Stmts {
		out.Stmts[i] = cloneStmt(b.Stmts[i])
	}
	return out
}

func cloneStmt(s hir.Stmt) hir.Stmt {
	out := s
	switch s.Kind {
	case hir.StmtLet:
		data, ok := s.Data.(hir.LetData)
		if !ok {
			return out
		}
		if data.Value != nil {
			data.Value = cloneExpr(data.Value)
		}
		if data.Pattern != nil {
			data.Pattern = cloneExpr(data.Pattern)
		}
		out.Data = data
	case hir.StmtExpr:
		data, ok := s.Data.(hir.ExprStmtData)
		if !ok {
			return out
		}
		if data.Expr != nil {
			data.Expr = cloneExpr(data.Expr)
		}
		out.Data = data
	case hir.StmtAssign:
		data, ok := s.Data.(hir.AssignData)
		if !ok {
			return out
		}
		if data.Target != nil {
			data.Target = cloneExpr(data.Target)
		}
		if data.Value != nil {
			data.Value = cloneExpr(data.Value)
		}
		out.Data = data
	case hir.StmtReturn:
		data, ok := s.Data.(hir.ReturnData)
		if !ok {
			return out
		}
		if data.Value != nil {
			data.Value = cloneExpr(data.Value)
		}
		out.Data = data
	case hir.StmtIf:
		data, ok := s.Data.(hir.IfStmtData)
		if !ok {
			return out
		}
		if data.Cond != nil {
			data.Cond = cloneExpr(data.Cond)
		}
		data.Then = cloneBlock(data.Then)
		data.Else = cloneBlock(data.Else)
		out.Data = data
	case hir.StmtWhile:
		data, ok := s.Data.(hir.WhileData)
		if !ok {
			return out
		}
		if data.Cond != nil {
			data.Cond = cloneExpr(data.Cond)
		}
		data.Body = cloneBlock(data.Body)
		out.Data = data
	case hir.StmtFor:
		data, ok := s.Data.(hir.ForData)
		if !ok {
			return out
		}
		if data.Init != nil {
			initClone := cloneStmt(*data.Init)
			data.Init = &initClone
		}
		if data.Cond != nil {
			data.Cond = cloneExpr(data.Cond)
		}
		if data.Post != nil {
			data.Post = cloneExpr(data.Post)
		}
		if data.Iterable != nil {
			data.Iterable = cloneExpr(data.Iterable)
		}
		data.Body = cloneBlock(data.Body)
		out.Data = data
	case hir.StmtBlock:
		data, ok := s.Data.(hir.BlockStmtData)
		if !ok {
			return out
		}
		data.Block = cloneBlock(data.Block)
		out.Data = data
	case hir.StmtDrop:
		data, ok := s.Data.(hir.DropData)
		if !ok {
			return out
		}
		if data.Value != nil {
			data.Value = cloneExpr(data.Value)
		}
		out.Data = data
	default:
		// break/continue etc: no payload.
	}
	return out
}

func cloneExpr(e *hir.Expr) *hir.Expr {
	if e == nil {
		return nil
	}
	out := *e
	switch e.Kind {
	case hir.ExprLiteral:
		if data, ok := e.Data.(hir.LiteralData); ok {
			out.Data = data
		}
	case hir.ExprVarRef:
		if data, ok := e.Data.(hir.VarRefData); ok {
			out.Data = data
		}
	case hir.ExprUnaryOp:
		data, ok := e.Data.(hir.UnaryOpData)
		if !ok {
			break
		}
		data.Operand = cloneExpr(data.Operand)
		out.Data = data
	case hir.ExprBinaryOp:
		data, ok := e.Data.(hir.BinaryOpData)
		if !ok {
			break
		}
		data.Left = cloneExpr(data.Left)
		data.Right = cloneExpr(data.Right)
		out.Data = data
	case hir.ExprCall:
		data, ok := e.Data.(hir.CallData)
		if !ok {
			break
		}
		data.Callee = cloneExpr(data.Callee)
		if len(data.Args) > 0 {
			data.Args = slices.Clone(data.Args)
			for i := range data.Args {
				data.Args[i] = cloneExpr(data.Args[i])
			}
		}
		out.Data = data
	case hir.ExprFieldAccess:
		data, ok := e.Data.(hir.FieldAccessData)
		if !ok {
			break
		}
		data.Object = cloneExpr(data.Object)
		out.Data = data
	case hir.ExprIndex:
		data, ok := e.Data.(hir.IndexData)
		if !ok {
			break
		}
		data.Object = cloneExpr(data.Object)
		data.Index = cloneExpr(data.Index)
		out.Data = data
	case hir.ExprStructLit:
		data, ok := e.Data.(hir.StructLitData)
		if !ok {
			break
		}
		if len(data.Fields) > 0 {
			fields := make([]hir.StructFieldInit, len(data.Fields))
			copy(fields, data.Fields)
			for i := range fields {
				fields[i].Value = cloneExpr(fields[i].Value)
			}
			data.Fields = fields
		}
		out.Data = data
	case hir.ExprArrayLit:
		data, ok := e.Data.(hir.ArrayLitData)
		if !ok {
			break
		}
		if len(data.Elements) > 0 {
			data.Elements = slices.Clone(data.Elements)
			for i := range data.Elements {
				data.Elements[i] = cloneExpr(data.Elements[i])
			}
		}
		out.Data = data
	case hir.ExprTupleLit:
		data, ok := e.Data.(hir.TupleLitData)
		if !ok {
			break
		}
		if len(data.Elements) > 0 {
			data.Elements = slices.Clone(data.Elements)
			for i := range data.Elements {
				data.Elements[i] = cloneExpr(data.Elements[i])
			}
		}
		out.Data = data
	case hir.ExprCompare:
		data, ok := e.Data.(hir.CompareData)
		if !ok {
			break
		}
		data.Value = cloneExpr(data.Value)
		if len(data.Arms) > 0 {
			arms := make([]hir.CompareArm, len(data.Arms))
			copy(arms, data.Arms)
			for i := range arms {
				arms[i].Pattern = cloneExpr(arms[i].Pattern)
				arms[i].Guard = cloneExpr(arms[i].Guard)
				arms[i].Result = cloneExpr(arms[i].Result)
			}
			data.Arms = arms
		}
		out.Data = data
	case hir.ExprTagTest:
		data, ok := e.Data.(hir.TagTestData)
		if !ok {
			break
		}
		data.Value = cloneExpr(data.Value)
		out.Data = data
	case hir.ExprTagPayload:
		data, ok := e.Data.(hir.TagPayloadData)
		if !ok {
			break
		}
		data.Value = cloneExpr(data.Value)
		out.Data = data
	case hir.ExprIterInit:
		data, ok := e.Data.(hir.IterInitData)
		if !ok {
			break
		}
		data.Iterable = cloneExpr(data.Iterable)
		out.Data = data
	case hir.ExprIterNext:
		data, ok := e.Data.(hir.IterNextData)
		if !ok {
			break
		}
		data.Iter = cloneExpr(data.Iter)
		out.Data = data
	case hir.ExprIf:
		data, ok := e.Data.(hir.IfData)
		if !ok {
			break
		}
		data.Cond = cloneExpr(data.Cond)
		data.Then = cloneExpr(data.Then)
		data.Else = cloneExpr(data.Else)
		out.Data = data
	case hir.ExprAwait:
		data, ok := e.Data.(hir.AwaitData)
		if !ok {
			break
		}
		data.Value = cloneExpr(data.Value)
		out.Data = data
	case hir.ExprSpawn:
		data, ok := e.Data.(hir.SpawnData)
		if !ok {
			break
		}
		data.Value = cloneExpr(data.Value)
		out.Data = data
	case hir.ExprAsync:
		data, ok := e.Data.(hir.AsyncData)
		if !ok {
			break
		}
		data.Body = cloneBlock(data.Body)
		out.Data = data
	case hir.ExprCast:
		data, ok := e.Data.(hir.CastData)
		if !ok {
			break
		}
		data.Value = cloneExpr(data.Value)
		out.Data = data
	case hir.ExprBlock:
		data, ok := e.Data.(hir.BlockExprData)
		if !ok {
			break
		}
		data.Block = cloneBlock(data.Block)
		out.Data = data
	default:
		out.Data = e.Data
	}
	return &out
}
