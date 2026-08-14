package llvm

import "testing"

// The Range layout is described TWICE, once per language, and neither
// description can see the other.
//
// `runtime/native/rt.h` defines `SurgeRange` and `SurgeRangeArrayIter` and
// guards them with `_Static_assert`s — but those only catch drift on the C side,
// because a C compiler cannot see a Go constant. This test is the other half:
// it pins the emitter's constants to the same numbers those assertions pin the
// structs to, so a change on either side fails something.
//
// The numbers are kicked LITERALLY rather than derived, for the same reason the
// diagnostic-code tests kick their numbers literally: a test that computes the
// value it checks agrees with whatever the code says.
//
// What breaks if they drift: `rt_range_free` sizes a block with `sizeof`, and
// `rt_alloc`/`rt_free` reconcile the size they are told rather than measuring
// the block, so a mismatch is a heap-accounting corruption rather than a
// compile error.
func TestRangeLayoutMatchesTheRuntimeHeader(t *testing.T) {
	cases := []struct {
		name  string
		got   int
		want  int
		field string
	}{
		{"sizeof(SurgeRange)", rangeBoundsSize, 24, "_Static_assert in rt.h"},
		{"sizeof(SurgeRangeArrayIter)", arrayIterSize, 40, "_Static_assert in rt.h"},
		{"offsetof(SurgeRangeArrayIter, index)", arrayIterIndexOff, 24, "_Static_assert in rt.h"},
		{"offsetof(SurgeRangeArrayIter, length)", arrayIterLengthOff, 32, "_Static_assert in rt.h"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("%s: emitter says %d, %s says %d", c.name, c.got, c.field, c.want)
		}
	}

	// The cursor deliberately REUSES the two bound slots for its data pointer
	// and element stride, which is what lets a cursor reach a slice helper and
	// read as an unbounded range instead of decoding a data pointer as a bound.
	// rt.h's comment on `SurgeRange.kind` states that contract; these two are
	// what implement it.
	if arrayIterDataOff != rangeStartOff {
		t.Fatalf("the cursor's data pointer must occupy the start slot, got %d vs %d", arrayIterDataOff, rangeStartOff)
	}
	if arrayIterStrideOff != rangeEndOff {
		t.Fatalf("the cursor's stride must occupy the end slot, got %d vs %d", arrayIterStrideOff, rangeEndOff)
	}
}
