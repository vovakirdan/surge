package vm

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

const (
	fsErrNotFound         uint64 = 1
	fsErrPermissionDenied uint64 = 2
	fsErrAlreadyExists    uint64 = 3
	fsErrInvalidPath      uint64 = 4
	fsErrNotDir           uint64 = 5
	fsErrNotFile          uint64 = 6
	fsErrIsDir            uint64 = 7
	fsErrInvalidData      uint64 = 8
	fsErrIo               uint64 = 9
	fsErrUnsupported      uint64 = 10
)

const (
	fsTypeFile uint8 = iota
	fsTypeDir
	fsTypeSymlink
	fsTypeOther
)

const (
	fsOpenRead uint32 = 1 << iota
	fsOpenWrite
	fsOpenCreate
	fsOpenTrunc
	fsOpenAppend
)

const fsOpenAllFlags = fsOpenRead | fsOpenWrite | fsOpenCreate | fsOpenTrunc | fsOpenAppend

const (
	fsSeekStart   = 0
	fsSeekCurrent = 1
	fsSeekEnd     = 2
)

func fsErrorMessage(code uint64) string {
	switch code {
	case fsErrNotFound:
		return "NotFound"
	case fsErrPermissionDenied:
		return "PermissionDenied"
	case fsErrAlreadyExists:
		return "AlreadyExists"
	case fsErrInvalidPath:
		return "InvalidPath"
	case fsErrNotDir:
		return "NotDir"
	case fsErrNotFile:
		return "NotFile"
	case fsErrIsDir:
		return "IsDir"
	case fsErrInvalidData:
		return "InvalidData"
	case fsErrUnsupported:
		return "Unsupported"
	default:
		return "Io"
	}
}

func fsInvalidPath(path string) bool {
	if path == "" {
		return true
	}
	return strings.IndexByte(path, 0) >= 0
}

func fsErrorCodeFromErr(err error) uint64 {
	if err == nil {
		return 0
	}
	if errors.Is(err, fs.ErrInvalid) {
		return fsErrInvalidPath
	}
	if errors.Is(err, os.ErrNotExist) {
		return fsErrNotFound
	}
	if errors.Is(err, os.ErrPermission) {
		return fsErrPermissionDenied
	}
	if errors.Is(err, os.ErrExist) {
		return fsErrAlreadyExists
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && pathErr.Err != nil {
		err = pathErr.Err
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return fsErrorCodeFromErrno(errno)
	}
	return fsErrIo
}

func fsErrorCodeFromErrno(errno syscall.Errno) uint64 {
	switch errno {
	case syscall.ENOENT:
		return fsErrNotFound
	case syscall.EPERM, syscall.EACCES:
		return fsErrPermissionDenied
	case syscall.EEXIST:
		return fsErrAlreadyExists
	case syscall.ENOTDIR:
		return fsErrNotDir
	case syscall.EISDIR:
		return fsErrIsDir
	case syscall.EINVAL, syscall.ENAMETOOLONG, syscall.ELOOP:
		return fsErrInvalidPath
	case syscall.ENOSYS, syscall.EOPNOTSUPP:
		return fsErrUnsupported
	default:
		return fsErrIo
	}
}

func (vm *VM) fsErrorValue(errType types.TypeID, code uint64) (Value, *VMError) {
	return vm.makeErrorLikeValue(errType, fsErrorMessage(code), code)
}

func (vm *VM) fsSuccessValue(dstType types.TypeID, payload Value) (Value, *VMError) {
	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return Value{}, vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	payloadType := tc.PayloadTypes[0]
	if payload.TypeID == types.NoTypeID && payloadType != types.NoTypeID {
		payload.TypeID = payloadType
	}
	if payload.TypeID != types.NoTypeID && vm.valueType(payload.TypeID) != vm.valueType(payloadType) {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "Erring Success payload type mismatch")
	}
	h := vm.Heap.AllocTag(dstType, tc.TagSym, []Value{payload})
	return MakeHandleTag(h, dstType), nil
}

func (vm *VM) fsMetadataValue(typeID types.TypeID, info os.FileInfo) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	sizeIdx, okSize := layout.IndexByName["size"]
	typeIdx, okType := layout.IndexByName["file_type"]
	readonlyIdx, okReadonly := layout.IndexByName["readonly"]
	if !okSize || !okType || !okReadonly {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "Metadata layout mismatch")
	}
	fields := make([]Value, len(layout.FieldTypes))
	for i, ft := range layout.FieldTypes {
		val, vmErr := vm.defaultValue(ft)
		if vmErr != nil {
			for j := range i {
				vm.dropValue(fields[j])
			}
			return Value{}, vmErr
		}
		fields[i] = val
	}

	size := info.Size()
	if size < 0 {
		size = 0
	}
	// #nosec G115 -- size is clamped to non-negative; file size fits in uint64.
	sizeU := uint64(size)

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
	readonly := mode.Perm()&0o222 == 0

	fields[sizeIdx] = vm.makeBigUint(layout.FieldTypes[sizeIdx], bignum.UintFromUint64(sizeU))
	fields[typeIdx] = MakeInt(int64(fileType), layout.FieldTypes[typeIdx])
	fields[readonlyIdx] = MakeBool(readonly, layout.FieldTypes[readonlyIdx])

	h := vm.Heap.AllocStruct(layout.TypeID, fields)
	return MakeHandleStruct(h, typeID), nil
}

func (vm *VM) fsDirEntryValue(typeID types.TypeID, name, path string, fileType uint8) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	nameIdx, okName := layout.IndexByName["name"]
	pathIdx, okPath := layout.IndexByName["path"]
	typeIdx, okType := layout.IndexByName["file_type"]
	if !okName || !okPath || !okType {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "DirEntry layout mismatch")
	}
	fields := make([]Value, len(layout.FieldTypes))
	for i, ft := range layout.FieldTypes {
		val, vmErr := vm.defaultValue(ft)
		if vmErr != nil {
			for j := range i {
				vm.dropValue(fields[j])
			}
			return Value{}, vmErr
		}
		fields[i] = val
	}

	nameType := layout.FieldTypes[nameIdx]
	pathType := layout.FieldTypes[pathIdx]
	nameHandle := vm.Heap.AllocString(nameType, name)
	pathHandle := vm.Heap.AllocString(pathType, path)
	fields[nameIdx] = MakeHandleString(nameHandle, nameType)
	fields[pathIdx] = MakeHandleString(pathHandle, pathType)
	fields[typeIdx] = MakeInt(int64(fileType), layout.FieldTypes[typeIdx])

	h := vm.Heap.AllocStruct(layout.TypeID, fields)
	return MakeHandleStruct(h, typeID), nil
}
