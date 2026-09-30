package driver

import (
	"testing"

	"surge/internal/diag"
)

const wrappedArgumentPrelude = wrappedProjectionPrelude + `fn firsts(s: &string[]) -> &string { return &s[0]; }
fn bad(o: Option<&mut string>, k: &mut string[]) -> nothing { apps(k); compare o { Some(v) => { print(*v); } nothing => {} }; return nothing; }
fn bads(o: Option<&string>, k: &mut string[]) -> nothing { apps(k); compare o { Some(v) => { print(*v); } nothing => {} }; return nothing; }
fn badt<T>(t: T, k: &mut string[]) -> T { apps(k); return t; }
fn bad2(o: Option<&mut string>, n: int) -> nothing { compare o { Some(v) => { print(*v); } nothing => {} }; return nothing; }
fn bare2(o: &mut string, n: int) -> nothing { print(*o); return nothing; }
fn bare2x(o: &mut string, k: &mut string[]) -> nothing { apps(k); print(*o); return nothing; }
fn badsb(o: &string, k: &mut string[]) -> nothing { apps(k); print(*o); return nothing; }
type Box = { n: int };
extern<Box> {
    fn bad(self: &Box, o: Option<&mut string>, k: &mut string[]) -> nothing { apps(k); compare o { Some(v) => { print(*v); } nothing => {} }; return nothing; }
}
`

// A reference carried by a value handed to a by-value parameter is a
// reference argument of the call, as the bare one is: the callee must not
// get both it and its container, and a later argument must not free what it
// points into.
func TestWrappedReferenceArgumentIsCheckedWithTheOthers(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"wrapper_then_its_container", `fn g(k: &mut string[]) -> int {
    bad(Some::<&mut string>(firstsm(k)), k);
    return 0;
}
`, "k);\n    return", diag.SemaBorrowConflict},
		{"shared_wrapper_then_its_container", `fn g(k: &mut string[]) -> int {
    bads(Some::<&string>(firsts(k)), k);
    return 0;
}
`, "k);\n    return", diag.SemaBorrowConflict},
		{"method_wrapper_then_its_container", `fn g(k: &mut string[]) -> int {
    let bx = Box{ n: 1 };
    bx.bad(Some::<&mut string>(firstsm(k)), k);
    return 0;
}
`, "k);\n    return", diag.SemaBorrowConflict},
		{"wrapper_then_a_later_argument_grows_it", `fn g(k: &mut string[]) -> int {
    bad2(Some::<&mut string>(firstsm(k)), appsr(k));
    return 0;
}
`, "k));\n    return", diag.SemaBorrowConflict},
		{"ternary_wrapper_then_its_container", `fn g(k: &mut string[], c: bool) -> int {
    bad(c ? Some::<&mut string>(firstsm(k)) : nothing, k);
    return 0;
}
`, "k);\n    return", diag.SemaBorrowConflict},
		{"ternary_shared_wrapper_then_its_container", `fn g(k: &mut string[], c: bool) -> int {
    bads(c ? Some::<&string>(firsts(k)) : nothing, k);
    return 0;
}
`, "k);\n    return", diag.SemaBorrowConflict},
		{"compare_shared_wrapper_then_its_container", `fn g(k: &mut string[], q: int) -> int {
    bads(compare q { 1 => Some::<&string>(firsts(k)); _ => nothing; }, k);
    return 0;
}
`, "k);\n    return", diag.SemaBorrowConflict},
		{"generic_ternary_wrapper_then_its_container", `fn g(k: &mut string[], c: bool) -> int {
    let r = badt(c ? Some::<&string>(firsts(k)) : nothing, k);
    return 0;
}
`, "k);\n    return", diag.SemaBorrowConflict},
		{"ternary_shared_element_then_its_container", `fn g(k: &mut string[], z: &mut string[], c: bool) -> int {
    badsb(c ? firsts(k) : firsts(z), k);
    return 0;
}
`, "k);\n    return", diag.SemaBorrowConflict},
		{"ternary_mut_element_then_its_container", `fn g(k: &mut string[], z: &mut string[], c: bool) -> int {
    bare2x(c ? firstsm(k) : firstsm(z), k);
    return 0;
}
`, "k);\n    return", diag.SemaBorrowConflict},
		{"bare_element_then_a_later_argument_grows_it", `fn g(k: &mut string[]) -> int {
    bare2(firstsm(k), appsr(k));
    return 0;
}
`, "k));\n    return", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, wrappedArgumentPrelude+row.text, row.code, row.snippet)
		})
	}
}

func TestWrappedReferenceArgumentControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"wrapper_then_a_disjoint_container", `fn g(k: &mut string[], z: &mut string[]) -> int {
    bad(Some::<&mut string>(firstsm(z)), k);
    return 0;
}
`},
		{"wrapper_then_a_later_argument_grows_another", `fn g(k: &mut string[], z: &mut string[]) -> int {
    bad2(Some::<&mut string>(firstsm(z)), appsr(k));
    return 0;
}
`},
		{"bare_element_then_a_later_argument_grows_another", `fn g(k: &mut string[], z: &mut string[]) -> int {
    bare2(firstsm(z), appsr(k));
    return 0;
}
`},
		{"ternary_wrapper_over_a_disjoint_container", `fn g(k: &mut string[], z: &mut string[], c: bool) -> int {
    bads(c ? Some::<&string>(firsts(z)) : nothing, k);
    return 0;
}
`},
		{"ternary_elements_of_disjoint_containers", `fn g(k: &mut string[], z: &mut string[], y: &mut string[], c: bool) -> int {
    badsb(c ? firsts(y) : firsts(z), k);
    return 0;
}
`},
		{"growth_first_then_the_wrapper", `fn g(k: &mut string[]) -> int {
    let n = appsr(k);
    bad2(Some::<&mut string>(firstsm(k)), n);
    return 0;
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, wrappedArgumentPrelude+row.text)
		})
	}
}
