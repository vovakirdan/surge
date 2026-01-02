package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/mir"
	"surge/internal/types"
)

type pollExecResult struct {
	hasStore bool
	storeLoc Location
	storeVal Value
	writes   []LocalWrite
	doJump   bool
	jumpBB   mir.BlockID
}

func (vm *VM) execInstrAwait(frame *Frame, instr *mir.Instr, writes []LocalWrite) (hasStore bool, storeLoc Location, storeVal Value, writesOut []LocalWrite, vmErr *VMError) {
	taskVal, vmErr := vm.evalOperand(frame, &instr.Await.Task)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	taskID, vmErr := vm.taskIDFromValue(taskVal)
	vm.dropValue(taskVal)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	dstType, vmErr := vm.awaitResultType(frame, instr.Await.Dst)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	res, vmErr := vm.runUntilDone(taskID, dstType)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	dst := instr.Await.Dst
	if len(dst.Proj) == 0 {
		switch dst.Kind {
		case mir.PlaceGlobal:
			vmErr = vm.writeGlobal(dst.Global, res)
			if vmErr != nil {
				return false, Location{}, Value{}, writes, vmErr
			}
			return true, Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}, res, writes, nil
		default:
			localID := dst.Local
			vmErr = vm.writeLocal(frame, localID, res)
			if vmErr != nil {
				return false, Location{}, Value{}, writes, vmErr
			}
			stored := frame.Locals[localID].V
			writes = append(writes, LocalWrite{
				LocalID: localID,
				Name:    frame.Locals[localID].Name,
				Value:   stored,
			})
			return false, Location{}, Value{}, writes, nil
		}
	}
	loc, vmErr := vm.EvalPlace(frame, dst)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	if vmErr := vm.storeLocation(loc, res); vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	return true, loc, res, writes, nil
}

func (vm *VM) execInstrSpawn(frame *Frame, instr *mir.Instr, writes []LocalWrite) (hasStore bool, storeLoc Location, storeVal Value, writesOut []LocalWrite, vmErr *VMError) {
	taskVal, vmErr := vm.evalOperand(frame, &instr.Spawn.Value)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	taskID, vmErr := vm.taskIDFromValue(taskVal)
	if vmErr != nil {
		vm.dropValue(taskVal)
		return false, Location{}, Value{}, writes, vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		vm.dropValue(taskVal)
		return false, Location{}, Value{}, writes, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	exec.Wake(taskID)
	dst := instr.Spawn.Dst
	if len(dst.Proj) == 0 {
		switch dst.Kind {
		case mir.PlaceGlobal:
			vmErr = vm.writeGlobal(dst.Global, taskVal)
			if vmErr != nil {
				return false, Location{}, Value{}, writes, vmErr
			}
			return true, Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}, taskVal, writes, nil
		default:
			localID := dst.Local
			vmErr = vm.writeLocal(frame, localID, taskVal)
			if vmErr != nil {
				return false, Location{}, Value{}, writes, vmErr
			}
			stored := frame.Locals[localID].V
			writes = append(writes, LocalWrite{
				LocalID: localID,
				Name:    frame.Locals[localID].Name,
				Value:   stored,
			})
			return false, Location{}, Value{}, writes, nil
		}
	}
	loc, vmErr := vm.EvalPlace(frame, dst)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	if vmErr := vm.storeLocation(loc, taskVal); vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	return true, loc, taskVal, writes, nil
}

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

