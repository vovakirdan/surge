package numlit

import "testing"

func TestInlineIntUsesFullLiteralText(t *testing.T) {
	for _, row := range []struct {
		name, text  string
		value, want int64
		inline      bool
	}{
		{"zero", "0", 7, 0, true},
		{"min", "-4611686018427387904", 0, FixiMin, true},
		{"max", "4611686018427387903", 0, FixiMax, true},
		{"below-min", "-4611686018427387905", 0, 0, false},
		{"above-max", "4611686018427387904", 0, 0, false},
		{"beyond-int64", "18446744073709551616", 0, 0, false},
		{"below-int64", "-18446744073709551616", 0, 0, false},
		{"hex", "0x3fff_ffff_ffff_ffff", 0, FixiMax, true},
		{"binary", "-0b1000", 0, -8, true},
		{"octal", "0o17", 0, 15, true},
		{"decimal-leading-zero", "010", 0, 10, true},
		{"malformed", "not-a-number", 0, 0, false},
		{"synthesized-min", "", FixiMin, FixiMin, true},
		{"synthesized-heap", "", FixiMax + 1, 0, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, ok := InlineInt(row.text, row.value)
			if ok != row.inline || (ok && got != row.want) {
				t.Fatalf("InlineInt(%q,%d)=(%d,%v), want (%d,%v)", row.text, row.value, got, ok, row.want, row.inline)
			}
		})
	}
}

func TestInlineUintUsesFullLiteralText(t *testing.T) {
	for _, row := range []struct {
		name, text  string
		value, want uint64
		inline      bool
	}{
		{"zero", "0", 7, 0, true},
		{"max", "9223372036854775807", 0, FixuMax, true},
		{"above-max", "9223372036854775808", 0, 0, false},
		{"beyond-uint64", "18446744073709551616", 0, 0, false},
		{"negative", "-1", 0, 0, false},
		{"hex", "0x7fff_ffff_ffff_ffff", 0, FixuMax, true},
		{"binary", "0b1000", 0, 8, true},
		{"octal", "0o17", 0, 15, true},
		{"decimal-leading-zero", "010", 0, 10, true},
		{"malformed", "not-a-number", 0, 0, false},
		{"synthesized-max", "", FixuMax, FixuMax, true},
		{"synthesized-heap", "", FixuMax + 1, 0, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, ok := InlineUint(row.text, row.value)
			if ok != row.inline || (ok && got != row.want) {
				t.Fatalf("InlineUint(%q,%d)=(%d,%v), want (%d,%v)", row.text, row.value, got, ok, row.want, row.inline)
			}
		})
	}
}
