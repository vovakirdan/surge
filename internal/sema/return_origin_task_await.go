package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/types"
)

// `await` is declared `await(self: own Task<T>) -> TaskResult<T>` (core/intrinsics.sg), and a Task
// handle is not Copy, so the exact match and the Copy arm of originalArgumentType refuse a
// receiver written without `own`: `t.await()`, `worker(x).await()`, `(async { ... }).await()`.
// The checker accepts that receiver and the call consumes the handle. The handle is one word
// naming a runtime task (returnOriginTaskLeaf), and what the running task borrows belongs to the
// task check (owner ruling 2026-09-15), which refuses every measured way a task that borrows a
// frame can leave it. So such a receiver is admitted at its own type, and the rest of the call is
// the ordinary body-less generic one, which already answers what the handle can carry out:
//   - the payload's origins travel in the handle's value (a call to an async fn yields its body's
//     summary), and the by-value formal holds no borrow, so a loan the handle carries meets the
//     loan-discard refusal at the receiver (loanGuardFormal);
//   - TaskResult<T> is fresh only under NoBorrowedState of T: a reference is refuted, and an
//     array, a map, a range, a task or a function value is unsupported, so a payload that can hold
//     a reference, a storage loan or a task keeps a named row at the call.
// An `own` receiver matched exactly before and still does; a reference, a pointer, a `far` handle
// (read from the crossing record instead) or an alias of Task keeps the refusal.

// awaitMovesTask answers whether fn is the certified core `await` and expr, its receiver, is the
// substituted Task handle itself, by value.
func (fn *returnOriginFunction) awaitMovesTask(u *returnOriginUnitIndex, expr ast.ExprID, params, args []types.TypeID) bool {
	if !returnOriginTaskAwait(fn) {
		return false
	}
	in := u.Sema.TypeInterner
	actual := u.Sema.ExprTypes[expr]
	typ, typed := in.Lookup(actual)
	return typed && typ.Kind == types.KindStruct && matchReturnOriginSourceType(in, fn.candidate.ReceiverType, actual, params, args) == ""
}

// returnOriginTaskAwait says fn is `await` of the core Task, by its retained declaration: a
// body-less core intrinsic of one template parameter under a Task<T> receiver whose only formal is
// `own Task<T>`. A name alone selects nothing.
func returnOriginTaskAwait(fn *returnOriginFunction) bool {
	if fn == nil || fn.name != "await" || !returnOriginCoreIntrinsic(fn, 1, 1) || !fn.candidate.HasSelf || len(fn.info.Params) != 1 {
		return false
	}
	in := fn.unit.Sema.TypeInterner
	receiver := fn.candidate.ReceiverType
	self, typed := in.Lookup(fn.info.Params[0])
	info, found := in.StructInfo(receiver)
	return typed && found && info != nil && self.Kind == types.KindOwn && self.Elem == receiver &&
		returnOriginTaskLeaf(fn, receiver) && slices.Equal(info.TypeArgs, fn.candidate.TemplateParams)
}
