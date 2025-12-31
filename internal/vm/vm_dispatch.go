package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/asyncrt"
	"surge/internal/mir"
)

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
		newFrame, vmErr := vm.execCall(frame, &instr.Call, &writes)
		if vmErr != nil {
			return false, nil, vmErr
		}
		if newFrame != nil {
			pushFrame = newFrame
		}

	case mir.InstrDrop:
		vmErr := vm.execInstrDrop(frame, instr)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrEndBorrow:
		vmErr := vm.execInstrEndBorrow(frame, instr)
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
		hasStore, storeLoc, storeVal, writes, doJump, jumpBB, vmErr = vm.execInstrPoll(frame, instr, writes)
		if vmErr != nil {
			return false, nil, vmErr
		}

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
	res, vmErr := vm.runUntilDone(taskID)
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

func (vm *VM) execInstrPoll(frame *Frame, instr *mir.Instr, writes []LocalWrite) (hasStore bool, storeLoc Location, storeVal Value, writesOut []LocalWrite, doJump bool, jumpBB mir.BlockID, vmErr *VMError) {
	taskVal, vmErr := vm.evalOperand(frame, &instr.Poll.Task)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, false, mir.NoBlockID, vmErr
	}
	taskID, vmErr := vm.taskIDFromValue(taskVal)
	vm.dropValue(taskVal)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, false, mir.NoBlockID, vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return false, Location{}, Value{}, writes, false, mir.NoBlockID, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	targetTask := exec.Task(taskID)
	if targetTask == nil {
		return false, Location{}, Value{}, writes, false, mir.NoBlockID, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", taskID))
	}
	current := exec.Current()
	if current == 0 {
		return false, Location{}, Value{}, writes, false, mir.NoBlockID, vm.eb.makeError(PanicUnimplemented, "async poll outside task")
	}
	if current == taskID {
		return false, Location{}, Value{}, writes, false, mir.NoBlockID, vm.eb.makeError(PanicInvalidHandle, "task cannot await itself")
	}
	if targetTask.Status != asyncrt.TaskWaiting && targetTask.Status != asyncrt.TaskDone {
		exec.Wake(taskID)
	}
	if targetTask.Status == asyncrt.TaskDone {
		resVal, ok := targetTask.Result.(Value)
		if !ok {
			return false, Location{}, Value{}, writes, false, mir.NoBlockID, vm.eb.makeError(PanicTypeMismatch, "invalid task result type")
		}
		res, vmErr := vm.cloneForShare(resVal)
		if vmErr != nil {
			return false, Location{}, Value{}, writes, false, mir.NoBlockID, vmErr
		}
		dst := instr.Poll.Dst
		if len(dst.Proj) == 0 {
			switch dst.Kind {
			case mir.PlaceGlobal:
				vmErr = vm.writeGlobal(dst.Global, res)
				if vmErr != nil {
					return false, Location{}, Value{}, writes, false, mir.NoBlockID, vmErr
				}
				return true, Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}, res, writes, true, instr.Poll.ReadyBB, nil
			default:
				localID := dst.Local
				vmErr = vm.writeLocal(frame, localID, res)
				if vmErr != nil {
					return false, Location{}, Value{}, writes, false, mir.NoBlockID, vmErr
				}
				stored := frame.Locals[localID].V
				writes = append(writes, LocalWrite{
					LocalID: localID,
					Name:    frame.Locals[localID].Name,
					Value:   stored,
				})
				return false, Location{}, Value{}, writes, true, instr.Poll.ReadyBB, nil
			}
		}
		loc, vmErr := vm.EvalPlace(frame, dst)
		if vmErr != nil {
			return false, Location{}, Value{}, writes, false, mir.NoBlockID, vmErr
		}
		if vmErr := vm.storeLocation(loc, res); vmErr != nil {
			return false, Location{}, Value{}, writes, false, mir.NoBlockID, vmErr
		}
		return true, loc, res, writes, true, instr.Poll.ReadyBB, nil
	}
	// Task not done - set pending park key and jump to pending block
	if targetTask.Kind != asyncrt.TaskKindCheckpoint {
		vm.asyncPendingParkKey = asyncrt.JoinKey(taskID)
	}
	return false, Location{}, Value{}, writes, true, instr.Poll.PendBB, nil
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
