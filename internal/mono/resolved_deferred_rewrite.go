package mono

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *monoBuilder) resolvedDeferredCall(
	caller symbols.SymbolID,
	callerArgs []types.TypeID,
	useID sema.DeferredUseID,
) (sema.ResolvedDeferredCall, bool, error) {
	if b == nil || useID == "" {
		return sema.ResolvedDeferredCall{}, false, nil
	}
	callerKey, err := canonicalCallableKey(b.identity, caller, callerArgs)
	if err != nil {
		return sema.ResolvedDeferredCall{}, false, err
	}
	call, ok := b.deferredCalls[deferredCallSiteKey{
		Caller: sema.InstanceKey{TemplateKey: callerKey.TemplateKey, ArgsKey: callerKey.ArgsKey},
		UseID:  useID,
	}]
	if !ok {
		return sema.ResolvedDeferredCall{}, false, nil
	}
	return sema.CloneResolvedDeferredCallForConsumer(&call), true, nil
}

func (b *monoBuilder) applyResolvedDeferredCall(call *hir.Expr, data *hir.CallData, resolved *sema.ResolvedDeferredCall) error {
	if call == nil || data == nil || resolved == nil {
		return fmt.Errorf("mono: missing deferred callable rewrite input")
	}
	if resolved.Outcome == sema.DeferredCallableBuiltinCopy {
		if resolved.Kind != sema.DeferredCloneCall {
			return fmt.Errorf("mono: deferred callable %s returned BuiltinCopy for non-clone use", resolved.UseID)
		}
		// Keep CallData.SymbolID so MIR can lower the intrinsic clone, but do
		// not present its callee expression as a generic first-class function
		// value. BuiltinCopy deliberately has no callable closure instance.
		if data.Callee != nil {
			name := "clone"
			if ref, ok := data.Callee.Data.(hir.VarRefData); ok && ref.Name != "" {
				name = ref.Name
			}
			data.Callee = &hir.Expr{
				Kind: hir.ExprVarRef,
				Type: types.NoTypeID,
				Span: call.Span,
				Data: hir.VarRefData{Name: name},
			}
		}
		return nil
	}
	if resolved.Outcome != sema.DeferredCallableResolved || !resolved.Callee.IsValid() {
		return fmt.Errorf("mono: deferred callable %s has invalid outcome", resolved.UseID)
	}
	entry := b.callableSymbol(resolved.Callee)
	if entry == nil || entry.Signature == nil {
		return fmt.Errorf("mono: deferred callable %s selected missing symbol %d", resolved.UseID, resolved.Callee)
	}
	if resolved.CalleeResultType == types.NoTypeID || len(resolved.CalleeParamTypes) == 0 && !resolved.StaticReceiver {
		return fmt.Errorf("mono: deferred callable %s has no exact concrete ABI", resolved.UseID)
	}

	switch resolved.Kind {
	case sema.DeferredMethodCall, sema.DeferredBoolCall:
		if resolved.StaticReceiver {
			if len(resolved.CalleeParamTypes) != len(data.Args) {
				return fmt.Errorf("mono: deferred static method %s selected signature arity %d for %d argument(s)", resolved.UseID, len(resolved.CalleeParamTypes), len(data.Args))
			}
			for i := range data.Args {
				data.Args[i] = b.adjustExprForType(data.Args[i], resolved.CalleeParamTypes[i])
			}
			break
		}
		if data.Callee == nil || data.Callee.Kind != hir.ExprFieldAccess {
			return fmt.Errorf("mono: deferred method %s lost its bound receiver", resolved.UseID)
		}
		field, ok := data.Callee.Data.(hir.FieldAccessData)
		if !ok || field.Object == nil || field.FieldName == "" {
			return fmt.Errorf("mono: deferred method %s has malformed bound receiver", resolved.UseID)
		}
		if len(resolved.CalleeParamTypes) != len(data.Args)+1 {
			return fmt.Errorf("mono: deferred method %s selected signature arity %d for %d argument(s)", resolved.UseID, len(resolved.CalleeParamTypes), len(data.Args))
		}
		args := make([]*hir.Expr, 0, len(data.Args)+1)
		args = append(args, b.adjustExprForType(field.Object, resolved.CalleeParamTypes[0]))
		for i := range data.Args {
			args = append(args, b.adjustExprForType(data.Args[i], resolved.CalleeParamTypes[i+1]))
		}
		data.Args = args
	case sema.DeferredCloneCall:
		if len(data.Args) != 1 || len(resolved.CalleeParamTypes) != 1 {
			return fmt.Errorf("mono: deferred clone %s selected a non-unary implementation", resolved.UseID)
		}
		data.Args[0] = b.adjustExprForType(data.Args[0], resolved.CalleeParamTypes[0])
	default:
		return fmt.Errorf("mono: deferred callable %s has unknown kind %d", resolved.UseID, resolved.Kind)
	}

	data.SymbolID = resolved.Callee
	data.Callee = &hir.Expr{
		Kind: hir.ExprVarRef,
		Type: types.NoTypeID,
		Span: call.Span,
		Data: hir.VarRefData{Name: b.monoName(resolved.Callee, resolved.CalleeTemplateArgs), SymbolID: resolved.Callee},
	}
	return nil
}

func (b *monoBuilder) callableSymbol(id symbols.SymbolID) *symbols.Symbol {
	if b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil {
		return nil
	}
	return b.mod.Symbols.Table.Symbols.Get(id)
}

func unresolvedBoundMethodCall(data *hir.CallData) bool {
	if data == nil || data.CrossingDispatch || data.SelectDispatch || data.SymbolID.IsValid() || data.Callee == nil || data.Callee.Kind != hir.ExprFieldAccess || data.Callee.Type != types.NoTypeID {
		return false
	}
	field, ok := data.Callee.Data.(hir.FieldAccessData)
	return ok && field.Object != nil && field.FieldName != ""
}
