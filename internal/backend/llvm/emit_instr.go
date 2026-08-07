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

// emitInstrDrop releases what the dropped place owns.
//
// The two shapes are dropped differently and it is worth saying which is which.
// A leaf handle — a string, a dynamic array, a counted scalar, a far channel —
// has its word read out of the slot, released, and the slot nulled so a stale
// handle can never free twice. A composite has no word: its bytes are the slot,
// so its generated glue is called on the ADDRESS and releases its members in
// place. There is nothing to null afterwards, because nothing was pointing
// anywhere.
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
		// A residual's container has no storage of its own to free any more:
		// its bytes belong to the frame, the enclosing value or the array that
		// declared them, and its live members were dropped one at a time just
		// above. Freeing anything here would be freeing memory this value never
		// owned.
		return nil
	}
	if fe.emitter.hasInlineStorage(baseType) {
		if !fe.emitter.typeOwnsHeap(baseType) {
			return nil
		}
		ptr, _, ptrErr := fe.emitPlacePtr(ins.Drop.Place)
		if ptrErr != nil {
			return ptrErr
		}
		fmt.Fprintf(&fe.emitter.buf, "  call void @%s(ptr %s)\n",
			fe.emitter.requireDropGlue(baseType), ptr)
		return nil
	}
	typesIn := fe.emitter.types
	isRefCounted := typesIn.IsRefCountedScalar(resolveValueType(typesIn, baseType))
	isString := isStringLike(typesIn, baseType)
	isFarChan := isFarChannelType(typesIn, baseType)
	elemType, dynamic, isArray := arrayElemType(typesIn, baseType)
	dynArray := isArray && dynamic
	if !isRefCounted && !isString && !dynArray && !isFarChan {
		return nil
	}
	ptr, ptrTy, align, err := fe.emitPlaceStorage(ins.Drop.Place)
	if err != nil {
		return err
	}
	if ptrTy != handleType {
		return nil
	}
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s, align %d\n", handle, ptr, align)
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
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s, align %d\n", ptr, align)
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
		return fe.storeIntoPlace(ins.Assign.Dst, val, ty)
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
		return fe.storeIntoPlace(ins.Assign.Dst, val, ty)
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
			return fe.storeIntoPlace(ins.Assign.Dst, val, ty)
		}
	}
	val, ty, err := fe.emitRValue(&ins.Assign.Src)
	if err != nil {
		return err
	}
	return fe.storeIntoPlace(ins.Assign.Dst, val, ty)
}

// storeIntoPlace writes a produced value into a destination place.
//
// This is where the two representations part. A scalar or handle is stored into
// its slot. A composite's `val` is the ADDRESS of the storage that holds it —
// a source place, a literal built in a temporary, a clone — so the assignment
// is a byte move into the destination's own storage, at the alignment the
// layout registry published for the type.
//
// The destination's spelling wins when the two disagree, because the
// destination is the storage that must end up holding a well-formed value.
func (fe *funcEmitter) storeIntoPlace(place mir.Place, val, valTy string) error {
	ptr, dstTy, align, err := fe.emitPlaceStorage(place)
	if err != nil {
		return err
	}
	if dstTy != valTy {
		valTy = dstTy
	}
	fe.emitValueStore(valTy, val, ptr, align)
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
