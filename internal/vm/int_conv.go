package vm

func asUint64(v int64) uint64 {
	return uint64(v) //nolint:gosec // G115: intentional bit-pattern reinterpretation for unsigned ops.
}

func asInt64(v uint64) int64 {
	return int64(v) //nolint:gosec // G115: intentional bit-pattern reinterpretation for fixed-width ints.
}
