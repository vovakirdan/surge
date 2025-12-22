package bignum

import (
	"fmt"
	"strings"
)

func FormatUint(u BigUint) string {
	limbs := trimLimbs(u.Limbs)
	if len(limbs) == 0 {
		return "0"
	}

	const base = uint32(1_000_000_000)

	cur := BigUint{Limbs: limbs}
	var parts []uint32
	for !cur.IsZero() {
		q, r, err := UintDivModSmall(cur, base)
		if err != nil {
			return "<format-error>"
		}
		parts = append(parts, r)
		cur = q
	}

	var sb strings.Builder
	last := parts[len(parts)-1]
	sb.WriteString(fmt.Sprintf("%d", last))
	for i := len(parts) - 2; i >= 0; i-- {
		sb.WriteString(fmt.Sprintf("%09d", parts[i]))
		if i == 0 {
			break
		}
	}
	return sb.String()
}

func FormatInt(i BigInt) string {
	limbs := trimLimbs(i.Limbs)
	if len(limbs) == 0 {
		return "0"
	}
	s := FormatUint(BigUint{Limbs: limbs})
	if i.Neg {
		return "-" + s
	}
	return s
}

func FormatFloat(f BigFloat) (string, error) {
	if f.IsZero() {
		return "0", nil
	}

	neg := f.Neg
	mant := BigUint{Limbs: trimLimbs(f.Mant.Limbs)}

	// Integer fast-path.
	if f.Exp >= 0 {
		maxInt := int64(^uint(0) >> 1)
		if int64(f.Exp) > maxInt {
			return "", ErrMaxLimbs
		}
		intMag, err := UintShl(mant, int(f.Exp))
		if err != nil {
			return "", err
		}
		s := FormatUint(intMag)
		if neg {
			return "-" + s, nil
		}
		return s, nil
	}

	n64 := -int64(f.Exp)
	maxInt := int64(^uint(0) >> 1)
	if n64 < 0 {
		n64 = 0
	}
	if n64 > maxInt {
		return "", ErrMaxLimbs
	}
	n := int(n64)
	if mant.TrailingZeros() >= n {
		intMag, err := UintShr(mant, n)
		if err != nil {
			return "", err
		}
		s := FormatUint(intMag)
		if neg {
			return "-" + s, nil
		}
		return s, nil
	}

	// Exact decimal for dyadic rationals: M / 2^n.
	intPart, err := UintShr(mant, n)
	if err != nil {
		return "", err
	}
	fracPart := uintLowBits(mant, n)

	pow5, err := UintPow5(n)
	if err != nil {
		return "", err
	}
	fracDigits, err := UintMul(fracPart, pow5)
	if err != nil {
		return "", err
	}

	intStr := FormatUint(intPart)
	fracStr := FormatUint(fracDigits)
	if len(fracStr) < n {
		fracStr = strings.Repeat("0", n-len(fracStr)) + fracStr
	}
	fracStr = strings.TrimRight(fracStr, "0")
	if fracStr == "" {
		if neg {
			return "-" + intStr, nil
		}
		return intStr, nil
	}

	// Canonical: scientific for non-integers.
	sci := toScientific(intStr, fracStr)
	if neg {
		return "-" + sci, nil
	}
	return sci, nil
}

func UintPow5(n int) (BigUint, error) {
	if n < 0 {
		return BigUint{}, fmt.Errorf("pow5: %w", ErrParse)
	}
	if n == 0 {
		return UintFromUint32(1), nil
	}
	result := UintFromUint32(1)
	base := UintFromUint32(5)
	for n > 0 {
		if n&1 == 1 {
			var err error
			result, err = UintMul(result, base)
			if err != nil {
				return BigUint{}, err
			}
		}
		n >>= 1
		if n == 0 {
			break
		}
		var err error
		base, err = UintMul(base, base)
		if err != nil {
			return BigUint{}, err
		}
	}
	return result, nil
}

func uintLowBits(u BigUint, bitsCount int) BigUint {
	if bitsCount <= 0 || u.IsZero() {
		return BigUint{}
	}
	limbs := trimLimbs(u.Limbs)
	wordCount := bitsCount / 32
	remBits := bitsCount % 32

	if wordCount >= len(limbs) {
		return BigUint{Limbs: limbs}
	}
	outLen := wordCount
	if remBits != 0 {
		outLen++
	}
	out := make([]uint32, outLen)
	copy(out, limbs[:outLen])
	if remBits != 0 {
		mask := uint32(1<<remBits) - 1
		out[outLen-1] &= mask
	}
	out = trimLimbs(out)
	return BigUint{Limbs: out}
}

func toScientific(intStr, fracStr string) string {
	// intStr has no leading zeros (except "0"), fracStr has no trailing zeros.
	if intStr != "0" {
		exp := len(intStr) - 1
		digits := intStr + fracStr
		return formatScientificDigits(digits, exp)
	}

	// Leading zeros after decimal.
	i := 0
	for i < len(fracStr) && fracStr[i] == '0' {
		i++
	}
	if i >= len(fracStr) {
		return "0"
	}
	exp := -(i + 1)
	digits := fracStr[i:]
	return formatScientificDigits(digits, exp)
}

func formatScientificDigits(digits string, exp int) string {
	if digits == "" {
		return "0"
	}
	first := digits[:1]
	rest := digits[1:]

	var sb strings.Builder
	sb.Grow(len(digits) + 8)
	sb.WriteString(first)
	if rest != "" {
		sb.WriteByte('.')
		sb.WriteString(rest)
	}
	if exp >= 0 {
		sb.WriteString("E+")
		sb.WriteString(fmt.Sprintf("%d", exp))
	} else {
		sb.WriteString("E-")
		sb.WriteString(fmt.Sprintf("%d", -exp))
	}
	return sb.String()
}
