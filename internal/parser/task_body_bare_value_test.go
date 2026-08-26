package parser

// `blocking { 42 }` / `async { 42 }`: a bare trailing expression in a task
// body is refused by the parser, the first stage that knows which body the
// expression ends, with the edit `ret 42;` (SynTaskBodyBareValue). Every
// other bare expression keeps the generic missing-`;` diagnostic, and every
// negative row pins that diagnostic so the parse is known to have reached
// the site.

import (
	"testing"

	"surge/internal/diag"
)

func TestTaskBodyBareValueNamesTheRetEdit(t *testing.T) {
	cases := []struct {
		name         string
		src          string
		wantBareBody bool
		wantMissing  bool // the generic SynExpectSemicolon instead
	}{
		{"blocking_bare", "fn f() -> Task<int> { return blocking { 42 }; }", true, false},
		{"async_bare", "fn f() -> Task<int> { return async { 40 + 2 }; }", true, false},
		{"async_bare_after_statements", "fn f() -> Task<int> { return async { let x: int = 1; x + 1 }; }", true, false},
		// A statement block inside the body is not the body.
		{"if_block_inside_body", "fn f(c: bool) -> Task<int> { return async { if c { 42 } ret 1; }; }", false, true},
		// A block expression inside the body keeps its legacy tail value.
		{"block_expr_inside_body", "fn f() -> Task<int> { return async { let v: int = { 5 }; ret v; }; }", false, false},
		// Bare but not last: the `;` really is missing.
		{"bare_not_last", "fn f() -> Task<int> { return async { 42 ret 1; }; }", false, true},
		{"semicolon_present", "fn f() -> Task<int> { return async { 42; }; }", false, false},
		{"function_body_tail", "fn f() -> int { 42 }", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, bag, _ := parseProgram(t, tc.src)
			if got := bagHasCode(bag, diag.SynTaskBodyBareValue); got != tc.wantBareBody {
				t.Fatalf("SYN2037 present=%v, want %v: %s", got, tc.wantBareBody, diagnosticsSummary(bag))
			}
			if got := bagHasCode(bag, diag.SynExpectSemicolon); got != tc.wantMissing {
				t.Fatalf("SYN2012 present=%v, want %v: %s", got, tc.wantMissing, diagnosticsSummary(bag))
			}
		})
	}
}

// The refusal spells the edit with the author's own expression and carries it
// as one always-safe fix: `ret ` before, `;` after.
func TestTaskBodyBareValueFix(t *testing.T) {
	_, bag, _ := parseProgram(t, "fn f() -> Task<int> { return blocking { 40 + 2 }; }")
	for _, d := range bag.Items() {
		if d.Code != diag.SynTaskBodyBareValue {
			continue
		}
		if want := "write `ret 40 + 2;`"; !containsText(d.Message, want) {
			t.Fatalf("message must spell the edit %q, got %q", want, d.Message)
		}
		if len(d.Fixes) != 1 || len(d.Fixes[0].Edits) != 2 {
			t.Fatalf("expected one fix with two edits (prefix and suffix), got %+v", d.Fixes)
		}
		if d.Fixes[0].Edits[0].NewText != "ret " || d.Fixes[0].Edits[1].NewText != ";" {
			t.Fatalf("expected `ret ` and `;` inserted, got %+v", d.Fixes[0].Edits)
		}
		if d.Fixes[0].Applicability != diag.FixApplicabilityAlwaysSafe {
			t.Fatalf("expected an always-safe fix, got %s", d.Fixes[0].Applicability)
		}
		return
	}
	t.Fatalf("expected SYN2037, got: %s", diagnosticsSummary(bag))
}

func containsText(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
