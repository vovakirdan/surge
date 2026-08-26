package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/diag"
)

// taskBodyPrelude declares the task surface the rows need without the real
// stdlib prelude: a nominal Task<T> is enough, because the rows never await.
const taskBodyPrelude = `
type Task<T> = { __opaque: int };
`

// taskBodyDiagnostics runs parse + sema on the prelude plus src and returns
// every diagnostic of both stages (parse first), so a row can pin a parser
// code, a sema code, a severity, or a fix.
func taskBodyDiagnostics(t *testing.T, src string) []*diag.Diagnostic {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, taskBodyPrelude+src)
	items := append([]*diag.Diagnostic(nil), parseBag.Items()...)
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	return append(items, semaBag.Items()...)
}

// taskBodyCodes keys every diagnostic as "CODE/severity" so a row can tell a
// warning from an error under the same number.
func taskBodyCodes(items []*diag.Diagnostic) map[string]bool {
	codes := map[string]bool{}
	for _, d := range items {
		codes[d.Code.ID()+"/"+strings.ToLower(d.Severity.String())] = true
	}
	return codes
}

func findTaskBodyDiagnostic(items []*diag.Diagnostic, code diag.Code) *diag.Diagnostic {
	for _, d := range items {
		if d.Code == code {
			return d
		}
	}
	return nil
}

