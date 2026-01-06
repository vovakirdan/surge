package bignum

import (
	"errors"
	"math/bits"
)

// MaxLimbs is the maximum number of limbs allowed.
const MaxLimbs = 1_000_000

var (
	// ErrMaxLimbs indicates the numeric size limit was exceeded.
	ErrMaxLimbs = errors.New("numeric size limit exceeded")
	// ErrDivByZero indicates an attempt to divide by zero.
	ErrDivByZero = errors.New("division by zero")
	ErrUnderflow = errors.New("unsigned underflow")
)

// BigUint represents a big unsigned integer.
type BigUint struct {
	// Limbs are base-2^32 little-endian (Limbs[0] is least significant).
	//
	// Canonical zero is represented as nil/empty slice.
	Limbs []uint32
}

// UintZero returns a zero BigUint.
func UintZero() BigUint { return BigUint{} }

// UintFromUint64 creates a BigUint from a uint64.
func UintFromUint64(v uint64) BigUint {
	if v == 0 {
		return BigUint{}
	}
	lo := uint32(v)       //nolint:gosec // G115: truncation is intentional (low limb).
	hi := uint32(v >> 32) //nolint:gosec // G115: truncation is intentional (high limb).
	if hi == 0 {
		return BigUint{Limbs: []uint32{lo}}
	}
	return BigUint{Limbs: []uint32{lo, hi}}
}

// UintFromUint32 creates a BigUint from a uint32.
func UintFromUint32(v uint32) BigUint {
	if v == 0 {
		return BigUint{}
	}
	return BigUint{Limbs: []uint32{v}}
}

// IsZero reports whether the unsigned integer is zero.
func (u BigUint) IsZero() bool {
	return len(trimLimbs(u.Limbs)) == 0
}

// IsOdd reports whether the unsigned integer is odd.
func (u BigUint) IsOdd() bool {
	limbs := trimLimbs(u.Limbs)
	return len(limbs) > 0 && (limbs[0]&1) == 1
}

func (u BigUint) BitLen() int {
	return bitLenLimbs(u.Limbs)
}

// TrailingZeros returns the number of trailing zero bits.
func (u BigUint) TrailingZeros() int {
	limbs := trimLimbs(u.Limbs)
	if len(limbs) == 0 {
		return 0
	}
	n := 0
	for _, limb := range limbs {
		if limb == 0 {
			n += 32
			continue
		}
		n += bits.TrailingZeros32(limb)
		break
	}
	return n
}

// Cmp compares two BigUint values.
func (u BigUint) Cmp(v BigUint) int {
	return cmpLimbs(u.Limbs, v.Limbs)
}

// Uint64 converts BigUint to uint64 if possible.
func (u BigUint) Uint64() (uint64, bool) {
	limbs := trimLimbs(u.Limbs)
	switch len(limbs) {
	case 0:
		return 0, true
	case 1:
		return uint64(limbs[0]), true
	case 2:
		return uint64(limbs[0]) | (uint64(limbs[1]) << 32), true
	default:
		return 0, false
	}
}

// UintAdd adds two BigUint values and returns the result.
func UintAdd(a, b BigUint) (BigUint, error) {
	al := trimLimbs(a.Limbs)
	bl := trimLimbs(b.Limbs)
	n := len(al)
	if len(bl) > n {
		n = len(bl)
	}
	if n == 0 {
		return BigUint{}, nil
	}

	out := make([]uint32, n+1)
	var carry uint64
	for i := range n {
		var av, bv uint64
		if i < len(al) {
			av = uint64(al[i])
		}
		if i < len(bl) {
			bv = uint64(bl[i])
		}
		sum := av + bv + carry
		out[i] = uint32(sum) //nolint:gosec // G115: truncation is intentional (limb arithmetic).
		carry = sum >> 32
	}
	out[n] = uint32(carry) //nolint:gosec // G115: truncation is intentional (limb arithmetic).
	out = trimLimbs(out)
	if len(out) > MaxLimbs {
		return BigUint{}, ErrMaxLimbs
	}
	return BigUint{Limbs: out}, nil
}

// UintAddSmall adds a uint32 to a BigUint.
func UintAddSmall(u BigUint, v uint32) (BigUint, error) {
	if v == 0 {
		return BigUint{Limbs: trimLimbs(u.Limbs)}, nil
	}
	limbs := trimLimbs(u.Limbs)
	if len(limbs) == 0 {
		return BigUint{Limbs: []uint32{v}}, nil
	}
	out := make([]uint32, len(limbs)+1)
	copy(out, limbs)

	var carry uint64
	sum := uint64(out[0]) + uint64(v)
	out[0] = uint32(sum) //nolint:gosec // G115: truncation is intentional (limb arithmetic).
	carry = sum >> 32
	for i := 1; carry != 0 && i < len(out); i++ {
		sum = uint64(out[i]) + carry
		out[i] = uint32(sum) //nolint:gosec // G115: truncation is intentional (limb arithmetic).
		carry = sum >> 32
	}
	out = trimLimbs(out)
	if len(out) > MaxLimbs {
		return BigUint{}, ErrMaxLimbs
	}
	return BigUint{Limbs: out}, nil
}

