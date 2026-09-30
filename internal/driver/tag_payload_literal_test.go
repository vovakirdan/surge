package driver

import (
	"testing"

	"surge/internal/diag"
)

// A tag constructor written where a union is expected types an array-literal
// argument against the payload it lands in (see the runtime rows in
// internal/vm/tag_payload_literal_runtime_test.go). The retyping only builds
// what was already accepted: a literal that the payload did not accept as it
// stood stays refused, and a constructor with no expected union types its
// literal exactly as before.
func TestTagPayloadLiteralKeepsItsRefusals(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"empty_literal_with_no_expected_union", `@entrypoint
fn main() -> int {
    let o = Some([]);
    return 0;
}
`, "[]", diag.SemaLiteralNeedsType},
		{"fixed_payload_of_another_length", `fn f() -> Option<int[3]> { return Some([1, 2]); }
`, "return Some([1, 2])", diag.SemaTypeMismatch},
		// Implicit `__to` of a call argument needs `@allow_to`; a tag
		// constructor has none, so the payload does not reach for one.
		{"elements_that_would_need_an_implicit_to", `fn f() -> Option<int[]> { return Some(["7"]); }
`, `return Some(["7"])`, diag.SemaTypeMismatch},
		{"narrowing_elements", `fn f() -> Option<int8[]> { let a: int = 3; return Some([a]); }
`, "return Some([a])", diag.SemaTypeMismatch},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, row.text, row.code, row.snippet)
		})
	}
}

func TestTagPayloadLiteralAcceptsTheExpectedPayload(t *testing.T) {
	rows := []struct{ name, text string }{
		// An empty literal takes its element type from the payload, as it
		// does from a typed let (`let xs: int[] = []`).
		{"empty_literal_under_an_expected_union", `fn f() -> Option<int[]> { return Some([]); }
fn g() -> Option<int[]> { let o: Option<int[]> = Some([]); return o; }
`},
		{"nested_constructor", `fn f() -> Option<Option<string[]>> { return Some(Some([])); }
`},
		{"ternary_branch", `fn f(c: bool) -> Option<int[]> { return c ? Some([]) : nothing; }
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, row.text)
		})
	}
}
