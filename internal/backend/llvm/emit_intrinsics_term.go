package llvm

import (
	"fmt"

	"surge/internal/mir"
)

func (fe *funcEmitter) emitTermNoop(call *mir.CallInstr, name string) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return fmt.Errorf("%s requires 0 arguments", name)
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @%s()\n", name)
	return nil
}

func (fe *funcEmitter) emitTermSetRawMode(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("term_set_raw_mode requires 1 argument")
	}
	val, valTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if valTy != "i1" {
		return fmt.Errorf("term_set_raw_mode expects bool")
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_term_set_raw_mode(i1 %s)\n", val)
	return nil
}

func (fe *funcEmitter) emitTermSize(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return fmt.Errorf("term_size requires 0 arguments")
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_term_size()\n", tmp)
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

func (fe *funcEmitter) emitTermWrite(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("term_write requires 1 argument")
	}
	val, valTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if valTy != "ptr" {
		return fmt.Errorf("term_write expects byte[] handle")
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_term_write(ptr %s)\n", val)
	return nil
}

func (fe *funcEmitter) emitTermReadEvent(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return fmt.Errorf("term_read_event requires 0 arguments")
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_term_read_event()\n", tmp)
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
