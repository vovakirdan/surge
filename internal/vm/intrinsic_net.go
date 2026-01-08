package vm

import (
	"net"
	"syscall"

	"surge/internal/mir"
)

func closeNetFD(fd int) {
	if err := syscall.Close(fd); err != nil {
		// Best-effort cleanup; preserve the original error.
		_ = err
	}
}

func (vm *VM) handleNetListen(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_listen requires 2 arguments")
	}
	addrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(addrVal)
	addrStr, vmErr := vm.extractStringValue(addrVal)
	if vmErr != nil {
		return vmErr
	}
	addrObj := vm.Heap.Get(addrStr.H)
	addr := string(vm.stringBytes(addrObj))
	portVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(portVal)
	port, vmErr := vm.uintValueToInt(portVal, "net listen port out of range")
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	if netInvalidAddr(addr) || port < 0 || port > 65535 {
		return vm.netWriteError(frame, dstLocal, errType, netErrInvalidAddr, writes)
	}
	ip := net.ParseIP(addr)
	ip4 := ip.To4()
	if ip == nil || ip4 == nil {
		return vm.netWriteError(frame, dstLocal, errType, netErrInvalidAddr, writes)
	}

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		closeNetFD(fd)
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		closeNetFD(fd)
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}

	addr4 := [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]}
	sa := &syscall.SockaddrInet4{Port: port, Addr: addr4}
	if err := syscall.Bind(fd, sa); err != nil {
		closeNetFD(fd)
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}
	if err := syscall.Listen(fd, syscall.SOMAXCONN); err != nil {
		closeNetFD(fd)
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		closeNetFD(fd)
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		closeNetFD(fd)
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	handle := vm.netNextListen
	if handle == 0 {
		handle = 1
	}
	vm.netNextListen = handle + 1
	listenerVal, vmErr := vm.netListenerValue(handle, tc.PayloadTypes[0])
	if vmErr != nil {
		closeNetFD(fd)
		return vmErr
	}
	vm.netListeners[handle] = &vmNetListener{fd: fd}
	return vm.netWriteSuccess(frame, dstLocal, dstType, listenerVal, writes)
}

func (vm *VM) handleNetCloseListener(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_close_listener requires 1 argument")
	}
	listenerVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(listenerVal)
	handle, vmErr := vm.netListenerHandleFromValue(listenerVal)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	entry := vm.netListeners[handle]
	if entry == nil || entry.closed {
		return vm.netWriteError(frame, dstLocal, errType, netErrNotConnected, writes)
	}
	delete(vm.netListeners, handle)
	entry.closed = true
	if err := syscall.Close(entry.fd); err != nil {
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	payload, vmErr := vm.defaultValue(tc.PayloadTypes[0])
	if vmErr != nil {
		return vmErr
	}
	return vm.netWriteSuccess(frame, dstLocal, dstType, payload, writes)
}

func (vm *VM) handleNetCloseConn(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_close_conn requires 1 argument")
	}
	connVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(connVal)
	handle, vmErr := vm.netConnHandleFromValue(connVal)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	entry := vm.netConns[handle]
	if entry == nil || entry.closed {
		return vm.netWriteError(frame, dstLocal, errType, netErrNotConnected, writes)
	}
	delete(vm.netConns, handle)
	entry.closed = true
	if err := syscall.Close(entry.fd); err != nil {
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	payload, vmErr := vm.defaultValue(tc.PayloadTypes[0])
	if vmErr != nil {
		return vmErr
	}
	return vm.netWriteSuccess(frame, dstLocal, dstType, payload, writes)
}

func (vm *VM) handleNetAccept(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_accept requires 1 argument")
	}
	listenerVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(listenerVal)
	handle, vmErr := vm.netListenerHandleFromValue(listenerVal)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	entry := vm.netListeners[handle]
	if entry == nil || entry.closed {
		return vm.netWriteError(frame, dstLocal, errType, netErrNotConnected, writes)
	}

	fd, _, err := syscall.Accept(entry.fd)
	if err != nil {
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		closeNetFD(fd)
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		closeNetFD(fd)
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		closeNetFD(fd)
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	connHandle := vm.netNextConn
	if connHandle == 0 {
		connHandle = 1
	}
	vm.netNextConn = connHandle + 1
	connVal, vmErr := vm.netConnValue(connHandle, tc.PayloadTypes[0])
	if vmErr != nil {
		closeNetFD(fd)
		return vmErr
	}
	vm.netConns[connHandle] = &vmNetConn{fd: fd}
	return vm.netWriteSuccess(frame, dstLocal, dstType, connVal, writes)
}

