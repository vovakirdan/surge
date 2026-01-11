package vm

import (
	"os"

	"surge/internal/mir"
)

func (vm *VM) handleFsMkdir(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_mkdir requires 2 arguments")
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
	recVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(recVal)
	if recVal.Kind != VKBool {
		return vm.eb.typeMismatch("bool", recVal.Kind.String())
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	if fsInvalidPath(path) {
		errVal, errVM := vm.fsErrorValue(errType, fsErrInvalidPath)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
	}

	var err error
	if recVal.Bool {
		// #nosec G301 -- permissions follow POSIX defaults; umask applies.
		err = os.MkdirAll(path, 0o777)
	} else {
		// #nosec G301 -- permissions follow POSIX defaults; umask applies.
		err = os.Mkdir(path, 0o777)
	}
	if err != nil {
		code := fsErrorCodeFromErr(err)
		errVal, errVM := vm.fsErrorValue(errType, code)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
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
	resVal, vmErr := vm.fsSuccessValue(dstType, payload)
	if vmErr != nil {
		vm.dropValue(payload)
		return vmErr
	}
	if writeErr := vm.writeLocal(frame, dstLocal, resVal); writeErr != nil {
		vm.dropValue(resVal)
		return writeErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   resVal,
	})
	return nil
}

func (vm *VM) handleFsRemoveFile(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_remove_file requires 1 argument")
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
		errVal, errVM := vm.fsErrorValue(errType, fsErrInvalidPath)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
	}

	info, err := os.Lstat(path)
	if err != nil {
		code := fsErrorCodeFromErr(err)
		errVal, errVM := vm.fsErrorValue(errType, code)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
	}
	if info.IsDir() {
		errVal, errVM := vm.fsErrorValue(errType, fsErrIsDir)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
	}

	if err := os.Remove(path); err != nil {
		code := fsErrorCodeFromErr(err)
		errVal, errVM := vm.fsErrorValue(errType, code)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
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
	resVal, vmErr := vm.fsSuccessValue(dstType, payload)
	if vmErr != nil {
		vm.dropValue(payload)
		return vmErr
	}
	if writeErr := vm.writeLocal(frame, dstLocal, resVal); writeErr != nil {
		vm.dropValue(resVal)
		return writeErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   resVal,
	})
	return nil
}

func (vm *VM) handleFsRemoveDir(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_remove_dir requires 2 arguments")
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
	recVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(recVal)
	if recVal.Kind != VKBool {
		return vm.eb.typeMismatch("bool", recVal.Kind.String())
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	if fsInvalidPath(path) {
		errVal, errVM := vm.fsErrorValue(errType, fsErrInvalidPath)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
	}

	info, err := os.Lstat(path)
	if err != nil {
		code := fsErrorCodeFromErr(err)
		errVal, errVM := vm.fsErrorValue(errType, code)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
	}
	if !info.IsDir() {
		errVal, errVM := vm.fsErrorValue(errType, fsErrNotDir)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
	}

	if recVal.Bool {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		code := fsErrorCodeFromErr(err)
		errVal, errVM := vm.fsErrorValue(errType, code)
		if errVM != nil {
			return errVM
		}
		if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
			vm.dropValue(errVal)
			return writeErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
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
	resVal, vmErr := vm.fsSuccessValue(dstType, payload)
	if vmErr != nil {
		vm.dropValue(payload)
		return vmErr
	}
	if writeErr := vm.writeLocal(frame, dstLocal, resVal); writeErr != nil {
		vm.dropValue(resVal)
		return writeErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   resVal,
	})
	return nil
}
