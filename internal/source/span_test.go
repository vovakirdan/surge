package source

import (
	"testing"
)

func TestSpan_ShiftLeft(t *testing.T) {
	tests := []struct {
		name     string
		span     Span
		shift    uint32
		expected Span
	}{
		{
			name:     "shift normal span left by 5",
			span:     Span{File: 1, Start: 10, End: 20},
			shift:    5,
			expected: Span{File: 1, Start: 5, End: 15},
		},
		{
			name:     "shift span left by 0",
			span:     Span{File: 1, Start: 10, End: 20},
			shift:    0,
			expected: Span{File: 1, Start: 10, End: 20},
		},
		{
			name:     "shift equals start - boundary case",
			span:     Span{File: 1, Start: 10, End: 20},
			shift:    10,
			expected: Span{File: 1, Start: 0, End: 10},
		},
		{
			name:     "shift larger than start - returns original",
			span:     Span{File: 1, Start: 10, End: 20},
			shift:    15,
			expected: Span{File: 1, Start: 10, End: 20},
		},
		{
			name:     "shift much larger than start",
			span:     Span{File: 1, Start: 5, End: 10},
			shift:    100,
			expected: Span{File: 1, Start: 5, End: 10},
		},
		{
			name:     "shift span at position 0",
			span:     Span{File: 1, Start: 0, End: 10},
			shift:    5,
			expected: Span{File: 1, Start: 0, End: 10},
		},
		{
			name:     "shift zero-length span",
			span:     Span{File: 1, Start: 10, End: 10},
			shift:    3,
			expected: Span{File: 1, Start: 7, End: 7},
		},
		{
			name:     "shift by 1",
			span:     Span{File: 2, Start: 100, End: 150},
			shift:    1,
			expected: Span{File: 2, Start: 99, End: 149},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.span.ShiftLeft(tt.shift)
			if result != tt.expected {
				t.Errorf("ShiftLeft() = %+v, want %+v", result, tt.expected)
			}
			// Verify file ID is preserved
			if result.File != tt.span.File {
				t.Errorf("File ID changed: got %d, want %d", result.File, tt.span.File)
			}
		})
	}
}

func TestSpan_ShiftRight(t *testing.T) {
	tests := []struct {
		name     string
		span     Span
		shift    uint32
		expected Span
	}{
		{
			name:     "shift normal span right by 5",
			span:     Span{File: 1, Start: 10, End: 20},
			shift:    5,
			expected: Span{File: 1, Start: 15, End: 25},
		},
		{
			name:     "shift span right by 0",
			span:     Span{File: 1, Start: 10, End: 20},
			shift:    0,
			expected: Span{File: 1, Start: 10, End: 20},
		},
		{
			name:     "shift equals span length - boundary case",
			span:     Span{File: 1, Start: 10, End: 20},
			shift:    10,
			expected: Span{File: 1, Start: 20, End: 30},
		},
		{
			name:     "shift larger than span length - returns original",
			span:     Span{File: 1, Start: 10, End: 20},
			shift:    11,
			expected: Span{File: 1, Start: 10, End: 20},
		},
		{
			name:     "shift much larger than span length",
			span:     Span{File: 1, Start: 5, End: 10},
			shift:    100,
			expected: Span{File: 1, Start: 5, End: 10},
		},
		{
			name:     "shift zero-length span",
			span:     Span{File: 1, Start: 10, End: 10},
			shift:    5,
			expected: Span{File: 1, Start: 10, End: 10},
		},
		{
			name:     "shift by 1",
			span:     Span{File: 2, Start: 100, End: 150},
			shift:    1,
			expected: Span{File: 2, Start: 101, End: 151},
		},
		{
			name:     "shift large span",
			span:     Span{File: 1, Start: 0, End: 1000},
			shift:    500,
			expected: Span{File: 1, Start: 500, End: 1500},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.span.ShiftRight(tt.shift)
			if result != tt.expected {
				t.Errorf("ShiftRight() = %+v, want %+v", result, tt.expected)
			}
			// Verify file ID is preserved
			if result.File != tt.span.File {
				t.Errorf("File ID changed: got %d, want %d", result.File, tt.span.File)
			}
		})
	}
}

func TestSpan_ZeroideToStart(t *testing.T) {
	tests := []struct {
		name     string
		span     Span
		expected Span
	}{
		{
			name:     "normal span",
			span:     Span{File: 1, Start: 10, End: 20},
			expected: Span{File: 1, Start: 10, End: 10},
		},
		{
			name:     "already zero-length span",
			span:     Span{File: 1, Start: 15, End: 15},
			expected: Span{File: 1, Start: 15, End: 15},
		},
		{
			name:     "span at position 0",
			span:     Span{File: 2, Start: 0, End: 100},
			expected: Span{File: 2, Start: 0, End: 0},
		},
		{
			name:     "large span",
			span:     Span{File: 3, Start: 1000, End: 5000},
			expected: Span{File: 3, Start: 1000, End: 1000},
		},
		{
			name:     "single character span",
			span:     Span{File: 1, Start: 42, End: 43},
			expected: Span{File: 1, Start: 42, End: 42},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.span.ZeroideToStart()
			if result != tt.expected {
				t.Errorf("ZeroideToStart() = %+v, want %+v", result, tt.expected)
			}
			// Verify zero-length invariant
			if result.Start != result.End {
				t.Errorf("Result is not zero-length: Start=%d, End=%d", result.Start, result.End)
			}
			// Verify file ID is preserved
			if result.File != tt.span.File {
				t.Errorf("File ID changed: got %d, want %d", result.File, tt.span.File)
			}
		})
	}
}

