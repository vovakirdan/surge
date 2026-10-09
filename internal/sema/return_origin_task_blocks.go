package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// The refusals the task-block transfer names. A capture or a payload whose type can carry a
// reference, a storage loan or a task is not modelled: inside the body a capture is read under
// this frame's binding although the body holds its own copy, and the payload leaves in a
// handle that outlives the expression.
const (
	returnOriginTaskBlockPayloadRefusal = "an `async` or `blocking` block whose value can hold a reference, a storage loan or a task needs its payload origin"
	returnOriginTaskBlockCaptureRefusal = "an `async` or `blocking` block that captures a value which can hold a reference, a storage loan or a task needs its capture origin"
)

// taskBlockTransfer is the transfer of `async { ... }` and `blocking { ... }`.
//
// The value is a Task handle made here. A local async body starts from a cold
// frame (mir/lower_expr_misc.go); a blocking body is published to a pool thread
// (mir/lower_blocking.go). Both receive their own captured values. Walking the
// body therefore uses a separate environment; its writes do not replace the
// caller's bindings. Captures that could reach borrowed storage remain refused.
//
// The body is a frame of its own: `return` cannot leave it (SEM3207), so it is walked as a
// block whose `ret` is its only exit and whose locals end where it ends, and every row it
// raises is reported. Captures must be inert, except that a local async block may take
// its own counted Channel with an inert payload: the runtime retains that object for
// the task, and no payload can carry a borrow of this frame. Blocking captures do not
// use this certificate. The payload T of the Task<T> must still be inert, so
// the handle carries out nothing borrowed. What a running task borrows is the task check's
// (owner ruling 2026-09-15); a payload that is itself a task is refused here, as an `on`
// reply is.
func (b *returnOriginBody) taskBlockTransfer(id ast.ExprID, kind ast.ExprKind, env returnOriginEnv) (returnOriginExprResult, error) {
	u := b.function.unit
	span := u.Builder.Exprs.Get(id).Span
	body, captures := ast.NoStmtID, u.Sema.BlockingCaptures
	if kind == ast.ExprAsync {
		if data, ok := u.Builder.Exprs.Async(id); ok && data != nil {
			body, captures = data.Body, u.Sema.AsyncCaptures
		}
	} else if data, ok := u.Builder.Exprs.Blocking(id); ok && data != nil {
		body = data.Body
	}
	scope := u.stmtScopes[body]
	if !body.IsValid() || !scope.IsValid() || captures == nil {
		return returnOriginExprResult{}, fmt.Errorf("return origins: task block at %v has no body scope or capture record", span)
	}
	walked, err := b.stmt(body, env.clone(), returnOriginTargets{scope: scope, block: scope})
	if err != nil {
		return returnOriginExprResult{}, err
	}
	for key := range walked.exits {
		if key.kind != returnOriginBlockResult || key.target != scope {
			return returnOriginExprResult{}, fmt.Errorf("return origins: task block at %v leaves by an exit its frame does not have", span)
		}
	}
	for _, capture := range captures[id] {
		sym := u.Symbols.Table.Symbols.Get(capture)
		// A counted handle owns its object; its payload must hide no borrowed state.
		if sym != nil && u.Sema.TypeInterner.IsRefCountedHandle(sym.Type) {
			payloads, handle := u.Sema.TypeInterner.RuntimeHandlePayloads(sym.Type)
			if handle && len(payloads) == 1 && b.crossingInert(payloads[0]) {
				continue
			}
		}
		if sym == nil || !b.crossingInert(sym.Type) && !b.nullTaskCapture(capture, sym.Type, env) {
			b.pending(span, returnOriginTaskBlockCaptureRefusal)
		}
	}
	out := originExprValue(env, returnOriginValueOf())
	in := u.Sema.TypeInterner
	payloads, handle := in.RuntimeHandlePayloads(u.Sema.ExprTypes[id])
	if !handle || !in.IsRuntimeHandleType(u.Sema.ExprTypes[id]) || len(payloads) != 1 || !b.crossingInert(payloads[0]) {
		b.pending(span, returnOriginTaskBlockPayloadRefusal)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return out, nil
}

// nullTaskCapture is the one Task capture this transfer certifies: a core Task binding
// that is still its declared default here -- the null handle, which names no runtime
// object and so holds no borrow (return_origin_requirements.go, the defaultable walk;
// owner ruling 2026-09-26). The block takes it by a consuming read when it is made
// (mir/lower_expr_misc.go, one read per capture), so only what happened before this
// point matters.
//
// Two facts must both hold. The flow fact: the binding was declared without an
// initializer and no `=` assignment reaches here on any path (defaultNull, cleared by
// every assign and joined by AND). The syntax fact: outside task-block bodies, whose
// captures are their own copies, the binding is never named except as the target of a
// plain `=`. Every other way to write it -- `&mut`, an implicit borrow of a receiver or
// argument, an operator -- names it somewhere else, and then nothing is certified. A
// Task obtained any other way still meets NoBorrowedState (R-i's fence, RV2-DEBT-365).
func (b *returnOriginBody) nullTaskCapture(capture symbols.SymbolID, typ types.TypeID, env returnOriginEnv) bool {
	binding, bound := env.bindings[capture]
	if !bound || !binding.defaultNull || !b.analyzer.isCoreTask(b.function.unit.Sema.TypeInterner, typ) {
		return false
	}
	return b.namedOnlyAsAssignTarget(capture)
}

// namedOnlyAsAssignTarget scans the owning unit's expressions, not a tree walk that
// could miss a kind: every expression resolved to the binding must be the left side of
// a plain `=` or lie inside an `async`/`blocking` body. A mention inside an `on` or
// `spawn on` body is refused whatever it is, since this walk does not claim to know
// how that body shares the binding.
func (b *returnOriginBody) namedOnlyAsAssignTarget(capture symbols.SymbolID) bool {
	u := b.function.unit
	targets := make(map[ast.ExprID]bool)
	var blocks, crossings []source.Span
	for raw := uint32(1); raw <= u.Builder.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		node := u.Builder.Exprs.Get(id)
		if node == nil {
			continue
		}
		switch node.Kind {
		case ast.ExprAsync, ast.ExprBlocking:
			blocks = append(blocks, node.Span)
		case ast.ExprOn:
			crossings = append(crossings, node.Span)
		case ast.ExprBinary:
			if data, ok := u.Builder.Exprs.Binary(id); ok && data != nil && data.Op == ast.ExprBinaryAssign {
				targets[data.Left] = true
			}
		}
	}
	for id, symID := range u.Symbols.ExprSymbols {
		if symID != capture {
			continue
		}
		node := u.Builder.Exprs.Get(id)
		if node == nil || node.Span.Empty() {
			return false
		}
		if returnOriginSpanWithin(node.Span, crossings) || !targets[id] && !returnOriginSpanWithin(node.Span, blocks) {
			return false
		}
	}
	return true
}

func returnOriginSpanWithin(span source.Span, regions []source.Span) bool {
	for _, region := range regions {
		if region.File == span.File && region.Start <= span.Start && span.End <= region.End {
			return true
		}
	}
	return false
}
