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
	// Before anything is written, decide where this instruction came from: a
	// bounds or conversion check emitted anywhere beneath here reports that
	// location, exactly as the VM reports the frame span it sets at the same
	// point (internal/vm/vm.go, setSpanForInstr).
	fe.span.advance(ins)
	// And record it for the backtrace, which asks the same question of an
	// address rather than of an instruction. Only a CHANGE writes a row; see
	// emit_trace_table.go.
	fe.emitTraceLineMarker(fe.span.span)
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
	_, _, isMap := typesIn.MapInfo(baseType)
	elemType, dynamic, isArray := arrayElemType(typesIn, baseType)
	dynArray := isArray && dynamic
	// A `Range` is a handle to a heap object exactly as the four above are, and
	// it was missing from this list rather than deliberately excluded: MIR emits
	// `drop` for `let r = 1..3`, the early return below swallowed it, and every
	// range a program materialised leaked. The for-loop cursor never did, because
	// its envelope release frees the same object through the same helper.
	_, isRange := rangeElemType(typesIn, baseType)
	// A local Task<T> is a handle the runtime refcounts, and a handle the
	// program will never await again -- one an abandoned frame or a container
	// still holds -- keeps its task and the result nobody took alive until
	// this reference is given back. Structured concurrency (SEM3107) means a
	// LIVE handle never reaches an ordinary scope exit; what reaches here is
	// unwinding, and unwinding must not leak.
	isTask := isTaskType(typesIn, baseType)
	if !isRefCounted && !isString && !dynArray && !isFarChan && !isRange && !isTask && !isMap {
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
	case isTask:
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_task_handle_drop(ptr %s)\n", handle)
	case dynArray:
		fe.emitter.emitDropDynArray(handle, elemType)
	case isMap:
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_map_free(ptr %s)\n", handle)
	case isRange:
		fe.emitRangeObjectFree(handle)
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
	return fe.storeIntoPlaceTyped(ins.Assign.Dst, val, ty, rvalueSemanticType(&ins.Assign.Src))
}

// rvalueSemanticType reports the Surge type a produced value has, where the
// rvalue carries one. It answers NoTypeID rather than guessing.
func rvalueSemanticType(rv *mir.RValue) types.TypeID {
	if rv == nil {
		return types.NoTypeID
	}
	if rv.Kind == mir.RValueUse {
		return rv.Use.Type
	}
	return types.NoTypeID
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
	return fe.storeIntoPlaceTyped(place, val, valTy, types.NoTypeID)
}

// storeIntoPlaceTyped is storeIntoPlace with the value's SEMANTIC type, which
// the spellings above cannot carry.
//
// It exists for one case the spellings get wrong: a value whose type is a BARE
// member of a union destination. Both are composites, so the destination's
// spelling wins and the whole union is memcpy'd out of the member — which for a
// handle means copying the union's size out of the pointee and never storing
// the handle at all. A union has to be MATERIALISED there: discriminant first,
// then the member's value at that case's offset.
func (fe *funcEmitter) storeIntoPlaceTyped(place mir.Place, val, valTy string, valueType types.TypeID) error {
	ptr, dstTy, align, err := fe.emitPlaceStorage(place)
	if err != nil {
		return err
	}
	if valueType != types.NoTypeID {
		dstType, terr := fe.placeBaseType(place)
		if terr == nil && dstType != types.NoTypeID {
			materialised, merr := fe.emitUnionMaterialiseBareMember(
				ptr, align, dstType, val, valTy, valueType)
			if merr != nil {
				return merr
			}
			if materialised {
				return nil
			}
			// The other direction: a union read back into one of its bare
			// members, which is what a compare arm's binding is. MIR spells
			// both as a plain move, so the narrowing has to happen here too.
			narrowed, narrowTy, ok, nerr := fe.emitUnionNarrowToBareMember(
				val, align, valueType, dstType)
			if nerr != nil {
				return nerr
			}
			if ok {
				val, valTy = narrowed, narrowTy
			}
		}
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
