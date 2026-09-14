package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *returnOriginBody) call(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	call, _ := u.Builder.Exprs.Call(id)
	if out, handled, err := b.deferredClone(id, call, env, targets); handled || err != nil {
		return out, err
	}
	span := u.Builder.Exprs.Get(id).Span
	symID := u.Symbols.ExprSymbols[id]
	sym := u.Symbols.Table.Symbols.Get(symID)
	var callee *returnOriginFunction
	var info *types.FnInfo
	var unresolved string
	if sym != nil && sym.Kind == symbols.SymbolFunction {
		callee, unresolved = b.resolveCallDeclaration(symID)
		if callee != nil {
			info = callee.info
			if len(callee.candidate.TemplateParams) != 0 {
				info, unresolved = b.genericCallInfo(id, callee)
			}
		}
	}
	deferred, err := u.deferredMethod(id, call)
	if err != nil {
		return returnOriginExprResult{}, err
	}
	var receiver ast.ExprID
	if sym != nil && sym.Signature != nil && sym.Signature.HasSelf {
		if member, ok := u.Builder.Exprs.Member(call.Target); ok && member != nil {
			receiver = member.Target
		}
	}
	if deferred != nil && !deferred.StaticReceiver {
		member, _ := u.Builder.Exprs.Member(call.Target)
		receiver = member.Target
	}
	callbackType := ast.NoTypeID
	callback := false
	var callbackValue returnOriginValue
	if deferred == nil && callee == nil && (sym == nil || sym.Kind != symbols.SymbolFunction) {
		info, callbackType, callback = b.callbackParameter(call.Target)
	}
	flow := returnOriginFlow{normal: env}
	values := make(map[ast.ExprID]returnOriginCallValue, len(call.Args)+1)
	evaluate := func(expr ast.ExprID) error {
		var err error
		flow, err = flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
			out, evalErr := b.expr(expr, next, targets)
			values[expr] = returnOriginCallValue{value: out.value.clone(), storage: out.storage.clone()}
			if _, implicit := u.Sema.ImplicitConversions[expr]; implicit {
				b.pending(span, "implicit argument conversion needs its resolved callable origin contract")
				values[expr] = returnOriginCallValue{value: returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})}
			}
			return out.flow, evalErr
		})
		return err
	}
	if receiver.IsValid() {
		if err := evaluate(receiver); err != nil {
			return returnOriginExprResult{}, err
		}
	} else if !callback && deferred == nil && (sym == nil || sym.Kind != symbols.SymbolFunction) {
		if err := evaluate(call.Target); err != nil {
			return returnOriginExprResult{}, err
		}
	}
	for _, arg := range call.Args {
		if err := evaluate(arg.Value); err != nil {
			return returnOriginExprResult{}, err
		}
	}
	if !flow.normal.reachable {
		return returnOriginExprResult{flow: flow}, nil
	}
	if info == nil && deferred == nil && (sym == nil || sym.Kind != symbols.SymbolFunction) {
		value := values[call.Target].value
		if len(value.callables) != 0 {
			info = b.callableType(u.Sema.ExprTypes[call.Target], span)
			callback, callbackValue = info != nil, value
		}
	}
	if info == nil || unresolved != "" {
		if unresolved == "" {
			unresolved = "call needs an exact body, canonical core contract, or opaque declaration promise"
		}
		b.pending(span, unresolved)
		value := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		if b.shape(id) == returnOriginRefFree {
			value = returnOriginValueOf()
		}
		return returnOriginExprResult{flow: flow, value: value}, nil
	}
	var slots []returnOriginArgument
	if callback {
		// Function-value calls retain only fixed positional ABI slots. Do not
		// invent names/defaults from another declaration of the same FnInfo.
		if call.HasNamedArgs() || len(call.Args) != len(info.Params) {
			b.pending(span, "opaque call lacks exact positional argument slots")
			return returnOriginExprResult{flow: flow, value: returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})}, nil
		}
		for _, arg := range call.Args {
			slots = append(slots, returnOriginArgument{exprs: []ast.ExprID{arg.Value}})
		}
	} else {
		slots, err = mapReturnOriginArguments(sym.Signature, call, receiver)
		if err != nil {
			return returnOriginExprResult{}, err
		}
	}
	if len(slots) != len(info.Params) {
		return returnOriginExprResult{}, fmt.Errorf("return origins: call at %v has inconsistent formal arity", span)
	}
	actuals := make([]returnOriginValue, len(slots))
	for i, slot := range slots {
		actuals[i] = returnOriginValueOf()
		if slot.defaulted {
			params := callee.unit.Builder.Items.GetFnParamIDs(callee.item)
			if i >= len(params) {
				return returnOriginExprResult{}, fmt.Errorf("return origins: default at %v has no original parameter", span)
			}
			param := callee.unit.Builder.Items.FnParam(params[i])
			// Other defaults need their owning expression's transfer, not the
			// importing caller's AST or a reconstructed expected binding type.
			if literal, ok := callee.unit.Builder.Exprs.Literal(param.Default); !ok || literal == nil {
				b.pending(span, "nonliteral default argument needs its owning expression transfer")
				actuals[i] = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
			}
		}
		for _, expr := range slot.exprs {
			value := b.callArgumentOrigin(expr, info.Params[i], values[expr])
			if returnOriginFnInfo(u.Sema.TypeInterner, info.Params[i]) != nil || len(values[expr].value.callables) != 0 {
				value = b.convertCallableArgument(callee, i, slot, expr, info.Params[i], value)
			}
			actuals[i] = actuals[i].join(value)
		}
		if kind, reference := returnOriginFormalBorrowKind(u.Sema.TypeInterner, info.Params[i]); reference && kind == BorrowMut {
			if returnOriginCallHasUnprovedEffects(u.Sema.TypeInterner, []types.TypeID{info.Params[i]}) {
				b.pending(span, "mutable argument may replace reference-bearing contents")
			}
		}
	}
	if returnOriginFnInfo(u.Sema.TypeInterner, info.Result) != nil {
		b.pending(span, "callable call result needs its destination and capture contract")
		return returnOriginExprResult{flow: flow, value: returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})}, nil
	}
	var summary returnOriginValue
	if callbackValue.normal {
		summary = b.callableValueSources(callbackValue, span)
		if returnOriginCallHasUnprovedEffects(u.Sema.TypeInterner, info.Params) {
			b.pending(span, "indirect call may change reference-bearing or callable contents")
		}
	} else if callee != nil && callee.item.Body.IsValid() {
		// A recursive body legitimately starts at NoNormalReturn. Its private
		// fixed point, not the declared upper bound, supplies actual precision.
		summary = b.analyzer.summaries[callee.key]
	} else {
		var sources []uint32
		var valid bool
		if callback {
			declared, complete := b.declaredCallable(u.Sema.ExprTypes[call.Target], callbackType, span)
			sources, valid = declared.slots, complete
		} else {
			sources, valid = b.declaredFunctionSources(callee, span)
		}
		summary = b.opaqueReturnSources(info, sources, valid, span)
		if returnOriginCallHasUnprovedEffects(u.Sema.TypeInterner, info.Params) {
			b.pending(span, "opaque call may change reference-bearing or callable contents")
		}
	}
	if !summary.normal {
		flow.normal = returnOriginEnv{}
		return returnOriginExprResult{flow: flow}, nil
	}
	value := returnOriginValueOf()
	for _, root := range summary.roots {
		if root.kind != returnOriginParam || int64(root.param) >= int64(len(actuals)) {
			b.pending(span, "callee returned an unproved source")
			value = value.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
			continue
		}
		value = value.join(actuals[root.param])
	}
	return returnOriginExprResult{flow: flow, value: value}, nil
}

