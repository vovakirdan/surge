package vm

import (
	"io"
	"os"

	"surge/internal/mir"
	"surge/internal/vm/bignum"
)

func (vm *VM) handleFsOpen(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_open requires 2 arguments")
	}
	pathVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(pathVal)
	strVal, vmErr := vm.extractStringValue(pathVal)
	if vmErr != nil {
		return vmErr
	}
	obj := vm.Heap.Get(strVal.H)
	path := vm.stringBytes(obj)
	flagsVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(flagsVal)
	flags, vmErr := vm.fsOpenFlagsFromValue(flagsVal)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	if fsInvalidPath(path) {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrInvalidPath, writes)
	}
	openMode, ok := fsOpenMode(flags)
	if !ok {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrInvalidData, writes)
	}
	// #nosec G302,G304 -- permissions follow POSIX defaults; path comes from program input.
	file, err := os.OpenFile(path, openMode, 0o666)
	if err != nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
	}

	handle := vm.fsNextHandle
	if handle == 0 {
		handle = 1
	}
	vm.fsNextHandle = handle + 1
	if vm.fsFiles == nil {
		vm.fsFiles = make(map[uint64]*vmFile)
	}
	vm.fsFiles[handle] = &vmFile{file: file, path: path}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		if cerr := file.Close(); cerr != nil {
			_ = cerr
		}
		delete(vm.fsFiles, handle)
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		if cerr := file.Close(); cerr != nil {
			_ = cerr
		}
		delete(vm.fsFiles, handle)
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	fileVal, vmErr := vm.fileValue(handle, tc.PayloadTypes[0])
	if vmErr != nil {
		if cerr := file.Close(); cerr != nil {
			_ = cerr
		}
		delete(vm.fsFiles, handle)
		return vmErr
	}
	return vm.fsWriteSuccess(frame, dstLocal, dstType, fileVal, writes)
}

func (vm *VM) handleFsClose(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_close requires 1 argument")
	}
	fileVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(fileVal)
	handle, vmErr := vm.fileHandleFromValue(fileVal)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}
	entry := vm.fsFiles[handle]
	if entry == nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrIo, writes)
	}
	delete(vm.fsFiles, handle)
	if err := entry.file.Close(); err != nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
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
	return vm.fsWriteSuccess(frame, dstLocal, dstType, payload, writes)
}

func (vm *VM) handleFsRead(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_read requires 3 arguments")
	}
	fileVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(fileVal)
	handle, vmErr := vm.fileHandleFromValue(fileVal)
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
	capacity, vmErr := vm.uintValueToInt(capVal, "fs read cap out of range")
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}
	entry := vm.fsFiles[handle]
	if entry == nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrIo, writes)
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
		zero := vm.makeBigUint(tc.PayloadTypes[0], bignum.UintFromUint64(0))
		return vm.fsWriteSuccess(frame, dstLocal, dstType, zero, writes)
	}

	buf := make([]byte, capacity)
	n, err := entry.file.Read(buf)
	if err != nil && err != io.EOF && n == 0 {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
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
	// #nosec G115 -- n is non-negative from io.Reader.
	count := vm.makeBigUint(tc.PayloadTypes[0], bignum.UintFromUint64(uint64(n)))
	return vm.fsWriteSuccess(frame, dstLocal, dstType, count, writes)
}

func (vm *VM) handleFsWrite(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_write requires 3 arguments")
	}
	fileVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(fileVal)
	handle, vmErr := vm.fileHandleFromValue(fileVal)
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
	n, vmErr := vm.uintValueToInt(lenVal, "fs write length out of range")
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}
	entry := vm.fsFiles[handle]
	if entry == nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrIo, writes)
	}

	data, vmErr := vm.readBytesFromPointer(bufVal, n)
	if vmErr != nil {
		return vmErr
	}
	written, err := entry.file.Write(data)
	if err != nil && written == 0 {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	// #nosec G115 -- written is non-negative from io.Writer.
	count := vm.makeBigUint(tc.PayloadTypes[0], bignum.UintFromUint64(uint64(written)))
	return vm.fsWriteSuccess(frame, dstLocal, dstType, count, writes)
}

