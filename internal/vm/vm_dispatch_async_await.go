package vm

import (
	"surge/internal/mir"
)

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
