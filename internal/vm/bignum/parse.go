package bignum

import (
	"errors"
	"fmt"
	"strings"
)

var ErrParse = errors.New("invalid numeric format")

func ParseUintLiteral(s string) (BigUint, error) {
	return parseUintString(s, false, true)
}

func ParseIntLiteral(s string) (BigInt, error) {
	u, err := ParseUintLiteral(s)
	if err != nil {
		return BigInt{}, err
	}
	if u.IsZero() {
		return BigInt{}, nil
	}
	return BigInt{Limbs: u.Limbs}, nil
}

func ParseUint(s string) (BigUint, error) {
	return parseUintString(s, true, false)
}

func ParseInt(s string) (BigInt, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return BigInt{}, ErrParse
	}
	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	u, err := parseUintString(s, true, false)
	if err != nil {
		return BigInt{}, err
	}
	if u.IsZero() {
		return BigInt{}, nil
	}
	return BigInt{Neg: neg, Limbs: u.Limbs}, nil
}

func parseUintString(s string, allowLeadingPlus, allowBasePrefix bool) (BigUint, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return BigUint{}, ErrParse
	}
	if allowLeadingPlus && s[0] == '+' {
		s = s[1:]
	}
	if s == "" {
		return BigUint{}, ErrParse
	}

	// Strip underscores.
	if strings.IndexByte(s, '_') >= 0 {
		var b strings.Builder
		b.Grow(len(s))
		for i := range len(s) {
			ch := s[i]
			if ch == '_' {
				continue
			}
			b.WriteByte(ch)
		}
		s = b.String()
	}

	base := uint32(10)
	if allowBasePrefix && len(s) > 2 && s[0] == '0' {
		switch s[1] {
		case 'x', 'X':
			base = 16
			s = s[2:]
		case 'b', 'B':
			base = 2
			s = s[2:]
		case 'o', 'O':
			base = 8
			s = s[2:]
		default:
		}
	}
	if s == "" {
		return BigUint{}, ErrParse
	}

	var out BigUint
	for i := range len(s) {
		d, ok := digitValue(s[i], base)
		if !ok {
			return BigUint{}, fmt.Errorf("%w: %q", ErrParse, s)
		}
		var err error
		out, err = UintMulSmall(out, base)
		if err != nil {
			return BigUint{}, err
		}
		out, err = UintAddSmall(out, d)
		if err != nil {
			return BigUint{}, err
		}
	}
	return out, nil
}

func digitValue(ch byte, base uint32) (uint32, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		d := uint32(ch - '0')
		return d, d < base
	case base == 16 && ch >= 'a' && ch <= 'f':
		return 10 + uint32(ch-'a'), true
	case base == 16 && ch >= 'A' && ch <= 'F':
		return 10 + uint32(ch-'A'), true
	default:
		return 0, false
	}
}

