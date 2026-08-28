package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

const (
	// Every Range<T> handle names an object that opens with runtime/native
	// rt.h's SurgeRange header, and the byte at rangeKindOff says which of the
	// two shapes follows it: `a..b` builds a pair of bounds, while
	// `arr.__range()` and a loop over an array build a cursor over elements.
	// The shared header is what lets one step routine serve both `iter_next`
	// and an explicit `.next()`: the language type Range<T> covers both shapes,
	// so only a runtime byte can tell a receiver apart. This mirrors the VM,
	// where the two are RangeDescriptor and RangeArrayIter on one heap object.
	rangeStartOff     = 0
	rangeEndOff       = 8
	rangeHasStartOff  = 16
	rangeHasEndOff    = 17
	rangeInclusiveOff = 18
	rangeKindOff      = 19

	rangeKindBounds    = 0
	rangeKindArrayIter = 1

	rangeBoundsSize = 24
	rangeAlign      = 8

	// Array cursor body. The data pointer and the element stride occupy the
	// header's two bound slots, which stay unread while has_start and has_end
	// are zero — so a cursor handed to a slice helper reads as an unbounded
	// range instead of having its data pointer decoded as a bound.
	arrayIterDataOff   = rangeStartOff
	arrayIterStrideOff = rangeEndOff
	arrayIterIndexOff  = 24
	arrayIterLengthOff = 32
	arrayIterSize      = 40
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

// emitRangeIterInit gives the loop a Range object of its own to step, so that
// running the loop does not consume the range it was handed. Both shapes are
// duplicated by the same code: the kind byte picks the size, and a shape is
// wholly contained in the bytes that size covers. That is also what makes
// `for x in arr.__range()` work — the loop copies whatever shape it is given
// rather than reading a cursor as if it were a pair of bounds.
//
// The bounds themselves are shared, not cloned: int and uint are Copy, so the
// copy names the same tagged words and owns nothing new.
func (fe *funcEmitter) emitRangeIterInit(op *mir.Operand, rangeType types.TypeID) (val, ty string, err error) {
	if _, ok := rangeElemType(fe.emitter.types, rangeType); !ok {
		return "", "", fmt.Errorf("range iter_init requires Range<T> type")
	}
	rangePtr, rangeTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", "", err
	}
	if rangeTy != "ptr" {
		return "", "", fmt.Errorf("range iter_init requires ptr, got %s", rangeTy)
	}
	// The size is read out of the source object, so this load runs BEFORE the
	// cursor's own guard and could not be protected by it. It does not need to
	// be: every Range object that reaches here comes from a producer that
	// already tested its allocation — the two constructors in the guard file,
	// the call-path test for the open-ended ones, and the two cursors below.
	// A second test here would be a branch no program can take.
	size := fe.emitRangeObjectSize(rangePtr)
	iterPtr := fe.emitCheckedAlloc(allocSiteRangeIter, rangeType, size, rangeAlign)
	fmt.Fprintf(&fe.emitter.buf,
		"  call void @llvm.memcpy.p0.p0.i64(ptr align %d %s, ptr align %d %s, i64 %s, i1 false)\n",
		rangeAlign, iterPtr, rangeAlign, rangePtr, size)
	return iterPtr, "ptr", nil
}

// emitRangeObjectFree releases ONE Range object.
//
// Four callers want exactly this and there used to be one caller and three
// leaks: the for-loop cursor's envelope release, the drop of a Range VALUE a
// binding owns, the release of a range moved into a runtime slice sink, and a
// Range member released by generated drop glue.
//
// The sizing and the null guard live in `rt_range_free` rather than here
// because the glue emitter cannot branch - it writes straight-line calls with
// no block of its own - and because a helper that four sites call should not be
// four copies of a select. See rt.h for why the BOUNDS are not released.
func (fe *funcEmitter) emitRangeObjectFree(handle string) {
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_range_free(ptr %s)\n", handle)
}

