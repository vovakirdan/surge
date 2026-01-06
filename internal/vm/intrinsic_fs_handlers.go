package vm

import (
	"os"
	"strings"

	"surge/internal/mir"
)

func (vm *VM) handleFsCwd(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_cwd requires 0 arguments")
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	cwd, err := os.Getwd()
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
	payloadType := tc.PayloadTypes[0]
	h := vm.Heap.AllocString(payloadType, cwd)
	payload := MakeHandleString(h, payloadType)
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

func (vm *VM) handleFsMetadata(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_metadata requires 1 argument")
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
	resVal, vmErr := vm.fsSuccessValue(dstType, metaVal)
	if vmErr != nil {
		vm.dropValue(metaVal)
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

func (vm *VM) handleFsReadDir(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_read_dir requires 1 argument")
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

	entries, err := os.ReadDir(path)
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
	arrType := tc.PayloadTypes[0]
	elemType, ok := vm.Types.ArrayInfo(arrType)
	if !ok {
		return vm.eb.makeError(PanicTypeMismatch, "rt_fs_read_dir requires DirEntry[] payload")
	}

	elems := make([]Value, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		fullPath := path
		if fullPath == "" || strings.HasSuffix(fullPath, "/") {
			fullPath += name
		} else {
			fullPath += "/" + name
		}
		mode := entry.Type()
		fileType := fsTypeOther
		switch {
		case mode&os.ModeSymlink != 0:
			fileType = fsTypeSymlink
		case mode.IsDir():
			fileType = fsTypeDir
		case mode.IsRegular():
			fileType = fsTypeFile
		}
		elem, elemErr := vm.fsDirEntryValue(elemType, name, fullPath, fileType)
		if elemErr != nil {
			for _, v := range elems {
				vm.dropValue(v)
			}
			return elemErr
		}
		elems = append(elems, elem)
	}

	arrHandle := vm.Heap.AllocArray(arrType, elems)
	arrVal := MakeHandleArray(arrHandle, arrType)
	resVal, vmErr := vm.fsSuccessValue(dstType, arrVal)
	if vmErr != nil {
		vm.dropValue(arrVal)
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
