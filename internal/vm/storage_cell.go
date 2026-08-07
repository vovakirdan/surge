package vm

import (
	"fmt"

	"surge/internal/symbols"
)

// A cell is one member of a composite as it sits in the arena: an exact byte
// extent taken from the layout registry, holding the VM's encoding of one
// value. The cell kinds below are the complete set of encodings; a member whose
// type maps to none of them is refused rather than approximated, because a
// guessed encoding would corrupt its neighbours the moment the guess is wrong.
type cellKind uint8

const (
	// cellUnsupported has no encoding and must be refused.
	cellUnsupported cellKind = iota
	// cellZeroSized occupies no bytes at all.
	cellZeroSized
	// cellBool occupies one byte holding 0 or 1.
	cellBool
	// cellSignedInt holds a two's-complement integer in its whole extent.
	cellSignedInt
	// cellUnsignedInt holds an unsigned integer in its whole extent.
	cellUnsignedInt
	// cellTaggedWord holds an unsized integer: either a small value inline or a
	// handle to the arbitrary-precision object that carries it.
	cellTaggedWord
	// cellHandle holds a heap handle: a string, a dynamic array, a map, a range,
	// a float, or a runtime-owned resource.
	cellHandle
	// cellFunc holds the symbol of a function value.
	cellFunc
	// cellRefIndex holds index+1 into the arena's referent table.
	cellRefIndex
	// cellComposite is laid out inline and is walked rather than encoded.
	cellComposite
)

// putBits writes the low bits of a value across an exact extent, little-endian,
// zeroing whatever the value does not reach so a reused extent never keeps a
// byte of the value it replaced.
func putBits(dst []byte, bits uint64) {
	for i := range dst {
		if i < 8 {
			dst[i] = byte((bits >> (8 * i)) & 0xFF)
			continue
		}
		dst[i] = 0
	}
}

// unsignedBits reads an extent as an unsigned integer.
func unsignedBits(src []byte) uint64 {
	bits := uint64(0)
	for i := range src {
		if i >= 8 {
			break
		}
		bits |= uint64(src[i]) << (8 * i)
	}
	return bits
}

// signedBits reads an extent as a two's-complement integer, sign-extending from
// the extent's own width rather than from 64 bits.
func signedBits(src []byte) int64 {
	bits := unsignedBits(src)
	width := len(src) * 8
	if width <= 0 || width >= 64 {
		return int64(bits) //nolint:gosec // a full-width extent is the value itself
	}
	shift := 64 - width
	return int64(bits<<shift) >> shift //nolint:gosec // deliberate sign extension
}

// taggedWordBits encodes an unsized integer. Bit 0 distinguishes the two cases
// so a reader never has to consult anything outside the extent: 1 means the
// remaining bits are the value, 0 means they are a handle. A zeroed extent
// therefore decodes as handle 0, which is the invalid handle, so uninitialized
// storage cannot read back as a plausible number.
func taggedWordBits(v Value) (uint64, error) {
	switch v.Kind {
	case VKInt:
		if v.Int > maxTaggedInt || v.Int < minTaggedInt {
			return 0, fmt.Errorf("storage: %d does not fit an unsized integer cell", v.Int)
		}
		return uint64(v.Int)<<1 | 1, nil //nolint:gosec // range-checked above
	case VKBigInt, VKBigUint:
		return uint64(v.H) << 1, nil
	case VKNothing, VKInvalid:
		// A ZEROED cell decodes as nothing — bit 0 clear means a handle, and
		// handle 0 names no object — so nothing has to encode back to zero or
		// an uninitialized member could be read but not written. Copying a
		// composite that has one is exactly that round trip, and the handle and
		// referent cells already draw the same line.
		return 0, nil
	default:
		return 0, fmt.Errorf("storage: %s is not an unsized integer", v.Kind)
	}
}

const (
	maxTaggedInt = int64(1)<<62 - 1
	minTaggedInt = -(int64(1) << 62)
)

// decodeTaggedWord splits an unsized-integer cell back into its two cases.
func decodeTaggedWord(bits uint64) (value int64, handle Handle, inline bool) {
	if bits&1 == 1 {
		return int64(bits) >> 1, 0, true //nolint:gosec // deliberate sign-preserving untag
	}
	return 0, Handle(bits >> 1), false //nolint:gosec // handles are 32-bit by construction
}

// handleBits encodes a heap handle into a cell.
func handleBits(v Value) (uint64, error) {
	if !v.IsHeap() {
		if v.Kind == VKNothing || v.Kind == VKInvalid {
			return 0, nil
		}
		return 0, fmt.Errorf("storage: %s is not handle-backed", v.Kind)
	}
	return uint64(v.H), nil
}

// funcBits encodes a function value into a cell.
func funcBits(v Value) (uint64, error) {
	if v.Kind != VKFunc {
		return 0, fmt.Errorf("storage: %s is not a function value", v.Kind)
	}
	return uint64(v.Sym), nil
}

func decodeFuncBits(bits uint64) symbols.SymbolID {
	return symbols.SymbolID(bits) //nolint:gosec // symbol ids are 32-bit by construction
}

// minimumCellSize is the extent a cell kind needs to hold its encoding. An
// extent smaller than this is a layout the VM cannot honour, and saying so
// beats writing over the next member.
func minimumCellSize(kind cellKind) uint64 {
	switch kind {
	case cellZeroSized, cellComposite, cellUnsupported:
		return 0
	case cellBool:
		return 1
	case cellHandle, cellFunc, cellRefIndex:
		return 4
	case cellTaggedWord:
		return 8
	default:
		return 1
	}
}

// checkCellExtent refuses an extent that cannot hold its kind's encoding.
func checkCellExtent(kind cellKind, size uint64) error {
	if need := minimumCellSize(kind); size < need {
		return fmt.Errorf("storage: a %s cell needs %d bytes, the layout gives %d", kind, need, size)
	}
	return nil
}

func (k cellKind) String() string {
	switch k {
	case cellZeroSized:
		return "zero-sized"
	case cellBool:
		return "bool"
	case cellSignedInt:
		return "signed integer"
	case cellUnsignedInt:
		return "unsigned integer"
	case cellTaggedWord:
		return "unsized integer"
	case cellHandle:
		return "handle"
	case cellFunc:
		return "function"
	case cellRefIndex:
		return "reference"
	case cellComposite:
		return "composite"
	default:
		return "unsupported"
	}
}
