package mono

import (
	"surge/internal/hir"
)

func (s *Subst) ApplyStmt(st *hir.Stmt) error {
	if s == nil || st == nil {
		return nil
	}
	switch st.Kind {
	case hir.StmtLet:
		data, ok := st.Data.(hir.LetData)
		if !ok {
			return nil
		}
		data.Type = s.Type(data.Type)
		data.Ownership = inferOwnership(s.Types, data.Type)
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		if data.Pattern != nil {
			if err := s.ApplyExpr(data.Pattern); err != nil {
				return err
			}
		}
		st.Data = data
	case hir.StmtExpr:
		data, ok := st.Data.(hir.ExprStmtData)
		if !ok {
			return nil
		}
		if data.Expr != nil {
			if err := s.ApplyExpr(data.Expr); err != nil {
				return err
			}
		}
		st.Data = data
	case hir.StmtAssign:
		data, ok := st.Data.(hir.AssignData)
		if !ok {
			return nil
		}
		if data.Target != nil {
			if err := s.ApplyExpr(data.Target); err != nil {
				return err
			}
		}
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		st.Data = data
	case hir.StmtReturn:
		data, ok := st.Data.(hir.ReturnData)
		if !ok {
			return nil
		}
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		st.Data = data
	case hir.StmtIf:
		data, ok := st.Data.(hir.IfStmtData)
		if !ok {
			return nil
		}
		if data.Cond != nil {
			if err := s.ApplyExpr(data.Cond); err != nil {
				return err
			}
		}
		if err := s.ApplyBlock(data.Then); err != nil {
			return err
		}
		if err := s.ApplyBlock(data.Else); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtWhile:
		data, ok := st.Data.(hir.WhileData)
		if !ok {
			return nil
		}
		if data.Cond != nil {
			if err := s.ApplyExpr(data.Cond); err != nil {
				return err
			}
		}
		if err := s.ApplyBlock(data.Body); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtFor:
		data, ok := st.Data.(hir.ForData)
		if !ok {
			return nil
		}
		if data.Init != nil {
			if err := s.ApplyStmt(data.Init); err != nil {
				return err
			}
		}
		if data.Cond != nil {
			if err := s.ApplyExpr(data.Cond); err != nil {
				return err
			}
		}
		if data.Post != nil {
			if err := s.ApplyExpr(data.Post); err != nil {
				return err
			}
		}
		data.VarType = s.Type(data.VarType)
		if data.Iterable != nil {
			if err := s.ApplyExpr(data.Iterable); err != nil {
				return err
			}
		}
		if err := s.ApplyBlock(data.Body); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtBlock:
		data, ok := st.Data.(hir.BlockStmtData)
		if !ok {
			return nil
		}
		if err := s.ApplyBlock(data.Block); err != nil {
			return err
		}
		st.Data = data
	case hir.StmtDrop:
		data, ok := st.Data.(hir.DropData)
		if !ok {
			return nil
		}
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		st.Data = data
	default:
	}
	return nil
}

