package vm

import "math"

// AddInt64Checked returns (a+b, ok). ok is false on signed overflow.
func AddInt64Checked(a, b int64) (int64, bool) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, false
	}
	return a + b, true
}

// SubInt64Checked returns (a-b, ok). ok is false on signed overflow.
func SubInt64Checked(a, b int64) (int64, bool) {
	if (b > 0 && a < math.MinInt64+b) || (b < 0 && a > math.MaxInt64+b) {
		return 0, false
	}
	return a - b, true
}

// MulInt64Checked returns (a*b, ok). ok is false on signed overflow.
func MulInt64Checked(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
		return 0, false
	}
	res := a * b
	if res/b != a {
		return 0, false
	}
	return res, true
}
