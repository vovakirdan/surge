package vm

import (
	"fmt"

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
	case mir.InstrTimeout:
		pollRes, pollErr := vm.execInstrTimeout(frame, instr, writes)
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