package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/mir"
	"surge/internal/types"
)

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

	for i := range instr.Select.Arms {
		arm := &instr.Select.Arms[i]
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

	for i := range instr.Select.Arms {
		arm := &instr.Select.Arms[i]
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
				delay, vmErr := vm.uintValueToInt(delayVal, "timeout duration out of range")
				if ownsDelay {
					vm.dropValue(delayVal)
				}
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
