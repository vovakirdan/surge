package driver

import (
	"testing"

	"surge/internal/diag"
)

const wrappedReferentPrelude = aliasThroughRefPrelude + `fn mkM() -> Map<string, string> { let mut m = Map::<string, string>.new(); m.insert(heap("k"), heap("v")); return m; }
fn insm(m: &mut Map<string, string>) -> nothing {
    let mut i = 0;
    while i < 50 { m.insert(heap("k") + (i to string), heap("w")); i = i + 1; }
    return nothing;
}
fn firsts(s: &string[]) -> &string { return &s[0]; }

`

// A shared reference reached through a reference parameter -- a map entry from
// `k.get_ref(&kk)`, an element from `firsts(xs)` -- held a loan on the
// referent when bound, but not when handed to a by-value constructor argument
// first: a grow through the parameter was accepted and
// the payload read the moved entry (valgrind: invalid read).
func TestReferentCarriedByAWrapperIsHeld(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"map_entry_in_a_constructor_then_inserted", `fn g(k: &mut Map<string, string>) -> int {
    let kk = heap("k");
    let e = Some(k.get_ref(&kk));
    insm(k);
    compare e { Some(Some(v)) => { print(*v); } _ => {} };
    return 0;
}
`, "k);\n    compare", diag.SemaBorrowConflict},
		{"element_in_a_constructor_then_grown", `fn g(xs: &mut string[]) -> int {
    let e = Some::<&string>(firsts(xs));
    apps(xs);
    compare e { Some(v) => { print(*v); } nothing => {} };
    return 0;
}
`, "xs);\n    compare", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, wrappedReferentPrelude+row.text, row.code, row.snippet)
		})
	}
}

func TestReferentCarriedByAWrapperControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"copied_length", `fn g(xs: &mut string[]) -> int {
    let t = (firsts(xs).__len() to int, 1);
    apps(xs);
    return t.0;
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, wrappedReferentPrelude+row.text)
		})
	}
}
