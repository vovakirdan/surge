package mono

import (
	"fmt"
	"slices"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *monoBuilder) callTypeArgs(caller, callee symbols.SymbolID, callerArgs []types.TypeID, span source.Span, kind InstantiationKind) ([]types.TypeID, bool, error) {
	if b == nil || b.inst == nil || span == (source.Span{}) {
		return nil, false, nil
	}
	callerKey, err := canonicalCallableKey(b.identity, caller, callerArgs)
	if err != nil {
		return nil, false, err
	}
	calleeKey, err := canonicalCallableKey(b.identity, callee, nil)
	if err != nil {
		return nil, false, err
	}
	args, ok := b.useSites[useSiteKey{Kind: kind, Caller: callerKey, CalleeTemplateKey: calleeKey.TemplateKey, Span: span}]
	return args, ok, nil
}

func (b *monoBuilder) callSiteInstantiation(caller symbols.SymbolID, callerArgs []types.TypeID, span source.Span, kind InstantiationKind) (symbols.SymbolID, []types.TypeID, bool, error) {
	if b == nil || b.inst == nil || span == (source.Span{}) {
		return symbols.NoSymbolID, nil, false, nil
	}
	callerKey, err := canonicalCallableKey(b.identity, caller, callerArgs)
	if err != nil {
		return symbols.NoSymbolID, nil, false, err
	}
	info, ok := b.callSites[callSiteKey{Kind: kind, Caller: callerKey, Span: span}]
	if !ok || !info.Callee.IsValid() || len(info.TypeArgs) == 0 {
		return symbols.NoSymbolID, nil, false, nil
	}
	return info.Callee, info.TypeArgs, true, nil
}

