package parser

import "fmt"

// splitNumericLiteral splits lit into its contiguous integer digit prefix and the remaining suffix.
// It recognizes optional base prefixes `0x`/`0X` (hex), `0b`/`0B` (binary), and `0o`/`0O` (octal).
// It returns an error for empty input, for a missing digit sequence after a base prefix, for a
// literal with no digits, or if the first non-digit character is a fractional/exponent marker
// ('.', 'e', 'E', 'p', 'P') which are not allowed here.
// The first return value is the numeric prefix, the second is the remaining suffix, and the third
// is a non-nil error on failure.
func splitNumericLiteral(lit string) (string, string, error) {
	if lit == "" {
		return "", "", fmt.Errorf("empty literal")
	}

	base := 10
	start := 0
	if len(lit) >= 2 && lit[0] == '0' {
		switch lit[1] {
		case 'x', 'X':
			base = 16
			start = 2
		case 'b', 'B':
			base = 2
			start = 2
		case 'o', 'O':
			base = 8
			start = 2
		}
	}

	end := start
	for end < len(lit) && isDigitForBase(lit[end], base) {
		end++
	}

	if end == start && start != 0 {
		return "", "", fmt.Errorf("missing digits after base prefix")
	}
	if end == 0 {
		return "", "", fmt.Errorf("missing digits in literal")
	}
	if end < len(lit) {
		switch lit[end] {
		case '.', 'e', 'E', 'p', 'P':
			return "", "", fmt.Errorf("fractional literals are not allowed in array sizes")
		}
	}

	return lit[:end], lit[end:], nil
}

// isDigitForBase reports whether b is a valid digit for the given base.
// It returns true for bases 2, 8, 10, and 16 when b is within the appropriate ASCII digit or hexadecimal range; for any other base it returns false.
func isDigitForBase(b byte, base int) bool {
	switch base {
	case 2:
		return b == '0' || b == '1'
	case 8:
		return b >= '0' && b <= '7'
	case 10:
		return b >= '0' && b <= '9'
	case 16:
		return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
	default:
		return false
	}
}

// isValidIntegerSuffix reports whether the string s is a valid integer suffix.
// An empty string is valid. If non-empty, the first character must be an ASCII letter and each subsequent character must be an ASCII letter or digit.
func isValidIntegerSuffix(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if i == 0 {
			if !isLetter(ch) {
				return false
			}
			continue
		}
		if !isLetter(ch) && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

// isLetter reports whether b is an ASCII alphabetic character ('A'–'Z' or 'a'–'z').
func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}