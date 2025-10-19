package parser

import "fmt"

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

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
