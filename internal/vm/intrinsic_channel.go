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
	payloadType, vmErr := vm.runtimeHandlePayloadType(dstType, "Channel")
	if vmErr != nil {
		return vmErr
	}
	channelCapacity := uint64(capacity) //nolint:gosec // capacity is bounded by uintValueToInt
	id := exec.ChanNew(channelCapacity)
	exec.ChanPreparePayloadCapacity(id)
	if vmErr := vm.registerAsyncChannelOwner(id, payloadType, channelCapacity); vmErr != nil {
		return vmErr
	}
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
	ownsValue := true
	defer func() {
		if ownsValue {
			vm.dropValue(val)
		}
	}()

	for {
		if vm.Halted {
			return nil
		}
		reservation, ready := exec.ChanReserveTrySend(chID)
		if ready {
			payload, vmErr := vm.stageReservedChannelSend(reservation, val)
			if vmErr != nil {
				reservation.Abort()
				return vmErr
			}
			ownsValue = false
			completed, committed := reservation.Commit(payload)
			if !committed || !completed {
				vm.dropAsyncPayload(payload)
				return vm.eb.invalidLocation("reserved channel send could not commit")
			}
			return nil
		}
		if exec.ChanIsClosed(chID) {
			return vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
		}
		ran, vmErr := vm.runReadyOne()
		if vmErr != nil {
			return vmErr
		}
		if vm.Halted {
			return nil
		}
		if !ran {
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
		if vm.Halted {
			return nil
		}
		payload, ok, receiveErr := vm.tryReceiveAsyncChannel(exec, chID)
		if receiveErr != nil {
			return receiveErr
		}
		if ok {
			doneVal, vmErr := vm.makeOptionSomeFromAsync(dstType, payload)
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
		if vm.Halted {
			return nil
		}
		if !ran {
			return vm.eb.makeError(PanicUnimplemented, "async deadlock")
		}
	}
}
