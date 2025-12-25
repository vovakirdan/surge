package bignum

import "errors"

// UintAnd returns the bitwise AND of a and b.
func UintAnd(a, b BigUint) BigUint {
	al := trimLimbs(a.Limbs)
	bl := trimLimbs(b.Limbs)
	if len(al) == 0 || len(bl) == 0 {
		return BigUint{}
	}
	n := len(al)
	if len(bl) < n {
		n = len(bl)
	}
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		out[i] = al[i] & bl[i]
	}
	out = trimLimbs(out)
	return BigUint{Limbs: out}
}

// UintOr returns the bitwise OR of a and b.
func UintOr(a, b BigUint) BigUint {
	al := trimLimbs(a.Limbs)
	bl := trimLimbs(b.Limbs)
	n := len(al)
	if len(bl) > n {
		n = len(bl)
	}
	if n == 0 {
		return BigUint{}
	}
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		var av, bv uint32
		if i < len(al) {
			av = al[i]
		}
		if i < len(bl) {
			bv = bl[i]
		}
		out[i] = av | bv
	}
	out = trimLimbs(out)
	return BigUint{Limbs: out}
}

// UintXor returns the bitwise XOR of a and b.
func UintXor(a, b BigUint) BigUint {
	al := trimLimbs(a.Limbs)
	bl := trimLimbs(b.Limbs)
	n := len(al)
	if len(bl) > n {
		n = len(bl)
	}
	if n == 0 {
		return BigUint{}
	}
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		var av, bv uint32
		if i < len(al) {
			av = al[i]
		}
		if i < len(bl) {
			bv = bl[i]
		}
		out[i] = av ^ bv
	}
	out = trimLimbs(out)
	return BigUint{Limbs: out}
}

// IntAnd returns the bitwise AND of a and b using two's complement semantics.
func IntAnd(a, b BigInt) (BigInt, error) {
	return intBitOp(a, b, UintAnd)
}

// IntOr returns the bitwise OR of a and b using two's complement semantics.
func IntOr(a, b BigInt) (BigInt, error) {
	return intBitOp(a, b, UintOr)
}

// IntXor returns the bitwise XOR of a and b using two's complement semantics.
func IntXor(a, b BigInt) (BigInt, error) {
	return intBitOp(a, b, UintXor)
}

// IntShl returns a << bitsCount using arithmetic shift semantics.
func IntShl(a BigInt, bitsCount int) (BigInt, error) {
	if bitsCount < 0 {
		return BigInt{}, errors.New("negative shift")
	}
	if bitsCount == 0 || a.IsZero() {
		return BigInt{Neg: a.Neg, Limbs: trimLimbs(a.Limbs)}, nil
	}
	mag := BigUint{Limbs: trimLimbs(a.Limbs)}
	shifted, err := UintShl(mag, bitsCount)
	if err != nil {
		return BigInt{}, err
	}
	if shifted.IsZero() {
		return BigInt{}, nil
	}
	return BigInt{Neg: a.Neg, Limbs: shifted.Limbs}, nil
}

// IntShr returns a >> bitsCount using arithmetic shift semantics.
func IntShr(a BigInt, bitsCount int) (BigInt, error) {
	if bitsCount < 0 {
		return BigInt{}, errors.New("negative shift")
	}
	if bitsCount == 0 || a.IsZero() {
		return BigInt{Neg: a.Neg, Limbs: trimLimbs(a.Limbs)}, nil
	}
	mag := BigUint{Limbs: trimLimbs(a.Limbs)}
	if !a.Neg {
		shifted, err := UintShr(mag, bitsCount)
		if err != nil {
			return BigInt{}, err
		}
		if shifted.IsZero() {
			return BigInt{}, nil
		}
		return BigInt{Neg: false, Limbs: shifted.Limbs}, nil
	}
	pow2, err := UintShl(UintFromUint64(1), bitsCount)
	if err != nil {
		return BigInt{}, err
	}
	pow2Minus1, err := UintSub(pow2, UintFromUint64(1))
	if err != nil {
		return BigInt{}, err
	}
	sum, err := UintAdd(mag, pow2Minus1)
	if err != nil {
		return BigInt{}, err
	}
	shifted, err := UintShr(sum, bitsCount)
	if err != nil {
		return BigInt{}, err
	}
	if shifted.IsZero() {
		return BigInt{}, nil
	}
	return BigInt{Neg: true, Limbs: shifted.Limbs}, nil
}

func intBitOp(a, b BigInt, op func(BigUint, BigUint) BigUint) (BigInt, error) {
	aa := BigUint{Limbs: trimLimbs(a.Limbs)}
	bb := BigUint{Limbs: trimLimbs(b.Limbs)}
	if aa.IsZero() && bb.IsZero() {
		return BigInt{}, nil
	}
	width := maxInt(aa.BitLen(), bb.BitLen()) + 1
	pow2, err := UintShl(UintFromUint64(1), width)
	if err != nil {
		return BigInt{}, err
	}
	repA, err := twosComplement(aa, a.Neg, pow2)
	if err != nil {
		return BigInt{}, err
	}
	repB, err := twosComplement(bb, b.Neg, pow2)
	if err != nil {
		return BigInt{}, err
	}
	res := op(repA, repB)
	if !uintBitSet(res.Limbs, width-1) {
		out := trimLimbs(res.Limbs)
		if len(out) == 0 {
			return BigInt{}, nil
		}
		return BigInt{Limbs: out}, nil
	}
	mag, err := UintSub(pow2, res)
	if err != nil {
		return BigInt{}, err
	}
	if mag.IsZero() {
		return BigInt{}, nil
	}
	return BigInt{Neg: true, Limbs: mag.Limbs}, nil
}

func twosComplement(mag BigUint, neg bool, pow2 BigUint) (BigUint, error) {
	if mag.IsZero() || !neg {
		return mag, nil
	}
	return UintSub(pow2, mag)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
