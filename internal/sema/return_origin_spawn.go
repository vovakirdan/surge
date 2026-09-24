package sema

import (
	"fmt"

	"surge/internal/ast"
)

// returnOriginSpawnOperandRefusal names a `spawn` whose value is not the Task handle its operand
// evaluates to. Typing gives the two one type (typeSpawnExpr), so on a checked program this row
// never stands; it keeps the transfer fail-closed if that ever changes.
const returnOriginSpawnOperandRefusal = "a `spawn` whose operand is not the Task handle it publishes needs its origin transfer"

// spawnTransfer is the transfer of `spawn X`.
//
// The operand is evaluated in this frame by its own transfer -- a call through its callee's
// summary, a name through its binding, an `async { }` block through taskBlockTransfer, which keeps
// its capture and payload tests -- and the result is that value unchanged. The runtime wakes the
// task the handle names and stores the same handle (mir/lower_expr_misc.go lowerSpawnExpr: one
// InstrSpawn over the operand; vm execInstrSpawn and llvm emitInstrSpawn store the operand's word),
// so what the operand's transfer proved about the handle, and about the payload whose origins
// travel in it, holds for the spawned handle. The result is a temporary, never a place of the
// operand's binding, so it has no storage of its own.
//
// Publishing moves the start of the body earlier (docs/RUNTIME.md 3.1: a call-made task is COLD
// until its first spawn, await, cancel or scope join), never outside the window between the call
// and the join. What the running task borrows in that window is the task check's (owner ruling
// 2026-09-15): every place with a binding it was lent is pinned until a join on every path, a lent
// temporary keeps RV2-DEBT-368's row at its argument, and a reference an inner block captures keeps
// P1u-TC2's and N-TASK-23/24's rows. `spawn on` is an ExprOn and is not this transfer.
func (b *returnOriginBody) spawnTransfer(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	span := u.Builder.Exprs.Get(id).Span
	data, ok := u.Builder.Exprs.Spawn(id)
	if !ok || data == nil || !data.Value.IsValid() {
		return returnOriginExprResult{}, fmt.Errorf("return origins: spawn at %v has no operand", span)
	}
	out, err := b.expr(data.Value, env, targets)
	if err != nil || !out.flow.normal.reachable {
		return out, err
	}
	out.storage = returnOriginValue{}
	in := u.Sema.TypeInterner
	ty := u.Sema.ExprTypes[id]
	payloads, handle := in.RuntimeHandlePayloads(ty)
	if ty != u.Sema.ExprTypes[data.Value] || !handle || !in.IsRuntimeHandleType(ty) || len(payloads) != 1 {
		b.pending(span, returnOriginSpawnOperandRefusal)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return out, nil
}