// emitRangeObjectSize reads how many bytes a Range object occupies out of its
// own kind byte. Allocation and release both ask this, because the two shapes
// are different sizes and the heap accounting in rt_alloc.c counts the size it
// is told rather than measuring the block.
func (fe *funcEmitter) emitRangeObjectSize(rangePtr string) string {
	kind := fe.emitRangeKind(rangePtr)
	isArrayIter := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, %d\n", isArrayIter, kind, rangeKindArrayIter)
	size := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, i64 %d, i64 %d\n", size, isArrayIter, arrayIterSize, rangeBoundsSize)
	return size
}

// emitRangeKind loads the byte that says which shape a Range object is.
func (fe *funcEmitter) emitRangeKind(rangePtr string) string {
	kindPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", kindPtr, rangePtr, rangeKindOff)
	kind := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", kind, kindPtr)
	return kind
}

// storeIterField writes one field of an iterator struct at the given offset.
func (fe *funcEmitter) storeIterField(base string, off int, ty, value string) {
	ptr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", ptr, base, off)
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, value, ptr)
}

// emitRangeNextIntrinsic emits an explicit `r.next()`. A `for` loop reaches the
// same step through the iter_next instruction, but a written-out call is an
// ordinary call to the `next` intrinsic, which is not a function any module
// defines — left to the direct-call path it would be looked up and not found.
//
// The receiver arrives through a `&mut`, so the argument is the address of the
// slot holding the Range handle, one indirection further out than iter_next's
// by-value cursor: the handle is loaded out before stepping. Nothing is written
// back, because the step mutates the object the handle names — which is exactly
// how the caller's own range advances.
func (fe *funcEmitter) emitRangeNextIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	if stripGenericSuffix(name) != "next" {
		return false, nil
	}
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("next requires 1 argument")
	}
	receiverType := operandValueType(fe.emitter.types, &call.Args[0])
	if call.Args[0].Kind != mir.OperandConst {
		if baseType, baseErr := fe.placeBaseType(call.Args[0].Place); baseErr == nil {
			receiverType = baseType
		}
	}
	if isRefType(fe.emitter.types, receiverType) {
		if pointee, ok := derefType(fe.emitter.types, receiverType); ok {
			receiverType = pointee
		}
	}
	elemType, ok := rangeElemType(fe.emitter.types, receiverType)
	if !ok {
		return false, nil
	}
	rangePtr, err := fe.emitRangeReceiverPtr(&call.Args[0])
	if err != nil {
		return true, err
	}
	res, err := fe.emitRangeStep(rangePtr, elemType)
	if err != nil {
		return true, err
	}
	if !call.HasDst {
		// The step already advanced the range; the answer is a value the caller
		// asked nothing about, and it lives in a stack slot, so dropping it on
		// the floor reclaims nothing and leaks nothing.
		return true, nil
	}
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return true, err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, res, ptr, dstAlign)
	return true, nil
}

// emitRangeReceiverPtr yields the Range handle a `.next()` receiver names. A
// receiver reaches the call as a reference — the address of the slot, or a
// `&mut Range<T>` parameter's own pointer — and one load through it is what the
// VM's handleRangeNext does when it sees VKRef or VKRefMut.
func (fe *funcEmitter) emitRangeReceiverPtr(op *mir.Operand) (string, error) {
	val, ty, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	if ty != "ptr" {
		return "", fmt.Errorf("next requires a ptr receiver, got %s", ty)
	}
	if !fe.operandIsRef(op, op.Type) {
		return val, nil
	}
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handle, val)
	return handle, nil
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
	out, err := fe.emitRangeStep(iterVal, elemType)
	if err != nil {
		return "", "", err
	}
	return out, "ptr", nil
}

