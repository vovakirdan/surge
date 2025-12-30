package vm

import "fmt"

func (vm *VM) failAsyncNotSupported(kind string, frame *Frame) *VMError {
	loc := "<unknown>"
	if frame != nil && frame.Func != nil {
		loc = fmt.Sprintf("%s bb%d ip%d", frame.Func.Name, frame.BB, frame.IP)
	}
	return vm.eb.makeError(PanicAsyncBackendNotImplemented,
		fmt.Sprintf("async backend not implemented: %s at %s", kind, loc))
}
