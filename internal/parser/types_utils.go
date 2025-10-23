package parser

import "fmt"

// splitNumericLiteral splits a numeric literal into its numeric prefix and the trailing suffix while validating the literal's format.
// 
// It recognizes optional base prefixes `0x`/`0X` (hex), `0b`/`0B` (binary), and `0o`/`0O` (octal) and consumes consecutive digits valid for the detected base.
// Returns an error for an empty input, when a base prefix is present but no digits follow, when no digits are present at all, or when the remainder begins with fractional/exponent indicators ('.', 'e', 'E', 'p', 'P') which are not allowed for array sizes.
// On success the first return is the consumed numeric portion and the second is the remaining suffix.
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

// isDigitForBase reports whether the byte b is a valid digit in the specified numeric base.
// Supported bases are 2, 8, 10, and 16; base 16 accepts '0'-'9', 'a'-'f', and 'A'-'F'. For unsupported bases it returns false.
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

// isValidIntegerSuffix reports whether s is a valid integer literal suffix.
// An empty suffix is valid. If non-empty, the first character must be an ASCII letter
// and remaining characters, if any, must be ASCII letters or ASCII digits; returns true if s meets these rules.
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

// isLetter reports whether b is an ASCII letter (A-Z or a-z).
func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}