// emitRangeStep advances one Range object by one element and yields the
// Option<T> that answers for it, dispatching on the object's kind byte. It is
// the whole of `iter_next` and the whole of an explicit `.next()`: the VM
// answers both from one handleRangeNext for the same reason, that the two are
// the same question asked of the same object.
//
// The step mutates the object the handle names, never the handle, so a caller
// that keeps a Range sees it advance without anything being written back to the
// slot it holds.
func (fe *funcEmitter) emitRangeStep(rangePtr string, elemType types.TypeID) (string, error) {
	optType, ok := optionTypeForElem(fe.emitter.types, elemType)
	if !ok {
		return "", fmt.Errorf("missing Option<T> type for a range step")
	}
	elemLLVM, err := fe.emitter.llvmValueType(elemType)
	if err != nil {
		return "", err
	}
	someIndex, meta, err := fe.emitter.tagCaseMeta(optType, "Some", symbols.NoSymbolID)
	if err != nil {
		return "", err
	}
	if len(meta.PayloadTypes) != 1 {
		return "", fmt.Errorf("tag %q expects 1 payload value, got %d", meta.TagName, len(meta.PayloadTypes))
	}
	payloadType := meta.PayloadTypes[0]

	// The slot that carries one arm's answer to the join. It goes to the entry
	// block through emitAllocaAligned rather than being written here, because
	// stepping is what a loop body does on every pass and an alloca in a loop
	// body grows the frame once per iteration.
	resPtr := fe.nextTemp()
	fe.emitAllocaAligned(resPtr, "ptr", alignPtr)

	kind := fe.emitRangeKind(rangePtr)
	isArrayIter := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, %d\n", isArrayIter, kind, rangeKindArrayIter)
	arrayBB := fe.nextInlineBlock()
	boundsBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isArrayIter, arrayBB, boundsBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", boundsBB)
	if stepErr := fe.emitRangeBoundsStep(rangePtr, elemType, optType, someIndex, payloadType, resPtr, contBB); stepErr != nil {
		return "", stepErr
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", arrayBB)
	idxPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", idxPtr, rangePtr, arrayIterIndexOff)
	idxVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", idxVal, idxPtr)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, rangePtr, arrayIterLengthOff)
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", lenVal, lenPtr)

	done := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sge i64 %s, %s\n", done, idxVal, lenVal)
	emptyBB := fe.nextInlineBlock()
	nonEmptyBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", done, emptyBB, nonEmptyBB)

	// An exhausted cursor keeps answering nothing: its index stays put, so a
	// second `.next()` reaches this arm again rather than reading past the end.
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", emptyBB)
	nothingVal, err := fe.emitTagValue(optType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nothingVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", nonEmptyBB)
	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, rangePtr, arrayIterDataOff)
	dataPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", dataPtr, dataPtrPtr)
	stridePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", stridePtr, rangePtr, arrayIterStrideOff)
	strideVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", strideVal, stridePtr)
	offset := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = mul i64 %s, %s\n", offset, idxVal, strideVal)
	elemPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n", elemPtr, dataPtr, offset)
	// OPAQUE BASE: the element address is built from a data word read out of
	// the cursor descriptor, which carries a length and a stride but no
	// alignment. For a fixed array that word is the array's own address, so a
	// `@packed` container's array reaches here — see
	// opaqueBaseElemStrideAlign.
	_, elemAlign, err := fe.emitter.opaqueBaseElemStrideAlign(elemType)
	if err != nil {
		return "", err
	}
	elemVal, elemOpTy, err := fe.emitStorageMemberLoad(elemLLVM, elemPtr, elemAlign)
	if err != nil {
		return "", err
	}
	nextIdx := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = add i64 %s, 1\n", nextIdx, idxVal)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", nextIdx, idxPtr)

	someVal, err := fe.emitTagValueSinglePayload(optType, someIndex, payloadType, elemVal, elemOpTy, elemType)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", someVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	out := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", out, resPtr)
	return out, nil
}