func (b *returnOriginBody) resolveCallDeclaration(id symbols.SymbolID) (*returnOriginFunction, string) {
	u := b.function.unit
	identity, err := u.callableIdentity(id, "")
	if err != nil {
		return nil, err.Error()
	}
	fn := b.analyzer.bodies[identity.BodyKey]
	if fn == nil {
		fn = b.analyzer.declarations[identity.BodyKey]
	}
	if fn == nil || fn.candidate == nil || fn.canonicalSourceKey != identity.SourceKey {
		return nil, "selected callable lacks its owning source declaration/body"
	}
	for _, candidate := range u.authority.CallableCandidates {
		if candidate.BodyKey != identity.BodyKey || candidate.SourceKey != identity.SourceKey {
			continue
		}
		if candidate.HasBody != fn.item.Body.IsValid() || !candidate.ReturnSources.Equal(fn.info.ReturnSources()) ||
			!slices.Equal(candidate.ParamTypes, fn.info.Params) || candidate.ResultType != fn.info.Result {
			return nil, "selected callable disagrees with its owning typed declaration"
		}
		return fn, ""
	}
	return nil, "selected callable has no canonical declaration authority"
}

// Recover the incoming parameter's actual source type syntax. Copies use the
// evaluated callable alternatives instead; neither route permits hidden
// captures to become explicit call-result sources.
func (b *returnOriginBody) callbackParameter(id ast.ExprID) (*types.FnInfo, ast.TypeID, bool) {
	fn := b.function
	u := fn.unit
	node := u.Builder.Exprs.Get(id)
	if node == nil || node.Kind != ast.ExprIdent {
		return nil, ast.NoTypeID, false
	}
	symID := u.Symbols.ExprSymbols[id]
	sym := u.Symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.Kind != symbols.SymbolParam || sym.Scope != fn.scope {
		return nil, ast.NoTypeID, false
	}
	info := returnOriginFnInfo(u.Sema.TypeInterner, u.Sema.ExprTypes[id])
	if info == nil {
		return nil, ast.NoTypeID, false
	}
	params := u.Builder.Items.GetFnParamIDs(fn.item)
	for i, param := range fn.params {
		if param == symID && i < len(params) && returnOriginFnInfo(u.Sema.TypeInterner, fn.info.Params[i]) == info {
			return info, u.Builder.Items.FnParam(params[i]).Type, true
		}
	}
	return nil, ast.NoTypeID, false
}

