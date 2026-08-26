package sema

import "testing"

// A parameter that converts its argument on the way in (`@allow_to`) and only
// BORROWS it (`&string`) takes a value of another type through the conversion:
// `print(1)` is `__to(1, string)` read once, the temporary released with the
// statement. Three things had to hold at once for that sentence to be true,
// and each was false before:
//
//   - the conversion targets the REFERENT, not `&string` (there is no
//     `__to(int, &string)`, and the candidate filter refused wrapper targets);
//   - a converted argument has no place of its own, so the borrow reads the
//     temporary the conversion produced instead of failing "not addressable";
//   - `int` reaches `__to` twice, once as `int` and once as `&int` through
//     autoref, and that pair is not an ambiguity: the receiver of the source's
//     own kind wins.
const allowToBorrowedPrelude = `
extern<int> {
    fn __to(self: int, _: string) -> string { return "int"; }
    @overload fn __to(self: &int, _: string) -> string { return "ref-int"; }
}
extern<float> {
    fn __to(self: float, _: string) -> string { return "float"; }
}
fn show(@allow_to s: &string) -> nothing {
}
`

func TestAllowToBorrowedParamConvertsItsArgument(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"int_literal", `fn f() -> nothing { show(1); }`},
		{"int_place", `fn f() -> nothing { let n: int = 7; show(n); }`},
		{"float_literal", `fn f() -> nothing { show(2.5); }`},
		{"string_stays_a_borrow", `fn f() -> nothing { let s: string = "x"; show(s); show(s); }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, allowToBorrowedPrelude+tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if semaBag.HasErrors() {
				t.Fatalf("expected a clean program, got %s", diagnosticsSummary(semaBag))
			}
		})
	}
}

// The converted temporary is the statement's to release: one temporary per
// converted argument, none for a string that was only borrowed.
func TestAllowToBorrowedParamReleasesTheConvertedTemporary(t *testing.T) {
	count := tempDropCount(t, allowToBorrowedPrelude+`
fn f() -> nothing {
    show(1);
    let n: int = 7;
    show(n);
    let s: string = "x";
    show(s);
}
`)
	if count != 2 {
		t.Fatalf("expected the two converted arguments flagged as temporaries, got %d", count)
	}
}

// Two receivers of one conversion are not two conversions. When only the
// `&int` receiver exists the value still converts through autoref.
func TestImplicitToPrefersTheReceiverOfTheSourcesKind(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
extern<int> {
    fn __to(self: &int, _: string) -> string { return "ref-int"; }
}
fn show(@allow_to s: &string) -> nothing {
}
fn f() -> nothing { show(1); }
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if semaBag.HasErrors() {
		t.Fatalf("a lone &int receiver must still convert a value: %s", diagnosticsSummary(semaBag))
	}
}
