package vm

import (
	"surge/internal/asyncrt"
	"surge/internal/types"
)

// How a completed task's result becomes a `TaskResult<T>` for one asker.
//
// The result itself lives in storage the TASK owns — a transport extent the
// producing poll copied it into before its activation retired — and every
// asker is served OUT of that storage without emptying it. That is why the
// payload step is a duplication rather than a move: today more than one asker
// can arrive (a cloned handle is a second one), and the canonical value has to
// still be there for the next.
//
// `cloneValueComposite` is the duplication, and it is the language's own: a
// value composite gets its own extent, a refcounted immutable block is shared
// and counted, plain bits copy. It generalises the retain this used to do —
// for everything that is not a composite the two are the same call — and it is
// the only spelling that is correct for a composite, which cannot be shared by
// counting because there is no count.
//
// The LAST asker does not duplicate at all. It moves the canonical value out
// and leaves the slot empty, because a duplication made for nobody is a copy
// and a destruction of the same value back to back. For a closed cohort of `E`
// entitlements all successfully consumed that is exactly `E-1` duplications
// and one move, which is the cost the storage model states.

func (vm *VM) taskResultFromTask(task *asyncrt.Task[Value], resultType types.TypeID) (Value, *VMError) {
	if task == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing task result")
	}
	switch task.ResultKind {
	case asyncrt.TaskResultSuccess:
		payload, vmErr := vm.takeCanonicalResult(task)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.taskResultValue(resultType, asyncrt.TaskResultSuccess, payload)
	case asyncrt.TaskResultCancelled:
		return vm.taskResultValue(resultType, asyncrt.TaskResultCancelled, Value{})
	default:
		return Value{}, vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
	}
}

func (vm *VM) taskResultValue(resultType types.TypeID, kind asyncrt.TaskResultKind, value Value) (Value, *VMError) {
	layout, vmErr := vm.tagLayoutFor(resultType)
	if vmErr != nil {
		return Value{}, vmErr
	}
	var (
		tagName string
		fields  []Value
	)
	switch kind {
	case asyncrt.TaskResultSuccess:
		tagName = "Success"
		tc, ok := layout.CaseByName(tagName)
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult missing Success tag")
		}
		if len(tc.PayloadTypes) != 1 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult Success expects payload")
		}
		// The payload arrives ALREADY OWNED: whoever asked for it has decided
		// whether this asker duplicates or moves, which is a question about
		// the cohort and not about the tag being built here.
		payload := value
		if tc.PayloadTypes[0] != types.NoTypeID {
			payload.TypeID = tc.PayloadTypes[0]
		}
		fields = []Value{payload}
		return vm.buildTag(vm.currentFrame(), resultType, tc.TagSym, fields)
	case asyncrt.TaskResultCancelled:
		tagName = "Cancelled"
		tc, ok := layout.CaseByName(tagName)
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult missing Cancelled tag")
		}
		if len(tc.PayloadTypes) != 0 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult Cancelled expects no payload")
		}
		return vm.buildTag(vm.currentFrame(), resultType, tc.TagSym, nil)
	default:
		return Value{}, vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
	}
}

// spawnTimeoutTask starts the runtime task a `timeout(t, ms)` operation drives,
// and records the claim that operation holds on its result.
//
// The claim is what makes the task's result owned rather than abandoned. A
// timeout task is spawned by the runtime and named by nothing in source, so no
// handle is ever created for it and no handle is ever freed for it; without a
// claim its cohort reads empty from the moment it exists, and completion
// therefore releases the result out from under the operation that asked for it.
// Registering a handle instead was tried in RV2-DEBT-167's lane and detonated
// on the spot, because a handle also has to be given up by something and
// nothing owns this one.
func (vm *VM) spawnTimeoutTask(exec *asyncrt.Executor[Value], state *timeoutState) asyncrt.TaskID {
	id := exec.SpawnTimeout(state)
	vm.taskClaimTaken(id)
	return id
}

// takeTimeoutOutcome takes a completed timeout task's result and retires the
// claim in the same step.
//
// The order is the model's: the value is taken FIRST and the claim retired
// afterwards, so the canonical result is pinned for exactly as long as somebody
// is reading it. Retiring first would release the value this call is about to
// duplicate.
func (vm *VM) takeTimeoutOutcome(task *asyncrt.Task[Value], resultType types.TypeID) (Value, *VMError) {
	if task == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing timeout task")
	}
	defer vm.taskClaimRetired(task.ID)
	switch task.ResultKind {
	case asyncrt.TaskResultSuccess:
		return vm.takeCanonicalResult(task)
	case asyncrt.TaskResultCancelled:
		return vm.taskResultValue(resultType, asyncrt.TaskResultCancelled, Value{})
	default:
		return Value{}, vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
	}
}

// takeCanonicalResult serves one asker out of the task's canonical result.
//
// Every asker but the last gets a duplication and leaves the value where it is,
// because somebody else can still come for it. The last one MOVES it and
// empties the slot: nothing can ask again, so duplicating would build a second
// value only to destroy the first.
//
// The slot is emptied in the same step the value leaves it. A moved-from slot
// that still named its value would be a second owner of it, and the release
// paths that later find an empty slot then correctly do nothing.
func (vm *VM) takeCanonicalResult(task *asyncrt.Task[Value]) (Value, *VMError) {
	if task == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing task")
	}
	if vm.taskHasOtherAskers(task.ID) {
		return vm.cloneValueComposite(task.ResultValue)
	}
	taken := task.ResultValue
	task.ResultValue = Value{}
	return taken, nil
}