func (s *Subst) ApplyExpr(e *hir.Expr) error {
	if s == nil || e == nil {
		return nil
	}
	e.Type = s.Type(e.Type)

	switch e.Kind {
	case hir.ExprUnaryOp:
		data, ok := e.Data.(hir.UnaryOpData)
		if !ok {
			return nil
		}
		if data.Operand != nil {
			if err := s.ApplyExpr(data.Operand); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprBinaryOp:
		data, ok := e.Data.(hir.BinaryOpData)
		if !ok {
			return nil
		}
		if data.Left != nil {
			if err := s.ApplyExpr(data.Left); err != nil {
				return err
			}
		}
		if data.Right != nil {
			if err := s.ApplyExpr(data.Right); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprCall:
		data, ok := e.Data.(hir.CallData)
		if !ok {
			return nil
		}
		if data.Callee != nil {
			if err := s.ApplyExpr(data.Callee); err != nil {
				return err
			}
		}
		for i := range data.Args {
			if err := s.ApplyExpr(data.Args[i]); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprFieldAccess:
		data, ok := e.Data.(hir.FieldAccessData)
		if !ok {
			return nil
		}
		if data.Object != nil {
			if err := s.ApplyExpr(data.Object); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprIndex:
		data, ok := e.Data.(hir.IndexData)
		if !ok {
			return nil
		}
		if data.Object != nil {
			if err := s.ApplyExpr(data.Object); err != nil {
				return err
			}
		}
		if data.Index != nil {
			if err := s.ApplyExpr(data.Index); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprStructLit:
		data, ok := e.Data.(hir.StructLitData)
		if !ok {
			return nil
		}
		data.TypeID = s.Type(data.TypeID)
		for i := range data.Fields {
			if data.Fields[i].Value != nil {
				if err := s.ApplyExpr(data.Fields[i].Value); err != nil {
					return err
				}
			}
		}
		e.Data = data
	case hir.ExprArrayLit:
		data, ok := e.Data.(hir.ArrayLitData)
		if !ok {
			return nil
		}
		for i := range data.Elements {
			if err := s.ApplyExpr(data.Elements[i]); err != nil {
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
			if err := s.ApplyExpr(data.Elements[i]); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprCompare:
		data, ok := e.Data.(hir.CompareData)
		if !ok {
			return nil
		}
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		for i := range data.Arms {
			if data.Arms[i].Pattern != nil {
				if err := s.ApplyExpr(data.Arms[i].Pattern); err != nil {
					return err
				}
			}
			if data.Arms[i].Guard != nil {
				if err := s.ApplyExpr(data.Arms[i].Guard); err != nil {
					return err
				}
			}
			if data.Arms[i].Result != nil {
				if err := s.ApplyExpr(data.Arms[i].Result); err != nil {
					return err
				}
			}
		}
		e.Data = data
	case hir.ExprTagTest:
		data, ok := e.Data.(hir.TagTestData)
		if !ok {
			return nil
		}
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprTagPayload:
		data, ok := e.Data.(hir.TagPayloadData)
		if !ok {
			return nil
		}
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprIterInit:
		data, ok := e.Data.(hir.IterInitData)
		if !ok {
			return nil
		}
		if data.Iterable != nil {
			if err := s.ApplyExpr(data.Iterable); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprIterNext:
		data, ok := e.Data.(hir.IterNextData)
		if !ok {
			return nil
		}
		if data.Iter != nil {
			if err := s.ApplyExpr(data.Iter); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprIf:
		data, ok := e.Data.(hir.IfData)
		if !ok {
			return nil
		}
		if data.Cond != nil {
			if err := s.ApplyExpr(data.Cond); err != nil {
				return err
			}
		}
		if data.Then != nil {
			if err := s.ApplyExpr(data.Then); err != nil {
				return err
			}
		}
		if data.Else != nil {
			if err := s.ApplyExpr(data.Else); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprAwait:
		data, ok := e.Data.(hir.AwaitData)
		if !ok {
			return nil
		}
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprTask:
		data, ok := e.Data.(hir.TaskData)
		if !ok {
			return nil
		}
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprSpawn:
		data, ok := e.Data.(hir.SpawnData)
		if !ok {
			return nil
		}
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprAsync:
		data, ok := e.Data.(hir.AsyncData)
		if !ok {
			return nil
		}
		if err := s.ApplyBlock(data.Body); err != nil {
			return err
		}
		e.Data = data
	case hir.ExprCast:
		data, ok := e.Data.(hir.CastData)
		if !ok {
			return nil
		}
		data.TargetTy = s.Type(data.TargetTy)
		if data.Value != nil {
			if err := s.ApplyExpr(data.Value); err != nil {
				return err
			}
		}
		e.Data = data
	case hir.ExprBlock:
		data, ok := e.Data.(hir.BlockExprData)
		if !ok {
			return nil
		}
		if err := s.ApplyBlock(data.Block); err != nil {
			return err
		}
		e.Data = data
	default:
	}

	return nil
}