// UintSub subtracts two BigUint values.
func UintSub(a, b BigUint) (BigUint, error) {
	if cmpLimbs(a.Limbs, b.Limbs) < 0 {
		return BigUint{}, ErrUnderflow
	}
	al := trimLimbs(a.Limbs)
	bl := trimLimbs(b.Limbs)
	if len(bl) == 0 {
		return BigUint{Limbs: al}, nil
	}
	out := make([]uint32, len(al))
	copy(out, al)
	subInPlace(out, bl)
	out = trimLimbs(out)
	return BigUint{Limbs: out}, nil
}

// UintMul multiplies two BigUint values.
func UintMul(a, b BigUint) (BigUint, error) {
	al := trimLimbs(a.Limbs)
	bl := trimLimbs(b.Limbs)
	if len(al) == 0 || len(bl) == 0 {
		return BigUint{}, nil
	}
	if len(al)+len(bl) > MaxLimbs {
		return BigUint{}, ErrMaxLimbs
	}

	out := make([]uint32, len(al)+len(bl))
	for i := range al {
		ai := uint64(al[i])
		var carry uint64
		for j := range bl {
			k := i + j
			sum := uint64(out[k]) + ai*uint64(bl[j]) + carry
			out[k] = uint32(sum) //nolint:gosec // G115: truncation is intentional (limb arithmetic).
			carry = sum >> 32
		}
		k := i + len(bl)
		for carry != 0 {
			sum := uint64(out[k]) + carry
			out[k] = uint32(sum) //nolint:gosec // G115: truncation is intentional (limb arithmetic).
			carry = sum >> 32
			k++
			if k >= len(out) && carry != 0 {
				return BigUint{}, ErrMaxLimbs
			}
		}
	}
	out = trimLimbs(out)
	return BigUint{Limbs: out}, nil
}

// UintMulSmall multiplies a BigUint by a uint32.
func UintMulSmall(u BigUint, m uint32) (BigUint, error) {
	if m == 0 || u.IsZero() {
		return BigUint{}, nil
	}
	if m == 1 {
		return BigUint{Limbs: trimLimbs(u.Limbs)}, nil
	}
	limbs := trimLimbs(u.Limbs)
	out := make([]uint32, len(limbs)+1)
	var carry uint64
	for i := range limbs {
		prod := uint64(limbs[i])*uint64(m) + carry
		out[i] = uint32(prod) //nolint:gosec // G115: truncation is intentional (limb arithmetic).
		carry = prod >> 32
	}
	out[len(limbs)] = uint32(carry) //nolint:gosec // G115: truncation is intentional (limb arithmetic).
	out = trimLimbs(out)
	if len(out) > MaxLimbs {
		return BigUint{}, ErrMaxLimbs
	}
	return BigUint{Limbs: out}, nil
}

// UintDivModSmall performs division with remainder on a BigUint by a uint32.
func UintDivModSmall(u BigUint, d uint32) (q BigUint, r uint32, err error) {
	if d == 0 {
		return BigUint{}, 0, ErrDivByZero
	}
	limbs := trimLimbs(u.Limbs)
	if len(limbs) == 0 {
		return BigUint{}, 0, nil
	}

	out := make([]uint32, len(limbs))
	var rem uint64
	for i := len(limbs) - 1; i >= 0; i-- {
		cur := (rem << 32) | uint64(limbs[i])
		out[i] = uint32(cur / uint64(d)) //nolint:gosec // G115: quotient fits in uint32.
		rem = cur % uint64(d)
		if i == 0 {
			break
		}
	}
	out = trimLimbs(out)
	return BigUint{Limbs: out}, uint32(rem), nil //nolint:gosec // G115: remainder fits in uint32.
}

// UintShl performs a left bit shift on a BigUint.
func UintShl(u BigUint, bitsCount int) (BigUint, error) {
	if bitsCount < 0 {
		return BigUint{}, errors.New("negative shift")
	}
	limbs := trimLimbs(u.Limbs)
	if len(limbs) == 0 || bitsCount == 0 {
		return BigUint{Limbs: limbs}, nil
	}
	wordShift := bitsCount / 32
	bitShift := bitsCount % 32

	out := make([]uint32, len(limbs)+wordShift+1)
	if bitShift == 0 {
		copy(out[wordShift:], limbs)
		out = trimLimbs(out)
		if len(out) > MaxLimbs {
			return BigUint{}, ErrMaxLimbs
		}
		return BigUint{Limbs: out}, nil
	}

	var carry uint32
	for i := range limbs {
		v := limbs[i]
		out[i+wordShift] = (v << bitShift) | carry
		carry = v >> (32 - bitShift)
	}
	out[len(limbs)+wordShift] = carry
	out = trimLimbs(out)
	if len(out) > MaxLimbs {
		return BigUint{}, ErrMaxLimbs
	}
	return BigUint{Limbs: out}, nil
}