// emitRangeBoundsStep emits the bounds-descriptor arm of a range step: it
// compares the current bound against the end (honoring inclusive), and on a hit
// yields the current value and advances the bound by one via the tagged int or
// uint runtime.
//
// A range with no start begins at zero and records that it now has one, and a
// range with no end never runs out — both are what the VM's
// rangeDescriptorNextValue does, and the second is why the end comparison sits
// behind a branch rather than a select: the comparison helper would be handed a
// null bound on a range that never had one.
func (fe *funcEmitter) emitRangeBoundsStep(rangePtr string, elemType, optType types.TypeID, someIndex int, payloadType types.TypeID, resPtr, contBB string) error {
	cmpFn, addFn, oneFn := "rt_bigint_cmp", "rt_bigint_add", "rt_bigint_from_i64"
	if isBigUintType(fe.emitter.types, elemType) {
		cmpFn, addFn, oneFn = "rt_biguint_cmp", "rt_biguint_add", "rt_biguint_from_u64"
	}

	curPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", curPtr, rangePtr, rangeStartOff)
	startVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", startVal, curPtr)
	hasStartPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", hasStartPtr, rangePtr, rangeHasStartOff)
	hasStart := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", hasStart, hasStartPtr)
	hasStartB := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne i8 %s, 0\n", hasStartB, hasStart)
	zero := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(i64 0)\n", zero, oneFn)
	cur := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, ptr %s, ptr %s\n", cur, hasStartB, startVal, zero)
	// Writing the defaulted start back before the end is consulted is what
	// turns an open-ended range into a walked one, exactly as the VM does it.
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", cur, curPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i8 1, ptr %s\n", hasStartPtr)

	hasEndPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", hasEndPtr, rangePtr, rangeHasEndOff)
	hasEnd := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", hasEnd, hasEndPtr)
	hasEndB := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne i8 %s, 0\n", hasEndB, hasEnd)

	boundedBB := fe.nextInlineBlock()
	yieldBB := fe.nextInlineBlock()
	doneBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", hasEndB, boundedBB, yieldBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", boundedBB)
	endPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", endPtr, rangePtr, rangeEndOff)
	end := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", end, endPtr)
	inclPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", inclPtr, rangePtr, rangeInclusiveOff)
	incl := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", incl, inclPtr)
	cmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i32 @%s(ptr %s, ptr %s)\n", cmp, cmpFn, cur, end)
	inclB := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne i8 %s, 0\n", inclB, incl)
	leCmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sle i32 %s, 0\n", leCmp, cmp)
	ltCmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i32 %s, 0\n", ltCmp, cmp)
	has := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, i1 %s, i1 %s\n", has, inclB, leCmp, ltCmp)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", has, yieldBB, doneBB)

	// An exhausted range keeps answering nothing: the start it stopped at is
	// still past the end, so asking again lands here again.
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
	someVal, err := fe.emitTagValueSinglePayload(optType, someIndex, payloadType, cur, handleType, elemType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", someVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)
	return nil
}

func arrayIterStride(stride, fixedLength uint64, dynamic bool) (uint64, error) {
	// A zero-length cursor never reaches element addressing, and a one-element
	// cursor can only address offset zero. Recording stride zero is exact for
	// both. For length >= 2, a stride larger than MaxInt64 cannot survive the
	// fixed layout's checked stride*length on a 64-bit target, and would not
	// survive being named as a signed word here either; keep the guard
	// defensive for malformed or incomplete layout registries.
	if !dynamic && fixedLength <= 1 {
		return 0, nil
	}
	if stride > (^uint64(0) >> 1) {
		return 0, fmt.Errorf("array iterator stride is too large")
	}
	return stride, nil
}

