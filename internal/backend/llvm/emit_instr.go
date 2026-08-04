package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitInstr(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	switch ins.Kind {
	case mir.InstrAssign:
		return fe.emitAssign(ins)
	case mir.InstrCall:
		return fe.emitCall(ins)
	case mir.InstrAwait:
		return fe.emitInstrAwait(ins)
	case mir.InstrSpawn:
		return fe.emitInstrSpawn(ins)
	case mir.InstrCrossing:
		return fe.emitInstrCrossing(ins)
	case mir.InstrBlocking:
		return fe.emitInstrBlocking(ins)
	case mir.InstrPoll:
		return fe.emitInstrPoll(ins)
	case mir.InstrJoinAll:
		return fe.emitInstrJoinAll(ins)
	case mir.InstrChanSend:
		return fe.emitInstrChanSend(ins)
	case mir.InstrChanRecv:
		return fe.emitInstrChanRecv(ins)
	case mir.InstrNetWait:
		return fe.emitInstrNetWait(ins)
	case mir.InstrTimeout:
		return fe.emitInstrTimeout(ins)
	case mir.InstrSelect:
		return fe.emitInstrSelect(ins)
	case mir.InstrDrop:
		return fe.emitInstrDrop(ins)
	case mir.InstrEnvelopeRelease:
		return fe.emitInstrEnvelopeRelease(ins)
	case mir.InstrEndBorrow, mir.InstrNop:
		return nil
	default:
		return fmt.Errorf("unsupported instruction kind %v", ins.Kind)
	}
}

// emitInstrDrop frees the dropped place's owned heap value for the leaf
// types (string, dynamic array) and nulls the slot so a stale handle can
// never free twice. Composite types wait for the recursive drop glue;
// until then their drops stay no-ops, exactly as before.
//
// A PROJECTED place is resolved through the same walk every other instruction
// uses. It used to be refused by `placeBaseType` and the error swallowed just
// below, which made `drop o.inner` a silent no-op — the statement compiled,
// emitted nothing, and freed nothing.
func (fe *funcEmitter) emitInstrDrop(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	baseType, err := fe.droppedPlaceType(ins.Drop.Place)
	if err != nil || baseType == types.NoTypeID {
		return nil
	}
	if ins.Drop.Shallow {
		return fe.emitShallowDrop(ins.Drop.Place, baseType)
	}
	typesIn := fe.emitter.types
	isRefCounted := typesIn.IsRefCountedScalar(resolveValueType(typesIn, baseType))
	isString := isStringLike(typesIn, baseType)
	isFarChan := isFarChannelType(typesIn, baseType)
	elemType, dynamic, isArray := arrayElemType(typesIn, baseType)
	dynArray := isArray && dynamic
	composite := false
	if fe.emitter.isBoxedComposite(baseType) {
		composite = fe.emitter.typeOwnsHeap(baseType)
		if !composite {
			// A boxed composite with no heap-owning field still owns its BOX.
			// Without this every such value leaks one block per construction —
			// `type R = { a: int, b: int }` lost 16 bytes per `R` built. The
			// exception used to be spelled for tag unions only, which is why
			// discarded union temps were reclaimed while plain structs and
			// tuples were not; the box is the same in all three cases and the
			// glue for such a type is a null check plus the free.
			if _, layoutErr := fe.emitter.layoutOf(baseType); layoutErr != nil {
				return layoutErr
			}
			composite = true
		}
	}
	if !isRefCounted && !isString && !dynArray && !composite && !isFarChan {
		return nil
	}
	ptr, ptrTy, err := fe.emitPlacePtr(ins.Drop.Place)
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return nil
	}
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handle, ptr)
	switch {
	case isRefCounted:
		// Giving back this place's reference, not destroying the block: the
		// same block may still be reachable from another binding, and only the
		// last release frees it.
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_bigfloat_release(ptr %s)\n", handle)
	case isString:
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_string_free(ptr %s)\n", handle)
	case isFarChan:
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_far_channel_handle_drop(ptr %s)\n", handle)
	case dynArray:
		fe.emitter.emitDropDynArray(handle, elemType)
	default: // boxed composite that owns heap
		fmt.Fprintf(&fe.emitter.buf, "  call void @%s(ptr %s)\n", fe.emitter.requireDropGlue(baseType), handle)
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", ptr)
	return nil
}