type returnOriginCallValue struct {
	value   returnOriginValue
	storage returnOriginValue
}

func (b *returnOriginBody) callArgumentOrigin(expr ast.ExprID, formal types.TypeID, actual returnOriginCallValue) returnOriginValue {
	u := b.function.unit
	in := u.Sema.TypeInterner
	if !returnOriginIsReference(in, formal) || returnOriginIsReference(in, u.Sema.ExprTypes[expr]) {
		return actual.value.clone()
	}
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	// The conversion's result may own different storage. Its existing Pending
	// obligation cannot be discharged by a borrow of the original expression.
	if _, converted := u.Sema.ImplicitConversions[expr]; converted {
		return unknown
	}
	span := u.Builder.Exprs.Get(expr).Span
	kind, reference := returnOriginFormalBorrowKind(in, formal)
	var evidence *BorrowInfo
	for i := range u.Sema.Borrows {
		borrow := &u.Sema.Borrows[i]
		if borrow.Life.FromExpr != expr {
			continue
		}
		if evidence != nil {
			b.pending(span, "implicit borrow has ambiguous expression evidence")
			return unknown
		}
		evidence = borrow
	}
	if !reference || evidence == nil || evidence.ID == NoBorrowID || evidence.Kind != kind ||
		evidence.Reserved || !evidence.Place.IsValid() {
		b.pending(span, "implicit borrow lacks an admitted borrow for this expression")
		return unknown
	}
	if !actual.storage.normal || len(actual.storage.roots) == 0 {
		b.pending(span, "implicit borrow needs its evaluated storage origin")
		return unknown
	}
	// FromExpr certifies the checker-admitted operation, not its old owner.
	// Current flow supplies the storage; expired/captured/unknown roots remain
	// visible instead of being reconstructed from historical Place/Bindings.
	b.checkExpired(actual.storage, span)
	return actual.storage.clone()
}

func returnOriginFormalBorrowKind(in *types.Interner, id types.TypeID) (BorrowKind, bool) {
	seen := make(map[types.TypeID]bool)
	for !seen[id] {
		seen[id] = true
		if target, ok := in.AliasTarget(id); ok {
			id = target
			continue
		}
		info, ok := in.Lookup(id)
		if !ok || info.Kind != types.KindReference {
			return BorrowShared, false
		}
		if info.Mutable {
			return BorrowMut, true
		}
		return BorrowShared, true
	}
	return BorrowShared, false
}

func (b *returnOriginBody) genericCallInfo(id ast.ExprID, callee *returnOriginFunction) (*types.FnInfo, string) {
	u := b.function.unit
	span := u.Builder.Exprs.Get(id).Span
	closure := u.authority.InstantiationClosure
	if closure == nil {
		return nil, "generic call lacks its finalized concrete use"
	}
	var found *ConcreteInstantiationUse
	for i := range closure.UseSites {
		use := &closure.UseSites[i]
		if use.CalleeTemplate != callee.candidate.Symbol || use.SourceKey != u.SourceKey || use.Site != span {
			continue
		}
		if found != nil {
			return nil, "generic call has ambiguous finalized concrete uses"
		}
		found = use
	}
	if found == nil {
		return nil, "generic call lacks its finalized concrete use"
	}
	fn, info, reason := b.analyzer.genericUseInfo(*found)
	if reason != "" {
		return nil, reason
	}
	if fn != callee || found.CallerTemplate != b.function.candidate.Symbol {
		return nil, "generic call disagrees with its selected source declaration"
	}
	return info, ""
}
