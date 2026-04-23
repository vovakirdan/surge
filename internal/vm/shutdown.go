package vm

type shutdownState struct {
	active     bool
	checkLeaks bool
}

func (vm *VM) requestShutdown(checkLeaks bool) {
	if vm == nil {
		return
	}
	if vm.pollDepth > 0 {
		// Poll frames are isolated from the outer caller stack, so drop them now
		// and drain async task payloads before deferring full-program cleanup until
		// runPoll restores the parent stack.
		vm.dropAllFrames()
		vm.Stack = nil
		vm.Halted = true
		vm.dropAsyncTasks()
		vm.deferredShutdown.active = true
		vm.deferredShutdown.checkLeaks = vm.deferredShutdown.checkLeaks || checkLeaks
		return
	}
	vm.finishShutdown(checkLeaks)
}

func (vm *VM) finishShutdown(checkLeaks bool) {
	if vm == nil {
		return
	}
	vm.dropAllFrames()
	vm.dropGlobals()
	vm.dropAsyncTasks()
	if checkLeaks {
		vm.checkLeaksOrPanic()
	}
	vm.Halted = true
	vm.Stack = nil
}
