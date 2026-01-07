package vm

import (
	"os"
	"path/filepath"

	"surge/internal/mir"
)

func (vm *VM) handleFsFileName(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_file_name requires 1 argument")
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
	name := filepath.Base(entry.path)

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	payloadType := tc.PayloadTypes[0]
	h := vm.Heap.AllocString(payloadType, name)
	payload := MakeHandleString(h, payloadType)
	return vm.fsWriteSuccess(frame, dstLocal, dstType, payload, writes)
}

func (vm *VM) handleFsFileType(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_file_type requires 1 argument")
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
	info, err := entry.file.Stat()
	if err != nil {
		return vm.fsWriteError(frame, dstLocal, errType, fsErrorCodeFromErr(err), writes)
	}
	mode := info.Mode()
	fileType := fsTypeOther
	switch {
	case mode&os.ModeSymlink != 0:
		fileType = fsTypeSymlink
	case mode.IsDir():
		fileType = fsTypeDir
	case mode.IsRegular():
		fileType = fsTypeFile
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	payload := MakeInt(int64(fileType), tc.PayloadTypes[0])
	return vm.fsWriteSuccess(frame, dstLocal, dstType, payload, writes)
}

func (vm *VM) handleFsFileMetadata(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_file_metadata requires 1 argument")
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
	info, err := entry.file.Stat()
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
	metaVal, vmErr := vm.fsMetadataValue(tc.PayloadTypes[0], info)
	if vmErr != nil {
		return vmErr
	}
	return vm.fsWriteSuccess(frame, dstLocal, dstType, metaVal, writes)
}
