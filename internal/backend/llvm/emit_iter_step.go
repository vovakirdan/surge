package llvm

import (
	"fmt"

	"surge/internal/symbols"
	"surge/internal/types"
)

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
	// The array keeps its element; the yielded scalar needs its own reference.
	if fe.emitter.types.IsRefCountedScalar(resolveAliasAndOwn(fe.emitter.types, elemType)) {
		fe.emitRetainValue(elemVal, elemOpTy, elemType)
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
