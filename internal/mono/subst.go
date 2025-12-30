package mono

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type Subst struct {
	Types     *types.Interner
	OwnerSym  symbols.SymbolID
	OwnerSyms []symbols.SymbolID
	TypeArgs  []types.TypeID
	NameArgs  map[source.StringID]types.TypeID

	cache map[types.TypeID]types.TypeID
}

func (s *Subst) ApplyFunc(fn *hir.Func) error {
	if s == nil || fn == nil {
		return nil
	}
	for i := range fn.Params {
		fn.Params[i].Type = s.Type(fn.Params[i].Type)
		fn.Params[i].Ownership = inferOwnership(s.Types, fn.Params[i].Type)
		if fn.Params[i].Default != nil {
			if err := s.ApplyExpr(fn.Params[i].Default); err != nil {
				return err
			}
		}
	}
	fn.Result = s.Type(fn.Result)
	if fn.Body != nil {
		if err := s.ApplyBlock(fn.Body); err != nil {
			return err
		}
	}
	return nil
}

func (s *Subst) ApplyBlock(b *hir.Block) error {
	if s == nil || b == nil {
		return nil
	}
	for i := range b.Stmts {
		if err := s.ApplyStmt(&b.Stmts[i]); err != nil {
			return err
		}
	}
	return nil
}

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

func (s *Subst) Type(id types.TypeID) types.TypeID {
	if s == nil || s.Types == nil || id == types.NoTypeID {
		return id
	}
	if s.cache == nil {
		s.cache = make(map[types.TypeID]types.TypeID, 32)
	} else if cached, ok := s.cache[id]; ok {
		return cached
	}

	out := s.typeNoCache(id)
	s.cache[id] = out
	return out
}

