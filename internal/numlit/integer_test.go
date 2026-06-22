package numlit

import "testing"

func TestParseUint64SurgeIntegerBases(t *testing.T) {
	tests := []struct {
		text string
		want uint64
	}{
		{text: "0", want: 0},
		{text: "010", want: 10},
		{text: "1_000", want: 1000},
		{text: "0xFF", want: 255},
		{text: "0b1010", want: 10},
		{text: "0o7", want: 7},
	}

	for _, tt := range tests {
		got, ok := ParseUint64(tt.text)
		if !ok {
			t.Fatalf("ParseUint64(%q) failed", tt.text)
		}
		if got != tt.want {
			t.Fatalf("ParseUint64(%q): want %d, got %d", tt.text, tt.want, got)
		}
	}
}

func TestParseInt64SurgeIntegerBases(t *testing.T) {
	tests := []struct {
		text string
		want int64
	}{
		{text: "010", want: 10},
		{text: "-010", want: -10},
		{text: "0x7f", want: 127},
		{text: "-0b1010", want: -10},
		{text: "-0o10", want: -8},
	}

	for _, tt := range tests {
		got, ok := ParseInt64(tt.text)
		if !ok {
			t.Fatalf("ParseInt64(%q) failed", tt.text)
		}
		if got != tt.want {
			t.Fatalf("ParseInt64(%q): want %d, got %d", tt.text, tt.want, got)
		}
	}
}
