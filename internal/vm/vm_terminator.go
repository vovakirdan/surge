package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/mir"
)

// execTerminator executes a block terminator.
func (vm *VM) execTerminator(frame *Frame, term *mir.Terminator) *VMError {
	// Trace terminator before execution
	if vm.Trace != nil {
		vm.Trace.TraceTerm(len(vm.Stack), frame.Func, frame.BB, term, frame.Span)
	}

	switch term.Kind {
	case mir.TermReturn:
		return vm.execTermReturn(frame, term)
	case mir.TermAsyncYield:
		return vm.execTermAsyncYield(frame, term)
	case mir.TermAsyncReturn:
		return vm.execTermAsyncReturn(frame, term)
	case mir.TermAsyncReturnCancelled:
		return vm.execTermAsyncReturnCancelled(frame, term)
	case mir.TermGoto:
		frame.BB = term.Goto.Target
		frame.IP = 0
	case mir.TermIf:
		return vm.execTermIf(frame, term)
	case mir.TermSwitchTag:
		return vm.execSwitchTag(frame, &term.SwitchTag)
	case mir.TermUnreachable:
		return vm.eb.makeError(PanicUnimplemented, "unreachable code executed")
	default:
		return vm.eb.unimplemented(fmt.Sprintf("terminator kind %d", term.Kind))
	}
	return nil
}

func (vm *VM) execTermReturn(frame *Frame, term *mir.Terminator) *VMError {
	// Get return value if any
	var retVal Value
	if term.Return.HasValue {
		val, vmErr := vm.evalOperand(frame, &term.Return.Value)
		if vmErr != nil {
			return vmErr
		}
		retVal = val
	}

	// A hidden-destination result is initialized in the caller's storage while
	// this activation is still standing. Recovering it afterwards would mean
	// recovering it from storage that has already been torn down.
	delivered, vmErr := vm.deliverResult(frame.Result, retVal)
	if vmErr != nil {
		return vmErr
	}

	// Implicit drops before returning.
	vm.dropFrameLocals(frame)

	// Pop current frame
	vm.Stack = vm.Stack[:len(vm.Stack)-1]
	if vmErr := vm.retireActivation(frame); vmErr != nil {
		return vmErr
	}

	// If stack not empty, store return value in caller's destination
	if len(vm.Stack) > 0 {
		callerFrame := vm.Stack[len(vm.Stack)-1]
		if !delivered {
			// The caller's IP points to the call instruction that was just executed
			// Find the call instruction and its destination
			block := callerFrame.CurrentBlock()
			if block != nil && callerFrame.IP < len(block.Instrs) {
				instr := &block.Instrs[callerFrame.IP]
				if instr.Kind == mir.InstrCall && instr.Call.HasDst {
					localID := instr.Call.Dst.Local
					vmErr := vm.writeLocal(callerFrame, localID, retVal)
					if vmErr != nil {
						return vmErr
					}
				}
			}
		}
		// Advance caller's IP past the call
		callerFrame.IP++
	} else if vm.captureReturn != nil {
		*vm.captureReturn = retVal
	}
	return nil
}

func (vm *VM) execTermAsyncYield(frame *Frame, term *mir.Terminator) *VMError {
	if vm.asyncCapture == nil {
		return vm.eb.unimplemented("async_yield outside async poll")
	}
	stateVal, vmErr := vm.evalOperand(frame, &term.AsyncYield.State)
	if vmErr != nil {
		return vmErr
	}
	statePins, vmErr := vm.collectTaskStatePins(stateVal)
	if vmErr != nil {
		return vmErr
	}
	vm.dropFrameLocals(frame)
	vm.Stack = vm.Stack[:len(vm.Stack)-1]
	if vmErr := vm.retireActivation(frame); vmErr != nil {
		return vmErr
	}
	vm.asyncCapture.set = true
	switch {
	case vm.currentTaskCancelled():
		vm.asyncCapture.kind = asyncrt.PollDoneCancelled
		vm.asyncCapture.parkKey = asyncrt.WakerKey{}
		vm.asyncPendingParkKey = asyncrt.WakerKey{}
	case vm.asyncPendingParkKey.IsValid():
		vm.asyncCapture.kind = asyncrt.PollParked
		vm.asyncCapture.parkKey = vm.asyncPendingParkKey
		vm.asyncPendingParkKey = asyncrt.WakerKey{}
	default:
		vm.asyncCapture.kind = asyncrt.PollYielded
		vm.asyncCapture.parkKey = asyncrt.WakerKey{}
	}
	vm.asyncCapture.state = stateVal
	vm.asyncCapture.pins = statePins
	return nil
}

