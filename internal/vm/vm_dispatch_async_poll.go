package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/mir"
)

func (vm *VM) execInstrPoll(frame *Frame, instr *mir.Instr, writes []LocalWrite) (pollExecResult, *VMError) {
	res := pollExecResult{writes: writes}

	taskVal, vmErr := vm.evalOperand(frame, &instr.Poll.Task)
	if vmErr != nil {
		return res, vmErr
	}
	taskID, vmErr := vm.taskIDFromValue(taskVal)
	vm.dropValue(taskVal)
	if vmErr != nil {
		return res, vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return res, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	targetTask := exec.Task(taskID)
	if targetTask == nil {
		return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", taskID))
	}
	current := exec.Current()
	if current == 0 {
		return res, vm.eb.makeError(PanicUnimplemented, "async poll outside task")
	}
	currentTask := exec.Task(current)
	if currentTask == nil {
		return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", current))
	}
	if current == taskID {
		return res, vm.eb.makeError(PanicInvalidHandle, "task cannot await itself")
	}
	if currentTask.Cancelled {
		res.doJump = true
		res.jumpBB = instr.Poll.PendBB
		return res, nil
	}
	if targetTask.Status != asyncrt.TaskWaiting && targetTask.Status != asyncrt.TaskDone {
		exec.Wake(taskID)
	}
	if targetTask.Status == asyncrt.TaskDone {
		dstType, vmErr := vm.awaitResultType(frame, instr.Poll.Dst)
		if vmErr != nil {
			return res, vmErr
		}
		doneVal, vmErr := vm.taskResultFromTask(targetTask, dstType)
		if vmErr != nil {
			return res, vmErr
		}
		dst := instr.Poll.Dst
		if len(dst.Proj) == 0 {
			switch dst.Kind {
			case mir.PlaceGlobal:
				vmErr = vm.writeGlobal(dst.Global, doneVal)
				if vmErr != nil {
					return res, vmErr
				}
				res.hasStore = true
				res.storeLoc = Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}
				res.storeVal = doneVal
				res.doJump = true
				res.jumpBB = instr.Poll.ReadyBB
				return res, nil
			default:
				localID := dst.Local
				vmErr = vm.writeLocal(frame, localID, doneVal)
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
				res.jumpBB = instr.Poll.ReadyBB
				return res, nil
			}
		}
		loc, vmErr := vm.EvalPlace(frame, dst)
		if vmErr != nil {
			return res, vmErr
		}
		if vmErr := vm.storeLocation(loc, doneVal); vmErr != nil {
			return res, vmErr
		}
		res.hasStore = true
		res.storeLoc = loc
		res.storeVal = doneVal
		res.doJump = true
		res.jumpBB = instr.Poll.ReadyBB
		return res, nil
	}
	// Task not done - set pending park key and jump to pending block
	if targetTask.Kind != asyncrt.TaskKindCheckpoint {
		vm.asyncPendingParkKey = asyncrt.JoinKey(taskID)
	}
	res.doJump = true
	res.jumpBB = instr.Poll.PendBB
	return res, nil
}

func (vm *VM) execInstrJoinAll(frame *Frame, instr *mir.Instr, writes []LocalWrite) (pollExecResult, *VMError) {
	res := pollExecResult{writes: writes}

	scopeVal, vmErr := vm.evalOperand(frame, &instr.JoinAll.Scope)
	if vmErr != nil {
		return res, vmErr
	}
	scopeID, vmErr := vm.scopeIDFromValue(scopeVal)
	vm.dropValue(scopeVal)
	if vmErr != nil {
		return res, vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return res, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	current := exec.Current()
	if current == 0 {
		return res, vm.eb.makeError(PanicUnimplemented, "async join_all outside task")
	}
	done, pending, failfast := exec.JoinAllChildrenBlocking(scopeID)
	if !done {
		if pending == 0 {
			return res, vm.eb.makeError(PanicUnimplemented, "async join_all missing pending child")
		}
		vm.asyncPendingParkKey = asyncrt.JoinKey(pending)
		res.doJump = true
		res.jumpBB = instr.JoinAll.PendBB
		return res, nil
	}

	resultType, vmErr := vm.joinResultType(frame, instr.JoinAll.Dst)
	if vmErr != nil {
		return res, vmErr
	}
	doneVal := MakeBool(failfast, resultType)
	dst := instr.JoinAll.Dst
	if len(dst.Proj) == 0 {
		switch dst.Kind {
		case mir.PlaceGlobal:
			vmErr = vm.writeGlobal(dst.Global, doneVal)
			if vmErr != nil {
				return res, vmErr
			}
			res.hasStore = true
			res.storeLoc = Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}
			res.storeVal = doneVal
			res.doJump = true
			res.jumpBB = instr.JoinAll.ReadyBB
			return res, nil
		default:
			localID := dst.Local
			vmErr = vm.writeLocal(frame, localID, doneVal)
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
			res.jumpBB = instr.JoinAll.ReadyBB
			return res, nil
		}
	}
	return res, vm.eb.makeError(PanicUnimplemented, "join_all destination projection unsupported")
}
