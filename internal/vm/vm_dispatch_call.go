package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// execCall executes a call instruction.
func (vm *VM) execCall(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) (*Frame, *VMError) {
	// Find the function to call.
	var targetFn *mir.Func
	switch call.Callee.Kind {
	case mir.CalleeSym:
		targetFn = vm.resolveCallTarget(frame, call)
		if targetFn == nil {
			// Support selected intrinsics and extern calls that are not lowered into MIR.
			return nil, vm.callIntrinsic(frame, call, writes)
		}
	case mir.CalleeValue:
		if call.Callee.Value.Type != types.NoTypeID {
			val, vmErr := vm.evalOperand(frame, &call.Callee.Value)
			if vmErr != nil {
				return nil, vmErr
			}
			defer vm.dropValue(val)
			if val.Kind != VKFunc {
				return nil, vm.eb.typeMismatch("function", val.Kind.String())
			}
			targetFn = vm.findFunctionBySym(val.Sym)
			if targetFn == nil {
				return nil, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("missing function sym %d", val.Sym))
			}
		} else {
			targetFn = vm.resolveCallTarget(frame, call)
			if targetFn == nil {
				return nil, vm.callIntrinsic(frame, call, writes)
			}
		}
	default:
		return nil, vm.eb.unimplemented("unknown call target")
	}

	// Evaluate arguments
	args := make([]Value, len(call.Args))
	for i := range call.Args {
		val, vmErr := vm.evalOperand(frame, &call.Args[i])
		if vmErr != nil {
			return nil, vmErr
		}
		args[i] = val
	}

	// How the arguments and the result travel is the callee's contract, not
	// this call site's choice, so it is asked for before either is moved.
	contract, vmErr := vm.contractOf(targetFn)
	if vmErr != nil {
		return nil, vmErr
	}

	// Push new frame
	newFrame := vm.activate(targetFn)
	newFrame.Result = contract.resultProtocolFor(callResultDest(frame, call))

	if vmErr := vm.passArguments(newFrame, contract, args); vmErr != nil {
		return nil, vmErr
	}

	return newFrame, nil
}

// callResultDest names the caller's slot for the result of one call. A call
// that discards its result provides no destination, which is a different fact
// from providing one that is never written.
func callResultDest(caller *Frame, call *mir.CallInstr) resultDest {
	if caller == nil || call == nil || !call.HasDst {
		return resultDest{}
	}
	return resultDest{Frame: caller, Local: call.Dst.Local, Has: true}
}
