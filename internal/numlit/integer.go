package numlit

import (
	"strconv"
	"strings"
)

const (
	maxInt64Magnitude = uint64(1)<<63 - 1
	minInt64Magnitude = uint64(1) << 63
)

// ParseUint64 parses Surge integer literal text into a uint64.
// It recognizes explicit 0x/0b/0o prefixes; otherwise the literal is decimal.
func ParseUint64(text string) (uint64, bool) {
	clean := cleanIntegerLiteral(text)
	if clean == "" || clean[0] == '+' || clean[0] == '-' {
		return 0, false
	}
	body, base, ok := integerBodyAndBase(clean)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseUint(body, base, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// ParseInt64 parses Surge integer literal text into an int64.
// It recognizes explicit 0x/0b/0o prefixes; otherwise the literal is decimal.
func ParseInt64(text string) (int64, bool) {
	clean := cleanIntegerLiteral(text)
	if clean == "" {
		return 0, false
	}
	negative := false
	switch clean[0] {
	case '-':
		negative = true
		clean = clean[1:]
	case '+':
		clean = clean[1:]
	}
	if clean == "" {
		return 0, false
	}
	value, ok := ParseUint64(clean)
	if !ok {
		return 0, false
	}
	if negative {
		if value > minInt64Magnitude {
			return 0, false
		}
		if value == minInt64Magnitude {
			return -1 << 63, true
		}
		signed, ok := checkedInt64Magnitude(value)
		if !ok {
			return 0, false
		}
		return -signed, true
	}
	return checkedInt64Magnitude(value)
}

func checkedInt64Magnitude(value uint64) (int64, bool) {
	if value > maxInt64Magnitude {
		return 0, false
	}
	signed, err := strconv.ParseInt(strconv.FormatUint(value, 10), 10, 64)
	if err != nil {
		return 0, false
	}
	return signed, true
}

func cleanIntegerLiteral(text string) string {
	return strings.ReplaceAll(strings.TrimSpace(text), "_", "")
}

func integerBodyAndBase(clean string) (body string, base int, ok bool) {
	base = 10
	body = clean
	if len(clean) > 2 && clean[0] == '0' {
		switch clean[1] {
		case 'x', 'X':
			base = 16
			body = clean[2:]
		case 'b', 'B':
			base = 2
			body = clean[2:]
		case 'o', 'O':
			base = 8
			body = clean[2:]
		}
	}
	if body == "" {
		return "", 0, false
	}
	return body, base, true
}
