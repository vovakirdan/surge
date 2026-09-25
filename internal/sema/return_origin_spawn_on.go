package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// The refusals the `spawn on` transfer names. A far task's result that can carry a reference,
// a storage loan or a task is not modelled: it is made on the far side from the body's own
// copies, and the analysis would read a borrow of such a copy as a borrow of the caller's.
const (
	returnOriginSpawnOnPayloadRefusal = "a `spawn on` task result that can hold a reference, a storage loan or a task needs its payload origin"
	returnOriginSpawnOnCaptureRefusal = "a `spawn on` capture that can hold a reference, a storage loan or a task needs its capture origin"
	returnOriginSpawnOnHandleRefusal  = "a `spawn on` whose value is not the far Task of its body's result needs its origin transfer"
)

// spawnOnTransfer is the transfer of `spawn on dst { ... }`.
//
// The destination is an expression of this frame and is evaluated first, as for `on`. The body
// is a frame of its own and is walked exactly as an `on` body is: its `ret` is its only exit, its
// locals end where it ends, and what it writes never reaches this frame. Unlike `on`, this frame
// does not wait for the body: the body is published on the destination as a task of its own and
// the value is a `far Task<T>` handle that names it. The task lives past this frame by design,
// so what it holds must be nothing of this frame: every capture is checked inert, and so is its
// result `T`, which comes back only through `far Task<T>.await()`. A handle over such a task
// carries no origin at all, so the value is fresh.
func (b *returnOriginBody) spawnOnTransfer(id ast.ExprID, data *ast.ExprOnData, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
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
	if err := b.crossingBody(data.Body, out.flow.normal.clone(), span); err != nil {
		return returnOriginExprResult{}, err
	}
	record, err := b.crossingRecord(id, span, CrossingLoweringSpawnOn)
	if err != nil {
		return returnOriginExprResult{}, err
	}
	b.crossingCaptures(record, returnOriginSpawnOnCaptureRefusal)
	out.value, out.storage = returnOriginValueOf(), returnOriginValue{}
	if !b.farTaskOf(u.Sema.ExprTypes[id], record.PayloadType) {
		b.pending(span, returnOriginSpawnOnHandleRefusal)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	} else if !b.crossingInert(record.PayloadType) {
		b.pending(span, returnOriginSpawnOnPayloadRefusal)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return out, nil
}

// crossingBody walks a crossing body as a frame of its own whose `ret` is its only exit.
func (b *returnOriginBody) crossingBody(body ast.StmtID, env returnOriginEnv, span source.Span) error {
	u := b.function.unit
	scope := u.stmtScopes[body]
	if !scope.IsValid() {
		return fmt.Errorf("return origins: crossing body at %v has no owning scope", span)
	}
	walked, err := b.stmt(body, env, returnOriginTargets{scope: scope, block: scope})
	if err != nil {
		return err
	}
	for key := range walked.exits {
		if key.kind != returnOriginBlockResult || key.target != scope {
			return fmt.Errorf("return origins: crossing body at %v leaves by an exit its frame does not have", span)
		}
	}
	return nil
}

// crossingRecord returns the checker's one record of kind for the crossing node id.
func (b *returnOriginBody) crossingRecord(id ast.ExprID, span source.Span, kinds ...CrossingLoweringKind) (*CrossingLoweringInfo, error) {
	u := b.function.unit
	var record *CrossingLoweringInfo
	for i := range u.Sema.CrossingLowering {
		entry := &u.Sema.CrossingLowering[i]
		match := false
		for _, kind := range kinds {
			match = match || entry.Kind == kind
		}
		if entry.Expr != id || !match {
			continue
		}
		if record != nil {
			return nil, fmt.Errorf("return origins: crossing at %v has two checker records", span)
		}
		record = entry
	}
	if record == nil {
		return nil, fmt.Errorf("return origins: crossing at %v has no checker record", span)
	}
	return record, nil
}

// crossingCaptures keeps a named row at every capture but an anchor's lease that is not inert.
func (b *returnOriginBody) crossingCaptures(record *CrossingLoweringInfo, refusal string) {
	for i := range record.Captures {
		capture := &record.Captures[i]
		if capture.Mode == CrossingCaptureAnchorLease {
			continue
		}
		if !b.crossingInert(capture.Type) {
			b.pending(capture.Span, refusal)
		}
	}
}

// farTaskOf says ty is `far Task<payload>`.
func (b *returnOriginBody) farTaskOf(ty, payload types.TypeID) bool {
	in := b.function.unit.Sema.TypeInterner
	far, ok := in.Lookup(returnOriginResolveAlias(in, ty))
	if !ok || far.Kind != types.KindFar {
		return false
	}
	payloads, handle := in.RuntimeHandlePayloads(far.Elem)
	return handle && in.IsRuntimeHandleType(far.Elem) && len(payloads) == 1 && payloads[0] == payload
}
