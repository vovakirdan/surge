package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// returnOriginCellCall is a source-body call whose every external cell formal
// received one explicit typed argument with a complete caller target set.
// targets maps a callee formal index to the caller cells that argument names.
type returnOriginCellCall struct {
	callee  *returnOriginFunction
	targets map[int][]uint32
}

// cellCallTargets admits a call only through the callee's own monomorphic body.
// Defaults, variadics, conversions, generic, callback or deferred selections,
// and any cell argument without a complete target certificate leave the call on
// its existing refusal path.
func (b *returnOriginBody) cellCallTargets(callee *returnOriginFunction, indirect bool, slots []returnOriginArgument, actuals []returnOriginValue) (returnOriginCellCall, bool) {
	u := b.function.unit
	if indirect || callee == nil || callee.candidate == nil || !callee.item.Body.IsValid() || len(callee.cellSlots) == 0 ||
		len(slots) != len(callee.info.Params) || len(actuals) != len(slots) {
		return returnOriginCellCall{}, false
	}
	call := returnOriginCellCall{callee: callee, targets: make(map[int][]uint32, len(callee.cellSlots))}
	for _, slot := range callee.cellSlots {
		i := int(slot)
		arg := slots[i]
		if arg.defaulted || len(arg.exprs) != 1 || (i < len(callee.candidate.Variadic) && callee.candidate.Variadic[i]) {
			return returnOriginCellCall{}, false
		}
		expr := arg.exprs[0]
		if _, converted := u.Sema.ImplicitConversions[expr]; converted {
			return returnOriginCellCall{}, false
		}
		want, _ := returnOriginCellDescriptor(u.Sema.TypeInterner, callee.info.Params[i])
		have, cell := returnOriginCellDescriptor(u.Sema.TypeInterner, u.Sema.ExprTypes[expr])
		targets, reason := b.function.externalCellTargets(u.Sema.ExprTypes[expr], actuals[i])
		if !cell || have != want || reason != "" {
			return returnOriginCellCall{}, false
		}
		call.targets[i] = targets
	}
	return call, true
}

// instantiateCellCall reads the result and every post-state from one frozen
// pre-call snapshot, then writes the completed contributions at once. Two
// formals reaching the same caller cell union before the write, and only a
// destination every contributor names as its sole target is replaced.
func (b *returnOriginBody) instantiateCellCall(call returnOriginCellCall, result types.TypeID, summary returnOriginValue,
	posts map[uint32]returnOriginValue, actuals []returnOriginValue, pre returnOriginEnv, span source.Span,
) (returnOriginValue, returnOriginEnv) {
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	value := b.substituteCellCall(call, summary, actuals, pre, span)
	if _, outer := returnOriginCellDescriptor(b.function.unit.Sema.TypeInterner, result); outer {
		_, calleeReason := call.callee.externalCellTargets(call.callee.info.Result, summary)
		_, callerReason := b.function.externalCellTargets(result, value)
		if calleeReason != "" || callerReason != "" {
			b.pending(span, "external cell result lacks its checked source-body target")
			value = unknown
		}
	}
	writes := make(map[uint32]returnOriginValue)
	weak := make(map[uint32]bool)
	for _, slot := range call.callee.mutableCellSlots {
		post, present := posts[slot]
		if present {
			post = b.substituteCellCall(call, post, actuals, pre, span)
		} else {
			b.pending(span, "external cell call lacks its callee's complete post-state")
			post = unknown
		}
		targets := call.targets[int(slot)]
		for _, dst := range targets {
			writes[dst] = writes[dst].join(post)
			weak[dst] = weak[dst] || len(targets) != 1
		}
	}
	exact := make(map[uint32]bool, len(writes))
	aliased := returnOriginValue{}
	for dst, contribution := range writes {
		exact[dst] = !weak[dst]
		aliased = aliased.join(contribution)
	}
	// A caller cell no formal names may still alias a written one.
	if aliased.normal {
		for _, slot := range b.function.cellSlots {
			if _, written := writes[slot]; !written {
				writes[slot] = aliased
			}
		}
	}
	return value, applyExternalCellWrites(pre, writes, exact)
}

// substituteCellCall rebases one callee payload into the caller: V(i) is the
// captured actual, and R(i) is what i's caller cells held before the call.
func (b *returnOriginBody) substituteCellCall(call returnOriginCellCall, payload returnOriginValue, actuals []returnOriginValue, pre returnOriginEnv, span source.Span) returnOriginValue {
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	out := returnOriginValueOf()
	if len(payload.callables) != 0 {
		b.pending(span, "callee returned an unproved source")
		out = unknown
	}
	for _, root := range payload.roots {
		targets, cell := call.targets[int(root.param)]
		switch {
		case root.kind != returnOriginParam || root.expired || int64(root.param) >= int64(len(actuals)):
			b.pending(span, "callee returned an unproved source")
			out = out.join(unknown)
		case root.selector == returnOriginInputValue:
			out = out.join(actuals[root.param])
		case !cell:
			b.pending(span, "referent-content result lacks its checked cell call transfer")
			out = out.join(unknown)
		default:
			out = out.join(b.cellContents(pre, targets, span))
		}
	}
	return out
}

// applyExternalCellWrites applies completed contributions to a clone of pre.
// Only a destination marked exact is replaced; the rest keep what they held.
func applyExternalCellWrites(pre returnOriginEnv, writes map[uint32]returnOriginValue, exact map[uint32]bool) returnOriginEnv {
	if !pre.reachable {
		return pre
	}
	out := pre.clone()
	for slot, value := range writes {
		if !exact[slot] {
			value = pre.cell(slot).join(value)
		}
		out.cells[slot] = value.clone()
	}
	return out
}

// refuseLegacyCellSummary guards the old substitution, which reads every Param
// root as the incoming value: a callee R(i) means cell contents only a checked
// cell call can load, and an outer-cell result needs its source-body target.
func (b *returnOriginBody) refuseLegacyCellSummary(id ast.ExprID, summary returnOriginValue, env returnOriginEnv, span source.Span) (returnOriginValue, returnOriginEnv, bool) {
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	for _, root := range summary.roots {
		if root.kind == returnOriginParam && root.selector == returnOriginInputContents {
			return unknown, b.taintExternalCellEffects(env, span, "referent-content result lacks its checked cell call transfer"), true
		}
	}
	u := b.function.unit
	if _, outer := returnOriginCellDescriptor(u.Sema.TypeInterner, u.Sema.ExprTypes[id]); outer {
		return unknown, b.taintExternalCellEffects(env, span, "external cell result lacks its checked source-body target"), true
	}
	return returnOriginValue{}, env, false
}

// refuseUncheckedCellResult applies the outer-cell result guard to a call that
// returns early, before reaching the checked source-body path.
func (b *returnOriginBody) refuseUncheckedCellResult(id ast.ExprID, out *returnOriginExprResult) {
	if !id.IsValid() || !out.flow.normal.reachable || !out.value.normal {
		return
	}
	span := b.function.unit.Builder.Exprs.Get(id).Span
	if value, next, refused := b.refuseLegacyCellSummary(id, returnOriginValueOf(), out.flow.normal, span); refused {
		out.value, out.flow.normal = value, next
	}
}