func (s *Subst) typeNoCache(id types.TypeID) types.TypeID {
	tt, ok := s.Types.Lookup(id)
	if !ok {
		return id
	}

	switch tt.Kind {
	case types.KindGenericParam:
		info, ok := s.Types.TypeParamInfo(id)
		if !ok || info == nil {
			return id
		}
		if !s.ownerMatches(symbols.SymbolID(info.Owner)) {
			if s.NameArgs != nil {
				if repl, ok := s.NameArgs[info.Name]; ok && repl != types.NoTypeID {
					return repl
				}
			}
			return id
		}
		idx := int(info.Index)
		if idx < 0 || idx >= len(s.TypeArgs) {
			if s.NameArgs != nil {
				if repl, ok := s.NameArgs[info.Name]; ok && repl != types.NoTypeID {
					return repl
				}
			}
			return id
		}
		if s.TypeArgs[idx] == types.NoTypeID {
			if s.NameArgs != nil {
				if repl, ok := s.NameArgs[info.Name]; ok && repl != types.NoTypeID {
					return repl
				}
			}
			return id
		}
		return s.TypeArgs[idx]

	case types.KindPointer, types.KindReference, types.KindOwn, types.KindArray:
		elem := s.Type(tt.Elem)
		if elem == tt.Elem {
			return id
		}
		clone := tt
		clone.Elem = elem
		return s.Types.Intern(clone)

	case types.KindTuple:
		info, ok := s.Types.TupleInfo(id)
		if !ok || info == nil || len(info.Elems) == 0 {
			return id
		}
		elems := make([]types.TypeID, len(info.Elems))
		changed := false
		for i := range info.Elems {
			elems[i] = s.Type(info.Elems[i])
			changed = changed || elems[i] != info.Elems[i]
		}
		if !changed {
			return id
		}
		return s.Types.RegisterTuple(elems)

	case types.KindFn:
		info, ok := s.Types.FnInfo(id)
		if !ok || info == nil {
			return id
		}
		params := make([]types.TypeID, len(info.Params))
		changed := false
		for i := range info.Params {
			params[i] = s.Type(info.Params[i])
			changed = changed || params[i] != info.Params[i]
		}
		result := s.Type(info.Result)
		changed = changed || result != info.Result
		if !changed {
			return id
		}
		return s.Types.RegisterFn(params, result)

	case types.KindStruct:
		info, ok := s.Types.StructInfo(id)
		if !ok || info == nil || len(info.TypeArgs) == 0 {
			return id
		}
		newArgs := make([]types.TypeID, len(info.TypeArgs))
		changed := false
		for i := range info.TypeArgs {
			newArgs[i] = s.Type(info.TypeArgs[i])
			changed = changed || newArgs[i] != info.TypeArgs[i]
		}
		if !changed {
			return id
		}
		if existing, ok := s.Types.FindStructInstance(info.Name, newArgs); ok {
			return existing
		}
		inst := s.Types.RegisterStructInstanceWithValues(info.Name, info.Decl, newArgs, s.Types.StructValueArgs(id))
		fields := s.Types.StructFields(id)
		for i := range fields {
			fields[i].Type = s.Type(fields[i].Type)
		}
		s.Types.SetStructFields(inst, fields)
		return inst

	case types.KindUnion:
		info, ok := s.Types.UnionInfo(id)
		if !ok || info == nil || len(info.TypeArgs) == 0 {
			return id
		}
		newArgs := make([]types.TypeID, len(info.TypeArgs))
		changed := false
		for i := range info.TypeArgs {
			newArgs[i] = s.Type(info.TypeArgs[i])
			changed = changed || newArgs[i] != info.TypeArgs[i]
		}
		if !changed {
			return id
		}
		if existing, ok := s.Types.FindUnionInstance(info.Name, newArgs); ok {
			return existing
		}
		inst := s.Types.RegisterUnionInstance(info.Name, info.Decl, newArgs)
		members := make([]types.UnionMember, len(info.Members))
		copy(members, info.Members)
		for i := range members {
			members[i].Type = s.Type(members[i].Type)
			if len(members[i].TagArgs) > 0 {
				tagArgs := make([]types.TypeID, len(members[i].TagArgs))
				for j := range members[i].TagArgs {
					tagArgs[j] = s.Type(members[i].TagArgs[j])
				}
				members[i].TagArgs = tagArgs
			}
		}
		s.Types.SetUnionMembers(inst, members)
		return inst

	case types.KindAlias:
		info, ok := s.Types.AliasInfo(id)
		if !ok || info == nil || len(info.TypeArgs) == 0 {
			return id
		}
		newArgs := make([]types.TypeID, len(info.TypeArgs))
		changed := false
		for i := range info.TypeArgs {
			newArgs[i] = s.Type(info.TypeArgs[i])
			changed = changed || newArgs[i] != info.TypeArgs[i]
		}
		if !changed {
			return id
		}
		if existing, ok := s.Types.FindAliasInstance(info.Name, newArgs); ok {
			return existing
		}
		target := s.Type(info.Target)
		if target == types.NoTypeID {
			return types.NoTypeID
		}
		inst := s.Types.RegisterAliasInstance(info.Name, info.Decl, newArgs)
		s.Types.SetAliasTarget(inst, target)
		return inst

	default:
		return id
	}
}

func inferOwnership(typesIn *types.Interner, ty types.TypeID) hir.Ownership {
	if typesIn == nil || ty == types.NoTypeID {
		return hir.OwnershipNone
	}
	t, ok := typesIn.Lookup(ty)
	if !ok {
		return hir.OwnershipNone
	}
	switch t.Kind {
	case types.KindReference:
		if t.Mutable {
			return hir.OwnershipRefMut
		}
		return hir.OwnershipRef
	case types.KindPointer:
		return hir.OwnershipPtr
	case types.KindOwn:
		return hir.OwnershipOwn
	case types.KindInt, types.KindUint, types.KindFloat, types.KindBool:
		return hir.OwnershipCopy
	default:
		return hir.OwnershipNone
	}
}

func (s *Subst) ownerMatches(owner symbols.SymbolID) bool {
	if owner == s.OwnerSym {
		return true
	}
	for _, alt := range s.OwnerSyms {
		if owner == alt {
			return true
		}
	}
	return false
}

func (s *Subst) DebugString() string {
	if s == nil {
		return "<nil>"
	}
	return fmt.Sprintf("owner=%d args=%d", s.OwnerSym, len(s.TypeArgs))
}
