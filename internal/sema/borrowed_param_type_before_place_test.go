package sema

import (
	"sort"
	"strings"
	"testing"
)

// A borrowed parameter answers the argument's TYPE before its PLACE. Once
// `print` took `&string` (owner addendum to RV2-DEBT-258, 2026-08-26),
// `print(42)` stopped saying `expected string, got int` and said `cannot take
// reference to temporary value; bind it to a variable first` instead - the
// addressability check ran first and hid the mismatch, sending the user to
// bind an int that no binding would make a string. matchArgument now asks
// conversionCost before isAddressableExpr; the place refusal stays for an
// argument the parameter could have borrowed.
//
// The harness is taskCloneCodes; the clean row asserts NO error so a prelude
// that fails to type-check cannot pass vacuously, and the refused rows beside
// it prove the same prelude reaches the call check.
const borrowedParamPrelude = `
fn peek(s: &string) -> nothing { return nothing; }
fn poke(s: &mut string) -> nothing { return nothing; }
fn make() -> string { return "made"; }
`

func TestBorrowedParamChecksTypeBeforePlace(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // every error code the program must report; nil means clean
	}{
		// A wrong type behind a shared borrow is a mismatch, not a place.
		{"int_literal_to_shared_string", `fn f() -> nothing { peek(42); return nothing; }`, []string{"SEM3015"}},
		{"int_binding_to_shared_string", `fn f() -> nothing { let n = 42; peek(n); return nothing; }`, []string{"SEM3015"}},
		// The same behind a mutable borrow.
		{"int_literal_to_mut_string", `fn f() -> nothing { poke(42); return nothing; }`, []string{"SEM3015"}},
		// A temporary of the RIGHT type behind a mutable borrow is still a
		// place refusal: nothing would be mutated.
		{"string_temporary_to_mut_string", `fn f() -> nothing { poke(make()); return nothing; }`, []string{"SEM3023"}},
		// The legal forms.
		{"string_binding_to_shared_string", `fn f() -> nothing { let s = "x"; peek(s); peek(s); return nothing; }`, nil},
		{"string_temporary_to_shared_string", `fn f() -> nothing { peek(make()); return nothing; }`, nil},
		{"string_binding_to_mut_string", `fn f() -> nothing { let mut s = "x"; poke(&mut s); return nothing; }`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sortedCodes(taskCloneCodes(t, borrowedParamPrelude+tc.src))
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected a clean program, got %v", got)
				}
				return
			}
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				if len(got) == 0 {
					t.Fatalf("expected %v, got clean", want)
				}
				t.Fatalf("expected %v, got %v", want, got)
			}
		})
	}
}
