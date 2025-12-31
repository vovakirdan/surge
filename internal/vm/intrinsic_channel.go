package vm

import "surge/internal/mir"

func (vm *VM) handleChannelNew(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "channel new missing destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicUnimplemented, "channel new expects 1 argument")
	}
	dstType := frame.Locals[call.Dst.Local].TypeID
	if !vm.isChannelType(dstType) {
		return vm.eb.unsupportedIntrinsic("new")
	}
	capVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(capVal)

	capacity, vmErr := vm.uintValueToInt(capVal, "channel capacity out of range")
	if vmErr != nil {
		return vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	id := exec.ChanNew(uint64(capacity)) //nolint:gosec // capacity is bounded by uintValueToInt
	chVal, vmErr := vm.channelValue(id, dstType)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, call.Dst.Local, chVal); vmErr != nil {
		vm.dropValue(chVal)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: call.Dst.Local,
			Name:    frame.Locals[call.Dst.Local].Name,
			Value:   chVal,
		})
	}
	return nil
}

func (vm *VM) handleMakeChannel(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	return vm.handleChannelNew(frame, call, writes)
}

func (vm *VM) handleChannelSend(frame *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return vm.eb.makeError(PanicTypeMismatch, "send requires a call")
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "send requires 2 arguments")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	if exec.Current() != 0 {
		return vm.eb.makeError(PanicUnimplemented, "channel send requires async lowering")
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

	for {
		if exec.ChanSendOrPark(chID, val) {
			return nil
		}
		if exec.ChanIsClosed(chID) {
			vm.dropValue(val)
			return vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
		}
		ran, vmErr := vm.runReadyOne()
		if vmErr != nil {
			vm.dropValue(val)
			return vmErr
		}
		if !ran {
			vm.dropValue(val)
			return vm.eb.makeError(PanicUnimplemented, "async deadlock")
		}
	}
}

func (vm *VM) handleChannelRecv(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "recv missing destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicUnimplemented, "recv expects 1 argument")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	if exec.Current() != 0 {
		return vm.eb.makeError(PanicUnimplemented, "channel recv requires async lowering")
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

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID

	for {
		valAny, ok := exec.ChanRecvOrPark(chID)
		if ok {
			v, ok := valAny.(Value)
			if !ok {
				return vm.eb.makeError(PanicTypeMismatch, "invalid channel recv value")
			}
			doneVal, vmErr := vm.makeOptionSome(dstType, v)
			if vmErr != nil {
				vm.dropValue(v)
				return vmErr
			}
			if vmErr := vm.writeLocal(frame, dstLocal, doneVal); vmErr != nil {
				vm.dropValue(doneVal)
				return vmErr
			}
			if writes != nil {
				*writes = append(*writes, LocalWrite{
					LocalID: dstLocal,
					Name:    frame.Locals[dstLocal].Name,
					Value:   doneVal,
				})
			}
			return nil
		}
		if exec.ChanIsClosed(chID) {
			doneVal, vmErr := vm.makeOptionNothing(dstType)
			if vmErr != nil {
				return vmErr
			}
			if vmErr := vm.writeLocal(frame, dstLocal, doneVal); vmErr != nil {
				vm.dropValue(doneVal)
				return vmErr
			}
			if writes != nil {
				*writes = append(*writes, LocalWrite{
					LocalID: dstLocal,
					Name:    frame.Locals[dstLocal].Name,
					Value:   doneVal,
				})
			}
			return nil
		}
		ran, vmErr := vm.runReadyOne()
		if vmErr != nil {
			return vmErr
		}
		if !ran {
			return vm.eb.makeError(PanicUnimplemented, "async deadlock")
		}
	}
}

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

	sent := exec.ChanTrySend(chID, val)
	if !sent {
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

	valAny, ok := exec.ChanTryRecv(chID)
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	var res Value
	if ok {
		v, ok := valAny.(Value)
		if !ok {
			return vm.eb.makeError(PanicTypeMismatch, "invalid channel recv value")
		}
		res, vmErr = vm.makeOptionSome(dstType, v)
		if vmErr != nil {
			vm.dropValue(v)
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
