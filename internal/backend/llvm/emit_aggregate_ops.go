package llvm

import (
	"fmt"
	"strings"

	"surge/internal/types"
)

// The operations a value that lives inline takes part in.
//
// A composite occupies a run of bytes at its own layout. It is never loaded
// into an SSA register — a sixty-four byte value has no register to be loaded
// into — so every operand naming one names the ADDRESS of its storage, and the
// operations below are the only ones that read or write through that address.
//
// Two spellings therefore travel together and must not be confused:
//
//   - the STORAGE type, `[N x i8]`, which an `alloca`, a `byval`, an `sret` and
//     a memcpy length all state;
//   - the OPERAND type, `ptr`, which is what an argument list, a temporary and
//     a returned value carry.
//
// `llvmType` answers the first. `operandTypeFor` answers the second. Every
// emission that moves a value goes through `emitValueStore` and
// `emitValueLoad`, which read the storage spelling and decide between a
// register move and a byte move — so a site that forgot the difference fails to
// emit rather than emitting a `load [64 x i8]` nothing can execute.

// storageRunPrefix and storageRunSuffix bracket the byte-run spelling produced
// by storageTypeForSize.
const (
	storageRunPrefix = "["
	storageRunSuffix = " x i8]"
)

// storageRunSize is the byte count of a storage spelling, or false for a type
// carried in a register.
func storageRunSize(llvmTy string) (uint64, bool) {
	if !strings.HasPrefix(llvmTy, storageRunPrefix) || !strings.HasSuffix(llvmTy, storageRunSuffix) {
		return 0, false
	}
	digits := llvmTy[len(storageRunPrefix) : len(llvmTy)-len(storageRunSuffix)]
	if digits == "" {
		return 0, false
	}
	var size uint64
	for i := range digits {
		ch := digits[i]
		if ch < '0' || ch > '9' {
			return 0, false
		}
		size = size*10 + uint64(ch-'0')
	}
	return size, true
}

// isStorageRun reports whether an LLVM type names inline storage rather than a
// value a register can hold.
func isStorageRun(llvmTy string) bool {
	_, ok := storageRunSize(llvmTy)
	return ok
}

// operandTypeFor is how a value of an LLVM type appears in an argument list, a
// temporary or a return: storage is named by address, everything else by value.
func operandTypeFor(llvmTy string) string {
	if isStorageRun(llvmTy) {
		return handleType
	}
	return llvmTy
}

// emitValueLoad reads the value held at ptr, given the type of that storage.
//
// For inline storage the value IS the address: there is nothing to load, and
// producing a copy here would duplicate a value the caller may only be reading.
// The copy, when one is wanted, is made by emitValueStore at the destination.
func (fe *funcEmitter) emitValueLoad(storageTy, ptr string) (val, opTy string, err error) {
	if isStorageRun(storageTy) {
		return ptr, handleType, nil
	}
	tmp := fe.nextTemp()
	if err := fe.emitLoad(tmp, storageTy, ptr); err != nil {
		return "", "", err
	}
	return tmp, storageTy, nil
}

// emitStorageMemberLoad reads a member out of the storage it sits in, at the
// alignment its address really has.
//
// A member that is itself a composite yields its address: reading a field of a
// value is not copying it out, and a copy — when one is wanted — is made by the
// assignment that receives it.
func (fe *funcEmitter) emitStorageMemberLoad(memberTy, ptr string, align uint64) (val, opTy string, err error) {
	if isStorageRun(memberTy) {
		return ptr, handleType, nil
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s, align %d\n", tmp, memberTy, ptr, align)
	return tmp, memberTy, nil
}

// emitValueStore writes val into the storage at ptr.
//
// `val` is an SSA value for a register type and the address of the source
// storage for a byte run, which is what emitValueLoad and every literal
// emission produce.
func (fe *funcEmitter) emitValueStore(storageTy, val, ptr string, align uint64) {
	size, inline := storageRunSize(storageTy)
	if !inline {
		if align == 0 {
			align = 1
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s, align %d\n", storageTy, val, ptr, align)
		return
	}
	if size == 0 {
		// A zero-sized value has no bytes to move. A zero-length memcpy would
		// be harmless but would also claim two dereferenceable addresses the
		// language never gave it.
		return
	}
	fe.emitStorageCopy(ptr, val, size, align)
}

// emitStorageCopy moves size bytes between two inline storages.
//
// The alignment is the one the layout registry published for the type, stated
// on both ends: a destination and a source of one type are equally aligned, and
// a packed or over-aligned value is exactly the case where LLVM's guess would
// be wrong in opposite directions.
func (fe *funcEmitter) emitStorageCopy(dst, src string, size, align uint64) {
	if size == 0 {
		return
	}
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&fe.emitter.buf,
		"  call void @llvm.memcpy.p0.p0.i64(ptr align %d %s, ptr align %d %s, i64 %d, i1 false)\n",
		align, dst, align, src, size)
}

// emitStorageAlloca reserves one function-scoped storage for a value composite
// and returns its address.
func (fe *funcEmitter) emitStorageAlloca(id types.TypeID) (string, error) {
	facts, err := fe.emitter.storageFactsOf(id)
	if err != nil {
		return "", err
	}
	ptr := fe.nextTemp()
	fe.emitAllocaAligned(ptr, storageTypeForSize(facts.Size), facts.Align)
	return ptr, nil
}

// emitTypedAlloca reserves the storage a value of typeID occupies, spelled
// llvmTy, at the alignment its own authority states.
func (fe *funcEmitter) emitTypedAlloca(name string, typeID types.TypeID, llvmTy string) error {
	align, err := fe.emitter.storageAlignOf(typeID, llvmTy)
	if err != nil {
		return err
	}
	fe.emitAllocaAligned(name, llvmTy, align)
	return nil
}

// storageAlignOf is the alignment every access to a value of this type states,
// or the natural alignment of its register type when it has no inline storage.
func (e *Emitter) storageAlignOf(id types.TypeID, llvmTy string) (uint64, error) {
	if isStorageRun(llvmTy) {
		facts, err := e.storageFactsOf(id)
		if err != nil {
			return 0, err
		}
		return facts.Align, nil
	}
	return naturalAlign(llvmTy)
}
