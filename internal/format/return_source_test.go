package format

import "testing"

func TestReturnSourceFormatting(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"named_empty_parens", "fn first(@return_source() a: &string) -> &string;\n", "fn first(@return_source a: &string) -> &string;\n"},
		{"callback_empty_parens", "type First = fn(@return_source() &string) -> &string;\n", "type First = fn(@return_source() &string) -> &string;\n"},
		{"nested", "type Nested = fn(fn(@return_source &string) -> &string) -> nothing;\n", "type Nested = fn(fn(@return_source &string) -> &string) -> nothing;\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			file, builder, fileID := parseSource(t, []byte(test.source))
			formatted, err := FormatFile(file, builder, fileID, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if string(formatted) != test.want {
				t.Fatalf("formatted = %q, want %q", formatted, test.want)
			}
			if ok, message := CheckRoundTrip(file, Options{}, 128); !ok {
				t.Fatalf("round trip failed: %s", message)
			}
		})
	}
}