func (fe *funcEmitter) emitAssign(ins *mir.Instr) error {
	if ins.Assign.Src.Kind == mir.RValueArrayLit {
		dstType, err := fe.placeBaseType(ins.Assign.Dst)
		if err != nil {
			return err
		}
		val, ty, err := fe.emitArrayLit(&ins.Assign.Src.ArrayLit, dstType)
		if err != nil {
			return err
		}
		ptr, dstTy, err := fe.emitPlacePtr(ins.Assign.Dst)
		if err != nil {
			return err
		}
		if dstTy != ty {
			ty = dstTy
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
		return nil
	}
	if ins.Assign.Src.Kind == mir.RValueTupleLit {
		dstType, err := fe.placeBaseType(ins.Assign.Dst)
		if err != nil {
			return err
		}
		val, ty, err := fe.emitTupleLit(&ins.Assign.Src.TupleLit, dstType)
		if err != nil {
			return err
		}
		ptr, dstTy, err := fe.emitPlacePtr(ins.Assign.Dst)
		if err != nil {
			return err
		}
		if dstTy != ty {
			ty = dstTy
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
		return nil
	}
	if ins.Assign.Src.Kind == mir.RValueUse && ins.Assign.Src.Use.Kind == mir.OperandConst && ins.Assign.Src.Use.Const.Kind == mir.ConstNothing {
		dstType, err := fe.placeBaseType(ins.Assign.Dst)
		if err == nil && dstType != types.NoTypeID {
			op := ins.Assign.Src.Use
			if op.Type == types.NoTypeID || isNothingType(fe.emitter.types, op.Type) {
				op.Type = dstType
			}
			if op.Const.Type == types.NoTypeID || isNothingType(fe.emitter.types, op.Const.Type) {
				op.Const.Type = dstType
			}
			val, ty, err := fe.emitOperand(&op)
			if err != nil {
				return err
			}
			ptr, dstTy, err := fe.emitPlacePtr(ins.Assign.Dst)
			if err != nil {
				return err
			}
			if dstTy != ty {
				ty = dstTy
			}
			fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
			return nil
		}
	}
	val, ty, err := fe.emitRValue(&ins.Assign.Src)
	if err != nil {
		return err
	}
	ptr, dstTy, err := fe.emitPlacePtr(ins.Assign.Dst)
	if err != nil {
		return err
	}
	if dstTy != ty {
		ty = dstTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
	return nil
}

func (fe *funcEmitter) emitRValue(rv *mir.RValue) (val, ty string, err error) {
	if rv == nil {
		return "", "", fmt.Errorf("nil rvalue")
	}
	switch rv.Kind {
	case mir.RValueUse:
		return fe.emitOperand(&rv.Use)
	case mir.RValueStructLit:
		return fe.emitStructLit(&rv.StructLit)
	case mir.RValueField:
		return fe.emitFieldAccess(&rv.Field)
	case mir.RValueIndex:
		return fe.emitIndexAccess(&rv.Index)
	case mir.RValueUnaryOp:
		return fe.emitUnary(&rv.Unary)
	case mir.RValueBinaryOp:
		return fe.emitBinary(&rv.Binary)
	case mir.RValueCast:
		return fe.emitCast(&rv.Cast)
	case mir.RValueTagTest:
		return fe.emitTagTest(&rv.TagTest)
	case mir.RValueTagPayload:
		return fe.emitTagPayload(&rv.TagPayload)
	case mir.RValueIterInit:
		return fe.emitIterInit(&rv.IterInit)
	case mir.RValueIterNext:
		return fe.emitIterNext(&rv.IterNext)
	case mir.RValueArrayLit, mir.RValueTupleLit:
		return "", "", fmt.Errorf("literal rvalue must be handled in assignment")
	default:
		return "", "", fmt.Errorf("unsupported rvalue kind %v", rv.Kind)
	}
}

// droppedPlaceType resolves the type a drop acts on, projections included.
// `placeBaseType` answers only for a whole local or global by design — it is
// the ENTRY to the projection walk, not a substitute for it.
func (fe *funcEmitter) droppedPlaceType(place mir.Place) (types.TypeID, error) {
	if len(place.Proj) == 0 {
		return fe.placeBaseType(place)
	}
	return fe.projectedPlaceTypeWithTargets(place, nil)
}

// emitShallowDrop frees a place's own box and leaves its contents alone. The
// fields still sitting in it are the ones that moved away, and releasing them
// here would free storage this value no longer owns.
//
// Same shape as the envelope release, which frees a synthesized box the same
// way; the difference is only which box and why.
func (fe *funcEmitter) emitShallowDrop(place mir.Place, baseType types.TypeID) error {
	if !fe.emitter.isBoxedComposite(baseType) {
		// Nothing of its own to free: a leaf's storage IS its value, and a
		// shallow drop of one would be a deep drop by another name.
		return nil
	}
	layoutInfo, layoutErr := fe.emitter.layoutOf(baseType)
	if layoutErr != nil {
		return layoutErr
	}
	size, align := layoutInfo.Size, layoutInfo.Align
	if size <= 0 {
		size = 1
	}
	if align <= 0 {
		align = 1
	}
	ptr, ptrTy, err := fe.emitPlacePtr(place)
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return nil
	}
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handle, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_free(ptr %s, i64 %d, i64 %d)\n", handle, size, align)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", ptr)
	return nil
}