// UintShr performs a right bit shift on a BigUint.
func UintShr(u BigUint, bitsCount int) (BigUint, error) {
	if bitsCount < 0 {
		return BigUint{}, errors.New("negative shift")
	}
	limbs := trimLimbs(u.Limbs)
	if len(limbs) == 0 || bitsCount == 0 {
		return BigUint{Limbs: limbs}, nil
	}
	wordShift := bitsCount / 32
	bitShift := bitsCount % 32
	if wordShift >= len(limbs) {
		return BigUint{}, nil
	}
	outLen := len(limbs) - wordShift
	out := make([]uint32, outLen)
	if bitShift == 0 {
		copy(out, limbs[wordShift:])
		out = trimLimbs(out)
		return BigUint{Limbs: out}, nil
	}

	var carry uint32
	for i := len(limbs) - 1; i >= wordShift; i-- {
		v := limbs[i]
		out[i-wordShift] = (v >> bitShift) | (carry << (32 - bitShift))
		carry = v & (uint32(1<<bitShift) - 1)
		if i == wordShift {
			break
		}
	}
	out = trimLimbs(out)
	return BigUint{Limbs: out}, nil
}

// UintDivMod performs division with remainder on two BigUint values.
func UintDivMod(a, b BigUint) (q, r BigUint, err error) {
	al := trimLimbs(a.Limbs)
	bl := trimLimbs(b.Limbs)
	if len(bl) == 0 {
		return BigUint{}, BigUint{}, ErrDivByZero
	}
	if len(al) == 0 {
		return BigUint{}, BigUint{}, nil
	}
	if cmpLimbs(al, bl) < 0 {
		return BigUint{}, BigUint{Limbs: al}, nil
	}

	shift := bitLenLimbs(al) - bitLenLimbs(bl)
	if shift < 0 {
		return BigUint{}, BigUint{Limbs: al}, nil
	}
	if shift/32+1 > MaxLimbs {
		return BigUint{}, BigUint{}, ErrMaxLimbs
	}

	denomShifted, err := UintShl(BigUint{Limbs: bl}, shift)
	if err != nil {
		return BigUint{}, BigUint{}, err
	}
	denom := make([]uint32, len(denomShifted.Limbs))
	copy(denom, denomShifted.Limbs)

	rem := make([]uint32, len(al))
	copy(rem, al)

	quot := make([]uint32, shift/32+1)
	for i := shift; i >= 0; i-- {
		if cmpLimbs(rem, denom) >= 0 {
			subInPlace(rem, denom)
			quot[i/32] |= uint32(1) << (i % 32)
		}
		shr1InPlace(denom)
		if i == 0 {
			break
		}
	}

	quot = trimLimbs(quot)
	rem = trimLimbs(rem)
	if len(quot) > MaxLimbs || len(rem) > MaxLimbs {
		return BigUint{}, BigUint{}, ErrMaxLimbs
	}
	return BigUint{Limbs: quot}, BigUint{Limbs: rem}, nil
}

func trimLimbs(limbs []uint32) []uint32 {
	for len(limbs) > 0 && limbs[len(limbs)-1] == 0 {
		limbs = limbs[:len(limbs)-1]
	}
	if len(limbs) == 0 {
		return nil
	}
	return limbs
}

func bitLenLimbs(limbs []uint32) int {
	limbs = trimLimbs(limbs)
	if len(limbs) == 0 {
		return 0
	}
	ms := limbs[len(limbs)-1]
	return (len(limbs)-1)*32 + (32 - bits.LeadingZeros32(ms))
}

func cmpLimbs(a, b []uint32) int {
	a = trimLimbs(a)
	b = trimLimbs(b)
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	for i := len(a) - 1; i >= 0; i-- {
		av := a[i]
		bv := b[i]
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
		if i == 0 {
			break
		}
	}
	return 0
}

func subInPlace(dst, sub []uint32) {
	var borrow uint64
	for i := 0; i < len(dst); i++ {
		av := uint64(dst[i])
		bv := uint64(0)
		if i < len(sub) {
			bv = uint64(sub[i])
		}
		tmp := av - bv - borrow
		dst[i] = uint32(tmp) //nolint:gosec // G115: truncation is intentional (limb arithmetic).
		if av < bv+borrow {
			borrow = 1
		} else {
			borrow = 0
		}
	}
}

func shr1InPlace(limbs []uint32) {
	var carry uint32
	for i := len(limbs) - 1; i >= 0; i-- {
		v := limbs[i]
		limbs[i] = (v >> 1) | (carry << 31)
		carry = v & 1
		if i == 0 {
			break
		}
	}
}
