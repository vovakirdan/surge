package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

const (
	// A for-loop iterator is a heap struct whose leading word selects the
	// body layout, so a bare Range<T> value and an array cursor can both flow
	// through iter_next typed only as Range<T>. (Array iteration was the only
	// shape the old code understood; a range value fell through and its
	// SurgeRange was misread as an array cursor.)
	iterStructSize  = 32
	iterStructAlign = 8
	iterKindOffset  = 0
	iterKindArray   = 0
	iterKindRange   = 1

	// Array body (kind == iterKindArray).
	arrayIterDataOff   = 8
	arrayIterIndexOff  = 16
	arrayIterLengthOff = 24

	// Range body (kind == iterKindRange): a mutable cursor over int/uint
	// bounds. current and end are tagged int/uint words; inclusive is 0/1.
	rangeIterCurOff  = 8
	rangeIterEndOff  = 16
	rangeIterInclOff = 24

	// SurgeRange source layout (runtime/native/rt.h).
	surgeRangeStartOff     = 0
	surgeRangeEndOff       = 8
	surgeRangeInclusiveOff = 18
)

func (fe *funcEmitter) emitIterInit(init *mir.IterInit) (val, ty string, err error) {
	if init == nil {
		return "", "", fmt.Errorf("nil iter_init")
	}
	iterType := operandValueType(fe.emitter.types, &init.Iterable)
	if iterType == types.NoTypeID && init.Iterable.Kind != mir.OperandConst {
		if baseType, baseErr := fe.placeBaseType(init.Iterable.Place); baseErr == nil {
			iterType = baseType
		}
	}
	if isRangeType(fe.emitter.types, iterType) {
		return fe.emitRangeIterInit(&init.Iterable, iterType)
	}
	if _, dynamic, ok := arrayElemType(fe.emitter.types, iterType); ok {
		return fe.emitArrayIterInit(&init.Iterable, iterType, dynamic)
	}
	return "", "", fmt.Errorf("unsupported iter_init iterable type")
}

// emitRangeIterInit turns a Range<T> value into a mutable cursor over its
// bounds. The bounds are copied out of the SurgeRange (int/uint are Copy, so
// this shares the tagged word, not ownership); the source range keeps its own
// drop. Only arbitrary-precision int/uint ranges (ptr-shaped bounds) take the
// cursor path — any other element shape would need native stepping and is left
// to the caller's existing handling.
func (fe *funcEmitter) emitRangeIterInit(op *mir.Operand, rangeType types.TypeID) (val, ty string, err error) {
	elemType, ok := rangeElemType(fe.emitter.types, rangeType)
	if !ok {
		return "", "", fmt.Errorf("range iter_init requires Range<T> type")
	}
	elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
	if err != nil {
		return "", "", err
	}
	rangePtr, rangeTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", "", err
	}
	if rangeTy != "ptr" || elemLLVM != "ptr" {
		// Non-bignum range element: keep the raw value; iter_next falls back to
		// its array-shaped read (unchanged pre-existing behavior).
		return rangePtr, "ptr", nil
	}

	startPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", startPtr, rangePtr, surgeRangeStartOff)
	startVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", startVal, startPtr)
	endSrc := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", endSrc, rangePtr, surgeRangeEndOff)
	endVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", endVal, endSrc)
	inclSrc := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", inclSrc, rangePtr, surgeRangeInclusiveOff)
	inclByte := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", inclByte, inclSrc)
	inclVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = zext i8 %s to i64\n", inclVal, inclByte)

	iterPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", iterPtr, iterStructSize, iterStructAlign)
	fe.storeIterField(iterPtr, iterKindOffset, "i64", fmt.Sprintf("%d", iterKindRange))
	fe.storeIterField(iterPtr, rangeIterCurOff, "ptr", startVal)
	fe.storeIterField(iterPtr, rangeIterEndOff, "ptr", endVal)
	fe.storeIterField(iterPtr, rangeIterInclOff, "i64", inclVal)
	return iterPtr, "ptr", nil
}

// storeIterField writes one field of an iterator struct at the given offset.
func (fe *funcEmitter) storeIterField(base string, off int, ty, value string) {
	ptr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", ptr, base, off)
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, value, ptr)
}

