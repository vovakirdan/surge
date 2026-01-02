package mono

import (
	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type callRewriteFunc func(call *hir.Expr, data *hir.CallData) error

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

func (b *monoBuilder) callTypeArgs(caller, callee symbols.SymbolID, span source.Span, kind InstantiationKind) ([]types.TypeID, bool) {
	if b == nil || b.inst == nil || span == (source.Span{}) {
		return nil, false
	}
	args, ok := b.useSites[useSiteKey{Kind: kind, Caller: caller, Callee: callee, Span: span}]
	return args, ok
}

func (b *monoBuilder) callSiteInstantiation(caller symbols.SymbolID, span source.Span, kind InstantiationKind) (symbols.SymbolID, []types.TypeID, bool) {
	if b == nil || b.inst == nil || span == (source.Span{}) {
		return symbols.NoSymbolID, nil, false
	}
	info, ok := b.callSites[callSiteKey{Kind: kind, Caller: caller, Span: span}]
	if !ok || !info.Callee.IsValid() || len(info.TypeArgs) == 0 {
		return symbols.NoSymbolID, nil, false
	}
	return info.Callee, info.TypeArgs, true
}

func (b *monoBuilder) rewriteCallsInFunc(fn *hir.Func, callerSym symbols.SymbolID, subst *Subst, stack []MonoKey) error {
	if b == nil || fn == nil || fn.Body == nil {
		return nil
	}
	rewrite := func(call *hir.Expr, data *hir.CallData) error {
		if call == nil || data == nil {
			return nil
		}
		kind := InstFn
		var (
			calleeSym symbols.SymbolID
			rawArgs   []types.TypeID
		)

		// Prefer the InstantiationMap: it records the exact callee SymbolID and the
		// (possibly implicit) inferred type args, which is critical for overloads.
		if callerSym.IsValid() && call.Span != (source.Span{}) {
			if callee, args, ok := b.callSiteInstantiation(callerSym, call.Span, InstTag); ok {
				kind = InstTag
				calleeSym = callee
				rawArgs = args
			} else if callee, args, ok := b.callSiteInstantiation(callerSym, call.Span, InstFn); ok {
				kind = InstFn
				calleeSym = callee
				rawArgs = args
			}
		}

		if !calleeSym.IsValid() {
			if data.SymbolID.IsValid() {
				calleeSym = data.SymbolID
			} else if data.Callee != nil && data.Callee.Kind == hir.ExprVarRef {
				if vr, ok := data.Callee.Data.(hir.VarRefData); ok {
					calleeSym = vr.SymbolID
				}
			}
		}
		if !calleeSym.IsValid() || !b.isCallableSymbol(calleeSym) {
			return nil
		}
		if kind == InstFn && b.isTagSymbol(calleeSym) {
			kind = InstTag
		}

		if len(rawArgs) == 0 && b.isGenericSymbol(calleeSym) {
			if args, ok := b.callTypeArgs(callerSym, calleeSym, call.Span, kind); ok {
				rawArgs = args
			}
		}

		var concreteArgs []types.TypeID
		if len(rawArgs) > 0 {
			concreteArgs = make([]types.TypeID, 0, len(rawArgs))
			for _, a := range rawArgs {
				if subst != nil {
					concreteArgs = append(concreteArgs, subst.Type(a))
				} else {
					concreteArgs = append(concreteArgs, a)
				}
			}
		}
		if len(concreteArgs) > 0 && subst != nil && !typeArgsAreConcrete(b.types, concreteArgs) {
			if b != nil && b.mod != nil && b.mod.Symbols != nil && b.mod.Symbols.Table != nil && b.mod.Symbols.Table.Symbols != nil {
				nameArgs := make(map[source.StringID]types.TypeID, len(subst.TypeArgs))
				if owner := b.mod.Symbols.Table.Symbols.Get(subst.OwnerSym); owner != nil && len(owner.TypeParams) == len(subst.TypeArgs) {
					for i, name := range owner.TypeParams {
						if name != source.NoStringID && subst.TypeArgs[i] != types.NoTypeID {
							nameArgs[name] = subst.TypeArgs[i]
						}
					}
				}
				for i, arg := range concreteArgs {
					if arg == types.NoTypeID || b.types == nil {
						continue
					}
					if info, ok := b.types.TypeParamInfo(arg); ok && info != nil {
						if repl, ok := nameArgs[info.Name]; ok && repl != types.NoTypeID {
							concreteArgs[i] = repl
						}
					}
				}
			}
		}
		if len(concreteArgs) == 0 {
			if b.isGenericSymbol(calleeSym) {
				return nil
			}
			if orig := b.origFuncBySym[calleeSym]; orig != nil && b.funcHasGenericTypes(orig) {
				return nil
			}
		}

		if b.isIntrinsicCloneSymbol(calleeSym) {
			handled, err := b.rewriteCloneCall(call, data, stack)
			if err != nil {
				return err
			}
			if handled {
				return nil
			}
		}

		if kind == InstTag {
			_, err := b.ensureFunc(calleeSym, concreteArgs, stack)
			return err
		}

		target, err := b.ensureFunc(calleeSym, concreteArgs, stack)
		if err != nil {
			return err
		}
		if target != nil && target.InstanceSym.IsValid() {
			data.SymbolID = target.InstanceSym
			if data.Callee != nil && data.Callee.Kind == hir.ExprVarRef {
				if vr, ok := data.Callee.Data.(hir.VarRefData); ok {
					vr.Name = b.monoName(calleeSym, concreteArgs)
					vr.SymbolID = target.InstanceSym
					data.Callee.Data = vr
				}
			}
		}
		return nil
	}
	return rewriteCallsInBlock(fn.Body, rewrite)
}
