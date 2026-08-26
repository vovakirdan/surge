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

// blockingTypeID names one of a blocking submission's two types as the number
// the runtime turns back into that type's DESCRIPTOR.
//
// It refuses a type the operation census never saw, because the runtime asks
// for the descriptor by id and a type without one answers the opaque word:
// eight bytes, no drop, no move. A state would then be freed at the word's
// width with its captures still inside it, and a result wider than a word
// would overflow the cell the job reserved for it. Both refusals are here,
// where the type is still legible, rather than in a runtime that has only a
// number.
//
// A type with no bytes is not a refusal: a body that answers `nothing` has
// nothing to describe, and the id it carries today is what it keeps.
func (fe *funcEmitter) blockingTypeID(role string, id types.TypeID) (types.TypeID, error) {
	resolved := resolveValueType(fe.emitter.types, id)
	if resolved == types.NoTypeID {
		return types.NoTypeID, nil
	}
	if llvmTy, err := fe.emitter.llvmType(resolved); err == nil && llvmTy == "void" {
		return resolved, nil
	}
	if !fe.emitter.valueOpsRegistryHas(resolved) {
		return types.NoTypeID, fmt.Errorf(
			"llvm: a blocking submission names a %s of type#%d with no operation descriptor, so "+
				"the runtime would carry it as the opaque word -- eight bytes, no drop -- and "+
				"free or publish it at that width; note: every registry type gets a descriptor, "+
				"so this type never reached the operation census",
			role, resolved)
	}
	return resolved, nil
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
	// The captures' own type, so the job destroys them through their
	// descriptor. It used to be a size and an alignment, which can free the
	// block and nothing inside it.
	stateType, err := fe.blockingTypeID("captured state", ins.Blocking.State.TypeID)
	if err != nil {
		return err
	}
	// The blocking body's RESULT type, so the job and the awaiting task bind
	// the same descriptor: the value moves between their storages rather than
	// being widened into a word on one side and rebuilt on the other.
	resultType := types.NoTypeID
	if body := fe.emitter.mod.Funcs[ins.Blocking.FuncID]; body != nil {
		resultType, err = fe.blockingTypeID("result", body.Result)
		if err != nil {
			return err
		}
	}
	callTmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call ptr @rt_blocking_submit(i64 %d, ptr %s, i64 %d, i64 %d)\n",
		callTmp,
		ins.Blocking.FuncID,
		stateVal,
		stateType,
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
