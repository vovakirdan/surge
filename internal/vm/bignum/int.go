package bignum

// BigInt represents a big signed integer.
type BigInt struct {
	Neg bool
	// Limbs are base-2^32 little-endian magnitude (Limbs[0] is least significant).
	//
	// Canonical zero is represented as Neg=false and nil/empty Limbs.
	Limbs []uint32
}

// IntZero returns a zero BigInt.
func IntZero() BigInt { return BigInt{} }

// IntFromInt64 creates a BigInt from an int64.
func IntFromInt64(v int64) BigInt {
	if v == 0 {
		return BigInt{}
	}
	if v > 0 {
		return BigInt{Limbs: UintFromUint64(uint64(v)).Limbs}
	}
	// v < 0
	u := uint64(-(v + 1)) //nolint:gosec // G115: -(v+1) is non-negative and fits in uint64 here.
	u++
	return BigInt{Neg: true, Limbs: UintFromUint64(u).Limbs}
}

// IntFromUint64 creates a BigInt from a uint64.
func IntFromUint64(v uint64) BigInt {
	if v == 0 {
		return BigInt{}
	}
	return BigInt{Limbs: UintFromUint64(v).Limbs}
}

// IsZero reports whether the integer is zero.
func (i BigInt) IsZero() bool {
	return len(trimLimbs(i.Limbs)) == 0
}

// Abs returns the absolute value as a BigUint.
func (i BigInt) Abs() BigUint {
	return BigUint{Limbs: trimLimbs(i.Limbs)}
}

// Negated returns the negated value.
func (i BigInt) Negated() BigInt {
	if i.IsZero() {
		return BigInt{}
	}
	return BigInt{Neg: !i.Neg, Limbs: trimLimbs(i.Limbs)}
}

// Cmp compares two BigInt values.
func (i BigInt) Cmp(j BigInt) int {
	ia := trimLimbs(i.Limbs)
	ja := trimLimbs(j.Limbs)
	switch {
	case len(ia) == 0 && len(ja) == 0:
		return 0
	case i.Neg != j.Neg:
		if i.Neg {
			return -1
		}
		return 1
	default:
		cmp := cmpLimbs(ia, ja)
		if i.Neg {
			return -cmp
		}
		return cmp
	}
}

// Int64 converts BigInt to int64 if possible.
func (i BigInt) Int64() (int64, bool) {
	mag, ok := BigUint{Limbs: trimLimbs(i.Limbs)}.Uint64()
	if !ok {
		return 0, false
	}
	if !i.Neg {
		if mag > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(mag), true
	}
	// Negative: allow magnitude up to 2^63.
	if mag > uint64(^uint64(0)>>1)+1 {
		return 0, false
	}
	if mag == uint64(^uint64(0)>>1)+1 {
		return -1 << 63, true
	}
	return -int64(mag), true
}

// IntAdd adds two BigInt values.
func IntAdd(a, b BigInt) (BigInt, error) {
	aa := BigUint{Limbs: trimLimbs(a.Limbs)}
	ba := BigUint{Limbs: trimLimbs(b.Limbs)}

	if a.Neg == b.Neg {
		sum, err := UintAdd(aa, ba)
		if err != nil {
			return BigInt{}, err
		}
		if sum.IsZero() {
			return BigInt{}, nil
		}
		return BigInt{Neg: a.Neg, Limbs: sum.Limbs}, nil
	}

	cmp := UintCmp(aa, ba)
	switch {
	case cmp == 0:
		return BigInt{}, nil
	case cmp > 0:
		diff, err := UintSub(aa, ba)
		if err != nil {
			return BigInt{}, err
		}
		if diff.IsZero() {
			return BigInt{}, nil
		}
		return BigInt{Neg: a.Neg, Limbs: diff.Limbs}, nil
	default:
		diff, err := UintSub(ba, aa)
		if err != nil {
			return BigInt{}, err
		}
		if diff.IsZero() {
			return BigInt{}, nil
		}
		return BigInt{Neg: b.Neg, Limbs: diff.Limbs}, nil
	}
}

// IntSub subtracts two BigInt values.
func IntSub(a, b BigInt) (BigInt, error) {
	return IntAdd(a, b.Negated())
}

// IntMul multiplies two BigInt values.
func IntMul(a, b BigInt) (BigInt, error) {
	aa := BigUint{Limbs: trimLimbs(a.Limbs)}
	ba := BigUint{Limbs: trimLimbs(b.Limbs)}
	prod, err := UintMul(aa, ba)
	if err != nil {
		return BigInt{}, err
	}
	if prod.IsZero() {
		return BigInt{}, nil
	}
	return BigInt{Neg: a.Neg != b.Neg, Limbs: prod.Limbs}, nil
}

// IntDivMod performs division with remainder on two BigInt values.
func IntDivMod(a, b BigInt) (q, r BigInt, err error) {
	aa := BigUint{Limbs: trimLimbs(a.Limbs)}
	ba := BigUint{Limbs: trimLimbs(b.Limbs)}
	if ba.IsZero() {
		return BigInt{}, BigInt{}, ErrDivByZero
	}
	if aa.IsZero() {
		return BigInt{}, BigInt{}, nil
	}
	qMag, rMag, err := UintDivMod(aa, ba)
	if err != nil {
		return BigInt{}, BigInt{}, err
	}
	if qMag.IsZero() {
		q = BigInt{}
	} else {
		q = BigInt{Neg: a.Neg != b.Neg, Limbs: qMag.Limbs}
	}
	if rMag.IsZero() {
		r = BigInt{}
	} else {
		r = BigInt{Neg: a.Neg, Limbs: rMag.Limbs}
	}
	return q, r, nil
}

// UintCmp compares two BigUint values and returns -1, 0, or 1.
func UintCmp(a, b BigUint) int { return a.Cmp(b) }
