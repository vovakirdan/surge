package llvm

import (
	"fmt"
)

// Memory operations carry their alignment explicitly.
//
// LLVM is willing to guess. An `alloca` with no alignment takes the preferred
// alignment of what it holds, and a `load` or `store` with none takes the
// preferred alignment of the type it moves. Those guesses are right exactly as
// long as the memory holds one naturally-aligned scalar, which is what every
// slot on this path holds today.
//
// They stop being right the moment a value is stored inline at its own layout.
// An `alloca [N x i8]` with no alignment canonicalizes to align 1, while an
// unattributed `store i64` through it still claims align 8 — so the two halves
// of one access disagree, and the disagreement is invisible until a target that
// cannot fix up a misaligned access runs the code. A packed field is the same
// hazard read from the other side: its address is genuinely not aligned to its
// own type, and an access that does not say so promises something false.
//
// Saying it always is what makes the guarantee checkable. An assertion that
// every access through storage carries an alignment can be enforced; one that
// says "carries an alignment, unless the guess happens to be right" cannot.

// alignPtr and alignWord are the alignments of the two LLVM types this backend
// names literally — a handle and a machine word. They are the same facts
// naturalAlign returns for "ptr" and "i64", written as constants at the sites
// where the type is itself a constant so that an emission whose type is already
// fixed cannot fail on it. TestLiteralAlignmentsMatchTheirTypes keeps the two
// spellings from drifting.
const (
	alignPtr  = 8
	alignWord = 8
)

// emitAlloca reserves storage for one value of an LLVM type.
func (fe *funcEmitter) emitAlloca(name, llvmTy string) error {
	align, err := naturalAlign(llvmTy)
	if err != nil {
		return err
	}
	fe.emitAllocaAligned(name, llvmTy, align)
	return nil
}

// emitAllocaAligned reserves storage whose alignment the caller already read
// from the layout registry, which is the only source for a byte run's.
//
// Every alloca lands in the entry block, wherever the emission asked for it.
// An alloca is reclaimed when the function returns, not when its block ends, so
// one written inside a loop body grows the frame once per iteration — and a
// composite temporary is exactly what a loop that constructs values asks for on
// every pass. Hoisting is always sound because an alloca reads nothing: its
// type and alignment are constants by the time it is emitted.
func (fe *funcEmitter) emitAllocaAligned(name, llvmTy string, align uint64) {
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&fe.entryAllocas, "  %s = alloca %s, align %d\n", name, llvmTy, align)
}

// emitLoad reads one value of an LLVM type from storage.
func (fe *funcEmitter) emitLoad(dst, llvmTy, ptr string) error {
	align, err := naturalAlign(llvmTy)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s, align %d\n", dst, llvmTy, ptr, align)
	return nil
}

// emitStore writes one value of an LLVM type into storage.
func (fe *funcEmitter) emitStore(llvmTy, val, ptr string) error {
	align, err := naturalAlign(llvmTy)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s, align %d\n", llvmTy, val, ptr, align)
	return nil
}

// naturalAlign is the alignment of memory holding exactly one value of an LLVM
// type. An unknown type is refused rather than defaulted, because a guessed
// alignment is the thing this file exists to remove.
//
// Inline storage has no natural alignment to read off its spelling: `[N x i8]`
// says how many bytes, never how they must be placed, and the answer lives in
// the layout registry. Refusing it here is what keeps a byte run from taking
// align 1 by default at a site that never asked the registry.
func naturalAlign(llvmTy string) (uint64, error) {
	if isStorageRun(llvmTy) {
		return 0, fmt.Errorf(
			"inline storage %s has no alignment of its own; read it from the layout registry", llvmTy)
	}
	_, align, err := llvmTypeStrideAlign(llvmTy)
	if err != nil {
		return 0, err
	}
	if align == 0 {
		return 0, fmt.Errorf("llvm type %s has no alignment", llvmTy)
	}
	return align, nil
}