func TestSpan_ZeroideToEnd(t *testing.T) {
	tests := []struct {
		name     string
		span     Span
		expected Span
	}{
		{
			name:     "normal span",
			span:     Span{File: 1, Start: 10, End: 20},
			expected: Span{File: 1, Start: 20, End: 20},
		},
		{
			name:     "already zero-length span",
			span:     Span{File: 1, Start: 15, End: 15},
			expected: Span{File: 1, Start: 15, End: 15},
		},
		{
			name:     "span at position 0",
			span:     Span{File: 2, Start: 0, End: 100},
			expected: Span{File: 2, Start: 100, End: 100},
		},
		{
			name:     "large span",
			span:     Span{File: 3, Start: 1000, End: 5000},
			expected: Span{File: 3, Start: 5000, End: 5000},
		},
		{
			name:     "single character span",
			span:     Span{File: 1, Start: 42, End: 43},
			expected: Span{File: 1, Start: 43, End: 43},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.span.ZeroideToEnd()
			if result != tt.expected {
				t.Errorf("ZeroideToEnd() = %+v, want %+v", result, tt.expected)
			}
			// Verify zero-length invariant
			if result.Start != result.End {
				t.Errorf("Result is not zero-length: Start=%d, End=%d", result.Start, result.End)
			}
			// Verify file ID is preserved
			if result.File != tt.span.File {
				t.Errorf("File ID changed: got %d, want %d", result.File, tt.span.File)
			}
		})
	}
}

// TestSpan_ChainedOperations tests combinations of span operations
func TestSpan_ChainedOperations(t *testing.T) {
	tests := []struct {
		name     string
		initial  Span
		ops      func(Span) Span
		expected Span
	}{
		{
			name:    "shift left then zeroide to start",
			initial: Span{File: 1, Start: 20, End: 30},
			ops: func(s Span) Span {
				return s.ShiftLeft(5).ZeroideToStart()
			},
			expected: Span{File: 1, Start: 15, End: 15},
		},
		{
			name:    "shift right then zeroide to end",
			initial: Span{File: 1, Start: 10, End: 20},
			ops: func(s Span) Span {
				return s.ShiftRight(5).ZeroideToEnd()
			},
			expected: Span{File: 1, Start: 25, End: 25},
		},
		{
			name:    "zeroide to start then shift right",
			initial: Span{File: 1, Start: 10, End: 20},
			ops: func(s Span) Span {
				return s.ZeroideToStart().ShiftRight(5)
			},
			expected: Span{File: 1, Start: 10, End: 10},
		},
		{
			name:    "multiple shifts",
			initial: Span{File: 1, Start: 50, End: 100},
			ops: func(s Span) Span {
				return s.ShiftLeft(10).ShiftRight(5)
			},
			expected: Span{File: 1, Start: 45, End: 95},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.ops(tt.initial)
			if result != tt.expected {
				t.Errorf("Chained operations = %+v, want %+v", result, tt.expected)
			}
		})
	}
}

// TestSpan_EdgeCases tests edge cases and potential overflow scenarios
func TestSpan_EdgeCases(t *testing.T) {
	t.Run("max uint32 values", func(t *testing.T) {
		maxSpan := Span{File: 1, Start: 0xFFFFFFFF, End: 0xFFFFFFFF}
		
		// ShiftLeft with max value
		result := maxSpan.ShiftLeft(1)
		if result.Start != 0xFFFFFFFE {
			t.Errorf("ShiftLeft(1) on max value: got Start=%d, want %d", result.Start, 0xFFFFFFFE)
		}
		
		// ShiftRight on zero-length at max should return original (shift > length)
		result = maxSpan.ShiftRight(1)
		if result != maxSpan {
			t.Errorf("ShiftRight(1) on zero-length max span should return original")
		}
	})

	t.Run("operations preserve file ID", func(t *testing.T) {
		fileIDs := []FileID{0, 1, 100, 0xFFFF}
		span := Span{Start: 10, End: 20}
		
		for _, fid := range fileIDs {
			span.File = fid
			
			if result := span.ShiftLeft(2); result.File != fid {
				t.Errorf("ShiftLeft changed FileID from %d to %d", fid, result.File)
			}
			if result := span.ShiftRight(2); result.File != fid {
				t.Errorf("ShiftRight changed FileID from %d to %d", fid, result.File)
			}
			if result := span.ZeroideToStart(); result.File != fid {
				t.Errorf("ZeroideToStart changed FileID from %d to %d", fid, result.File)
			}
			if result := span.ZeroideToEnd(); result.File != fid {
				t.Errorf("ZeroideToEnd changed FileID from %d to %d", fid, result.File)
			}
		}
	})
}
