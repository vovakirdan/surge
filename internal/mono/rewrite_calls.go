package mono

import (
	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
		// Convert bound-method calls into direct method calls before normal rewriting.
		b.rewriteBoundMethodCall(call, data)
		kind := InstFn
		var (
			calleeSym symbols.SymbolID
			rawArgs   []types.TypeID
		)

		knownCallee := symbols.NoSymbolID
		if data.SymbolID.IsValid() {
			knownCallee = data.SymbolID
		} else if data.Callee != nil && data.Callee.Kind == hir.ExprVarRef {
			if vr, ok := data.Callee.Data.(hir.VarRefData); ok {
				knownCallee = vr.SymbolID
			}
		}

		// Prefer the InstantiationMap: it records the exact callee SymbolID and the
		// (possibly implicit) inferred type args, which is critical for overloads.
		if callerSym.IsValid() && call.Span != (source.Span{}) {
			if callee, args, ok := b.callSiteInstantiation(callerSym, call.Span, InstTag); ok {
				if !knownCallee.IsValid() || callee == knownCallee {
					kind = InstTag
					calleeSym = callee
					rawArgs = args
				}
			}
			if !calleeSym.IsValid() {
				if callee, args, ok := b.callSiteInstantiation(callerSym, call.Span, InstFn); ok {
					if !knownCallee.IsValid() || callee == knownCallee {
						kind = InstFn
						calleeSym = callee
						rawArgs = args
					}
				}
			}
		}

		if !calleeSym.IsValid() {
			calleeSym = knownCallee
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

func (b *monoBuilder) rewriteFuncValuesInFunc(fn *hir.Func, callerSym symbols.SymbolID, subst *Subst, stack []MonoKey) error {
	if b == nil || fn == nil || fn.Body == nil {
		return nil
	}
	rewrite := func(expr *hir.Expr, data *hir.VarRefData) error {
		if expr == nil || data == nil {
			return nil
		}
		if b.types == nil || expr.Type == types.NoTypeID {
			return nil
		}
		if tt, ok := b.types.Lookup(resolveAlias(b.types, expr.Type)); !ok || tt.Kind != types.KindFn {
			return nil
		}
		calleeSym := data.SymbolID
		if !calleeSym.IsValid() || !b.isCallableSymbol(calleeSym) {
			return nil
		}

		kind := InstFn
		var rawArgs []types.TypeID

		if callerSym.IsValid() && expr.Span != (source.Span{}) {
			if callee, args, ok := b.callSiteInstantiation(callerSym, expr.Span, InstTag); ok {
				if callee == calleeSym {
					kind = InstTag
					calleeSym = callee
					rawArgs = args
				}
			}
			if len(rawArgs) == 0 {
				if callee, args, ok := b.callSiteInstantiation(callerSym, expr.Span, InstFn); ok {
					if callee == calleeSym {
						kind = InstFn
						calleeSym = callee
						rawArgs = args
					}
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
			if args, ok := b.callTypeArgs(callerSym, calleeSym, expr.Span, kind); ok {
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

		if len(concreteArgs) > 0 && !typeArgsAreConcrete(b.types, concreteArgs) {
			return nil
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
			data.Name = b.monoName(calleeSym, concreteArgs)
		}
		return nil
	}
	return rewriteVarRefsInBlock(fn.Body, rewrite)
}