// Owner ruling 2026-08-26 (Q-C, RV2-DEBT-161): `ret` leaves the block with
// its value, `return` leaves the function, and an async/blocking body is a
// block. Every accepting row is paired with a mismatching twin so a vacuous
// pass (the analysis never reached the body) cannot go unnoticed.
func TestTaskBodyRetAndReturn(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // "CODE/severity" entries that must be present
		ban  []string // entries that must be absent
	}{
		// `ret` gives the body its value, and that value has a type.
		{"ret_in_async_value", `fn f() -> Task<int> { return async { ret 1; }; }`, nil, []string{"SEM3015/error", "SEM3134/error", "SEM3208/error"}},
		{"ret_in_async_typed", `fn f() -> Task<string> { return async { ret 1; }; }`, []string{"SEM3015/error"}, nil},
		{"ret_in_blocking_value", `fn f() -> Task<int> { return blocking { ret 1; }; }`, nil, []string{"SEM3015/error", "SEM3134/error", "SEM3208/error"}},
		{"ret_in_blocking_typed", `fn f() -> Task<string> { return blocking { ret 1; }; }`, []string{"SEM3015/error"}, nil},
		{"bare_ret_is_nothing", `fn f() -> Task<nothing> { return async { let x: int = 1; ret; }; }`, nil, []string{"SEM3015/error", "SEM3208/error"}},
		{"ret_inferred_without_expectation", `fn f() -> int { let t = async { ret "s"; }; let u: Task<int> = t; return 0; }`, []string{"SEM3015/error"}, nil},

		// `return` is refused in both bodies and names `ret`.
		{"return_in_async", `fn f() -> Task<int> { return async { return 42; }; }`, []string{"SEM3207/error"}, []string{"SEM3015/error", "SEM3208/error"}},
		{"return_in_blocking", `fn f() -> Task<int> { return blocking { return 42; }; }`, []string{"SEM3207/error"}, []string{"SEM3015/error", "SEM3208/error"}},
		{"return_in_if_in_async", `fn f(c: bool) -> Task<int> { return async { if c { return 1; } ret 2; }; }`, []string{"SEM3207/error"}, []string{"SEM3208/error"}},
		{"bare_return_in_async", `fn f() -> Task<nothing> { return async { return; }; }`, []string{"SEM3207/error"}, nil},

		// A block expression inside the body keeps its own `ret`: the inner
		// one yields the block, the outer one the body. The typed twin shows
		// the inner `ret` did not become the body's value.
		{"ret_in_block_expr_in_async", `fn f() -> Task<int> { return async { let v: int = { ret 5; }; ret v + 1; }; }`, nil, []string{"SEM3015/error", "SEM3207/error", "SEM3208/error"}},
		{"ret_in_block_expr_in_async_typed", `fn f() -> Task<string> { return async { let v: int = { ret 5; }; ret v; }; }`, []string{"SEM3015/error"}, nil},
		{"block_expr_ret_is_not_the_body_value", `fn f() -> Task<int> { return async { let v: int = { ret 5; }; }; }`, []string{"SEM3208/error"}, []string{"SEM3015/error"}},
		{"return_in_block_expr_in_async", `fn f() -> Task<int> { return async { let v: int = { return 5; }; ret v; }; }`, []string{"SEM3207/error"}, nil},

		// A nested body has its own exit.
		{"nested_async_ret", `fn f() -> Task<Task<int>> { return async { let inner: Task<int> = async { ret 7; }; ret inner; }; }`, nil, []string{"SEM3015/error", "SEM3207/error", "SEM3208/error"}},
		{"nested_async_ret_typed", `fn f() -> Task<Task<int>> { return async { let inner: Task<string> = async { ret 7; }; ret inner; }; }`, []string{"SEM3015/error"}, nil},
		{"nested_async_return", `fn f() -> Task<Task<int>> { return async { let inner: Task<int> = async { return 7; }; ret inner; }; }`, []string{"SEM3207/error"}, nil},
		{"nested_blocking_in_async_return", `fn f() -> Task<Task<int>> { return async { let inner: Task<int> = blocking { return 7; }; ret inner; }; }`, []string{"SEM3207/error"}, nil},

		// The `ret` sites must agree, and a valued body must `ret` on every path.
		{"ret_sites_disagree", `fn f(c: bool) -> Task<int> { return async { if c { ret 1; } ret "x"; }; }`, []string{"SEM3015/error"}, nil},
		{"ret_on_some_paths_only", `fn f(c: bool) -> Task<int> { return async { if c { ret 1; } }; }`, []string{"SEM3208/error"}, []string{"SEM3015/error"}},
		{"ret_on_every_path", `fn f(c: bool) -> Task<int> { return async { if c { ret 1; } else { ret 2; } }; }`, nil, []string{"SEM3208/error", "SEM3015/error"}},
		{"bare_ret_in_valued_body", `fn f(c: bool) -> Task<int> { return async { if c { ret; } ret 2; }; }`, []string{"SEM3015/error"}, nil},

		// A body without `ret` yields nothing: an error where a value is
		// expected (and no cascading mismatch), a warning where the last
		// statement computes a value and drops it, silence otherwise.
		{"no_ret_wants_value", `fn f() -> Task<int> { return async { let x: int = 1; }; }`, []string{"SEM3208/error"}, []string{"SEM3015/error"}},
		{"no_ret_nothing_ok", `fn f() -> Task<nothing> { return async { let x: int = 1; }; }`, nil, []string{"SEM3208/error", "SEM3208/warning", "SEM3015/error"}},
		{"discarded_tail_warns", `fn f() -> Task<nothing> { return async { 42; }; }`, []string{"SEM3208/warning"}, []string{"SEM3208/error", "SEM3015/error"}},
		{"discarded_tail_warns_unconstrained", `fn f() -> int { let t = blocking { 42; }; let u: Task<nothing> = t; return 0; }`, []string{"SEM3208/warning"}, []string{"SEM3208/error", "SEM3015/error"}},
		{"discarded_call_is_quiet", `fn g() -> int { return 1; } fn f() -> Task<nothing> { return async { g(); }; }`, nil, []string{"SEM3208/warning", "SEM3208/error"}},

		// A trailing `compare` is the one expression statement that may end
		// without `;`, so the parser accepts it and says nothing. It is still
		// not the body's value: where `Task<T>` is expected the body owes a
		// `ret`, whatever the arms are worth.
		{"compare_tail_wants_value", `fn f(c: bool) -> Task<int> { return async { compare c { true => 1; finally => 2; } }; }`, []string{"SEM3208/error"}, []string{"SEM3015/error"}},
		{"compare_tail_wants_value_semi", `fn f(c: bool) -> Task<int> { return async { compare c { true => 1; finally => 2; }; }; }`, []string{"SEM3208/error"}, []string{"SEM3015/error"}},
		{"compare_tail_other_type", `fn f(c: bool) -> Task<int> { return blocking { compare c { true => "a"; finally => "b"; } }; }`, []string{"SEM3208/error"}, []string{"SEM3015/error"}},
		{"compare_tail_warns_unconstrained", `fn f(c: bool) -> Task<nothing> { return async { compare c { true => 1; finally => 2; } }; }`, []string{"SEM3208/warning"}, []string{"SEM3208/error", "SEM3015/error"}},
		{"compare_tail_nothing_is_quiet", `fn f(c: bool) -> Task<nothing> { return async { compare c { true => nothing; finally => nothing; } }; }`, nil, []string{"SEM3208/warning", "SEM3208/error", "SEM3015/error"}},
		{"compare_tail_after_ret_is_quiet", `fn f(c: bool) -> Task<int> { return async { compare c { true => 1; finally => 2; } ret 3; }; }`, nil, []string{"SEM3208/warning", "SEM3208/error", "SEM3015/error"}},

		// `ret` at function level stays refused.
		{"ret_outside_block", `fn f() -> int { ret 1; }`, []string{"SEM3134/error"}, nil},
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
}

