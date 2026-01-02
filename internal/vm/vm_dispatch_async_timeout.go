package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/mir"
)

func (vm *VM) execInstrTimeout(frame *Frame, instr *mir.Instr, writes []LocalWrite) (pollExecResult, *VMError) {
	res := pollExecResult{writes: writes}

	exec := vm.ensureExecutor()
	if exec == nil {
		return res, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	current := exec.Current()
	if current == 0 {
		return res, vm.eb.makeError(PanicUnimplemented, "async timeout outside task")
	}
	currentTask := exec.Task(current)
	if currentTask == nil {
		return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", current))
	}

	timeoutID := currentTask.TimeoutTaskID
	if timeoutID == 0 {
		taskVal, vmErr := vm.evalOperand(frame, &instr.Timeout.Task)
		if vmErr != nil {
			return res, vmErr
		}
		ownsTask := taskVal.IsHeap()
		if ownsTask {
			defer vm.dropValue(taskVal)
		}
		targetID, vmErr := vm.taskIDFromValue(taskVal)
		if vmErr != nil {
			return res, vmErr
		}
		if targetID == current {
			return res, vm.eb.makeError(PanicInvalidHandle, "task cannot await itself")
		}

		delayVal, vmErr := vm.evalOperand(frame, &instr.Timeout.Ms)
		if vmErr != nil {
			return res, vmErr
		}
		defer vm.dropValue(delayVal)
		delay, vmErr := vm.uintValueToInt(delayVal, "timeout duration out of range")
		if vmErr != nil {
			return res, vmErr
		}

		resultType, vmErr := vm.joinResultType(frame, instr.Timeout.Dst)
		if vmErr != nil {
			return res, vmErr
		}

		timeoutID = exec.SpawnTimeout(&timeoutState{
			target:     targetID,
			delayMs:    uint64(delay), //nolint:gosec // delay is bounded by uintValueToInt
			resultType: resultType,
		})
		currentTask.TimeoutTaskID = timeoutID
	}

	timeoutTask := exec.Task(timeoutID)
	if timeoutTask == nil {
		return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", timeoutID))
	}
	if timeoutTask.Status != asyncrt.TaskWaiting && timeoutTask.Status != asyncrt.TaskDone {
		exec.Wake(timeoutID)
	}
	if timeoutTask.Status == asyncrt.TaskDone {
		var doneVal Value
		switch timeoutTask.ResultKind {
		case asyncrt.TaskResultSuccess:
			val, ok := timeoutTask.ResultValue.(Value)
			if !ok {
				return res, vm.eb.makeError(PanicTypeMismatch, "invalid timeout result type")
			}
			clone, vmErr := vm.cloneForShare(val)
			if vmErr != nil {
				return res, vmErr
			}
			doneVal = clone
		case asyncrt.TaskResultCancelled:
			resultType, vmErr := vm.joinResultType(frame, instr.Timeout.Dst)
			if vmErr != nil {
				return res, vmErr
			}
			cancelled, vmErr := vm.taskResultValue(resultType, asyncrt.TaskResultCancelled, Value{})
			if vmErr != nil {
				return res, vmErr
			}
			doneVal = cancelled
		default:
			return res, vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
		}

		currentTask.TimeoutTaskID = 0
		dst := instr.Timeout.Dst
		if len(dst.Proj) == 0 {
			switch dst.Kind {
			case mir.PlaceGlobal:
				vmErr := vm.writeGlobal(dst.Global, doneVal)
				if vmErr != nil {
					return res, vmErr
				}
				res.hasStore = true
				res.storeLoc = Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}
				res.storeVal = doneVal
				res.doJump = true
				res.jumpBB = instr.Timeout.ReadyBB
				return res, nil
			default:
				localID := dst.Local
				vmErr := vm.writeLocal(frame, localID, doneVal)
				if vmErr != nil {
					return res, vmErr
				}
				stored := frame.Locals[localID].V
				writes = append(writes, LocalWrite{
					LocalID: localID,
					Name:    frame.Locals[localID].Name,
					Value:   stored,
				})
				res.writes = writes
				res.doJump = true
				res.jumpBB = instr.Timeout.ReadyBB
				return res, nil
			}
		}
		return res, vm.eb.makeError(PanicUnimplemented, "timeout destination projection unsupported")
	}

	vm.asyncPendingParkKey = asyncrt.JoinKey(timeoutID)
	res.doJump = true
	res.jumpBB = instr.Timeout.PendBB
	return res, nil
}
