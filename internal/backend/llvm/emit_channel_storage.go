package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// A channel now takes and gives ADDRESSES, so these are the two shapes every
// channel call site needs: where the value it is sending already lives, and a
// place for the value it is about to receive.
//
// This is what replaces emitValueToI64 / emitI64ToValue on this path. Those
// existed because the runtime entry points carried a machine word, which meant
// a composite had to be boxed on the way in and adopted on the way out. The
// entry points carry storage now, so the box goes with them: a value is moved
// straight from where the caller has it into the cell, at the element's own
// stride.

// emitChannelValueAddress addresses the value an operand names.
//
// An operand that names a place already has an address, and that address is
// what the runtime moves FROM -- no copy, no box. A constant has no storage of
// its own, so one is reserved here; that slot is the caller's staging, and the
// move empties it exactly as it would empty a binding.
func (fe *funcEmitter) emitChannelValueAddress(op *mir.Operand) (string, error) {
	ptr, _, err := fe.emitOperandStorage(op)
	if err != nil {
		return "", err
	}
	return ptr, nil
}

// emitChannelPayloadSlot reserves storage for a value a receive is about to
// produce, sized and aligned for the element itself.
//
// The old spelling was `alloca i64` for every element alike, which is the
// word-shaped ABI in one line: it fits a pointer, so a composite had to be one.
// Asking the type for its storage is what makes a sub-word element take one
// byte and a zero-sized element take none.
func (fe *funcEmitter) emitChannelPayloadSlot(payloadType types.TypeID) (ptr, storageTy string, err error) {
	// Inline-storage types answer with their byte run; everything else answers
	// with its own LLVM type. Asking only the first would refuse a bool, and
	// asking only the second would give a composite a pointer slot again.
	if fe.emitter.hasInlineStorage(payloadType) {
		storageTy, err = fe.emitter.storageTypeOf(payloadType)
	} else {
		storageTy, err = fe.emitter.llvmType(payloadType)
	}
	if err != nil {
		return "", "", fmt.Errorf("channel payload storage: %w", err)
	}
	if storageTy == "void" {
		// A zero-sized element has no bytes to hold, but the runtime still
		// needs a legal address to hand the move: the descriptor's move for
		// such a type writes nothing, and its lifecycle is tracked by the
		// slot's own header. One byte is the whole cost, and it is the
		// caller's frame rather than the channel's ring.
		storageTy = "i8"
	}
	ptr = fe.nextTemp()
	if isStorageRun(storageTy) {
		// A byte run says how many bytes and never how they must be placed, so
		// its alignment comes from the layout registry rather than from its
		// spelling. Taking the default here would give a composite element
		// align 1 and hand the runtime a slot the element's own move may not
		// write to.
		layoutInfo, layoutErr := fe.emitter.layoutOf(payloadType)
		if layoutErr != nil {
			return "", "", fmt.Errorf("channel payload storage: %w", layoutErr)
		}
		fe.emitAllocaAligned(ptr, storageTy, layoutInfo.Align)
		return ptr, storageTy, nil
	}
	if _, err := fe.emitAlloca(ptr, storageTy); err != nil {
		return "", "", err
	}
	return ptr, storageTy, nil
}

// emitChannelPayloadValue reads back what a receive wrote into the slot, in
// whatever form the rest of the emitter expects to hand on -- a loaded scalar,
// or the address itself where the type lives in inline storage.
func (fe *funcEmitter) emitChannelPayloadValue(
	payloadType types.TypeID,
	storageTy, ptr string,
) (val, opTy string, err error) {
	if declared, declErr := fe.emitter.llvmType(payloadType); declErr == nil && declared == "void" {
		// Nothing was stored, so nothing is read. A zero-sized value's carrier
		// is the byte its own value type is spelled as, and its content is not
		// a value at all -- there is nothing to load, and nothing to convert
		// from.
		valueTy, err := fe.emitter.llvmValueType(payloadType)
		if err != nil {
			return "", "", err
		}
		return "0", valueTy, nil
	}
	return fe.emitValueLoad(storageTy, ptr)
}

// channelElementTypeOf names the element of the channel an operand refers to.
//
// A receive needs this before it has anything to receive INTO: the slot is
// sized from the element, not from the Option the caller will end up holding.
func (fe *funcEmitter) channelElementTypeOf(op *mir.Operand) types.TypeID {
	if op == nil {
		return types.NoTypeID
	}
	return channelElemType(fe.emitter.types, resolveValueType(fe.emitter.types, op.Type))
}