func (vm *VM) execInstrChanSend(frame *Frame, instr *mir.Instr, writes []LocalWrite) (pollExecResult, *VMError) {
	res := pollExecResult{writes: writes}

	exec := vm.ensureExecutor()
	if exec == nil {
		return res, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	current := exec.Current()
	if current == 0 {
		return res, vm.eb.makeError(PanicUnimplemented, "async channel send outside task")
	}
	task := exec.Task(current)
	if task == nil {
		return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", current))
	}

	switch task.ResumeKind {
	case asyncrt.ResumeChanSendAck:
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = nil
		res.doJump = true
		res.jumpBB = instr.ChanSend.ReadyBB
		return res, nil
	case asyncrt.ResumeChanSendClosed:
		resumeVal := task.ResumeValue
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = nil
		if v, ok := resumeVal.(Value); ok {
			vm.dropValue(v)
		}
		return res, vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
	}

	chVal, vmErr := vm.evalOperand(frame, &instr.ChanSend.Channel)
	if vmErr != nil {
		return res, vmErr
	}
	chID, vmErr := vm.channelIDFromValue(chVal)
	vm.dropValue(chVal)
	if vmErr != nil {
		return res, vmErr
	}

	val, vmErr := vm.evalOperand(frame, &instr.ChanSend.Value)
	if vmErr != nil {
		return res, vmErr
	}

	if exec.ChanSendOrPark(chID, val) {
		res.doJump = true
		res.jumpBB = instr.ChanSend.ReadyBB
		return res, nil
	}
	if exec.ChanIsClosed(chID) {
		vm.dropValue(val)
		return res, vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
	}
	if task.Cancelled {
		vm.dropValue(val)
		res.doJump = true
		res.jumpBB = instr.ChanSend.PendBB
		return res, nil
	}
	vm.asyncPendingParkKey = asyncrt.ChannelSendKey(chID)
	res.doJump = true
	res.jumpBB = instr.ChanSend.PendBB
	return res, nil
}

func (vm *VM) execInstrChanRecv(frame *Frame, instr *mir.Instr, writes []LocalWrite) (pollExecResult, *VMError) {
	res := pollExecResult{writes: writes}

	exec := vm.ensureExecutor()
	if exec == nil {
		return res, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	current := exec.Current()
	if current == 0 {
		return res, vm.eb.makeError(PanicUnimplemented, "async channel recv outside task")
	}
	task := exec.Task(current)
	if task == nil {
		return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", current))
	}

	storeResult := func(doneVal Value) (pollExecResult, *VMError) {
		dst := instr.ChanRecv.Dst
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
				res.jumpBB = instr.ChanRecv.ReadyBB
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
				res.jumpBB = instr.ChanRecv.ReadyBB
				return res, nil
			}
		}
		return res, vm.eb.makeError(PanicUnimplemented, "chan_recv destination projection unsupported")
	}

	switch task.ResumeKind {
	case asyncrt.ResumeChanRecvValue:
		resumeVal := task.ResumeValue
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = nil
		v, ok := resumeVal.(Value)
		if !ok {
			return res, vm.eb.makeError(PanicTypeMismatch, "invalid channel recv resume value")
		}
		dstType, vmErr := vm.joinResultType(frame, instr.ChanRecv.Dst)
		if vmErr != nil {
			vm.dropValue(v)
			return res, vmErr
		}
		doneVal, vmErr := vm.makeOptionSome(dstType, v)
		if vmErr != nil {
			vm.dropValue(v)
			return res, vmErr
		}
		return storeResult(doneVal)
	case asyncrt.ResumeChanRecvClosed:
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = nil
		dstType, vmErr := vm.joinResultType(frame, instr.ChanRecv.Dst)
		if vmErr != nil {
			return res, vmErr
		}
		doneVal, vmErr := vm.makeOptionNothing(dstType)
		if vmErr != nil {
			return res, vmErr
		}
		return storeResult(doneVal)
	}

	chVal, vmErr := vm.evalOperand(frame, &instr.ChanRecv.Channel)
	if vmErr != nil {
		return res, vmErr
	}
	chID, vmErr := vm.channelIDFromValue(chVal)
	vm.dropValue(chVal)
	if vmErr != nil {
		return res, vmErr
	}

	valAny, ok := exec.ChanRecvOrPark(chID)
	if ok {
		v, ok := valAny.(Value)
		if !ok {
			return res, vm.eb.makeError(PanicTypeMismatch, "invalid channel recv value")
		}
		dstType, vmErr := vm.joinResultType(frame, instr.ChanRecv.Dst)
		if vmErr != nil {
			vm.dropValue(v)
			return res, vmErr
		}
		doneVal, vmErr := vm.makeOptionSome(dstType, v)
		if vmErr != nil {
			vm.dropValue(v)
			return res, vmErr
		}
		return storeResult(doneVal)
	}

	if exec.ChanIsClosed(chID) {
		dstType, vmErr := vm.joinResultType(frame, instr.ChanRecv.Dst)
		if vmErr != nil {
			return res, vmErr
		}
		doneVal, vmErr := vm.makeOptionNothing(dstType)
		if vmErr != nil {
			return res, vmErr
		}
		return storeResult(doneVal)
	}
	if task.Cancelled {
		res.doJump = true
		res.jumpBB = instr.ChanRecv.PendBB
		return res, nil
	}
	vm.asyncPendingParkKey = asyncrt.ChannelRecvKey(chID)
	res.doJump = true
	res.jumpBB = instr.ChanRecv.PendBB
	return res, nil
}

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