func (fe *funcEmitter) emitIterNext(next *mir.IterNext) (val, ty string, err error) {
	if next == nil {
		return "", "", fmt.Errorf("nil iter_next")
	}
	iterVal, iterTy, err := fe.emitValueOperand(&next.Iter)
	if err != nil {
		return "", "", err
	}
	if iterTy != "ptr" {
		return "", "", fmt.Errorf("iter_next requires ptr, got %s", iterTy)
	}
	iterType := operandValueType(fe.emitter.types, &next.Iter)
	elemType, ok := rangeElemType(fe.emitter.types, iterType)
	if !ok {
		return "", "", fmt.Errorf("iter_next requires Range<T> type")
	}
	optType, ok := optionTypeForElem(fe.emitter.types, elemType)
	if !ok {
		return "", "", fmt.Errorf("missing Option<T> type for iter_next")
	}
	elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
	if err != nil {
		return "", "", err
	}
	elemSize, elemAlign, err := llvmTypeSizeAlign(elemLLVM)
	if err != nil {
		return "", "", err
	}
	if elemAlign <= 0 {
		elemAlign = 1
	}
	stride := roundUpInt(elemSize, elemAlign)

	someIndex, meta, err := fe.emitter.tagCaseMeta(optType, "Some", symbols.NoSymbolID)
	if err != nil {
		return "", "", err
	}
	if len(meta.PayloadTypes) != 1 {
		return "", "", fmt.Errorf("tag %q expects 1 payload value, got %d", meta.TagName, len(meta.PayloadTypes))
	}
	payloadType := meta.PayloadTypes[0]

	resPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", resPtr)

	// Dispatch on the iterator's leading kind word: a range cursor steps its
	// bounds; an array cursor walks its backing store.
	kindPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", kindPtr, iterVal, iterKindOffset)
	kindVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", kindVal, kindPtr)
	isRange := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i64 %s, %d\n", isRange, kindVal, iterKindRange)
	rangeBB := fe.nextInlineBlock()
	arrayBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isRange, rangeBB, arrayBB)

	// --- range cursor ---
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", rangeBB)
	if stepErr := fe.emitRangeIterStep(iterVal, elemType, optType, someIndex, payloadType, resPtr, contBB); stepErr != nil {
		return "", "", stepErr
	}

	// --- array cursor ---
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", arrayBB)
	idxPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", idxPtr, iterVal, arrayIterIndexOff)
	idxVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", idxVal, idxPtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, iterVal, arrayIterLengthOff)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", lenVal, lenPtr)

	done := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sge i64 %s, %s\n", done, idxVal, lenVal)
	emptyBB := fe.nextInlineBlock()
	nonEmptyBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", done, emptyBB, nonEmptyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", emptyBB)
	nothingVal, err := fe.emitTagValue(optType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return "", "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nothingVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", nonEmptyBB)
	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, iterVal, arrayIterDataOff)
	dataPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)
	offset := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %d\n", offset, idxVal, stride)
	elemPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", elemPtr, dataPtr, offset)
	elemVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", elemVal, elemLLVM, elemPtr)
	nextIdx := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = add i64 %s, 1\n", nextIdx, idxVal)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", nextIdx, idxPtr)

	someVal, err := fe.emitTagValueSinglePayload(optType, someIndex, payloadType, elemVal, elemLLVM, elemType)
	if err != nil {
		return "", "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", someVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	out := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", out, resPtr)
	return out, "ptr", nil
}

