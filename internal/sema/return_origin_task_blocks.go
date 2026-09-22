package sema

import (
	"fmt"

	"surge/internal/ast"
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
// The value is a Task handle made here; the body runs later, as a function of its own
// (mir/lower_expr_misc.go:170-229) or as a job on a pool thread (mir/lower_blocking.go), and
// each takes its captures, the checker's record of the node, into its own state by a consuming
// read before it starts (hir/lower_expr_control.go:186-190, 214-218). Nothing of the body runs
// before this frame goes on, so this frame continues with the environment it had, whenever
// the expression is reached, and nothing the body writes reaches its bindings.
//
// The body is a frame of its own: `return` cannot leave it (SEM3207), so it is walked as a
// block whose `ret` is its only exit and whose locals end where it ends, and every row it
// raises is reported. Two things are asked of types alone. Every recorded capture must be
// inert, so the body can reach nothing of this frame that holds a reference, a loan or a task,
// and what it writes cannot be this frame's. The payload T of the Task<T> must be inert, so
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
		if sym := u.Symbols.Table.Symbols.Get(capture); sym == nil || !b.crossingInert(sym.Type) {
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
