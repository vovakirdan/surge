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
		vm.transportRelease(task.ResumeValue)
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = Value{}
		res.doJump = true
		res.jumpBB = instr.ChanSend.PendBB
		return res, nil
	}

	switch task.ResumeKind {
	case asyncrt.ResumeChanSendAck:
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = Value{}
		res.doJump = true
		res.jumpBB = instr.ChanSend.ReadyBB
		return res, nil
	case asyncrt.ResumeChanSendClosed:
		resumeVal := task.ResumeValue
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = Value{}
		vm.transportRelease(resumeVal)
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
	// COPY IN. The runtime holds the payload — in a buffer, or in this task's
	// own send queue entry while it is parked — and this activation rewinds
	// before either is read. A composite that stayed in the sender's arena
	// would be resolved against a generation that has moved on.
	val, vmErr = vm.transportCopyIn(val)
	if vmErr != nil {
		return res, vmErr
	}

	if exec.ChanSendOrPark(chID, val) {
		res.doJump = true
		res.jumpBB = instr.ChanSend.ReadyBB
		return res, nil
	}
	// Below here the payload was NOT taken: ChanSendOrPark refuses before it
	// queues anything, and the parking path returns false having queued the
	// value, so these two exits own what they release.
	if exec.ChanIsClosed(chID) {
		vm.transportRelease(val)
		return res, vm.eb.makeError(PanicInvalidHandle, "send on closed channel")
	}
	if task.Cancelled {
		vm.transportRelease(val)
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

	if task.Cancelled {
		// Cancel-before-receive. The payload reached this task and the receive
		// that would have consumed it never runs, so this is where it is
		// released — exactly once, and not again by the shutdown drain, which
		// clears the resume value below.
		vm.transportRelease(task.ResumeValue)
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = Value{}
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
		resumeVal := task.ResumeValue
		task.ResumeKind = asyncrt.ResumeNone
		task.ResumeValue = Value{}
		v := resumeVal
		// COPY OUT: the payload becomes this frame's, and the transport's copy
		// is given up in the same step.
		v, vmErr := vm.transportCopyOut(frame, v)
		if vmErr != nil {
			return res, vmErr
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
		task.ResumeValue = Value{}
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
		v := valAny
		v, vmErr = vm.transportCopyOut(frame, v)
		if vmErr != nil {
			return res, vmErr
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
