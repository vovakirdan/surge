package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/mir"
)

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

	if task.Cancelled {
		// A resume value on a cancelled task is a handover that will not
		// happen: the receive it was delivered for never runs.
		vm.dropAsyncPayload(task.ResumeValue)
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = asyncPayload{}
		res.doJump = true
		res.jumpBB = instr.ChanSend.PendBB
		return res, nil
	}

	switch task.ResumeKind {
	case asyncrt.ResumeChanSendAck:
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = asyncPayload{}
		res.doJump = true
		res.jumpBB = instr.ChanSend.ReadyBB
		return res, nil
	case asyncrt.ResumeChanSendClosed:
		resumePayload := task.ResumeValue
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = asyncPayload{}
		vm.dropAsyncPayload(resumePayload)
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

	reservation, ready := exec.ChanReserveSendOrPark(chID)
	if !ready {
		if exec.ChanIsClosed(chID) {
			return res, vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
		}
		vm.asyncPendingParkKey = asyncrt.ChannelSendKey(chID)
		res.doJump = true
		res.jumpBB = instr.ChanSend.PendBB
		return res, nil
	}
	val, vmErr := vm.evalOperand(frame, &instr.ChanSend.Value)
	if vmErr != nil {
		reservation.Abort()
		return res, vmErr
	}
	payload, vmErr := vm.stageReservedChannelSend(reservation, val)
	if vmErr != nil {
		reservation.Abort()
		vm.dropValue(val)
		return res, vmErr
	}
	completed, committed := reservation.Commit(payload)
	if !committed {
		vm.dropAsyncPayload(payload)
		if exec.ChanIsClosed(chID) {
			return res, vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
		}
		return res, vm.eb.invalidLocation("reserved async send could not commit")
	}
	if completed {
		res.doJump = true
		res.jumpBB = instr.ChanSend.ReadyBB
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

	if task.Cancelled {
		// Cancel-before-receive. The payload reached this task and the receive
		// that would have consumed it never runs, so this is where it is
		// released — exactly once, and not again by the shutdown drain, which
		// clears the resume value below.
		vm.dropAsyncPayload(task.ResumeValue)
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = asyncPayload{}
		res.doJump = true
		res.jumpBB = instr.ChanRecv.PendBB
		return res, nil
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
		resumePayload := task.ResumeValue
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = asyncPayload{}
		dstType, vmErr := vm.joinResultType(frame, instr.ChanRecv.Dst)
		if vmErr != nil {
			vm.dropAsyncPayload(resumePayload)
			return res, vmErr
		}
		doneVal, vmErr := vm.makeOptionSomeFromAsync(dstType, resumePayload)
		if vmErr != nil {
			return res, vmErr
		}
		return storeResult(doneVal)
	case asyncrt.ResumeChanRecvClosed:
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = asyncPayload{}
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

	payload, ok, receiveErr := vm.tryReceiveAsyncChannel(exec, chID)
	if receiveErr != nil {
		return res, receiveErr
	}
	if ok {
		dstType, resultTypeErr := vm.joinResultType(frame, instr.ChanRecv.Dst)
		if resultTypeErr != nil {
			vm.dropAsyncPayload(payload)
			return res, resultTypeErr
		}
		doneVal, optionErr := vm.makeOptionSomeFromAsync(dstType, payload)
		if optionErr != nil {
			return res, optionErr
		}
		return storeResult(doneVal)
	}

	if exec.ChanIsClosed(chID) {
		dstType, resultTypeErr := vm.joinResultType(frame, instr.ChanRecv.Dst)
		if resultTypeErr != nil {
			return res, resultTypeErr
		}
		doneVal, optionErr := vm.makeOptionNothing(dstType)
		if optionErr != nil {
			return res, optionErr
		}
		return storeResult(doneVal)
	}
	if task.Cancelled {
		res.doJump = true
		res.jumpBB = instr.ChanRecv.PendBB
		return res, nil
	}
	parkSeq, vmErr := vm.nextAsyncParkSequence(current)
	if vmErr != nil {
		return res, vmErr
	}
	if payload, ok, receiveErr := vm.receiveOrParkAsyncChannel(exec, chID, parkSeq); receiveErr != nil {
		return res, receiveErr
	} else if ok {
		vm.dropAsyncPayload(payload)
		return res, vm.eb.invalidLocation("channel became ready between single-threaded reserve steps")
	}
	vm.asyncPendingParkKey = asyncrt.ChannelRecvKey(chID)
	res.doJump = true
	res.jumpBB = instr.ChanRecv.PendBB
	return res, nil
}
