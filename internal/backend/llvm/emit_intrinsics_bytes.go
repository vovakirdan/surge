package llvm

import (
	"fmt"

	"surge/internal/mir"
)

func (fe *funcEmitter) emitByteArrayAppendRange(call *mir.CallInstr) error {
	if len(call.Args) != 4 {
		return fmt.Errorf("rt_byte_array_append_range requires 4 arguments")
	}
	dstSlot, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	srcHead, err := fe.emitByteArrayHandle(&call.Args[1])
	if err != nil {
		return err
	}
	start64, err := fe.emitUintOperandToI64(&call.Args[2], "byte range start out of range")
	if err != nil {
		return err
	}
	len64, err := fe.emitUintOperandToI64(&call.Args[3], "byte range length out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_byte_array_append_range(ptr %s, ptr %s, i64 %s, i64 %s)\n", dstSlot, srcHead, start64, len64)
	return nil
}

func (fe *funcEmitter) emitByteArrayDropPrefix(call *mir.CallInstr) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_byte_array_drop_prefix requires 2 arguments")
	}
	slot, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	count64, err := fe.emitUintOperandToI64(&call.Args[1], "byte drop count out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_byte_array_drop_prefix(ptr %s, i64 %s)\n", slot, count64)
	return nil
}

func (fe *funcEmitter) emitByteParseUint64Token(call *mir.CallInstr) error {
	if len(call.Args) != 5 {
		return fmt.Errorf("rt_byte_parse_uint64_token requires 5 arguments")
	}
	dataHead, err := fe.emitByteArrayHandle(&call.Args[0])
	if err != nil {
		return err
	}
	start64, err := fe.emitUint64BitsOperand(&call.Args[1])
	if err != nil {
		return err
	}
	end64, err := fe.emitUint64BitsOperand(&call.Args[2])
	if err != nil {
		return err
	}
	valuePtr, valueTy, err := fe.emitValueOperand(&call.Args[3])
	if err != nil {
		return err
	}
	if valueTy != "ptr" {
		return fmt.Errorf("rt_byte_parse_uint64_token value out param must be ptr, got %s", valueTy)
	}
	nextPtr, nextTy, err := fe.emitValueOperand(&call.Args[4])
	if err != nil {
		return err
	}
	if nextTy != "ptr" {
		return fmt.Errorf("rt_byte_parse_uint64_token next out param must be ptr, got %s", nextTy)
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_byte_parse_uint64_token(ptr %s, i64 %s, i64 %s, ptr %s, ptr %s)\n", tmp, dataHead, start64, end64, valuePtr, nextPtr)
	if !call.HasDst {
		return nil
	}
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != "i1" {
		dstTy = "i1"
	}
	fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
	return nil
}

func (fe *funcEmitter) emitUint64BitsOperand(op *mir.Operand) (string, error) {
	val, ty, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	if ty == "i64" {
		return val, nil
	}
	return fe.coerceIntToI64(val, ty, operandValueType(fe.emitter.types, op))
}