func ParseFloat(s string) (BigFloat, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return BigFloat{}, ErrParse
	}

	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	if s == "" {
		return BigFloat{}, ErrParse
	}

	// Strip underscores for v1.
	if strings.IndexByte(s, '_') >= 0 {
		var b strings.Builder
		b.Grow(len(s))
		for i := range len(s) {
			ch := s[i]
			if ch == '_' {
				continue
			}
			b.WriteByte(ch)
		}
		s = b.String()
	}

	i := 0
	digits := make([]byte, 0, len(s))
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		digits = append(digits, s[i])
		i++
	}
	if len(digits) == 0 {
		return BigFloat{}, ErrParse
	}

	fracDigits := 0
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			digits = append(digits, s[i])
			fracDigits++
			i++
		}
	}

	exp10 := 0
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i >= len(s) {
			return BigFloat{}, ErrParse
		}
		expNeg := false
		if s[i] == '+' {
			i++
		} else if s[i] == '-' {
			expNeg = true
			i++
		}
		if i >= len(s) {
			return BigFloat{}, ErrParse
		}
		if s[i] < '0' || s[i] > '9' {
			return BigFloat{}, ErrParse
		}
		val := 0
		const maxExp = 1_000_000 // reasonable limit for exponent
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			if val > maxExp {
				return BigFloat{}, fmt.Errorf("%w: exponent too large", ErrParse)
			}
			val = val*10 + int(s[i]-'0')
			i++
		}
		exp10 = val
		if expNeg {
			exp10 = -exp10
		}
	}

	if i != len(s) {
		return BigFloat{}, ErrParse
	}

	// Remove leading zeros to avoid huge work in ParseUint.
	digitsStr := strings.TrimLeft(string(digits), "0")
	if digitsStr == "" {
		return BigFloat{}, nil
	}

	n, err := parseUintString(digitsStr, false, false)
	if err != nil {
		return BigFloat{}, err
	}

	k := exp10 - fracDigits
	num := n
	den := UintFromUint32(1)
	if k >= 0 {
		pow, powErr := UintPow10(k)
		if powErr != nil {
			return BigFloat{}, powErr
		}
		num, err = UintMul(num, pow)
		if err != nil {
			return BigFloat{}, err
		}
	} else {
		pow, powErr := UintPow10(-k)
		if powErr != nil {
			return BigFloat{}, powErr
		}
		den = pow
	}

	f, err := floatFromRatio(neg, num, den)
	if err != nil {
		return BigFloat{}, err
	}
	return f, nil
}

func UintPow10(n int) (BigUint, error) {
	if n < 0 {
		return BigUint{}, errors.New("negative pow10")
	}
	if n == 0 {
		return UintFromUint32(1), nil
	}
	result := UintFromUint32(1)
	base := UintFromUint32(10)
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

func floatFromRatio(neg bool, num, den BigUint) (BigFloat, error) {
	if num.IsZero() {
		return BigFloat{}, nil
	}
	if den.IsZero() {
		return BigFloat{}, ErrDivByZero
	}

	e0, err := floorLog2Ratio(num, den)
	if err != nil {
		return BigFloat{}, err
	}

	scale := (MantissaBits - 1) - e0

	scaledNum := num
	scaledDen := den
	if scale >= 0 {
		if scale > int(^uint(0)>>1) {
			return BigFloat{}, ErrMaxLimbs
		}
		scaledNum, err = UintShl(num, scale)
		if err != nil {
			return BigFloat{}, err
		}
	} else {
		if -scale > int(^uint(0)>>1) {
			return BigFloat{}, ErrMaxLimbs
		}
		scaledDen, err = UintShl(den, -scale)
		if err != nil {
			return BigFloat{}, err
		}
	}

	q, r, err := UintDivMod(scaledNum, scaledDen)
	if err != nil {
		return BigFloat{}, err
	}
	q, err = roundQuotientToEven(q, r, scaledDen)
	if err != nil {
		return BigFloat{}, err
	}

	exp64 := int64(e0) - int64(MantissaBits-1)
	if exp64 < -1<<31 || exp64 > (1<<31)-1 {
		return BigFloat{}, ErrMaxLimbs
	}
	exp := int32(exp64)
	mant, exp, err := normalizeMantissa(q, exp)
	if err != nil {
		return BigFloat{}, err
	}
	if mant.IsZero() {
		return BigFloat{}, nil
	}
	return BigFloat{Neg: neg, Mant: mant, Exp: exp}, nil
}

func floorLog2Ratio(num, den BigUint) (int, error) {
	if num.IsZero() || den.IsZero() {
		return 0, ErrDivByZero
	}
	if num.Cmp(den) >= 0 {
		d := num.BitLen() - den.BitLen()
		e := d
		shifted, err := UintShl(den, e)
		if err != nil {
			return 0, err
		}
		if num.Cmp(shifted) < 0 {
			e--
		} else {
			shifted2, err := UintShl(den, e+1)
			if err == nil && num.Cmp(shifted2) >= 0 {
				e++
			}
		}
		return e, nil
	}

	d := den.BitLen() - num.BitLen()
	s := d
	shifted, err := UintShl(num, s)
	if err != nil {
		return 0, err
	}
	if shifted.Cmp(den) < 0 {
		s++
	}
	return -s, nil
}
