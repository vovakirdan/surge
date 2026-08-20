package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// Array concatenation, which arrives on the magic-binary path wearing the same
// name scalar addition does.
//
// `a + b` on two arrays resolves to `__add`, and the `::<T>` in the
// monomorphised name is the ELEMENT type — `__add::<uint>` on a pair of
// `uint[]`, not on two numbers. So it has to be told apart from arithmetic
// before anything asks whether the operands are numeric, which is why the test
// below runs ahead of canEmitMagicBinary rather than inside it.
//
// The result is a THIRD array. Both operands are borrows, so neither may be
// emptied into it and neither may be left sharing what it handed over: the
// elements are DUPLICATED into a buffer the new header owns outright. That is
// what keeps the drop of the result and the drops of the two sources from
// reaching the same bytes.

// emitArrayConcatIntrinsic handles `__add` when its operands are arrays.
//
// It reports handled=false for anything that is not an array pair, leaving the
// numeric and string paths exactly as they were.
func (fe *funcEmitter) emitArrayConcatIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || len(call.Args) != 2 {
		return false, nil
	}
	elem, isConcat, err := fe.arrayConcatElem(&call.Args[0], &call.Args[1])
	if !isConcat {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if !call.HasDst {
		// Nothing observes the new array, and building one has no effect the
		// program can see: the sources are borrows and come back unchanged.
		return true, nil
	}
	val, err := fe.emitArrayConcatValue(&call.Args[0], &call.Args[1], elem)
	if err != nil {
		return true, err
	}
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return true, err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, val, ptr, dstAlign)
	return true, nil
}

// arrayConcatElem answers whether a `+` on these two operands is array
// concatenation, and with which element type.
//
// isConcat says the operands are arrays and the caller owns the operation; err
// then says the backend cannot lower this particular pair, and the caller must
// not fall through to a path that would answer a different question.
func (fe *funcEmitter) arrayConcatElem(left, right *mir.Operand) (elem types.TypeID, isConcat bool, err error) {
	leftElem, leftDynamic, leftIsArray := fe.operandArrayElem(left)
	rightElem, rightDynamic, rightIsArray := fe.operandArrayElem(right)
	if !leftIsArray && !rightIsArray {
		return types.NoTypeID, false, nil
	}
	if !leftIsArray || !rightIsArray {
		return types.NoTypeID, true, fmt.Errorf("array concatenation needs two arrays")
	}
	if !leftDynamic || !rightDynamic {
		// A fixed array carries its length in its TYPE, and the declared result
		// of concatenating two of them is ArrayFixed<T, N> — one N, for a value
		// that holds 2N elements. Producing the longer buffer would hand back
		// storage every reader of that type walks by the shorter length. The
		// language owes this an answer before the backend can lower it.
		return types.NoTypeID, true, fmt.Errorf("fixed-array concatenation has no native lowering: the result type names one length for a value of two")
	}
	if leftElem != rightElem {
		return types.NoTypeID, true, fmt.Errorf("array concatenation operands have different element types (type#%d, type#%d)", leftElem, rightElem)
	}
	return leftElem, true, nil
}

// emitArrayConcatValue builds the joined array and returns the handle.
//
// The per-element duplication is decided here rather than in the runtime,
// because only the compiler knows what an element owns. An element that owns
// heap gets clone glue; one whose bits are the whole value gets a null function
// pointer and the runtime moves its bytes.
func (fe *funcEmitter) emitArrayConcatValue(left, right *mir.Operand, elem types.TypeID) (string, error) {
	// HANDLE-BACKED by construction: arrayConcatElem refuses a fixed array
	// outright, so both operands are handles over runtime-owned buffers.
	stride, elemAlign, err := fe.emitter.handleArrayElemStrideAlign(elem)
	if err != nil {
		return "", err
	}
	if elemAlign == 0 {
		elemAlign = 1
	}
	if !fe.emitter.canDuplicateValue(elem) {
		// Only a dynamic array element reaches here, and it is refused for the
		// same reason a cloned task handle refuses one: a second owner of that
		// buffer is not something the runtime can make, and copying the handle
		// would give two arrays one buffer to free.
		return "", fmt.Errorf("array concatenation cannot duplicate an element of type#%d", elem)
	}
	leftSlot, err := fe.emitHandleOperandPtr(left)
	if err != nil {
		return "", err
	}
	rightSlot, err := fe.emitHandleOperandPtr(right)
	if err != nil {
		return "", err
	}
	cloneFn := "null"
	if fe.emitter.typeOwnsHeap(elem) {
		cloneFn = "@" + fe.emitter.requireCloneElemGlue(elem)
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call ptr @rt_array_concat(ptr %s, ptr %s, i64 %d, i64 %d, ptr %s)\n",
		tmp, leftSlot, rightSlot, stride, elemAlign, cloneFn)
	return tmp, nil
}

// operandArrayElem reads the element type out of an operand that may be an
// array or a borrow of one, and says whether the array is dynamic.
//
// It looks through the borrow itself, the way emitHandleOperandPtr does: the
// intrinsic is declared taking `&Array<T>`, so every operand that reaches
// concatenation is a reference and asking the reference for its element type
// would answer no every time.
func (fe *funcEmitter) operandArrayElem(op *mir.Operand) (elem types.TypeID, dynamic, ok bool) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil || op == nil {
		return types.NoTypeID, false, false
	}
	opType := operandValueType(fe.emitter.types, op)
	if opType == types.NoTypeID && op.Kind != mir.OperandConst {
		if base, baseErr := fe.placeBaseType(op.Place); baseErr == nil {
			opType = base
		}
	}
	if isRefType(fe.emitter.types, opType) {
		if next, derefOK := derefType(fe.emitter.types, opType); derefOK {
			opType = next
		}
	}
	return arrayElemType(fe.emitter.types, opType)
}
