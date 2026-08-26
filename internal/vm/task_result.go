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

func (vm *VM) taskResultFromTask(task *asyncrt.Task[Value], resultType types.TypeID) (Value, *VMError) {
	if task == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing task result")
	}
	switch task.ResultKind {
	case asyncrt.TaskResultSuccess:
		return vm.taskResultValue(resultType, asyncrt.TaskResultSuccess, task.ResultValue)
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
		payload, vmErr := vm.cloneValueComposite(value)
		if vmErr != nil {
			return Value{}, vmErr
		}
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
