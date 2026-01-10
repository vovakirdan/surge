package vm

import (
	"errors"
	"math"
	"strings"
	"syscall"

	"surge/internal/mir"
	"surge/internal/types"
)

type vmNetListener struct {
	fd     int
	closed bool
}

type vmNetConn struct {
	fd     int
	closed bool
}

const (
	netErrWouldBlock        uint64 = 1
	netErrTimedOut          uint64 = 2
	netErrConnectionReset   uint64 = 3
	netErrConnectionRefused uint64 = 4
	netErrNotConnected      uint64 = 5
	netErrAddrInUse         uint64 = 6
	netErrInvalidAddr       uint64 = 7
	netErrIo                uint64 = 8
	netErrUnsupported       uint64 = 9
)

func netErrorMessage(code uint64) string {
	switch code {
	case netErrWouldBlock:
		return "WouldBlock"
	case netErrTimedOut:
		return "TimedOut"
	case netErrConnectionReset:
		return "ConnectionReset"
	case netErrConnectionRefused:
		return "ConnectionRefused"
	case netErrNotConnected:
		return "NotConnected"
	case netErrAddrInUse:
		return "AddrInUse"
	case netErrInvalidAddr:
		return "InvalidAddr"
	case netErrUnsupported:
		return "Unsupported"
	default:
		return "Io"
	}
}

func netInvalidAddr(addr string) bool {
	if addr == "" {
		return true
	}
	return strings.IndexByte(addr, 0) >= 0
}

func netErrorCodeFromErr(err error) uint64 {
	if err == nil {
		return 0
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return netErrorCodeFromErrno(errno)
	}
	return netErrIo
}

func netErrorCodeFromErrno(errno syscall.Errno) uint64 {
	switch errno {
	case syscall.EAGAIN:
		return netErrWouldBlock
	case syscall.ETIMEDOUT:
		return netErrTimedOut
	case syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE:
		return netErrConnectionReset
	case syscall.ECONNREFUSED:
		return netErrConnectionRefused
	case syscall.ENOTCONN:
		return netErrNotConnected
	case syscall.EADDRINUSE:
		return netErrAddrInUse
	case syscall.EADDRNOTAVAIL, syscall.EINVAL:
		return netErrInvalidAddr
	case syscall.EAFNOSUPPORT, syscall.EPROTONOSUPPORT, syscall.ENOSYS, syscall.EOPNOTSUPP:
		return netErrUnsupported
	default:
		return netErrIo
	}
}

func (vm *VM) netErrorValue(errType types.TypeID, code uint64) (Value, *VMError) {
	return vm.makeErrorLikeValue(errType, netErrorMessage(code), code)
}

func (vm *VM) netSuccessValue(dstType types.TypeID, payload Value) (Value, *VMError) {
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

func (vm *VM) netWriteError(frame *Frame, dstLocal mir.LocalID, errType types.TypeID, code uint64, writes *[]LocalWrite) *VMError {
	errVal, vmErr := vm.netErrorValue(errType, code)
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

func (vm *VM) netWriteSuccess(frame *Frame, dstLocal mir.LocalID, dstType types.TypeID, payload Value, writes *[]LocalWrite) *VMError {
	resVal, vmErr := vm.netSuccessValue(dstType, payload)
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

func (vm *VM) netListenerHandleFromValue(val Value) (uint64, *VMError) {
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
			return 0, vm.eb.makeError(PanicInvalidHandle, "negative TcpListener handle")
		}
		return uint64(val.Int), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "TcpListener handle out of range")
		}
		return uint64(n), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok {
			return 0, vm.eb.makeError(PanicInvalidHandle, "TcpListener handle out of range")
		}
		return n, nil
	case VKHandleStruct:
		obj := vm.Heap.Get(val.H)
		if obj == nil || obj.Kind != OKStruct {
			return 0, vm.eb.typeMismatch("struct", "invalid TcpListener handle")
		}
		layout, vmErr := vm.layouts.Struct(val.TypeID)
		if vmErr != nil {
			return 0, vmErr
		}
		idx, ok := layout.IndexByName["__opaque"]
		if !ok {
			return 0, vm.eb.makeError(PanicTypeMismatch, "TcpListener missing __opaque field")
		}
		if idx < 0 || idx >= len(obj.Fields) {
			return 0, vm.eb.makeError(PanicOutOfBounds, "TcpListener __opaque field out of range")
		}
		return vm.netListenerHandleFromValue(obj.Fields[idx])
	default:
		return 0, vm.eb.typeMismatch("TcpListener", val.Kind.String())
	}
}

func (vm *VM) netListenerValue(handle uint64, typeID types.TypeID) (Value, *VMError) {
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
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "TcpListener missing __opaque field")
	}
	fieldType := layout.FieldTypes[idx]
	if handle > uint64(math.MaxInt64) {
		return Value{}, vm.eb.makeError(PanicInvalidHandle, "TcpListener handle out of range")
	}
	fields[idx] = MakeInt(int64(handle), fieldType)
	h := vm.Heap.AllocStruct(typeID, fields)
	return MakeHandleStruct(h, typeID), nil
}

func (vm *VM) netConnHandleFromValue(val Value) (uint64, *VMError) {
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
			return 0, vm.eb.makeError(PanicInvalidHandle, "negative TcpConn handle")
		}
		return uint64(val.Int), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "TcpConn handle out of range")
		}
		return uint64(n), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok {
			return 0, vm.eb.makeError(PanicInvalidHandle, "TcpConn handle out of range")
		}
		return n, nil
	case VKHandleStruct:
		obj := vm.Heap.Get(val.H)
		if obj == nil || obj.Kind != OKStruct {
			return 0, vm.eb.typeMismatch("struct", "invalid TcpConn handle")
		}
		layout, vmErr := vm.layouts.Struct(val.TypeID)
		if vmErr != nil {
			return 0, vmErr
		}
		idx, ok := layout.IndexByName["__opaque"]
		if !ok {
			return 0, vm.eb.makeError(PanicTypeMismatch, "TcpConn missing __opaque field")
		}
		if idx < 0 || idx >= len(obj.Fields) {
			return 0, vm.eb.makeError(PanicOutOfBounds, "TcpConn __opaque field out of range")
		}
		return vm.netConnHandleFromValue(obj.Fields[idx])
	default:
		return 0, vm.eb.typeMismatch("TcpConn", val.Kind.String())
	}
}

func (vm *VM) netConnValue(handle uint64, typeID types.TypeID) (Value, *VMError) {
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
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "TcpConn missing __opaque field")
	}
	fieldType := layout.FieldTypes[idx]
	if handle > uint64(math.MaxInt64) {
		return Value{}, vm.eb.makeError(PanicInvalidHandle, "TcpConn handle out of range")
	}
	fields[idx] = MakeInt(int64(handle), fieldType)
	h := vm.Heap.AllocStruct(typeID, fields)
	return MakeHandleStruct(h, typeID), nil
}
