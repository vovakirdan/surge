package vm

import (
	"fmt"

	"fortio.org/safecast"

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

// execInstr executes a single instruction.
func (vm *VM) execInstr(frame *Frame, instr *mir.Instr) (advanceIP bool, pushFrame *Frame, vmErr *VMError) {
	var writes []LocalWrite
	var (
		storeLoc Location
		storeVal Value
		hasStore bool
	)
	var (
		doJump bool
		jumpBB mir.BlockID
	)

	switch instr.Kind {
	case mir.InstrAssign:
		hasStore, storeLoc, storeVal, writes, vmErr = vm.execInstrAssign(frame, instr, writes)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrCall:
		var newFrame *Frame
		newFrame, vmErr = vm.execCall(frame, &instr.Call, &writes)
		if vmErr != nil {
			return false, nil, vmErr
		}
		if newFrame != nil {
			pushFrame = newFrame
		}

	case mir.InstrDrop:
		vmErr = vm.execInstrDrop(frame, instr)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrEndBorrow:
		vmErr = vm.execInstrEndBorrow(frame, instr)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrAwait:
		hasStore, storeLoc, storeVal, writes, vmErr = vm.execInstrAwait(frame, instr, writes)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrSpawn:
		hasStore, storeLoc, storeVal, writes, vmErr = vm.execInstrSpawn(frame, instr, writes)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrPoll:
		pollRes, pollErr := vm.execInstrPoll(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB

	case mir.InstrJoinAll:
		pollRes, pollErr := vm.execInstrJoinAll(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB
	case mir.InstrChanSend:
		pollRes, pollErr := vm.execInstrChanSend(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB
	case mir.InstrChanRecv:
		pollRes, pollErr := vm.execInstrChanRecv(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB

	case mir.InstrNop:
		// Nothing to do

	default:
		return false, nil, vm.eb.unimplemented(fmt.Sprintf("instruction kind %d", instr.Kind))
	}

	// Trace the instruction
	if vm.Trace != nil {
		vm.Trace.TraceInstr(len(vm.Stack), frame.Func, frame.BB, frame.IP, instr, frame.Span, writes)
		if hasStore {
			vm.Trace.TraceStore(storeLoc, storeVal)
		}
	}

	if doJump {
		frame.BB = jumpBB
		frame.IP = 0
		return false, nil, nil
	}
	if pushFrame != nil {
		return false, pushFrame, nil
	}
	return true, nil, nil
}

func (vm *VM) execInstrAssign(frame *Frame, instr *mir.Instr, writes []LocalWrite) (hasStore bool, storeLoc Location, storeVal Value, writesOut []LocalWrite, vmErr *VMError) {
	val, vmErr := vm.evalRValue(frame, &instr.Assign.Src)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	dst := instr.Assign.Dst
	if len(dst.Proj) == 0 {
		switch dst.Kind {
		case mir.PlaceGlobal:
			vmErr = vm.writeGlobal(dst.Global, val)
			if vmErr != nil {
				return false, Location{}, Value{}, writes, vmErr
			}
			return true, Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}, val, writes, nil
		default:
			localID := dst.Local
			vmErr = vm.writeLocal(frame, localID, val)
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
	if vmErr := vm.storeLocation(loc, val); vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	return true, loc, val, writes, nil
}

func (vm *VM) execInstrDrop(frame *Frame, instr *mir.Instr) *VMError {
	switch instr.Drop.Place.Kind {
	case mir.PlaceGlobal:
		return vm.execDropGlobal(instr.Drop.Place.Global)
	default:
		return vm.execDrop(frame, instr.Drop.Place.Local)
	}
}

func (vm *VM) execInstrEndBorrow(frame *Frame, instr *mir.Instr) *VMError {
	switch instr.EndBorrow.Place.Kind {
	case mir.PlaceGlobal:
		globalID := instr.EndBorrow.Place.Global
		if int(globalID) < 0 || int(globalID) >= len(vm.Globals) {
			return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", globalID))
		}
		slot := &vm.Globals[globalID]
		slot.V = Value{}
		slot.IsInit = false
		slot.IsMoved = false
		slot.IsDropped = false
	default:
		localID := instr.EndBorrow.Place.Local
		if int(localID) < 0 || int(localID) >= len(frame.Locals) {
			return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", localID))
		}
		slot := &frame.Locals[localID]
		slot.V = Value{}
		slot.IsInit = false
		slot.IsMoved = false
		slot.IsDropped = false
	}
	return nil
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
	if current == taskID {
		return res, vm.eb.makeError(PanicInvalidHandle, "task cannot await itself")
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

// execCall executes a call instruction.
func (vm *VM) execCall(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) (*Frame, *VMError) {
	// Find the function to call.
	var targetFn *mir.Func
	switch call.Callee.Kind {
	case mir.CalleeSym:
		if !call.Callee.Sym.IsValid() {
			return nil, vm.callIntrinsic(frame, call, writes)
		}
		targetFn = vm.findFunctionBySym(call.Callee.Sym)
		if targetFn == nil {
			// Support selected intrinsics and extern calls that are not lowered into MIR.
			return nil, vm.callIntrinsic(frame, call, writes)
		}
	case mir.CalleeValue:
		targetFn = vm.findFunction(call.Callee.Name)
		if targetFn == nil {
			return nil, vm.callIntrinsic(frame, call, writes)
		}
	default:
		return nil, vm.eb.unimplemented("unknown call target")
	}

	// Evaluate arguments
	args := make([]Value, len(call.Args))
	for i := range call.Args {
		val, vmErr := vm.evalOperand(frame, &call.Args[i])
		if vmErr != nil {
			return nil, vmErr
		}
		args[i] = val
	}

	// Push new frame
	newFrame := NewFrame(targetFn)

	// Pass arguments as first locals (params)
	if len(args) > len(newFrame.Locals) {
		return nil, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("too many arguments: got %d, expected at most %d", len(args), len(newFrame.Locals)))
	}
	for i, arg := range args {
		localID, err := safecast.Conv[mir.LocalID](i)
		if err != nil {
			return nil, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("invalid argument index %d", i))
		}
		if vmErr := vm.writeLocal(newFrame, localID, arg); vmErr != nil {
			return nil, vmErr
		}
	}

	return newFrame, nil
}
