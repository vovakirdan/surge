package bignum

import (
	"errors"

	"fortio.org/safecast"
)

// MantissaBits is the number of bits in the mantissa.
const MantissaBits = 256

// BigFloat represents a big floating-point number.
type BigFloat struct {
	Neg  bool
	Mant BigUint
	Exp  int32 // value = (-1)^Neg * Mant * 2^Exp
}

// FloatZero returns a zero BigFloat.
func FloatZero() BigFloat { return BigFloat{} }

// IsZero reports whether the float is zero.
func (f BigFloat) IsZero() bool {
	return f.Mant.IsZero()
}

// Cmp compares two BigFloat values.
func (f BigFloat) Cmp(g BigFloat) int {
	if f.IsZero() && g.IsZero() {
		return 0
	}
	if f.Neg != g.Neg {
		if f.Neg {
			return -1
		}
		return 1
	}
	// For normalized, fixed-precision floats, exponent ordering is total.
	if f.Exp < g.Exp {
		if f.Neg {
			return 1
		}
		return -1
	}
	if f.Exp > g.Exp {
		if f.Neg {
			return -1
		}
		return 1
	}
	cmp := f.Mant.Cmp(g.Mant)
	if f.Neg {
		return -cmp
	}
	return cmp
}

// FloatNeg negates a BigFloat.
func FloatNeg(f BigFloat) BigFloat {
	if f.IsZero() {
		return BigFloat{}
	}
	f.Neg = !f.Neg
	return f
}

// FloatFromUint converts a BigUint to BigFloat.
func FloatFromUint(u BigUint) (BigFloat, error) {
	if u.IsZero() {
		return BigFloat{}, nil
	}
	mant, exp, err := normalizeMantissa(u, 0)
	if err != nil {
		return BigFloat{}, err
	}
	return BigFloat{Neg: false, Mant: mant, Exp: exp}, nil
}

func FloatFromInt(i BigInt) (BigFloat, error) {
	if i.IsZero() {
		return BigFloat{}, nil
	}
	mant, exp, err := normalizeMantissa(i.Abs(), 0)
	if err != nil {
		return BigFloat{}, err
	}
	return BigFloat{Neg: i.Neg, Mant: mant, Exp: exp}, nil
}

func FloatToIntTrunc(f BigFloat) (BigInt, error) {
	if f.IsZero() {
		return BigInt{}, nil
	}
	mag := f.Mant
	if f.Exp > 0 {
		maxInt := int64(^uint(0) >> 1)
		if int64(f.Exp) > maxInt {
			return BigInt{}, ErrMaxLimbs
		}
		var err error
		mag, err = UintShl(mag, int(f.Exp))
		if err != nil {
			return BigInt{}, err
		}
	} else if f.Exp < 0 {
		maxInt := int64(^uint(0) >> 1)
		shift := -int64(f.Exp)
		if shift > maxInt {
			return BigInt{}, nil
		}
		var err error
		mag, err = UintShr(mag, int(shift))
		if err != nil {
			return BigInt{}, err
		}
	}
	if mag.IsZero() {
		return BigInt{}, nil
	}
	return BigInt{Neg: f.Neg, Limbs: mag.Limbs}, nil
}

// FloatToUintTrunc converts a BigFloat to BigUint by truncation.
func FloatToUintTrunc(f BigFloat) (BigUint, error) {
	if f.Neg && !f.IsZero() {
		return BigUint{}, errors.New("negative float to uint")
	}
	i, err := FloatToIntTrunc(f)
	if err != nil {
		return BigUint{}, err
	}
	if i.Neg && !i.IsZero() {
		return BigUint{}, errors.New("negative float to uint")
	}
	return i.Abs(), nil
}

// FloatAdd adds two BigFloat values.
func FloatAdd(a, b BigFloat) (BigFloat, error) {
	if a.IsZero() {
		return b, nil
	}
	if b.IsZero() {
		return a, nil
	}
	if a.Exp < b.Exp {
		a, b = b, a
	}
	delta64 := int64(a.Exp) - int64(b.Exp)
	if delta64 > int64(^uint(0)>>1) {
		// b is so much smaller it rounds to zero
		return a, nil
	}
	delta := int(delta64)

	bm, err := shiftRightRoundToEven(b.Mant, delta)
	if err != nil {
		return BigFloat{}, err
	}

	if a.Neg == b.Neg {
		sum, err := UintAdd(a.Mant, bm)
		if err != nil {
			return BigFloat{}, err
		}
		mant, exp, err := normalizeMantissa(sum, a.Exp)
		if err != nil {
			return BigFloat{}, err
		}
		return BigFloat{Neg: a.Neg, Mant: mant, Exp: exp}, nil
	}

	// Different signs => subtract magnitudes.
	cmp := a.Mant.Cmp(bm)
	switch {
	case cmp == 0:
		return BigFloat{}, nil
	case cmp > 0:
		diff, err := UintSub(a.Mant, bm)
		if err != nil {
			return BigFloat{}, err
		}
		mant, exp, err := normalizeMantissa(diff, a.Exp)
		if err != nil {
			return BigFloat{}, err
		}
		return BigFloat{Neg: a.Neg, Mant: mant, Exp: exp}, nil
	default:
		diff, err := UintSub(bm, a.Mant)
		if err != nil {
			return BigFloat{}, err
		}
		mant, exp, err := normalizeMantissa(diff, a.Exp)
		if err != nil {
			return BigFloat{}, err
		}
		return BigFloat{Neg: b.Neg, Mant: mant, Exp: exp}, nil
	}
}

