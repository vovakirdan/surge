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

	// Implicit drops before returning.
	vm.dropFrameLocals(frame)

	// Pop current frame
	vm.Stack = vm.Stack[:len(vm.Stack)-1]

	// If stack not empty, store return value in caller's destination
	if len(vm.Stack) > 0 {
		callerFrame := &vm.Stack[len(vm.Stack)-1]
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
	vm.dropFrameLocals(frame)
	vm.Stack = vm.Stack[:len(vm.Stack)-1]
	vm.asyncCapture.set = true
	if vm.asyncPendingParkKey.IsValid() {
		vm.asyncCapture.kind = asyncrt.PollParked
		vm.asyncCapture.parkKey = vm.asyncPendingParkKey
		vm.asyncPendingParkKey = asyncrt.WakerKey{}
	} else {
		vm.asyncCapture.kind = asyncrt.PollYielded
		vm.asyncCapture.parkKey = asyncrt.WakerKey{}
	}
	vm.asyncCapture.state = stateVal
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
	var retVal Value
	if term.AsyncReturn.HasValue {
		val, vmErr := vm.evalOperand(frame, &term.AsyncReturn.Value)
		if vmErr != nil {
			return vmErr
		}
		retVal = val
	}
	vm.dropFrameLocals(frame)
	vm.Stack = vm.Stack[:len(vm.Stack)-1]
	vm.asyncCapture.set = true
	vm.asyncCapture.kind = asyncrt.PollDone
	vm.asyncCapture.parkKey = asyncrt.WakerKey{}
	vm.asyncPendingParkKey = asyncrt.WakerKey{}
	vm.asyncCapture.state = stateVal
	vm.asyncCapture.value = retVal
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
