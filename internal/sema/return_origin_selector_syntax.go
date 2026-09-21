package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/types"
)

// `is` and `heir` test a value against a type or tag written as syntax. The
// checker resolves that operand into IsOperands/HeirOperands and never types it,
// and lowering reads the record, so the right side is never evaluated and has no
// origin. The tested value is only read; a bool keeps none of its loans (G6).
// left is the caller's evaluated left operand, written only once the record is found.
func (b *returnOriginBody) typeTest(id ast.ExprID, data *ast.ExprBinaryData, left *returnOriginExprResult) (returnOriginExprResult, bool) {
	u := b.function.unit
	recorded := false
	switch data.Op {
	case ast.ExprBinaryIs:
		_, recorded = u.Sema.IsOperands[id]
	case ast.ExprBinaryHeir:
		_, recorded = u.Sema.HeirOperands[id]
	}
	if !recorded {
		return returnOriginExprResult{}, false
	}
	span := u.Builder.Exprs.Get(id).Span
	if b.shape(id) != returnOriginRefFree {
		b.pending(span, "type test result has an unresolved type")
		left.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	} else {
		left.value = b.discardLoans(left.value, span)
	}
	left.storage = returnOriginValue{}
	return *left, true
}

// Enum::Variant is a compile-time constant: lowering emits a literal and never
// reads the target, so the member has no origin.
func (b *returnOriginBody) enumVariant(id ast.ExprID, data *ast.ExprMemberData, use EnumVariantUse, env returnOriginEnv) returnOriginExprResult {
	span := b.function.unit.Builder.Exprs.Get(id).Span
	if use.Target != data.Target || use.Enum == types.NoTypeID || b.shape(id) != returnOriginRefFree {
		return b.unknownExpr(env, span, "enum variant lacks its checked enum target")
	}
	return originExprValue(env, returnOriginValueOf())
}

// far Task<T>.await()/.cancel() and far Channel<T>.share() are typed as
// crossings (CrossingLowering), not as methods: no callee, and the selector is
// never typed. Lowering emits the crossing from that record. await/cancel
// consume the handle; share keeps it and mints a sibling lease. The result is
// produced by the owning shard.
func (b *returnOriginBody) farSelector(id ast.ExprID, call *ast.ExprCallData, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, bool, error) {
	u := b.function.unit
	var record *CrossingLoweringInfo
	for i := range u.Sema.CrossingLowering {
		entry := &u.Sema.CrossingLowering[i]
		if entry.Expr != id || !farSelectorKind(entry.Kind) {
			continue
		}
		if record != nil {
			return returnOriginExprResult{}, true, fmt.Errorf("return origins: duplicate far selector evidence for expression %d in %s", id, u.SourceKey)
		}
		record = entry
	}
	if record == nil {
		return returnOriginExprResult{}, false, nil
	}
	member, ok := u.Builder.Exprs.Member(call.Target)
	if !ok || member == nil || record.ReceiverExpr != member.Target || record.ResultType != u.Sema.ExprTypes[id] ||
		record.ConsumesHandle == (record.Kind == CrossingLoweringChannelShare) {
		return returnOriginExprResult{}, true, fmt.Errorf("return origins: inconsistent far selector evidence for expression %d in %s", id, u.SourceKey)
	}
	out, err := b.expr(member.Target, env, targets)
	if err != nil || !out.flow.normal.reachable {
		return out, true, err
	}
	span := u.Builder.Exprs.Get(id).Span
	if len(call.Args) != 0 { // the checker types a stray argument without refusing it (spawn_on_crossing.go:172–175)
		b.pending(span, "`await()`, `cancel()` and `share()` on a far handle take no arguments; remove the argument")
		out.value, out.storage = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}), returnOriginValue{}
		return out, true, nil
	}
	if b.shape(id) != returnOriginRefFree {
		b.pending(span, "far selector result needs its payload crossing contract")
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	} else {
		out.value = b.discardLoans(out.value, span)
	}
	out.storage = returnOriginValue{}
	return out, true, nil
}

// The crossings whose member target is selector syntax over a far handle.
func farSelectorKind(kind CrossingLoweringKind) bool {
	return kind == CrossingLoweringFarTaskAwait || kind == CrossingLoweringFarTaskCancel || kind == CrossingLoweringChannelShare
}