func (fe *funcEmitter) emitArrayIterInit(op *mir.Operand, arrType types.TypeID, dynamic bool) (val, ty string, err error) {
	if fe.emitter == nil || fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	if op == nil {
		return "", "", fmt.Errorf("nil iterable operand")
	}
	elemType, actualDynamic, ok := arrayElemType(fe.emitter.types, arrType)
	if !ok || actualDynamic != dynamic {
		return "", "", fmt.Errorf("array iter_init has inconsistent array type")
	}
	// One stride for both shapes. A dynamic array used to step by a pointer
	// while a fixed one stepped by the element's own size; both hold the
	// elements themselves now, so both step by what the layout registry froze.
	stride, err := fe.emitter.arrayElemStride(elemType)
	if err != nil {
		return "", "", err
	}
	var fixedLength uint64
	if !dynamic {
		if _, length, ok := arrayFixedInfo(fe.emitter.types, arrType); ok {
			fixedLength = uint64(length)
		} else if tt, ok := fe.emitter.types.Lookup(resolveValueType(fe.emitter.types, arrType)); ok && tt.Kind == types.KindArray && tt.Count != types.ArrayDynamicLength {
			fixedLength = uint64(tt.Count)
		} else {
			return "", "", fmt.Errorf("missing fixed array length for iter_init")
		}
	}
	strideVal, err := arrayIterStride(stride, fixedLength, dynamic)
	if err != nil {
		return "", "", err
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
		// A fixed array has no header to indirect through: the operand address
		// IS the element buffer, the same address element indexing walks.
		dataPtr = handlePtr
		lenVal = fmt.Sprintf("%d", fixedLength)
	}

	// A refused cursor names the ARRAY being walked rather than the cursor
	// shape: Range<T> is the runtime handle, and the array is what the reader
	// wrote down.
	iterPtr := fe.emitCheckedAlloc(allocSiteArrayIter, arrType, fmt.Sprintf("%d", arrayIterSize), rangeAlign)

	// The cursor opens with a descriptor header saying what it is and that it
	// has no bounds. Both flags matter: they name the shape for the step
	// routine, and they keep the slice helpers in the C runtime away from the
	// data pointer and stride the cursor keeps in the two bound slots.
	fe.storeIterField(iterPtr, arrayIterDataOff, "ptr", dataPtr)
	fe.storeIterField(iterPtr, arrayIterStrideOff, "i64", fmt.Sprintf("%d", strideVal))
	fe.storeIterField(iterPtr, rangeHasStartOff, "i8", "0")
	fe.storeIterField(iterPtr, rangeHasEndOff, "i8", "0")
	fe.storeIterField(iterPtr, rangeInclusiveOff, "i8", "0")
	fe.storeIterField(iterPtr, rangeKindOff, "i8", fmt.Sprintf("%d", rangeKindArrayIter))
	fe.storeIterField(iterPtr, arrayIterIndexOff, "i64", "0")
	fe.storeIterField(iterPtr, arrayIterLengthOff, "i64", lenVal)

	return iterPtr, "ptr", nil
}

// emitInstrEnvelopeRelease frees a synthesized-after-sema heap box
// (mir.InstrEnvelopeRelease) — a for-loop iterator protocol envelope
// (step box or cursor) or a `compare` expression's boxed-union
// scrutinee — null-guarded the same way emitInstrDrop is: load the
// handle, free it, null the slot so a stale read can never free twice
// (rt_free itself is also null-safe).
//
// Cursor asks the CURSOR ITSELF for its size, through the kind byte the
// two Range shapes share (see the comment at the top of this file). The
// place's declared type must never be consulted for size/align here:
// Range<T> is a runtime handle type, so its layout is the pointer, not
// the object the pointer names — and freeing by the wrong size
// mis-reports the heap accounting rt_alloc.c keeps. Only the for-loop
// cursor ever sets Cursor=true, and asking the object costs a branch
// because a released slot is nulled and a second release must not read a
// kind byte out of a null handle.
//
// The non-cursor case instead uses the place's OWN declared type's
// layout: for a for-loop step, that type IS the concrete Option<T> the
// box was allocated as; for a compare scrutinee, it IS the concrete
// union type the scrutinee temp was allocated as. Either way the
// layout matches the real allocation exactly, and the free is shallow
// by construction — it must never recurse into the payload, which has
// already moved into a binding elsewhere (a recursive drop here would
// free that value a second time).
func (fe *funcEmitter) emitInstrEnvelopeRelease(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	var size, align uint64
	if !ins.EnvelopeRelease.Cursor {
		baseType, err := fe.placeBaseType(ins.EnvelopeRelease.Place)
		if err != nil {
			return err
		}
		if baseType == types.NoTypeID {
			return fmt.Errorf("envelope release has no physical type")
		}
		layoutInfo, layoutErr := fe.emitter.layoutOf(baseType)
		if layoutErr != nil {
			return layoutErr
		}
		size, align = layoutInfo.Size, layoutInfo.Align
		if size <= 0 {
			size = 1
		}
		if align <= 0 {
			align = 1
		}
	}
	ptr, ptrTy, err := fe.emitPlacePtr(ins.EnvelopeRelease.Place)
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return nil
	}
	handle := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", handle, ptr)
	if ins.EnvelopeRelease.Cursor {
		fe.emitRangeObjectFree(handle)
	} else {
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_free(ptr %s, i64 %d, i64 %d)\n", handle, size, align)
	}
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