func (vm *VM) handleNetRead(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_read requires 3 arguments")
	}
	connVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(connVal)
	handle, vmErr := vm.netConnHandleFromValue(connVal)
	if vmErr != nil {
		return vmErr
	}
	bufVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(bufVal)
	capVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(capVal)
	capacity, vmErr := vm.uintValueToInt(capVal, "net read cap out of range")
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	entry := vm.netConns[handle]
	if entry == nil || entry.closed {
		return vm.netWriteError(frame, dstLocal, errType, netErrNotConnected, writes)
	}
	if capacity == 0 {
		layout, layoutErr := vm.tagLayoutFor(dstType)
		if layoutErr != nil {
			return layoutErr
		}
		tc, ok := layout.CaseByName("Success")
		if !ok || len(tc.PayloadTypes) != 1 {
			return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
		}
		zeroVal, makeErr := vm.makeUintForType(tc.PayloadTypes[0], 0)
		if makeErr != nil {
			return makeErr
		}
		return vm.netWriteSuccess(frame, dstLocal, dstType, zeroVal, writes)
	}

	buf := make([]byte, capacity)
	var n int
	var err error
	for {
		n, err = syscall.Read(entry.fd, buf)
		if err == syscall.EINTR {
			continue
		}
		break
	}
	if err != nil {
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}
	if n > 0 {
		if writeErr := vm.writeBytesToPointer(bufVal, buf[:n]); writeErr != nil {
			return writeErr
		}
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	countVal, makeErr := vm.makeUintForType(tc.PayloadTypes[0], uint64(n)) //nolint:gosec // n from syscall.Read is non-negative.
	if makeErr != nil {
		return makeErr
	}
	return vm.netWriteSuccess(frame, dstLocal, dstType, countVal, writes)
}

func (vm *VM) handleNetWrite(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_write requires 3 arguments")
	}
	connVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(connVal)
	handle, vmErr := vm.netConnHandleFromValue(connVal)
	if vmErr != nil {
		return vmErr
	}
	bufVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(bufVal)
	lenVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(lenVal)
	length, vmErr := vm.uintValueToInt(lenVal, "net write length out of range")
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	entry := vm.netConns[handle]
	if entry == nil || entry.closed {
		return vm.netWriteError(frame, dstLocal, errType, netErrNotConnected, writes)
	}
	if length == 0 {
		layout, layoutErr := vm.tagLayoutFor(dstType)
		if layoutErr != nil {
			return layoutErr
		}
		tc, ok := layout.CaseByName("Success")
		if !ok || len(tc.PayloadTypes) != 1 {
			return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
		}
		zeroVal, makeErr := vm.makeUintForType(tc.PayloadTypes[0], 0)
		if makeErr != nil {
			return makeErr
		}
		return vm.netWriteSuccess(frame, dstLocal, dstType, zeroVal, writes)
	}

	data, vmErr := vm.readBytesFromPointer(bufVal, length)
	if vmErr != nil {
		return vmErr
	}
	var n int
	var err error
	for {
		n, err = syscall.Write(entry.fd, data)
		if err == syscall.EINTR {
			continue
		}
		break
	}
	if err != nil {
		return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	countVal, makeErr := vm.makeUintForType(tc.PayloadTypes[0], uint64(n)) //nolint:gosec // n from syscall.Write is non-negative.
	if makeErr != nil {
		return makeErr
	}
	return vm.netWriteSuccess(frame, dstLocal, dstType, countVal, writes)
}

func (vm *VM) handleNetWaitAccept(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "rt_net_wait_accept missing destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_wait_accept requires 1 argument")
	}
	listenerVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(listenerVal)
	handle, vmErr := vm.netListenerHandleFromValue(listenerVal)
	if vmErr != nil {
		return vmErr
	}

	fd := -1
	if entry := vm.netListeners[handle]; entry != nil && !entry.closed {
		fd = entry.fd
	}

	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	id := exec.SpawnNetAccept(&netWaitState{fd: fd})
	taskVal, vmErr := vm.taskValue(id, frame.Locals[call.Dst.Local].TypeID)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, call.Dst.Local, taskVal); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: call.Dst.Local,
			Name:    frame.Locals[call.Dst.Local].Name,
			Value:   taskVal,
		})
	}
	return nil
}

func (vm *VM) handleNetWaitReadable(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "rt_net_wait_readable missing destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_wait_readable requires 1 argument")
	}
	connVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(connVal)
	handle, vmErr := vm.netConnHandleFromValue(connVal)
	if vmErr != nil {
		return vmErr
	}

	fd := -1
	if entry := vm.netConns[handle]; entry != nil && !entry.closed {
		fd = entry.fd
	}

	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	id := exec.SpawnNetRead(&netWaitState{fd: fd})
	taskVal, vmErr := vm.taskValue(id, frame.Locals[call.Dst.Local].TypeID)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, call.Dst.Local, taskVal); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: call.Dst.Local,
			Name:    frame.Locals[call.Dst.Local].Name,
			Value:   taskVal,
		})
	}
	return nil
}

func (vm *VM) handleNetWaitWritable(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "rt_net_wait_writable missing destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_wait_writable requires 1 argument")
	}
	connVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(connVal)
	handle, vmErr := vm.netConnHandleFromValue(connVal)
	if vmErr != nil {
		return vmErr
	}

	fd := -1
	if entry := vm.netConns[handle]; entry != nil && !entry.closed {
		fd = entry.fd
	}

	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	id := exec.SpawnNetWrite(&netWaitState{fd: fd})
	taskVal, vmErr := vm.taskValue(id, frame.Locals[call.Dst.Local].TypeID)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, call.Dst.Local, taskVal); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: call.Dst.Local,
			Name:    frame.Locals[call.Dst.Local].Name,
			Value:   taskVal,
		})
	}
	return nil
}
