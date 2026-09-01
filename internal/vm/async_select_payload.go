package vm

import "surge/internal/asyncrt"

func (vm *VM) consumeSelectedChannelPayload(
	exec *asyncrt.Executor[asyncPayload],
	task asyncrt.TaskID,
	arm uint32,
	channel asyncrt.ChannelID,
) *VMError {
	payload, ok, receiveErr := vm.tryReceiveAsyncChannel(exec, channel)
	if receiveErr != nil {
		return receiveErr
	}
	if !ok {
		return nil
	}
	channelOwner, vmErr := vm.asyncChannelOwner(channel)
	if vmErr != nil {
		vm.dropAsyncPayload(payload)
		return vmErr
	}
	selectOwner, vmErr := vm.asyncSelectOwner(task, arm, channelOwner.typeID)
	if vmErr != nil {
		vm.dropAsyncPayload(payload)
		return vmErr
	}
	selected, vmErr := vm.moveAsyncPayloadIntoOwner(payload, selectOwner, asyncSlotSelect)
	if vmErr != nil {
		vm.dropAsyncPayload(payload)
		return vmErr
	}
	vm.dropAsyncPayload(selected)
	return nil
}

func (vm *VM) sendSelectedChannelPayload(
	exec *asyncrt.Executor[asyncPayload],
	task asyncrt.TaskID,
	arm uint32,
	channel asyncrt.ChannelID,
	value Value,
) *VMError {
	reservation, ready := exec.ChanReserveTrySend(channel)
	if !ready {
		vm.dropValue(value)
		if exec.ChanIsClosed(channel) {
			return vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
		}
		return vm.eb.invalidLocation("selected channel send is no longer ready")
	}
	channelOwner, vmErr := vm.asyncChannelOwner(channel)
	if vmErr != nil {
		reservation.Abort()
		vm.dropValue(value)
		return vmErr
	}
	selectOwner, vmErr := vm.asyncSelectOwner(task, arm, channelOwner.typeID)
	if vmErr != nil {
		reservation.Abort()
		vm.dropValue(value)
		return vmErr
	}
	staged, vmErr := vm.stageAsyncPayloadInto(selectOwner, asyncSlotSelect, value)
	if vmErr != nil {
		reservation.Abort()
		vm.dropValue(value)
		return vmErr
	}
	routed, vmErr := vm.routeAsyncPayloadToChannel(reservation, staged)
	if vmErr != nil {
		reservation.Abort()
		vm.dropAsyncPayload(staged)
		return vmErr
	}
	completed, committed := reservation.Commit(routed)
	if !committed || !completed {
		vm.dropAsyncPayload(routed)
		return vm.eb.invalidLocation("selected channel send could not commit")
	}
	return nil
}
