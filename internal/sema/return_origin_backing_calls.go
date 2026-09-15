package sema

import (
	"slices"

	"surge/internal/source"
	"surge/internal/types"
)

// returnOriginBackingCall is a source-body call whose every container formal
// received one explicit argument with a proven backing target set.
type returnOriginBackingCall struct {
	callee     *returnOriginFunction
	containers map[int]returnOriginIndexType
	targets    map[int]returnOriginBackingTargets
}

// backingCallTargets admits a call only through the callee's own body. Generic
// bodies are admitted; defaults, variadics, conversions, callback or deferred
// selections, a callee mixing cell and container formals, and any container
// argument without a complete target proof leave the call on its refusal path.
func (b *returnOriginBody) backingCallTargets(callee *returnOriginFunction, indirect bool, slots []returnOriginArgument, actuals []returnOriginValue,
	pre returnOriginEnv,
) (returnOriginBackingCall, bool) {
	u := b.function.unit
	in := u.Sema.TypeInterner
	if indirect || callee == nil || callee.candidate == nil || !callee.item.Body.IsValid() || len(callee.backingSlots) == 0 || len(callee.cellSlots) != 0 ||
		len(slots) != len(callee.info.Params) || len(actuals) != len(slots) {
		return returnOriginBackingCall{}, false
	}
	call := returnOriginBackingCall{callee: callee, containers: make(map[int]returnOriginIndexType, len(callee.backingSlots)),
		targets: make(map[int]returnOriginBackingTargets, len(callee.backingSlots))}
	for _, slot := range callee.backingSlots {
		i := int(slot)
		arg := slots[i]
		if arg.defaulted || len(arg.exprs) != 1 || (i < len(callee.candidate.Variadic) && callee.candidate.Variadic[i]) {
			return returnOriginBackingCall{}, false
		}
		expr := arg.exprs[0]
		if _, converted := u.Sema.ImplicitConversions[expr]; converted {
			return returnOriginBackingCall{}, false
		}
		actual, canonical := returnOriginContainer(in, u.Sema.ExprTypes[expr])
		formal, _ := returnOriginContainer(in, callee.info.Params[i])
		if !canonical || actual.family != formal.family {
			return returnOriginBackingCall{}, false
		}
		targets, proven := b.backingTargets(actual, actuals[i], pre, slices.Contains(callee.mutableBackingSlots, slot))
		if !proven {
			return returnOriginBackingCall{}, false
		}
		call.containers[i], call.targets[i] = actual, targets
	}
	return call, true
}

// instantiateBackingCall reads the result and every post-state from one frozen
// pre-call snapshot and applies each writable formal's post as a weak write.
func (b *returnOriginBody) instantiateBackingCall(call returnOriginBackingCall, summary returnOriginValue, posts map[uint32]returnOriginValue,
	actuals []returnOriginValue, pre returnOriginEnv, span source.Span,
) (returnOriginValue, returnOriginEnv) {
	value := b.substituteBackingCall(call, summary, actuals, pre, span)
	env := pre
	for _, slot := range call.callee.mutableBackingSlots {
		post, present := posts[slot]
		if present {
			post = b.substituteBackingCall(call, post, actuals, pre, span)
		} else {
			b.pending(span, "container call lacks its callee's complete post-state")
			post = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
		env = b.storeBackingContents(env, call.containers[int(slot)], call.targets[int(slot)], post, nil, span)
	}
	return value, env
}

// substituteBackingCall rebases one callee payload into the caller: V(i) is the
// captured actual, E(i) what i's targets held before the call, and L(i) the
// loans those targets carried; any other root stays an explicit refusal.
func (b *returnOriginBody) substituteBackingCall(call returnOriginBackingCall, payload returnOriginValue, actuals []returnOriginValue, pre returnOriginEnv,
	span source.Span,
) returnOriginValue {
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	out := returnOriginValueOf()
	if len(payload.callables) != 0 {
		b.pending(span, "callee returned an unproved source")
		out = unknown
	}
	for _, root := range payload.roots {
		i := int(root.param)
		targets, backed := call.targets[i]
		switch {
		case root.kind != returnOriginParam || root.expired || int64(root.param) >= int64(len(actuals)):
			b.pending(span, "callee returned an unproved source")
			out = out.join(unknown)
		case root.selector == returnOriginInputValue:
			out = out.join(actuals[root.param])
		case root.selector == returnOriginInputElements && backed:
			out = out.join(b.loadBackingContents(pre, call.containers[i], targets, span))
		case root.selector == returnOriginInputLoans && backed:
			out = out.join(returnOriginTargetLoans(pre, targets))
		default:
			b.pending(span, "container-content result lacks its checked backing call transfer")
			out = out.join(unknown)
		}
	}
	return out
}

// returnOriginTargetLoans is what the targets' container values carry as loans:
// an owning local's value, or the caller's own L(slot) for a formal.
func returnOriginTargetLoans(env returnOriginEnv, t returnOriginBackingTargets) returnOriginValue {
	out := returnOriginValueOf()
	for _, root := range t.locals {
		out = out.join(env.value(root.binding))
	}
	for _, slot := range t.slots {
		out = out.join(returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: slot, selector: returnOriginInputLoans}))
	}
	return out
}

// loanGuardFormal is the G6-ii qualification: a by-value formal whose type holds
// no borrow would silently erase a loan its actual carries. Only a source body's
// original generic formal is exempt; its template seeding keeps the loan.
func (b *returnOriginBody) loanGuardFormal(callee *returnOriginFunction, signature *returnOriginSignature, info *types.FnInfo, callback bool, i int) bool {
	in := b.function.unit.Sema.TypeInterner
	free := func(params []types.TypeID) bool {
		return i < len(params) && !returnOriginIsReference(in, params[i]) && returnOriginTypeShape(in, params[i], nil) == returnOriginRefFree
	}
	switch {
	case callback || callee == nil:
		return info != nil && free(info.Params)
	case callee.item.Body.IsValid():
		return !callee.directTemplateParam(callee.info.Params[i]) && !types.ContainsGenericParam(in, callee.info.Params[i]) && free(callee.info.Params)
	case signature != nil:
		return free(signature.params)
	default:
		return free(callee.info.Params)
	}
}

// refuseLegacyBackingSummary guards the old substitution, which reads every
// Param root as the incoming value: E and L roots need a checked backing call.
func (b *returnOriginBody) refuseLegacyBackingSummary(summary returnOriginValue, env returnOriginEnv, span source.Span) (returnOriginValue, returnOriginEnv, bool) {
	for _, root := range summary.roots {
		if root.kind == returnOriginParam && (root.selector == returnOriginInputElements || root.selector == returnOriginInputLoans) {
			return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}),
				b.taintExternalCellEffects(env, span, "container-content result lacks its checked backing call transfer"), true
		}
	}
	return returnOriginValue{}, env, false
}
