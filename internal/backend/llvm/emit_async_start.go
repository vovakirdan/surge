package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// Starting asynchronous work, and naming the handle it produces.
//
// A spawn hands a poll function to the scheduler and gets a Task back; a
// blocking call hands a body to the blocking pool and gets the same shape back.
// Both produce a runtime-owned handle rather than a value, which is why the two
// helpers here exist: one asks whether a type IS that handle, and the other
// reads the handle word out of an operand.
//
// Everything that WAITS on the work — await, poll, join_all, timeout, select —
// lives beside this, in emit_async.go, along with the suspension terminators.

func isTaskType(typesIn *types.Interner, typeID types.TypeID) bool {
	if typesIn == nil || typeID == types.NoTypeID {
		return false
	}
	typeID = resolveAliasAndOwn(typesIn, typeID)
	tt, ok := typesIn.Lookup(typeID)
	if !ok || tt.Kind != types.KindStruct {
		if info, aliasOK := typesIn.AliasInfo(typeID); aliasOK && info != nil && typesIn.Strings != nil {
			name, nameOK := typesIn.Strings.Lookup(info.Name)
			return nameOK && name == "Task"
		}
		return false
	}
	info, ok := typesIn.StructInfo(typeID)
	if !ok || info == nil || typesIn.Strings == nil {
		return false
	}
	name, ok := typesIn.Strings.Lookup(info.Name)
	return ok && name == "Task"
}

func (fe *funcEmitter) emitTaskHandleOperand(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil task operand")
	}
	val, valTy, typeID, err := fe.emitToSource(op)
	if err != nil {
		return "", err
	}
	if !isTaskType(fe.emitter.types, typeID) {
		return "", fmt.Errorf("expected Task handle, got type#%d", typeID)
	}
	if valTy != "ptr" {
		return "", fmt.Errorf("expected Task pointer, got %s", valTy)
	}
	return val, nil
}

func (fe *funcEmitter) emitInstrSpawn(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	val, err := fe.emitTaskHandleOperand(&ins.Spawn.Value)
	if err != nil {
		return fmt.Errorf("spawn expects Task pointer: %w", err)
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_task_wake(ptr %s)\n", val)
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.Spawn.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, val, ptr, dstAlign)
	return nil
}

func (fe *funcEmitter) emitInstrBlocking(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	stateVal, stateTy, err := fe.emitStructLit(&ins.Blocking.State)
	if err != nil {
		return err
	}
	if stateTy != "ptr" {
		return fmt.Errorf("blocking expects state pointer, got %s", stateTy)
	}
	layout, err := fe.emitter.layoutOf(ins.Blocking.State.TypeID)
	if err != nil {
		return err
	}
	size := layout.Size
	align := layout.Align
	if align <= 0 {
		align = 1
	}
	// The blocking body's RESULT type, so the job and the awaiting task bind
	// the same descriptor: the value moves between their storages rather than
	// being widened into a word on one side and rebuilt on the other.
	resultType := types.NoTypeID
	if body := fe.emitter.mod.Funcs[ins.Blocking.FuncID]; body != nil {
		resultType = resolveValueType(fe.emitter.types, body.Result)
	}
	callTmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call ptr @rt_blocking_submit(i64 %d, ptr %s, i64 %d, i64 %d, i64 %d)\n",
		callTmp,
		ins.Blocking.FuncID,
		stateVal,
		size,
		align,
		resultType)
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.Blocking.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, callTmp, ptr, dstAlign)
	return nil
}