// The `return` -> `ret` fix is applied unasked only when the program it
// produces is proven well-typed: the returned value already has the type the
// body must yield, or nothing constrains the body's value.
func TestTaskBodyReturnFixApplicability(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want diag.FixApplicability
	}{
		{"matching_type", `fn f() -> Task<int> { return async { return 42; }; }`, diag.FixApplicabilityAlwaysSafe},
		{"unconstrained", `fn f() -> nothing { let t = async { return 42; }; return nothing; }`, diag.FixApplicabilityAlwaysSafe},
		{"mismatching_type", `fn f() -> Task<string> { return async { return 42; }; }`, diag.FixApplicabilityManualReview},
		{"bare_return_wants_value", `fn f() -> Task<int> { return async { return; }; }`, diag.FixApplicabilityManualReview},
		{"bare_return_nothing", `fn f() -> Task<nothing> { return blocking { return; }; }`, diag.FixApplicabilityAlwaysSafe},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := taskBodyDiagnostics(t, tc.src)
			d := findTaskBodyDiagnostic(items, diag.SemaTaskBodyReturn)
			if d == nil {
				t.Fatalf("expected SEM3207, got: %s", joinCodes(taskBodyCodes(items)))
			}
			if !strings.Contains(d.Message, "write `ret <expr>;`") {
				t.Fatalf("the refusal must name the edit, got %q", d.Message)
			}
			if len(d.Fixes) != 1 || len(d.Fixes[0].Edits) != 1 {
				t.Fatalf("expected one single-edit fix, got %+v", d.Fixes)
			}
			edit := d.Fixes[0].Edits[0]
			if edit.NewText != "ret" || edit.OldText != "return" {
				t.Fatalf("expected the keyword replaced, got %+v", edit)
			}
			if d.Fixes[0].Applicability != tc.want {
				t.Fatalf("applicability: got %s, want %s", d.Fixes[0].Applicability, tc.want)
			}
		})
	}
}

// A body that never `ret`s where a value is expected names the edit, and
// offers it as a fix only when the discarded last value already has the
// expected type.
func TestTaskBodyNoValueFix(t *testing.T) {
	items := taskBodyDiagnostics(t, `fn f() -> Task<int> { return async { let x: int = 1; x + 1; }; }`)
	d := findTaskBodyDiagnostic(items, diag.SemaTaskBodyNoValue)
	if d == nil || d.Severity != diag.SevError {
		t.Fatalf("expected a SEM3208 error, got: %s", joinCodes(taskBodyCodes(items)))
	}
	if len(d.Fixes) != 1 || len(d.Fixes[0].Edits) != 1 || d.Fixes[0].Edits[0].NewText != "ret " {
		t.Fatalf("expected a single `ret ` insertion, got %+v", d.Fixes)
	}
	if d.Fixes[0].Applicability != diag.FixApplicabilityAlwaysSafe {
		t.Fatalf("a matching tail makes the edit always safe, got %s", d.Fixes[0].Applicability)
	}
	items = taskBodyDiagnostics(t, `fn f() -> Task<int> { return async { let x: int = 1; "s"; }; }`)
	d = findTaskBodyDiagnostic(items, diag.SemaTaskBodyNoValue)
	if d == nil || len(d.Fixes) != 0 {
		t.Fatalf("a tail of another type must not be offered as the value, got %+v", d)
	}
}

// The discarded-tail warning offers `ret` but never applies it unasked: whether
// the value was the body's is the author's call.
func TestTaskBodyDiscardedTailFixIsManual(t *testing.T) {
	items := taskBodyDiagnostics(t, `fn f() -> Task<nothing> { return async { 42; }; }`)
	d := findTaskBodyDiagnostic(items, diag.SemaTaskBodyNoValue)
	if d == nil || d.Severity != diag.SevWarning {
		t.Fatalf("expected a SEM3208 warning, got: %s", joinCodes(taskBodyCodes(items)))
	}
	if len(d.Fixes) != 1 || len(d.Fixes[0].Edits) != 1 || d.Fixes[0].Edits[0].NewText != "ret " {
		t.Fatalf("expected a single `ret ` insertion, got %+v", d.Fixes)
	}
	if d.Fixes[0].Applicability != diag.FixApplicabilityManualReview {
		t.Fatalf("expected a manual-review fix, got %s", d.Fixes[0].Applicability)
	}
}

// A trailing `compare` is the one expression statement the parser lets end
// without `;`, so the `ret` edit it is offered has to supply that `;` too:
// `ret compare c { ... };`.
func TestTaskBodyCompareTailFix(t *testing.T) {
	items := taskBodyDiagnostics(t, `fn f(c: bool) -> Task<int> { return async { compare c { true => 1; finally => 2; } }; }`)
	d := findTaskBodyDiagnostic(items, diag.SemaTaskBodyNoValue)
	if d == nil || d.Severity != diag.SevError {
		t.Fatalf("expected a SEM3208 error, got: %s", joinCodes(taskBodyCodes(items)))
	}
	if len(d.Fixes) != 1 || len(d.Fixes[0].Edits) != 2 {
		t.Fatalf("expected one fix editing both ends, got %+v", d.Fixes)
	}
	if d.Fixes[0].Edits[0].NewText != "ret " || d.Fixes[0].Edits[1].NewText != ";" {
		t.Fatalf("expected `ret ` before and `;` after, got %+v", d.Fixes[0].Edits)
	}
	// A tail of another type is not the body's value, so no edit is offered.
	items = taskBodyDiagnostics(t, `fn f(c: bool) -> Task<int> { return async { compare c { true => "a"; finally => "b"; } }; }`)
	d = findTaskBodyDiagnostic(items, diag.SemaTaskBodyNoValue)
	if d == nil || len(d.Fixes) != 0 {
		t.Fatalf("a tail of another type must not be offered as the value, got %+v", d)
	}
}
