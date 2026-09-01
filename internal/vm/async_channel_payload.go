package vm

import (
	"surge/internal/asyncrt"
)

func (vm *VM) stageReservedChannelSend(
	reservation asyncrt.ChannelSendReservation[asyncPayload],
	value Value,
) (asyncPayload, *VMError) {
	channelOwner, vmErr := vm.asyncChannelOwner(reservation.ChannelID())
	if vmErr != nil {
		return asyncPayload{}, vmErr
	}
	switch reservation.Route() {
	case asyncrt.ChannelSendRing:
		return vm.stageAsyncPayloadInto(channelOwner, asyncSlotRing, value)
	case asyncrt.ChannelSendPark:
		return vm.stageAsyncPayloadInto(channelOwner, asyncSlotPark, value)
	case asyncrt.ChannelSendRendezvous:
		receiver := reservation.Receiver()
		sequence := vm.currentAsyncParkSequence(receiver)
		if sequence == 0 {
			return asyncPayload{}, vm.eb.invalidLocation("receiver has no park sequence")
		}
		owner, vmErr := vm.asyncResumeOwner(receiver, channelOwner.typeID)
		if vmErr != nil {
			return asyncPayload{}, vmErr
		}
		payload, vmErr := vm.stageAsyncPayloadInto(owner, asyncSlotResume, value)
		if vmErr != nil {
			return asyncPayload{}, vmErr
		}
		return vm.bindAsyncPayloadParkSequence(payload, sequence), nil
	default:
		return asyncPayload{}, vm.eb.invalidLocation("channel send has no concrete route")
	}
}

func (vm *VM) commitReservedTrySend(
	reservation asyncrt.ChannelSendReservation[asyncPayload],
	value Value,
) (bool, *VMError) {
	payload, vmErr := vm.stageReservedChannelSend(reservation, value)
	if vmErr != nil {
		reservation.Abort()
		vm.dropValue(value)
		return false, vmErr
	}
	completed, committed := reservation.Commit(payload)
	if !committed {
		vm.dropAsyncPayload(payload)
		return false, vm.eb.invalidLocation("reserved try_send could not commit")
	}
	return completed, nil
}

func (vm *VM) routeAsyncPayloadToChannel(
	reservation asyncrt.ChannelSendReservation[asyncPayload],
	payload asyncPayload,
) (asyncPayload, *VMError) {
	channelOwner, vmErr := vm.asyncChannelOwner(reservation.ChannelID())
	if vmErr != nil {
		return asyncPayload{}, vmErr
	}
	switch reservation.Route() {
	case asyncrt.ChannelSendRing:
		return vm.moveAsyncPayloadIntoOwner(payload, channelOwner, asyncSlotRing)
	case asyncrt.ChannelSendPark:
		return vm.moveAsyncPayloadIntoOwner(payload, channelOwner, asyncSlotPark)
	case asyncrt.ChannelSendRendezvous:
		receiver := reservation.Receiver()
		sequence := vm.currentAsyncParkSequence(receiver)
		if sequence == 0 {
			return asyncPayload{}, vm.eb.invalidLocation("receiver has no park sequence")
		}
		owner, vmErr := vm.asyncResumeOwner(receiver, channelOwner.typeID)
		if vmErr != nil {
			return asyncPayload{}, vmErr
		}
		next, vmErr := vm.moveAsyncPayloadIntoOwner(payload, owner, asyncSlotResume)
		if vmErr != nil {
			return asyncPayload{}, vmErr
		}
		return vm.bindAsyncPayloadParkSequence(next, sequence), nil
	default:
		return asyncPayload{}, vm.eb.invalidLocation("channel send has no concrete route")
	}
}

func (vm *VM) refillAsyncChannelRing(
	id asyncrt.ChannelID,
	payload asyncPayload,
) (asyncPayload, bool, *VMError) {
	owner, vmErr := vm.asyncChannelOwner(id)
	if vmErr != nil {
		return asyncPayload{}, false, vmErr
	}
	next, vmErr := vm.moveAsyncPayloadIntoOwner(payload, owner, asyncSlotRing)
	if vmErr != nil {
		return asyncPayload{}, false, vmErr
	}
	return next, true, nil
}

func (vm *VM) tryReceiveAsyncChannel(
	exec *asyncrt.Executor[asyncPayload],
	id asyncrt.ChannelID,
) (asyncPayload, bool, *VMError) {
	return vm.receiveAsyncChannel(exec, id, false)
}

func (vm *VM) receiveOrParkAsyncChannel(
	exec *asyncrt.Executor[asyncPayload],
	id asyncrt.ChannelID,
) (asyncPayload, bool, *VMError) {
	return vm.receiveAsyncChannel(exec, id, true)
}

func (vm *VM) receiveAsyncChannel(
	exec *asyncrt.Executor[asyncPayload],
	id asyncrt.ChannelID,
	allowPark bool,
) (asyncPayload, bool, *VMError) {
	var refillErr *VMError
	transfer := func(staged asyncPayload) (asyncPayload, bool) {
		next, transferred, vmErr := vm.refillAsyncChannelRing(id, staged)
		refillErr = vmErr
		return next, transferred
	}
	var payload asyncPayload
	var ok bool
	if allowPark {
		payload, ok = exec.ChanRecvOrParkTransfer(id, transfer)
	} else {
		payload, ok = exec.ChanTryRecvTransfer(id, transfer)
	}
	if refillErr != nil {
		if ok {
			vm.dropAsyncPayload(payload)
		}
		return asyncPayload{}, false, refillErr
	}
	return payload, ok, nil
}