func (vm *VM) handleFsSeek(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_seek requires 3 arguments")
	}
	fileVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(fileVal)
	handle, vmErr := vm.fileHandleFromValue(fileVal)
	if vmErr != nil {
		return vmErr
	}
	offsetVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(offsetVal)
	offset, vmErr := vm.int64FromValue(offsetVal, "seek offset out of range")
	if vmErr != nil {
		return vmErr
	}
	whenceVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(whenceVal)
	whence, ok, vmErr := vm.fsSeekWhenceFromValue(whenceVal)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}
	entry := vm.fsFiles[handle]
	if entry == nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrIo, writes)
	}
	if !ok {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrInvalidData, writes)
	}

	pos, err := entry.file.Seek(offset, whence)
	if err != nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
	}
	if pos < 0 {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrInvalidData, writes)
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	count := vm.makeBigUint(tc.PayloadTypes[0], bignum.UintFromUint64(uint64(pos)))
	return vm.fsWriteSuccess(frame, dstLocal, dstType, count, writes)
}

func (vm *VM) handleFsFlush(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_flush requires 1 argument")
	}
	fileVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(fileVal)
	handle, vmErr := vm.fileHandleFromValue(fileVal)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}
	entry := vm.fsFiles[handle]
	if entry == nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrIo, writes)
	}
	if err := entry.file.Sync(); err != nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
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
	return vm.fsWriteSuccess(frame, dstLocal, dstType, payload, writes)
}

func (vm *VM) handleFsReadFile(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_read_file requires 1 argument")
	}
	pathVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(pathVal)
	strVal, vmErr := vm.extractStringValue(pathVal)
	if vmErr != nil {
		return vmErr
	}
	obj := vm.Heap.Get(strVal.H)
	path := vm.stringBytes(obj)

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	if fsInvalidPath(path) {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrInvalidPath, writes)
	}
	// #nosec G304 -- path is provided by the program input.
	data, err := os.ReadFile(path)
	if err != nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	arrType := tc.PayloadTypes[0]
	elemType, ok := vm.Types.ArrayInfo(arrType)
	if !ok {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_read_file requires byte[] payload")
	}

	elems := make([]Value, len(data))
	for i, b := range data {
		elems[i] = MakeInt(int64(b), elemType)
	}
	arrHandle := vm.Heap.AllocArray(arrType, elems)
	arrVal := MakeHandleArray(arrHandle, arrType)
	return vm.fsWriteSuccess(frame, dstLocal, dstType, arrVal, writes)
}

func (vm *VM) handleFsWriteFile(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 4 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_write_file requires 4 arguments")
	}
	pathVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(pathVal)
	strVal, vmErr := vm.extractStringValue(pathVal)
	if vmErr != nil {
		return vmErr
	}
	obj := vm.Heap.Get(strVal.H)
	path := vm.stringBytes(obj)
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
	flagsVal, vmErr := vm.evalOperand(frame, &call.Args[3])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(flagsVal)
	dataLen, vmErr := vm.uintValueToInt(lenVal, "fs write length out of range")
	if vmErr != nil {
		return vmErr
	}
	flags, vmErr := vm.fsOpenFlagsFromValue(flagsVal)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	if fsInvalidPath(path) {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrInvalidPath, writes)
	}
	openMode, ok := fsOpenMode(flags)
	if !ok {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrInvalidData, writes)
	}
	data, vmErr := vm.readBytesFromPointer(bufVal, dataLen)
	if vmErr != nil {
		return vmErr
	}
	// #nosec G302,G304 -- permissions follow POSIX defaults; path comes from program input.
	file, err := os.OpenFile(path, openMode, 0o666)
	if err != nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			_ = cerr
		}
	}()
	written := 0
	for written < len(data) {
		n, err := file.Write(data[written:])
		if err != nil {
			return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
		}
		if n == 0 {
			return vm.fsWriteError(frame, dstLocal, errType, fsErrIo, writes)
		}
		written += n
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
	return vm.fsWriteSuccess(frame, dstLocal, dstType, payload, writes)
}
