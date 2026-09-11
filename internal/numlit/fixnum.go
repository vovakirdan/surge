package numlit

// Fixnum bounds match the native tagged word representation. Zero is NULL;
// other inline values use bit zero as their tag and the remaining 63 bits.
const (
	FixiMin = -(int64(1) << 62)
	FixiMax = (int64(1) << 62) - 1
	FixuMax = (uint64(1) << 63) - 1
)

// InlineInt classifies literal text when present, otherwise a synthesized
// value. An overflowing or invalid spelling must never fall back to value.
func InlineInt(text string, value int64) (int64, bool) {
	if text != "" {
		var ok bool
		value, ok = ParseInt64(text)
		if !ok {
			return 0, false
		}
	}
	return value, value >= FixiMin && value <= FixiMax
}

// InlineUint is the unsigned half of InlineInt.
func InlineUint(text string, value uint64) (uint64, bool) {
	if text != "" {
		var ok bool
		value, ok = ParseUint64(text)
		if !ok {
			return 0, false
		}
	}
	return value, value <= FixuMax
}
