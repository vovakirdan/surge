package llvm

import (
	"fmt"

	"surge/internal/mir"
)

// join_all: waiting on a scope's children rather than on one task. It asks a
// different question from await and poll -- how did the SET finish, not what
// did one task answer -- and its destination is a flag rather than a value.

func (fe *funcEmitter) emitInstrJoinAll(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	scopeVal, scopeTy, err := fe.emitValueOperand(&ins.JoinAll.Scope)
	if err != nil {
		return err
	}
	if scopeTy != "ptr" {
		return fmt.Errorf("join_all expects scope handle, got %s", scopeTy)
	}
	pendingPtr := fe.nextTemp()
	failfastPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", pendingPtr, alignWord)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i1, align %d\n", failfastPtr, 1)
	doneVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_scope_join_all(ptr %s, ptr %s, ptr %s)\n", doneVal, scopeVal, pendingPtr, failfastPtr)
	readyBB := fmt.Sprintf("bb.inline.join_ready%d", fe.inlineBlock)
	fe.inlineBlock++
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%bb%d\n", doneVal, readyBB, ins.JoinAll.PendBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
	failfastVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i1, ptr %s\n", failfastVal, failfastPtr)
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.JoinAll.Dst)
	if err != nil {
		return err
	}
	if dstTy != "i1" {
		dstTy = "i1"
	}
	fe.emitValueStore(dstTy, failfastVal, ptr, dstAlign)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.JoinAll.ReadyBB)

	fe.blockTerminated = true
	return nil
}