func (vm *VM) execInstrSelect(frame *Frame, instr *mir.Instr, writes []LocalWrite) (pollExecResult, *VMError) {
	res := pollExecResult{writes: writes}

	exec := vm.ensureExecutor()
	if exec == nil {
		return res, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	current := exec.Current()
	if current == 0 {
		return res, vm.eb.makeError(PanicUnimplemented, "async select outside task")
	}
	currentTask := exec.Task(current)
	if currentTask == nil {
		return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", current))
	}

	clearSelect := func() {
		if currentTask.SelectID != 0 {
			exec.SelectClear(currentTask.SelectID)
			currentTask.SelectID = 0
		}
	}

	if currentTask.Cancelled {
		clearSelect()
		res.doJump = true
		res.jumpBB = instr.Select.PendBB
		return res, nil
	}

	var (
		selectedIndex   = -1
		selectedKind    mir.SelectArmKind
		selectedTaskID  asyncrt.TaskID
		selectedChanID  asyncrt.ChannelID
		selectedTimeout bool
		defaultIndex    = -1
	)

	resolveTaskID := func(op mir.Operand) (asyncrt.TaskID, *VMError) {
		val, vmErr := vm.evalOperand(frame, &op)
		if vmErr != nil {
			return 0, vmErr
		}
		taskID, vmErr := vm.taskIDFromValue(val)
		vm.dropValue(val)
		if vmErr != nil {
			return 0, vmErr
		}
		return taskID, nil
	}

	resolveChanID := func(op mir.Operand) (asyncrt.ChannelID, *VMError) {
		val, vmErr := vm.evalOperand(frame, &op)
		if vmErr != nil {
			return 0, vmErr
		}
		chID, vmErr := vm.channelIDFromValue(val)
		vm.dropValue(val)
		if vmErr != nil {
			return 0, vmErr
		}
		return chID, nil
	}

	for i, arm := range instr.Select.Arms {
		switch arm.Kind {
		case mir.SelectArmDefault:
			defaultIndex = i
		case mir.SelectArmTask:
			taskID, vmErr := resolveTaskID(arm.Task)
			if vmErr != nil {
				return res, vmErr
			}
			target := exec.Task(taskID)
			if target == nil {
				return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", taskID))
			}
			if target.Status != asyncrt.TaskWaiting && target.Status != asyncrt.TaskDone {
				exec.Wake(taskID)
			}
			if target.Status == asyncrt.TaskDone {
				selectedIndex = i
				selectedKind = arm.Kind
				selectedTaskID = taskID
				break
			}
		case mir.SelectArmChanRecv:
			chID, vmErr := resolveChanID(arm.Channel)
			if vmErr != nil {
				return res, vmErr
			}
			if exec.ChanCanRecv(chID) {
				selectedIndex = i
				selectedKind = arm.Kind
				selectedChanID = chID
				break
			}
		case mir.SelectArmChanSend:
			chID, vmErr := resolveChanID(arm.Channel)
			if vmErr != nil {
				return res, vmErr
			}
			if exec.ChanIsClosed(chID) || exec.ChanCanSend(chID) {
				selectedIndex = i
				selectedKind = arm.Kind
				selectedChanID = chID
				break
			}
		case mir.SelectArmTimeout:
			taskID, vmErr := resolveTaskID(arm.Task)
			if vmErr != nil {
				return res, vmErr
			}
			target := exec.Task(taskID)
			if target == nil {
				return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", taskID))
			}
			if target.Status != asyncrt.TaskWaiting && target.Status != asyncrt.TaskDone {
				exec.Wake(taskID)
			}
			if target.Status == asyncrt.TaskDone {
				selectedIndex = i
				selectedKind = arm.Kind
				selectedTaskID = taskID
				selectedTimeout = false
				break
			}
			if currentTask.SelectID != 0 {
				timerID := exec.SelectTimer(currentTask.SelectID, i)
				if timerID != 0 && !exec.TimerActive(timerID) {
					selectedIndex = i
					selectedKind = arm.Kind
					selectedTaskID = taskID
					selectedTimeout = true
					break
				}
			}
		}
		if selectedIndex >= 0 {
			break
		}
	}

	if selectedIndex < 0 && defaultIndex >= 0 {
		selectedIndex = defaultIndex
		selectedKind = mir.SelectArmDefault
	}

	if selectedIndex >= 0 {
		if currentTask.SelectID != 0 {
			clearSelect()
		}

		switch selectedKind {
		case mir.SelectArmChanRecv:
			val, ok := exec.ChanTryRecv(selectedChanID)
			if ok {
				if v, isVal := val.(Value); isVal {
					vm.dropValue(v)
				}
			}
		case mir.SelectArmChanSend:
			if exec.ChanIsClosed(selectedChanID) {
				return res, vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
			}
			arm := instr.Select.Arms[selectedIndex]
			val, vmErr := vm.evalOperand(frame, &arm.Value)
			if vmErr != nil {
				return res, vmErr
			}
			if !exec.ChanTrySend(selectedChanID, val) {
				if exec.ChanIsClosed(selectedChanID) {
					vm.dropValue(val)
					return res, vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
				}
				vm.dropValue(val)
				return res, vm.eb.makeError(PanicUnimplemented, "select send not ready")
			}
		case mir.SelectArmTimeout:
			if selectedTimeout {
				exec.Cancel(selectedTaskID)
				exec.Wake(selectedTaskID)
			}
		}

		dst := instr.Select.Dst
		if len(dst.Proj) == 0 {
			var selType types.TypeID
			switch dst.Kind {
			case mir.PlaceGlobal:
				if int(dst.Global) < 0 || int(dst.Global) >= len(vm.Globals) {
					return res, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", dst.Global))
				}
				selType = vm.Globals[dst.Global].TypeID
			default:
				if int(dst.Local) < 0 || int(dst.Local) >= len(frame.Locals) {
					return res, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", dst.Local))
				}
				selType = frame.Locals[dst.Local].TypeID
			}
			selVal := MakeInt(int64(selectedIndex), selType)
			switch dst.Kind {
			case mir.PlaceGlobal:
				if vmErr := vm.writeGlobal(dst.Global, selVal); vmErr != nil {
					return res, vmErr
				}
				res.hasStore = true
				res.storeLoc = Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}
				res.storeVal = selVal
			default:
				localID := dst.Local
				if vmErr := vm.writeLocal(frame, localID, selVal); vmErr != nil {
					return res, vmErr
				}
				stored := frame.Locals[localID].V
				writes = append(writes, LocalWrite{
					LocalID: localID,
					Name:    frame.Locals[localID].Name,
					Value:   stored,
				})
				res.writes = writes
			}
			res.doJump = true
			res.jumpBB = instr.Select.ReadyBB
			return res, nil
		}
		return res, vm.eb.makeError(PanicUnimplemented, "select destination projection unsupported")
	}

	selectID := currentTask.SelectID
	if selectID == 0 {
		selectID = exec.SelectNew(current)
		currentTask.SelectID = selectID
	} else {
		exec.SelectClearWaiters(selectID)
	}

	for i, arm := range instr.Select.Arms {
		switch arm.Kind {
		case mir.SelectArmTask:
			taskID, vmErr := resolveTaskID(arm.Task)
			if vmErr != nil {
				return res, vmErr
			}
			target := exec.Task(taskID)
			if target == nil {
				return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", taskID))
			}
			if target.Status != asyncrt.TaskWaiting && target.Status != asyncrt.TaskDone {
				exec.Wake(taskID)
			}
			exec.SelectSubscribeKey(selectID, asyncrt.JoinKey(taskID))
		case mir.SelectArmChanRecv:
			chID, vmErr := resolveChanID(arm.Channel)
			if vmErr != nil {
				return res, vmErr
			}
			exec.SelectSubscribeRecv(selectID, chID)
		case mir.SelectArmChanSend:
			chID, vmErr := resolveChanID(arm.Channel)
			if vmErr != nil {
				return res, vmErr
			}
			exec.SelectSubscribeSend(selectID, chID)
		case mir.SelectArmTimeout:
			taskID, vmErr := resolveTaskID(arm.Task)
			if vmErr != nil {
				return res, vmErr
			}
			target := exec.Task(taskID)
			if target == nil {
				return res, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", taskID))
			}
			if target.Status != asyncrt.TaskWaiting && target.Status != asyncrt.TaskDone {
				exec.Wake(taskID)
			}
			exec.SelectSubscribeKey(selectID, asyncrt.JoinKey(taskID))

			timerID := exec.SelectTimer(selectID, i)
			if timerID == 0 {
				delayVal, vmErr := vm.evalOperand(frame, &arm.Ms)
				if vmErr != nil {
					return res, vmErr
				}
				ownsDelay := delayVal.IsHeap()
				if ownsDelay {
					defer vm.dropValue(delayVal)
				}
				delay, vmErr := vm.uintValueToInt(delayVal, "timeout duration out of range")
				if vmErr != nil {
					return res, vmErr
				}
				timerID = exec.TimerScheduleAfter(current, uint64(delay)) //nolint:gosec // delay bounded by uintValueToInt
				exec.SelectSetTimer(selectID, i, timerID)
			}
		}
	}

	vm.asyncPendingParkKey = asyncrt.SelectKey(selectID)
	res.doJump = true
	res.jumpBB = instr.Select.PendBB
	return res, nil
}

