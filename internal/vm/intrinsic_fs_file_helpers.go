package vm

import (
	"io"
	"math"
	"os"

	"surge/internal/mir"
	"surge/internal/types"
)

type vmFile struct {
	file *os.File
	path string
}

func (vm *VM) fileHandleFromValue(val Value) (uint64, *VMError) {
	if val.Kind == VKRef || val.Kind == VKRefMut {
		loaded, vmErr := vm.loadLocationRaw(val.Loc)
		if vmErr != nil {
			return 0, vmErr
		}
		val = loaded
	}
	switch val.Kind {
	case VKInt:
		if val.Int < 0 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "negative file handle")
		}
		return uint64(val.Int), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "file handle out of range")
		}
		return uint64(n), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok {
			return 0, vm.eb.makeError(PanicInvalidHandle, "file handle out of range")
		}
		return n, nil
	case VKHandleStruct:
		obj := vm.Heap.Get(val.H)
		if obj == nil || obj.Kind != OKStruct {
			return 0, vm.eb.typeMismatch("struct", "invalid File handle")
		}
		layout, vmErr := vm.layouts.Struct(val.TypeID)
		if vmErr != nil {
			return 0, vmErr
		}
		idx, ok := layout.IndexByName["__opaque"]
		if !ok {
			return 0, vm.eb.makeError(PanicTypeMismatch, "File missing __opaque field")
		}
		if idx < 0 || idx >= len(obj.Fields) {
			return 0, vm.eb.makeError(PanicOutOfBounds, "File __opaque field out of range")
		}
		return vm.fileHandleFromValue(obj.Fields[idx])
	default:
		return 0, vm.eb.typeMismatch("File", val.Kind.String())
	}
}

func (vm *VM) fileValue(handle uint64, typeID types.TypeID) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	fields := make([]Value, len(layout.FieldNames))
	for i := range fields {
		fields[i] = Value{Kind: VKInvalid}
	}
	idx, ok := layout.IndexByName["__opaque"]
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "File missing __opaque field")
	}
	fieldType := layout.FieldTypes[idx]
	if handle > uint64(math.MaxInt64) {
		return Value{}, vm.eb.makeError(PanicInvalidHandle, "file handle out of range")
	}
	fields[idx] = MakeInt(int64(handle), fieldType)
	h := vm.Heap.AllocStruct(typeID, fields)
	return MakeHandleStruct(h, typeID), nil
}

func (vm *VM) fsOpenFlagsFromValue(val Value) (uint32, *VMError) {
	raw, vmErr := vm.uintValueToInt(val, "fs open flags out of range")
	if vmErr != nil {
		return 0, vmErr
	}
	if raw < 0 || raw > math.MaxUint32 {
		return 0, vm.eb.invalidNumericConversion("fs open flags out of range")
	}
	return uint32(raw), nil
}

func fsOpenMode(flags uint32) (int, bool) {
	if flags&^fsOpenAllFlags != 0 {
		return 0, false
	}
	read := flags&fsOpenRead != 0
	write := flags&fsOpenWrite != 0
	if !read && !write {
		return 0, false
	}
	mode := 0
	switch {
	case read && write:
		mode = os.O_RDWR
	case write:
		mode = os.O_WRONLY
	default:
		mode = os.O_RDONLY
	}
	if flags&fsOpenCreate != 0 {
		mode |= os.O_CREATE
	}
	if flags&fsOpenTrunc != 0 {
		mode |= os.O_TRUNC
	}
	if flags&fsOpenAppend != 0 {
		mode |= os.O_APPEND
	}
	return mode, true
}

func (vm *VM) fsSeekWhenceFromValue(val Value) (whence int, ok bool, err *VMError) {
	raw, vmErr := vm.intValueToInt(val, "seek whence out of range")
	if vmErr != nil {
		return 0, false, vmErr
	}
	switch raw {
	case fsSeekStart:
		return io.SeekStart, true, nil
	case fsSeekCurrent:
		return io.SeekCurrent, true, nil
	case fsSeekEnd:
		return io.SeekEnd, true, nil
	default:
		return 0, false, nil
	}
}

func (vm *VM) fsWriteError(frame *Frame, dstLocal mir.LocalID, errType types.TypeID, code uint64, writes *[]LocalWrite) *VMError {
	errVal, vmErr := vm.fsErrorValue(errType, code)
	if vmErr != nil {
		return vmErr
	}
	if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
		vm.dropValue(errVal)
		return writeErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
	}
	return nil
}

func (vm *VM) fsWriteSuccess(frame *Frame, dstLocal mir.LocalID, dstType types.TypeID, payload Value, writes *[]LocalWrite) *VMError {
	resVal, vmErr := vm.fsSuccessValue(dstType, payload)
	if vmErr != nil {
		vm.dropValue(payload)
		return vmErr
	}
	if writeErr := vm.writeLocal(frame, dstLocal, resVal); writeErr != nil {
		vm.dropValue(resVal)
		return writeErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   resVal,
		})
	}
	return nil
}