func (vm *VM) execTermAsyncReturn(frame *Frame, term *mir.Terminator) *VMError {
	if vm.asyncCapture == nil {
		return vm.eb.unimplemented("async_return outside async poll")
	}
	stateVal, vmErr := vm.evalOperand(frame, &term.AsyncReturn.State)
	if vmErr != nil {
		return vmErr
	}
	var result Value
	if term.AsyncReturn.HasValue {
		val, vmErr := vm.evalOperand(frame, &term.AsyncReturn.Value)
		if vmErr != nil {
			return vmErr
		}
		result = val
	}
	// Completion initializes the task's exact owner slot before the producing
	// activation retires. The slot, rather than this frame, owns the value from
	// this point onward.
	exec := vm.ensureExecutor()
	if exec == nil || exec.Current() == 0 {
		return vm.eb.invalidLocation("async return has no task owner")
	}
	retVal, vmErr := vm.stageAsyncTaskResult(exec.Current(), result)
	if vmErr != nil {
		return vmErr
	}
	vm.releaseFinishedTaskState(stateVal)
	vm.dropFrameLocals(frame)
	vm.Stack = vm.Stack[:len(vm.Stack)-1]
	if vmErr := vm.retireActivation(frame); vmErr != nil {
		return vmErr
	}
	vm.asyncCapture.set = true
	vm.asyncCapture.kind = asyncrt.PollDoneSuccess
	vm.asyncCapture.parkKey = asyncrt.WakerKey{}
	vm.asyncPendingParkKey = asyncrt.WakerKey{}
	vm.asyncCapture.state = Value{}
	vm.asyncCapture.value = retVal
	return nil
}

// releaseFinishedTaskState gives back what a state still holds at the terminator
// that ends its computation for good.
//
// A yield hands the state on to the scheduler and pins the bytes it lives in,
// so a later reader still finds them. These terminators have no later reader:
// they leave the stack, and the activation they leave retires the arena the
// state sits in. Releasing after that retirement reaches a stale reference and
// silently gives back nothing, which is how a resume counter kept its own heap
// object alive past the end of every async program. Doing it here is the last
// moment the bytes can still be read.
func (vm *VM) releaseFinishedTaskState(state Value) {
	if vm == nil || state.Kind == VKInvalid {
		return
	}
	vm.dropValue(state)
}

func (vm *VM) execTermAsyncReturnCancelled(frame *Frame, term *mir.Terminator) *VMError {
	if vm.asyncCapture == nil {
		return vm.eb.unimplemented("async_cancel outside async poll")
	}
	stateVal, vmErr := vm.evalOperand(frame, &term.AsyncReturnCancelled.State)
	if vmErr != nil {
		return vmErr
	}
	vm.releaseFinishedTaskState(stateVal)
	vm.dropFrameLocals(frame)
	vm.Stack = vm.Stack[:len(vm.Stack)-1]
	if vmErr := vm.retireActivation(frame); vmErr != nil {
		return vmErr
	}
	vm.asyncCapture.set = true
	vm.asyncCapture.kind = asyncrt.PollDoneCancelled
	vm.asyncCapture.parkKey = asyncrt.WakerKey{}
	vm.asyncPendingParkKey = asyncrt.WakerKey{}
	vm.asyncCapture.state = Value{}
	vm.asyncCapture.value = asyncPayload{}
	return nil
}

func (vm *VM) execTermIf(frame *Frame, term *mir.Terminator) *VMError {
	cond, vmErr := vm.evalOperand(frame, &term.If.Cond)
	if vmErr != nil {
		return vmErr
	}
	if cond.Kind != VKBool {
		return vm.eb.typeMismatch("bool", cond.Kind.String())
	}
	if cond.Bool {
		frame.BB = term.If.Then
	} else {
		frame.BB = term.If.Else
	}
	frame.IP = 0
	return nil
}