func (vm *VM) awaitResultType(frame *Frame, dst mir.Place) (types.TypeID, *VMError) {
	if len(dst.Proj) != 0 {
		return types.NoTypeID, vm.eb.makeError(PanicUnimplemented, "await destination projection unsupported")
	}
	switch dst.Kind {
	case mir.PlaceGlobal:
		if int(dst.Global) < 0 || int(dst.Global) >= len(vm.Globals) {
			return types.NoTypeID, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", dst.Global))
		}
		return vm.Globals[dst.Global].TypeID, nil
	default:
		if int(dst.Local) < 0 || int(dst.Local) >= len(frame.Locals) {
			return types.NoTypeID, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", dst.Local))
		}
		return frame.Locals[dst.Local].TypeID, nil
	}
}

func (vm *VM) joinResultType(frame *Frame, dst mir.Place) (types.TypeID, *VMError) {
	if len(dst.Proj) != 0 {
		return types.NoTypeID, vm.eb.makeError(PanicUnimplemented, "join_all destination projection unsupported")
	}
	switch dst.Kind {
	case mir.PlaceGlobal:
		if int(dst.Global) < 0 || int(dst.Global) >= len(vm.Globals) {
			return types.NoTypeID, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", dst.Global))
		}
		return vm.Globals[dst.Global].TypeID, nil
	default:
		if int(dst.Local) < 0 || int(dst.Local) >= len(frame.Locals) {
			return types.NoTypeID, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", dst.Local))
		}
		return frame.Locals[dst.Local].TypeID, nil
	}
}