// emitRangeIterStep emits the range-cursor body of iter_next: it compares the
// current bound against the end (honoring inclusive), and on a hit yields the
// current value and advances the cursor by one via the tagged int/uint runtime.
func (fe *funcEmitter) emitRangeIterStep(iterVal string, elemType, optType types.TypeID, someIndex int, payloadType types.TypeID, resPtr, contBB string) error {
	cmpFn, addFn, oneFn := "rt_bigint_cmp", "rt_bigint_add", "rt_bigint_from_i64"
	if isBigUintType(fe.emitter.types, elemType) {
		cmpFn, addFn, oneFn = "rt_biguint_cmp", "rt_biguint_add", "rt_biguint_from_u64"
	}

	curPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", curPtr, iterVal, rangeIterCurOff)
	cur := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", cur, curPtr)
	endPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", endPtr, iterVal, rangeIterEndOff)
	end := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", end, endPtr)
	inclPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", inclPtr, iterVal, rangeIterInclOff)
	incl := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", incl, inclPtr)

	cmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i32 @%s(ptr %s, ptr %s)\n", cmp, cmpFn, cur, end)
	inclB := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne i64 %s, 0\n", inclB, incl)
	leCmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sle i32 %s, 0\n", leCmp, cmp)
	ltCmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i32 %s, 0\n", ltCmp, cmp)
	has := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, i1 %s, i1 %s\n", has, inclB, leCmp, ltCmp)

	yieldBB := fe.nextInlineBlock()
	doneBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", has, yieldBB, doneBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", doneBB)
	nothingVal, err := fe.emitTagValue(optType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nothingVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", yieldBB)
	one := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(i64 1)\n", one, oneFn)
	nextCur := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(ptr %s, ptr %s)\n", nextCur, addFn, cur, one)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nextCur, curPtr)
	someVal, err := fe.emitTagValueSinglePayload(optType, someIndex, payloadType, cur, "ptr", elemType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", someVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)
	return nil
}

func (fe *funcEmitter) emitArrayIterInit(op *mir.Operand, arrType types.TypeID, dynamic bool) (val, ty string, err error) {
	if fe.emitter == nil || fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	if op == nil {
		return "", "", fmt.Errorf("nil iterable operand")
	}
	handlePtr, err := fe.emitHandleOperandPtr(op)
	if err != nil {
		return "", "", err
	}

	var dataPtr string
	var lenVal string
	if dynamic {
		lenVal = fe.emitArrayLen(handlePtr)
		head := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", head, handlePtr)
		dataPtrPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, head, arrayDataOffset)
		dataPtr = fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)
	} else {
		head := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", head, handlePtr)
		dataPtr = head
		if _, length, ok := arrayFixedInfo(fe.emitter.types, arrType); ok {
			lenVal = fmt.Sprintf("%d", length)
		} else if tt, ok := fe.emitter.types.Lookup(resolveValueType(fe.emitter.types, arrType)); ok && tt.Kind == types.KindArray && tt.Count != types.ArrayDynamicLength {
			lenVal = fmt.Sprintf("%d", tt.Count)
		} else {
			return "", "", fmt.Errorf("missing fixed array length for iter_init")
		}
	}

	iterPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", iterPtr, iterStructSize, iterStructAlign)

	fe.storeIterField(iterPtr, iterKindOffset, "i64", fmt.Sprintf("%d", iterKindArray))
	fe.storeIterField(iterPtr, arrayIterDataOff, "ptr", dataPtr)
	fe.storeIterField(iterPtr, arrayIterIndexOff, "i64", "0")
	fe.storeIterField(iterPtr, arrayIterLengthOff, "i64", lenVal)

	return iterPtr, "ptr", nil
}

// emitInstrIterRelease frees a for-loop iterator protocol envelope
// (mir.InstrIterRelease), null-guarded the same way emitInstrDrop is:
// load the handle, free it, null the slot so a stale read can never
// free twice (rt_free itself is also null-safe).
//
// Cursor selects a FIXED-size free using the iterator's own runtime
// layout (iterStructSize/iterStructAlign): the place's declared type is
// a type-checking fiction (see the kind-word comment at the top of this
// file) and must never be consulted for size/align here — for a bignum
// element type it would describe Range<T>'s real fields, not this
// opaque cursor's byte layout, and freeing by the wrong shape would
// dereference garbage.
//
// The step (non-cursor) envelope instead uses ITS OWN declared type's
// layout: that type IS the concrete Option<T> the box was allocated
// as, so its layout matches exactly. The free is shallow by
// construction — it must never recurse into the payload, which has
// already moved into the loop binding (a recursive drop here would
// free that value a second time).
func (fe *funcEmitter) emitInstrIterRelease(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	size, align := iterStructSize, iterStructAlign
	if !ins.IterRelease.Cursor {
		baseType, err := fe.placeBaseType(ins.IterRelease.Place)
		if err != nil || baseType == types.NoTypeID {
			return nil
		}
		layoutInfo, layoutErr := fe.emitter.layoutOf(resolveValueType(fe.emitter.types, baseType))
		if layoutErr != nil {
			return nil
		}
		size, align = layoutInfo.Size, layoutInfo.Align
		if size <= 0 {
			size = 1
		}
		if align <= 0 {
			align = 1
		}
	}
	ptr, ptrTy, err := fe.emitPlacePtr(ins.IterRelease.Place)
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

func rangeElemType(typesIn *types.Interner, id types.TypeID) (types.TypeID, bool) {
	if typesIn == nil || typesIn.Strings == nil || id == types.NoTypeID {
		return types.NoTypeID, false
	}
	id = resolveValueType(typesIn, id)
	info, ok := typesIn.StructInfo(id)
	if !ok || info == nil || len(info.TypeArgs) != 1 {
		return types.NoTypeID, false
	}
	if typesIn.Strings.MustLookup(info.Name) != "Range" {
		return types.NoTypeID, false
	}
	return info.TypeArgs[0], true
}

func optionTypeForElem(typesIn *types.Interner, elem types.TypeID) (types.TypeID, bool) {
	if typesIn == nil || typesIn.Strings == nil || elem == types.NoTypeID {
		return types.NoTypeID, false
	}
	optionName := typesIn.Strings.Intern("Option")
	id, ok := typesIn.FindUnionInstance(optionName, []types.TypeID{elem})
	return id, ok
}
