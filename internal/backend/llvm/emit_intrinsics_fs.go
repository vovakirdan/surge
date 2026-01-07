package llvm

import (
	"fmt"

	"surge/internal/mir"
)

func (fe *funcEmitter) emitFsIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	switch name {
	case "rt_fs_open":
		return true, fe.emitFsOpen(call)
	case "rt_fs_close":
		return true, fe.emitFsUnary(call, "rt_fs_close")
	case "rt_fs_read":
		return true, fe.emitFsRead(call)
	case "rt_fs_write":
		return true, fe.emitFsWrite(call)
	case "rt_fs_seek":
		return true, fe.emitFsSeek(call)
	case "rt_fs_flush":
		return true, fe.emitFsUnary(call, "rt_fs_flush")
	case "rt_fs_read_file":
		return true, fe.emitFsReadFile(call)
	case "rt_fs_write_file":
		return true, fe.emitFsWriteFile(call)
	case "rt_fs_file_name":
		return true, fe.emitFsUnary(call, "rt_fs_file_name")
	case "rt_fs_file_type":
		return true, fe.emitFsUnary(call, "rt_fs_file_type")
	case "rt_fs_file_metadata":
		return true, fe.emitFsUnary(call, "rt_fs_file_metadata")
	default:
		return false, nil
	}
}

func (fe *funcEmitter) emitFsOpen(call *mir.CallInstr) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_fs_open requires 2 arguments")
	}
	pathVal, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	flags64, err := fe.emitUintOperandToI64(&call.Args[1], "fs open flags out of range")
	if err != nil {
		return err
	}
	flagsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to i32\n", flagsVal, flags64)
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_fs_open(ptr %s, i32 %s)\n", tmp, pathVal, flagsVal)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitFsRead(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_fs_read requires 3 arguments")
	}
	fileVal, fileTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if fileTy != "ptr" {
		return fmt.Errorf("rt_fs_read expects File handle")
	}
	bufVal, bufTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if bufTy != "ptr" {
		return fmt.Errorf("rt_fs_read expects *byte buffer")
	}
	cap64, err := fe.emitUintOperandToI64(&call.Args[2], "fs read cap out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_fs_read(ptr %s, ptr %s, i64 %s)\n", tmp, fileVal, bufVal, cap64)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitFsWrite(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_fs_write requires 3 arguments")
	}
	fileVal, fileTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if fileTy != "ptr" {
		return fmt.Errorf("rt_fs_write expects File handle")
	}
	bufVal, bufTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if bufTy != "ptr" {
		return fmt.Errorf("rt_fs_write expects *byte buffer")
	}
	len64, err := fe.emitUintOperandToI64(&call.Args[2], "fs write length out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_fs_write(ptr %s, ptr %s, i64 %s)\n", tmp, fileVal, bufVal, len64)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitFsSeek(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_fs_seek requires 3 arguments")
	}
	fileVal, fileTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if fileTy != "ptr" {
		return fmt.Errorf("rt_fs_seek expects File handle")
	}
	offset64, err := fe.emitIntOperandToI64(&call.Args[1], "fs seek offset out of range")
	if err != nil {
		return err
	}
	whence64, err := fe.emitIntOperandToI64(&call.Args[2], "fs seek whence out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_fs_seek(ptr %s, i64 %s, i64 %s)\n", tmp, fileVal, offset64, whence64)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitFsReadFile(call *mir.CallInstr) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("rt_fs_read_file requires 1 argument")
	}
	pathVal, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_fs_read_file(ptr %s)\n", tmp, pathVal)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitFsWriteFile(call *mir.CallInstr) error {
	if len(call.Args) != 4 {
		return fmt.Errorf("rt_fs_write_file requires 4 arguments")
	}
	pathVal, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	bufVal, bufTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if bufTy != "ptr" {
		return fmt.Errorf("rt_fs_write_file expects *byte buffer")
	}
	len64, err := fe.emitUintOperandToI64(&call.Args[2], "fs write length out of range")
	if err != nil {
		return err
	}
	flags64, err := fe.emitUintOperandToI64(&call.Args[3], "fs open flags out of range")
	if err != nil {
		return err
	}
	flagsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to i32\n", flagsVal, flags64)
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_fs_write_file(ptr %s, ptr %s, i64 %s, i32 %s)\n", tmp, pathVal, bufVal, len64, flagsVal)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) emitFsUnary(call *mir.CallInstr, name string) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("%s requires 1 argument", name)
	}
	val, ty, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if ty != "ptr" {
		return fmt.Errorf("%s expects File handle", name)
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(ptr %s)\n", tmp, name, val)
	return fe.storePtrResult(call, tmp)
}

func (fe *funcEmitter) storePtrResult(call *mir.CallInstr, val string) error {
	if !call.HasDst {
		return nil
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, val, ptr)
	return nil
}