// FloatSub subtracts two BigFloat values.
func FloatSub(a, b BigFloat) (BigFloat, error) {
	return FloatAdd(a, FloatNeg(b))
}

// FloatMul multiplies two BigFloat values.
func FloatMul(a, b BigFloat) (BigFloat, error) {
	if a.IsZero() || b.IsZero() {
		return BigFloat{}, nil
	}
	prod, err := UintMul(a.Mant, b.Mant)
	if err != nil {
		return BigFloat{}, err
	}
	exp := a.Exp + b.Exp
	mant, exp, err := normalizeMantissa(prod, exp)
	if err != nil {
		return BigFloat{}, err
	}
	return BigFloat{Neg: a.Neg != b.Neg, Mant: mant, Exp: exp}, nil
}

// FloatDiv divides two BigFloat values.
func FloatDiv(a, b BigFloat) (BigFloat, error) {
	if b.IsZero() {
		return BigFloat{}, ErrDivByZero
	}
	if a.IsZero() {
		return BigFloat{}, nil
	}

	scaled, err := UintShl(a.Mant, MantissaBits)
	if err != nil {
		return BigFloat{}, err
	}
	q, r, err := UintDivMod(scaled, b.Mant)
	if err != nil {
		return BigFloat{}, err
	}
	q, err = roundQuotientToEven(q, r, b.Mant)
	if err != nil {
		return BigFloat{}, err
	}
	exp := a.Exp - b.Exp - MantissaBits
	mant, exp, err := normalizeMantissa(q, exp)
	if err != nil {
		return BigFloat{}, err
	}
	return BigFloat{Neg: a.Neg != b.Neg, Mant: mant, Exp: exp}, nil
}

func normalizeMantissa(m BigUint, exp int32) (BigUint, int32, error) {
	if m.IsZero() {
		return BigUint{}, 0, nil
	}
	bl := m.BitLen()
	switch {
	case bl == MantissaBits:
		return BigUint{Limbs: trimLimbs(m.Limbs)}, exp, nil
	case bl > MantissaBits:
		shift := bl - MantissaBits
		rounded, err := shiftRightRoundToEven(m, shift)
		if err != nil {
			return BigUint{}, 0, err
		}
		delta, err := safecast.Conv[int32](shift)
		if err != nil {
			return BigUint{}, 0, ErrMaxLimbs
		}
		exp += delta
		if rounded.BitLen() > MantissaBits {
			rounded, err = shiftRightRoundToEven(rounded, 1)
			if err != nil {
				return BigUint{}, 0, err
			}
			exp++
		}
		return rounded, exp, nil
	default:
		shift := MantissaBits - bl
		shifted, err := UintShl(m, shift)
		if err != nil {
			return BigUint{}, 0, err
		}
		delta, err := safecast.Conv[int32](shift)
		if err != nil {
			return BigUint{}, 0, ErrMaxLimbs
		}
		exp -= delta
		return shifted, exp, nil
	}
}

func shiftRightRoundToEven(m BigUint, bitsCount int) (BigUint, error) {
	if bitsCount <= 0 || m.IsZero() {
		return BigUint{Limbs: trimLimbs(m.Limbs)}, nil
	}
	if bitsCount > m.BitLen() {
		return BigUint{}, nil
	}

	halfSet := uintBitSet(m.Limbs, bitsCount-1)
	lowSet := uintAnyLowBitSet(m.Limbs, bitsCount-1)

	shifted, err := UintShr(m, bitsCount)
	if err != nil {
		return BigUint{}, err
	}
	if !halfSet {
		return shifted, nil
	}
	if lowSet {
		return UintAddSmall(shifted, 1)
	}
	if shifted.IsOdd() {
		return UintAddSmall(shifted, 1)
	}
	return shifted, nil
}

func uintBitSet(limbs []uint32, bit int) bool {
	if bit < 0 {
		return false
	}
	limbs = trimLimbs(limbs)
	word := bit / 32
	if word < 0 || word >= len(limbs) {
		return false
	}
	return (limbs[word] & (uint32(1) << (bit % 32))) != 0
}

func uintAnyLowBitSet(limbs []uint32, bitsCount int) bool {
	if bitsCount <= 0 {
		return false
	}
	limbs = trimLimbs(limbs)
	fullWords := bitsCount / 32
	remBits := bitsCount % 32
	for i := 0; i < fullWords && i < len(limbs); i++ {
		if limbs[i] != 0 {
			return true
		}
	}
	if remBits == 0 {
		return false
	}
	if fullWords >= len(limbs) {
		return false
	}
	mask := uint32(1<<remBits) - 1
	return (limbs[fullWords] & mask) != 0
}

func roundQuotientToEven(q, r, denom BigUint) (BigUint, error) {
	if r.IsZero() {
		return q, nil
	}
	twoR, err := UintShl(r, 1)
	if err != nil {
		return BigUint{}, err
	}
	cmp := twoR.Cmp(denom)
	switch {
	case cmp < 0:
		return q, nil
	case cmp > 0:
		return UintAddSmall(q, 1)
	default:
		if q.IsOdd() {
			return UintAddSmall(q, 1)
		}
		return q, nil
	}
}
