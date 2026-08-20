package llvm

import (
	"fmt"

	"surge/internal/mir"
)

// The five intrinsics that move or reserve raw bytes.
//
// They are grouped because they share an obligation the other intrinsics do
// not have: each one takes a size and an alignment that the caller computed,
// and passing a wrong pair here is not a type error anywhere -- it is a
// mis-sized heap block that surfaces much later as a corrupt read.

func (fe *funcEmitter) emitRtAlloc(call *mir.CallInstr) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_alloc requires 2 arguments")
	}
	size64, err := fe.emitUintOperandToI64(&call.Args[0], "alloc size out of range")
	if err != nil {
		return err
	}
	align64, err := fe.emitUintOperandToI64(&call.Args[1], "alloc align out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %s, i64 %s)\n", tmp, size64, align64)
	if call.HasDst {
		ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
		if err != nil {
			return err
		}
		if !isStorageRun(dstTy) {
			dstTy = handleType
		}
		fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
		return nil
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_free(ptr %s, i64 %s, i64 %s)\n", tmp, size64, align64)
	return nil
}

func (fe *funcEmitter) emitRtFree(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_free requires 3 arguments")
	}
	ptrVal, ptrTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return fmt.Errorf("rt_free expects *byte pointer")
	}
	size64, err := fe.emitUintOperandToI64(&call.Args[1], "free size out of range")
	if err != nil {
		return err
	}
	align64, err := fe.emitUintOperandToI64(&call.Args[2], "free align out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_free(ptr %s, i64 %s, i64 %s)\n", ptrVal, size64, align64)
	return nil
}

func (fe *funcEmitter) emitRtRealloc(call *mir.CallInstr) error {
	if len(call.Args) != 4 {
		return fmt.Errorf("rt_realloc requires 4 arguments")
	}
	ptrVal, ptrTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return fmt.Errorf("rt_realloc expects *byte pointer")
	}
	oldSize64, err := fe.emitUintOperandToI64(&call.Args[1], "old size out of range")
	if err != nil {
		return err
	}
	newSize64, err := fe.emitUintOperandToI64(&call.Args[2], "new size out of range")
	if err != nil {
		return err
	}
	align64, err := fe.emitUintOperandToI64(&call.Args[3], "realloc align out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_realloc(ptr %s, i64 %s, i64 %s, i64 %s)\n", tmp, ptrVal, oldSize64, newSize64, align64)
	if call.HasDst {
		ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
		if err != nil {
			return err
		}
		if !isStorageRun(dstTy) {
			dstTy = handleType
		}
		fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
		return nil
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_free(ptr %s, i64 %s, i64 %s)\n", tmp, newSize64, align64)
	return nil
}

func (fe *funcEmitter) emitRtMemcpy(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_memcpy requires 3 arguments")
	}
	dstVal, dstTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		return fmt.Errorf("rt_memcpy expects *byte destination")
	}
	srcVal, srcTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if srcTy != "ptr" {
		return fmt.Errorf("rt_memcpy expects *byte source")
	}
	n64, err := fe.emitUintOperandToI64(&call.Args[2], "memcpy length out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_memcpy(ptr %s, ptr %s, i64 %s)\n", dstVal, srcVal, n64)
	return nil
}

func (fe *funcEmitter) emitRtMemmove(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_memmove requires 3 arguments")
	}
	dstVal, dstTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		return fmt.Errorf("rt_memmove expects *byte destination")
	}
	srcVal, srcTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if srcTy != "ptr" {
		return fmt.Errorf("rt_memmove expects *byte source")
	}
	n64, err := fe.emitUintOperandToI64(&call.Args[2], "memmove length out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_memmove(ptr %s, ptr %s, i64 %s)\n", dstVal, srcVal, n64)
	return nil
}
