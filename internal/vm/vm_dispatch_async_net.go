package vm

import (
	"surge/internal/asyncrt"
	"surge/internal/mir"
)

func (vm *VM) execInstrNetWait(frame *Frame, instr *mir.Instr, writes []LocalWrite) (pollExecResult, *VMError) {
	res := pollExecResult{writes: writes}

	exec := vm.ensureExecutor()
	if exec == nil {
		return res, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	current := exec.Current()
	if current == 0 {
		return res, vm.eb.makeError(PanicUnimplemented, "async net wait outside task")
	}
	task := exec.Task(current)
	if task == nil {
		return res, vm.eb.makeError(PanicInvalidHandle, "invalid current task")
	}
	if task.Cancelled {
		res.doJump = true
		res.jumpBB = instr.NetWait.PendBB
		return res, nil
	}

	fd, key, wantWrite, vmErr := vm.netWaitTarget(frame, instr)
	if vmErr != nil {
		return res, vmErr
	}
	if fd <= 0 {
		res.doJump = true
		res.jumpBB = instr.NetWait.ReadyBB
		return res, nil
	}
	ready, err := netFdReady(fd, wantWrite)
	if err != nil || ready {
		res.doJump = true
		res.jumpBB = instr.NetWait.ReadyBB
		return res, nil
	}

	vm.asyncPendingParkKey = key
	res.doJump = true
	res.jumpBB = instr.NetWait.PendBB
	return res, nil
}

func (vm *VM) netWaitTarget(frame *Frame, instr *mir.Instr) (fd int, key asyncrt.WakerKey, wantWrite bool, vmErr *VMError) {
	val, vmErr := vm.evalOperand(frame, &instr.NetWait.Handle)
	if vmErr != nil {
		return -1, asyncrt.WakerKey{}, false, vmErr
	}
	defer vm.dropValue(val)

	switch instr.NetWait.Kind {
	case mir.NetWaitAccept:
		handle, vmErr := vm.netListenerHandleFromValue(val)
		if vmErr != nil {
			return -1, asyncrt.WakerKey{}, false, vmErr
		}
		fd := -1
		if entry := vm.netListeners[handle]; entry != nil && !entry.closed {
			fd = entry.fd
		}
		return fd, asyncrt.NetAcceptKey(fd), false, nil
	case mir.NetWaitRead:
		handle, vmErr := vm.netConnHandleFromValue(val)
		if vmErr != nil {
			return -1, asyncrt.WakerKey{}, false, vmErr
		}
		fd := -1
		if entry := vm.netConns[handle]; entry != nil && !entry.closed {
			fd = entry.fd
		}
		return fd, asyncrt.NetReadKey(fd), false, nil
	case mir.NetWaitWrite:
		handle, vmErr := vm.netConnHandleFromValue(val)
		if vmErr != nil {
			return -1, asyncrt.WakerKey{}, false, vmErr
		}
		fd := -1
		if entry := vm.netConns[handle]; entry != nil && !entry.closed {
			fd = entry.fd
		}
		return fd, asyncrt.NetWriteKey(fd), true, nil
	default:
		return -1, asyncrt.WakerKey{}, false, vm.eb.makeError(PanicUnimplemented, "unknown net wait kind")
	}
}
