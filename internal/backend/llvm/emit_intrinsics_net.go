package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitNetIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	switch name {
	case "rt_net_listen":
		return true, fe.emitNetListen(call)
	case "rt_net_close_listener":
		return true, fe.emitNetUnary(call, "rt_net_close_listener", "TcpListener")
	case "rt_net_close_conn":
		return true, fe.emitNetUnary(call, "rt_net_close_conn", "TcpConn")
	case "rt_net_accept":
		return true, fe.emitNetAccept(call)
	case "rt_net_read":
		return true, fe.emitNetRead(call)
	case "rt_net_write":
		return true, fe.emitNetWrite(call)
	case "rt_net_wait_accept":
		return true, fe.emitNetWait(call, "rt_net_wait_accept", "TcpListener")
	case "rt_net_wait_readable":
		return true, fe.emitNetWait(call, "rt_net_wait_readable", "TcpConn")
	case "rt_net_wait_writable":
		return true, fe.emitNetWait(call, "rt_net_wait_writable", "TcpConn")
	default:
		return false, nil
	}
}

func (fe *funcEmitter) emitNetListen(call *mir.CallInstr) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_net_listen requires 2 arguments")
	}
	addrVal, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	port64, err := fe.emitUintOperandToI64(&call.Args[1], "net listen port out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_net_listen(ptr %s, i64 %s)\n", tmp, addrVal, port64)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitNetAccept(call *mir.CallInstr) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("rt_net_accept requires 1 argument")
	}
	listenerVal, err := fe.emitNetHandle(&call.Args[0], "TcpListener")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_net_accept(ptr %s)\n", tmp, listenerVal)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitNetRead(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_net_read requires 3 arguments")
	}
	connVal, err := fe.emitNetHandle(&call.Args[0], "TcpConn")
	if err != nil {
		return err
	}
	bufVal, bufTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if bufTy != "ptr" {
		return fmt.Errorf("rt_net_read expects *byte buffer")
	}
	cap64, err := fe.emitUintOperandToI64(&call.Args[2], "net read cap out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_net_read(ptr %s, ptr %s, i64 %s)\n", tmp, connVal, bufVal, cap64)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitNetWrite(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_net_write requires 3 arguments")
	}
	connVal, err := fe.emitNetHandle(&call.Args[0], "TcpConn")
	if err != nil {
		return err
	}
	bufVal, bufTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if bufTy != "ptr" {
		return fmt.Errorf("rt_net_write expects *byte buffer")
	}
	len64, err := fe.emitUintOperandToI64(&call.Args[2], "net write length out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_net_write(ptr %s, ptr %s, i64 %s)\n", tmp, connVal, bufVal, len64)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitNetUnary(call *mir.CallInstr, name, kind string) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("%s requires 1 argument", name)
	}
	val, err := fe.emitNetHandle(&call.Args[0], kind)
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(ptr %s)\n", tmp, name, val)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitNetWait(call *mir.CallInstr, name, kind string) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("%s requires 1 argument", name)
	}
	val, err := fe.emitNetHandle(&call.Args[0], kind)
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(ptr %s)\n", tmp, name, val)
	if call.HasDst {
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	}
	return nil
}

func (fe *funcEmitter) emitNetHandle(op *mir.Operand, kind string) (string, error) {
	if op == nil {
		return "", fmt.Errorf("missing %s handle", kind)
	}
	val, valTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	if valTy != "ptr" {
		return "", fmt.Errorf("expected %s handle, got %s", kind, valTy)
	}
	if op.Kind == mir.OperandAddrOf || op.Kind == mir.OperandAddrOfMut {
		return val, nil
	}
	opType := op.Type
	if opType == types.NoTypeID && op.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(op.Place); err == nil {
			opType = baseType
		}
	}
	if isRefType(fe.emitter.types, opType) {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", tmp, val)
		return tmp, nil
	}
	return val, nil
}
