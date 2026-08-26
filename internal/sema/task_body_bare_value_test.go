package sema

import (
	"testing"

	"surge/internal/diag"
)

// A bare trailing expression in an async/blocking body is refused where the
// mistake is visible — at the parser, which names `ret 42;` — and sema adds
// nothing on top: no missing-value error, no mismatch, no missing-`;`.
func TestTaskBodyBareValueIsDiagnosedOnce(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
		ban  []string
	}{
		{"blocking", `fn f() -> Task<int> { return blocking { 42 }; }`, []string{"SYN2037/error"}, []string{"SYN2012/error", "SEM3208/error", "SEM3208/warning", "SEM3015/error"}},
		{"async", `fn f() -> Task<int> { return async { 42 }; }`, []string{"SYN2037/error"}, []string{"SYN2012/error", "SEM3208/error", "SEM3015/error"}},
		// A nested block keeps the legacy block-value path: no SYN2037 there.
		{"inner_block", `fn f() -> Task<int> { return async { let v: int = { 5 }; ret v; }; }`, nil, []string{"SYN2037/error", "SEM3207/error", "SEM3208/error"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := taskBodyDiagnostics(t, tc.src)
			codes := taskBodyCodes(items)
			for _, w := range tc.want {
				if !codes[w] {
					t.Fatalf("expected %s, got: %s", w, joinCodes(codes))
				}
			}
			for _, b := range tc.ban {
				if codes[b] {
					t.Fatalf("did not expect %s, got: %s", b, joinCodes(codes))
				}
			}
		})
	}
	items := taskBodyDiagnostics(t, `fn f() -> Task<int> { return blocking { 40 + 2 }; }`)
	d := findTaskBodyDiagnostic(items, diag.SynTaskBodyBareValue)
	if d == nil || len(d.Fixes) != 1 || len(d.Fixes[0].Edits) != 2 || d.Fixes[0].Applicability != diag.FixApplicabilityAlwaysSafe {
		t.Fatalf("expected SYN2037 with one always-safe two-edit fix, got %+v", d)
	}
}
