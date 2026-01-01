package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
)

// execCall executes a call instruction.
func (vm *VM) execCall(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) (*Frame, *VMError) {
	// Find the function to call.
	var targetFn *mir.Func
	switch call.Callee.Kind {
	case mir.CalleeSym:
		if !call.Callee.Sym.IsValid() {
			return nil, vm.callIntrinsic(frame, call, writes)
		}
		targetFn = vm.findFunctionBySym(call.Callee.Sym)
		if targetFn == nil {
			// Support selected intrinsics and extern calls that are not lowered into MIR.
			return nil, vm.callIntrinsic(frame, call, writes)
		}
	case mir.CalleeValue:
		targetFn = vm.findFunction(call.Callee.Name)
		if targetFn == nil {
			return nil, vm.callIntrinsic(frame, call, writes)
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

	// Push new frame
	newFrame := NewFrame(targetFn)

	// Pass arguments as first locals (params)
	if len(args) > len(newFrame.Locals) {
		return nil, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("too many arguments: got %d, expected at most %d", len(args), len(newFrame.Locals)))
	}
	for i, arg := range args {
		localID, err := safecast.Conv[mir.LocalID](i)
		if err != nil {
			return nil, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("invalid argument index %d", i))
		}
		if vmErr := vm.writeLocal(newFrame, localID, arg); vmErr != nil {
			return nil, vmErr
		}
	}

	return newFrame, nil
}
