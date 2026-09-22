package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// The refusals the crossing transfers name. A reply, or a value an anchored operation moves
// through a channel's ring, whose type can carry a reference, a storage loan or a task is not
// modelled: the analysis would read it as the caller's, and it is the far side's.
const (
	returnOriginOnReplyRefusal           = "an `on` crossing reply that can hold a reference, a storage loan or a task needs its reply origin"
	returnOriginAnchoredOperationRefusal = "an anchored channel operation that moves a reference, a storage loan or a task needs its channel crossing contract"
	returnOriginOnCaptureRefusal         = "an `on` crossing capture that can hold a reference, a storage loan or a task needs its capture origin"
)

// onCrossing is the transfer of an immediate `on dst { ... }` crossing (Block 2).
//
// The destination is an expression of this frame and is evaluated first
// (mir/lower_expr_crossing.go:71-77). The body is a frame of its own: it is lowered into a
// poll function whose state holds every capture by value, each read consuming
// (lower_expr_crossing.go:78-108, 248-328); a capture of reference type is refused by the
// checker, and any other capture that is not inert by onCrossingCaptures; and `return` cannot
// leave through it (SEM3147). So the body is walked as a block whose `ret` is its only exit and
// whose locals end where it ends, and what it writes never reaches this frame's bindings. This frame goes on whenever the
// destination was evaluated: the caller waits for the reply, and the reply can be `Cancelled`
// without the body finishing.
//
// The value is the reply, `TaskResult<T>`, made on the far side. Inside the body a capture is
// read under the caller's binding although it is the body's own copy, so a borrow of it looks
// like a borrow of the caller's storage; a reply is therefore fresh only when its type can hold
// nothing borrowed at all (crossingInert), and any other reply stays a named refusal.
func (b *returnOriginBody) onCrossing(id ast.ExprID, data *ast.ExprOnData, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	span := u.Builder.Exprs.Get(id).Span
	out := originExprValue(env, returnOriginValueOf())
	if data.Dest.IsValid() {
		dest, err := b.expr(data.Dest, env, targets)
		if err != nil || !dest.flow.normal.reachable {
			return dest, err
		}
		out = dest
	}
	scope := u.stmtScopes[data.Body]
	if !scope.IsValid() {
		return returnOriginExprResult{}, fmt.Errorf("return origins: on crossing body at %v has no owning scope", span)
	}
	body, err := b.stmt(data.Body, out.flow.normal.clone(), returnOriginTargets{scope: scope, block: scope})
	if err != nil {
		return returnOriginExprResult{}, err
	}
	for key := range body.exits {
		if key.kind != returnOriginBlockResult || key.target != scope {
			return returnOriginExprResult{}, fmt.Errorf("return origins: on crossing body at %v leaves by an exit its frame does not have", span)
		}
	}
	if err := b.onCrossingCaptures(id, span); err != nil {
		return returnOriginExprResult{}, err
	}
	out.value, out.storage = returnOriginValueOf(), returnOriginValue{}
	if !b.crossingInert(u.Sema.ExprTypes[id]) {
		b.pending(span, returnOriginOnReplyRefusal)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return out, nil
}

// onCrossingCaptures refuses, by name, a capture the body could hold something of this frame
// through. Inside the body a capture is read under the caller's binding although it is the
// body's own copy, so a borrow reached through it would look like the caller's storage. The
// checker's gate is not relied on for this: it read only a capture's surface type, and `own &T`
// passed it as an owned Copy value (N-TASK-27 review, B1). Every capture but the anchor's lease
// must be inert (crossingInert), as the reply must; one that is not keeps a named row at the
// capture. This refuses some programs the runtime would carry safely, such as a dynamic array
// that moves in: its loans are not modelled here.
func (b *returnOriginBody) onCrossingCaptures(id ast.ExprID, span source.Span) error {
	u := b.function.unit
	var record *CrossingLoweringInfo
	for i := range u.Sema.CrossingLowering {
		entry := &u.Sema.CrossingLowering[i]
		if entry.Expr != id || (entry.Kind != CrossingLoweringOnPlacement && entry.Kind != CrossingLoweringOnFarHandle) {
			continue
		}
		if record != nil {
			return fmt.Errorf("return origins: on crossing at %v has two checker records", span)
		}
		record = entry
	}
	if record == nil {
		return fmt.Errorf("return origins: on crossing at %v has no checker record", span)
	}
	for i := range record.Captures {
		capture := &record.Captures[i]
		if capture.Mode == CrossingCaptureAnchorLease {
			continue
		}
		if !b.crossingInert(capture.Type) {
			b.pending(capture.Span, returnOriginOnCaptureRefusal)
		}
	}
	return nil
}

// anchoredOperation is the transfer of a channel operation, or `close()`, through the anchor
// of an `on far_handle` block. The checker types it as a remote operation of that crossing
// and records the call in CrossingDispatchCalls (typeFarHandleCall,
// on_crossing_capture.go:37-83, 166-178): it has no callee and its selector is never typed.
// It runs on the owner's shard: a sent value goes into the channel's ring, which outlives
// this frame, and a received one comes out of it. Both are admitted only when their type is
// inert; anything else stays a named refusal.
func (b *returnOriginBody) anchoredOperation(id ast.ExprID, call *ast.ExprCallData, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, bool, error) {
	u := b.function.unit
	if _, dispatched := u.Sema.CrossingDispatchCalls[id]; !dispatched {
		return returnOriginExprResult{}, false, nil
	}
	member, ok := u.Builder.Exprs.Member(call.Target)
	if !ok || member == nil {
		return returnOriginExprResult{}, true, fmt.Errorf("return origins: anchored operation %d has no receiver in %s", id, u.SourceKey)
	}
	inert := b.crossingInert(u.Sema.ExprTypes[id])
	exprs := make([]ast.ExprID, 0, len(call.Args)+1)
	exprs = append(exprs, member.Target)
	for _, arg := range call.Args {
		exprs = append(exprs, arg.Value)
		inert = inert && b.crossingInert(u.Sema.ExprTypes[arg.Value])
	}
	flow := returnOriginFlow{normal: env}
	for _, expr := range exprs {
		var err error
		flow, err = flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
			evaluated, evalErr := b.expr(expr, next, targets)
			return evaluated.flow, evalErr
		})
		if err != nil {
			return returnOriginExprResult{}, true, err
		}
	}
	out := returnOriginExprResult{flow: flow}
	if !flow.normal.reachable {
		return out, true, nil
	}
	out.value = returnOriginValueOf()
	if !inert {
		b.pending(u.Builder.Exprs.Get(id).Span, returnOriginAnchoredOperationRefusal)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return out, true, nil
}

// crossingInert says a value of this type can cross between two frames without carrying
// anything of the frame it was made in: no reference, no storage loan, and no part a
// handle or a raw pointer could hide a borrow in (hidesBorrow). It is the question
// callConfinesBorrows asks of a confined call's result.
func (b *returnOriginBody) crossingInert(id types.TypeID) bool {
	if returnOriginTypeShape(b.function.unit.Sema.TypeInterner, id, nil) != returnOriginRefFree {
		return false
	}
	if b.loanWalk(id, true, b.analyzer.loanCarrier) {
		return false
	}
	return !b.hidesBorrow(id)
}