func (b *monoBuilder) rewriteCallsInFunc(fn *hir.Func, callerSym symbols.SymbolID, subst *Subst, stack []MonoKey) error {
	if b == nil || fn == nil {
		return nil
	}
	callerArgsKey := ""
	var callerArgs []types.TypeID
	if subst != nil {
		callerArgs = subst.TypeArgs
		callerArgsKey = typeArgsKey(subst.TypeArgs)
	}
	rewrite := func(call *hir.Expr, data *hir.CallData) error {
		if call == nil || data == nil {
			return nil
		}
		if data.SelectDispatch {
			// The outer call-shaped node in a select/race arm is syntax for a
			// MIR select descriptor, not a callable. rewriteCallsInExpr has
			// already visited its receiver and arguments at this point.
			return nil
		}
		var deferred *sema.ResolvedDeferredCall
		switch {
		case data.DeferredUseID != "" && b.closure != nil:
			resolved, ok, err := b.resolvedDeferredCall(callerSym, callerArgs, data.DeferredUseID)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("mono: deferred callable %s has no authoritative resolution for caller %d[%s]", data.DeferredUseID, callerSym, callerArgsKey)
			}
			deferred = &resolved
			if err := b.applyResolvedDeferredCall(call, data, &resolved); err != nil {
				return err
			}
			if resolved.Outcome == sema.DeferredCallableBuiltinCopy {
				// Copy cloning is the intrinsic identity/copy operation, not a
				// materializable generic callable in the authoritative closure.
				return nil
			}
		case b.closure == nil:
			// Compatibility for low-level embedders without the sema authority.
			b.rewriteBoundMethodCall(call, data)
		case unresolvedBoundMethodCall(data):
			return fmt.Errorf("mono: unresolved bound method at %s has no DeferredUseID", call.Span)
		}
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

		if deferred != nil && deferred.Outcome == sema.DeferredCallableResolved {
			calleeSym = deferred.Callee
			rawArgs = slices.Clone(deferred.CalleeTemplateArgs)
		}

		// Prefer the InstantiationMap: it records the exact callee SymbolID and the
		// (possibly implicit) inferred type args, which is critical for overloads.
		if !calleeSym.IsValid() && callerSym.IsValid() && call.Span != (source.Span{}) {
			callee, args, ok, err := b.callSiteInstantiation(callerSym, callerArgs, call.Span, InstTag)
			if err != nil {
				return err
			}
			if ok {
				matches, matchErr := b.sameCallableTemplate(callee, knownCallee)
				if matchErr != nil {
					return matchErr
				}
				if !knownCallee.IsValid() || matches {
					kind = InstTag
					calleeSym = callee
					rawArgs = args
				}
			}
			if !calleeSym.IsValid() {
				callee, args, ok, err = b.callSiteInstantiation(callerSym, callerArgs, call.Span, InstFn)
				if err != nil {
					return err
				}
				if ok {
					matches, matchErr := b.sameCallableTemplate(callee, knownCallee)
					if matchErr != nil {
						return matchErr
					}
					if !knownCallee.IsValid() || matches {
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
			args, ok, err := b.callTypeArgs(callerSym, calleeSym, callerArgs, call.Span, kind)
			if err != nil {
				return err
			}
			if ok {
				rawArgs = args
			}
		}

		var concreteArgs []types.TypeID
		if len(rawArgs) > 0 {
			concreteArgs = make([]types.TypeID, 0, len(rawArgs))
			for _, a := range rawArgs {
				if b.closure == nil && subst != nil {
					concreteArgs = append(concreteArgs, subst.Type(a))
				} else {
					concreteArgs = append(concreteArgs, a)
				}
			}
		}
		if b.closure == nil && len(concreteArgs) > 0 && subst != nil && !typeArgsAreConcrete(b.types, concreteArgs) {
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
				if b.closure != nil {
					return fmt.Errorf("mono: generic call to %s at %s has no authoritative concrete instantiation", b.monoName(calleeSym, nil), call.Span)
				}
				return nil
			}
			if orig := b.origFuncBySym[calleeSym]; orig != nil && b.funcHasGenericTypes(orig) {
				if b.closure != nil {
					return fmt.Errorf("mono: generic call to %s at %s has no authoritative concrete ABI", b.monoName(calleeSym, nil), call.Span)
				}
				return nil
			}
		}

		if b.isIntrinsicCloneSymbol(calleeSym) && deferred == nil {
			handled, err := b.rewriteCloneCall(call, data)
			if err != nil {
				return err
			}
			if handled {
				return nil
			}
		}

		if kind == InstTag {
			_, err := b.ensureCallableInstance(calleeSym, concreteArgs, stack)
			return err
		}

		instanceSym, err := b.ensureCallableInstance(calleeSym, concreteArgs, stack)
		if err != nil {
			return err
		}
		if instanceSym.IsValid() {
			data.SymbolID = instanceSym
			if data.Callee != nil && data.Callee.Kind == hir.ExprVarRef {
				if vr, ok := data.Callee.Data.(hir.VarRefData); ok {
					vr.Name = b.monoName(calleeSym, concreteArgs)
					vr.SymbolID = instanceSym
					data.Callee.Data = vr
				}
			}
		}
		return nil
	}
	for i := range fn.Params {
		if err := rewriteCallsInExpr(fn.Params[i].Default, rewrite); err != nil {
			return err
		}
	}
	return rewriteCallsInBlock(fn.Body, rewrite)
}

func (b *monoBuilder) rewriteFuncValuesInFunc(fn *hir.Func, callerSym symbols.SymbolID, subst *Subst, stack []MonoKey) error {
	if b == nil || fn == nil {
		return nil
	}
	var callerArgs []types.TypeID
	if subst != nil {
		callerArgs = subst.TypeArgs
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
			resolvedCallee, args, ok, err := b.callSiteInstantiation(callerSym, callerArgs, expr.Span, InstTag)
			if err != nil {
				return err
			}
			if ok {
				matches, matchErr := b.sameCallableTemplate(resolvedCallee, calleeSym)
				if matchErr != nil {
					return matchErr
				}
				if matches {
					kind = InstTag
					calleeSym = resolvedCallee
					rawArgs = args
				}
			}
			if len(rawArgs) == 0 {
				resolvedCallee, args, ok, err = b.callSiteInstantiation(callerSym, callerArgs, expr.Span, InstFn)
				if err != nil {
					return err
				}
				if ok {
					matches, matchErr := b.sameCallableTemplate(resolvedCallee, calleeSym)
					if matchErr != nil {
						return matchErr
					}
					if matches {
						kind = InstFn
						calleeSym = resolvedCallee
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
			args, ok, err := b.callTypeArgs(callerSym, calleeSym, callerArgs, expr.Span, kind)
			if err != nil {
				return err
			}
			if ok {
				rawArgs = args
			}
		}

		var concreteArgs []types.TypeID
		if len(rawArgs) > 0 {
			concreteArgs = make([]types.TypeID, 0, len(rawArgs))
			for _, a := range rawArgs {
				if b.closure == nil && subst != nil {
					concreteArgs = append(concreteArgs, subst.Type(a))
				} else {
					concreteArgs = append(concreteArgs, a)
				}
			}
		}
		if b.closure == nil && len(concreteArgs) > 0 && subst != nil && !typeArgsAreConcrete(b.types, concreteArgs) {
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
				if b.closure != nil {
					return fmt.Errorf("mono: generic function value %s at %s has no authoritative concrete instantiation", b.monoName(calleeSym, nil), expr.Span)
				}
				return nil
			}
			if orig := b.origFuncBySym[calleeSym]; orig != nil && b.funcHasGenericTypes(orig) {
				if b.closure != nil {
					return fmt.Errorf("mono: generic function value %s at %s has no authoritative concrete ABI", b.monoName(calleeSym, nil), expr.Span)
				}
				return nil
			}
		}

		if len(concreteArgs) > 0 && !typeArgsAreConcrete(b.types, concreteArgs) {
			if b.closure != nil {
				return fmt.Errorf("mono: generic function value %s at %s has non-concrete authoritative type arguments", b.monoName(calleeSym, concreteArgs), expr.Span)
			}
			return nil
		}

		if kind == InstTag {
			_, err := b.ensureCallableInstance(calleeSym, concreteArgs, stack)
			return err
		}

		instanceSym, err := b.ensureCallableInstance(calleeSym, concreteArgs, stack)
		if err != nil {
			return err
		}
		if instanceSym.IsValid() {
			data.SymbolID = instanceSym
			data.Name = b.monoName(calleeSym, concreteArgs)
		}
		return nil
	}
	for i := range fn.Params {
		if err := rewriteVarRefsInExpr(fn.Params[i].Default, rewrite); err != nil {
			return err
		}
	}
	return rewriteVarRefsInBlock(fn.Body, rewrite)
}
