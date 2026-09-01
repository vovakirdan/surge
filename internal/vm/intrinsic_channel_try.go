package vm

import "surge/internal/mir"

func (vm *VM) handleChannelTrySend(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "try_send missing destination")
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicUnimplemented, "try_send expects 2 arguments")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}

	chVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	chID, vmErr := vm.channelIDFromValue(chVal)
	vm.dropValue(chVal)
	if vmErr != nil {
		return vmErr
	}
	val, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	reservation, ready := exec.ChanReserveTrySend(chID)
	sent := false
	if ready {
		sent, vmErr = vm.commitReservedTrySend(exec, reservation, val)
		if vmErr != nil {
			return vmErr
		}
	} else {
		vm.dropValue(val)
	}
	boolType := frame.Locals[call.Dst.Local].TypeID
	res := MakeBool(sent, boolType)
	if vmErr := vm.writeLocal(frame, call.Dst.Local, res); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: call.Dst.Local,
			Name:    frame.Locals[call.Dst.Local].Name,
			Value:   res,
		})
	}
	return nil
}

func (vm *VM) handleChannelTryRecv(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "try_recv missing destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicUnimplemented, "try_recv expects 1 argument")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}

	chVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	chID, vmErr := vm.channelIDFromValue(chVal)
	vm.dropValue(chVal)
	if vmErr != nil {
		return vmErr
	}

	payload, ok, receiveErr := vm.tryReceiveAsyncChannel(exec, chID)
	if receiveErr != nil {
		return receiveErr
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	var res Value
	if ok {
		res, vmErr = vm.makeOptionSomeFromAsync(dstType, payload)
		if vmErr != nil {
			return vmErr
		}
	} else {
		res, vmErr = vm.makeOptionNothing(dstType)
		if vmErr != nil {
			return vmErr
		}
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		vm.dropValue(res)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   res,
		})
	}
	return nil
}

func (vm *VM) handleChannelClose(frame *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return vm.eb.makeError(PanicTypeMismatch, "close requires a call")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "close expects 1 argument")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	chVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	chID, vmErr := vm.channelIDFromValue(chVal)
	vm.dropValue(chVal)
	if vmErr != nil {
		return vmErr
	}
	exec.ChanClose(chID)
	return nil
}
