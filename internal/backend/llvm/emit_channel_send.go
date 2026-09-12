package llvm

import (
	"fmt"

	"surge/internal/mir"
)

func (fe *funcEmitter) emitInstrChanSend(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	chVal, err := fe.emitChannelHandle(&ins.ChanSend.Channel)
	if err != nil {
		return err
	}
	srcPtr, err := fe.emitChannelSendPollSource(&ins.ChanSend.Value)
	if err != nil {
		return err
	}
	callee := "rt_channel_send"
	if ins.ChanSend.YieldAfterHandoff {
		callee = "rt_channel_send_yield"
	}
	if ins.ChanSend.Value.Kind == mir.OperandRetain {
		callee += "_offer"
	}
	okVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @%s(ptr %s, ptr %s)\n", okVal, callee, chVal, srcPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb%d\n", okVal, ins.ChanSend.ReadyBB, ins.ChanSend.PendBB)
	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitChannelSendPollSource(op *mir.Operand) (string, error) {
	if op.Kind != mir.OperandRetain {
		return fe.emitChannelValueAddress(op)
	}
	// Evaluate once: OperandRetain both loads the original and acquires the
	// reference this poll offers. The callback may clear its source, so it must
	// receive a disposable word, never the live binding or a packed field.
	value, llvmTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	slot := fe.nextTemp()
	align, err := fe.emitAlloca(slot, llvmTy)
	if err != nil {
		return "", err
	}
	// emitAlloca hoists the slot to entry; only the retained value and store
	// are repeated by the poll loop. The runtime takes or drops it this call.
	fe.emitValueStore(llvmTy, value, slot, align)
	return slot, nil
}
