package driver

import (
	"testing"

	"surge/internal/diag"
)

const wrappedProjectionPrelude = aliasThroughRefPrelude + `fn firstsm(s: &mut string[]) -> &mut string { return &mut s[0]; }

`

// A `&mut` a call returns into what its argument points at, built into a
// value that carries it -- `Some::<&mut T>(call)`, `Some(p)`, a choice, or an
// annotated `let` that wraps it implicitly --
// held no loan: the wrapper's binding is not a reference, and a reference
// parameter stands on no loan of this frame. A grow or a replace was accepted
// and the payload then read freed memory (valgrind: invalid read). The binding
// now holds the loans the bare binding `let e = firstsm(k)` holds.
func TestProjectionCarriedByAWrapperIsHeld(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"explicit_some_of_a_call_then_grown", `fn g(k: &mut string[]) -> int {
    let e = Some::<&mut string>(firstsm(k));
    apps(k);
    compare e { Some(v) => { print(*v); } nothing => {} };
    return 0;
}
`, "k);\n    compare", diag.SemaBorrowConflict},
		{"explicit_some_of_a_call_then_replaced", `fn g(k: &mut string[]) -> int {
    let e = Some::<&mut string>(firstsm(k));
    *k = [heap("q")];
    compare e { Some(v) => { print(*v); } nothing => {} };
    return 0;
}
`, "*k = [heap", diag.SemaBorrowMutation},
		{"some_of_a_reference_binding_then_grown", `fn g(k: &mut string[]) -> int {
    let mut e: Option<&mut string> = nothing;
    {
        let p = firstsm(k);
        e = Some::<&mut string>(p);
    }
    apps(k);
    compare e { Some(v) => { print(*v); } nothing => {} };
    return 0;
}
`, "k);\n    compare", diag.SemaBorrowConflict},
		{"annotated_let_wraps_implicitly_then_grown", `fn g(k: &mut string[]) -> int {
    let e: Option<&mut string> = firstsm(k);
    apps(k);
    compare e { Some(v) => { print(*v); } nothing => {} };
    return 0;
}
`, "k);\n    compare", diag.SemaBorrowConflict},
		{"ternary_of_a_wrapper_then_grown", `fn g(k: &mut string[], c: bool) -> int {
    let e = c ? Some::<&mut string>(firstsm(k)) : nothing;
    apps(k);
    compare e { Some(v) => { print(*v); } nothing => {} };
    return 0;
}
`, "k);\n    compare", diag.SemaBorrowConflict},
		{"int_element_wrapper_assigned_then_grown", `fn g(k: &mut int[]) -> int {
    let mut e: Option<&mut int> = nothing;
    e = Some::<&mut int>(firstm(k));
    app(k);
    compare e { Some(v) => { *v = 5; } nothing => {} };
    return 0;
}
`, "k);\n    compare", diag.SemaBorrowConflict},
		{"wrapper_as_a_compare_scrutinee_grown_in_its_arm", `fn g(k: &mut string[]) -> int {
    compare Some::<&mut string>(firstsm(k)) { Some(v) => { apps(k); print(*v); } nothing => {} };
    return 0;
}
`, "k); print", diag.SemaBorrowConflict},
		{"call_as_a_compare_scrutinee_grown_in_its_arm", `fn g(k: &mut string[]) -> int {
    compare firstsm(k) { v => { apps(k); print(*v); } };
    return 0;
}
`, "k); print", diag.SemaBorrowConflict},
		{"wrapper_left_a_loop_by_ret_then_grown", `fn g(r: &mut int[]) -> int {
    let e = {
        let mut i = 0;
        while i < 3 { if i == 1 { ret Some::<&mut int>(firstm(r)); } i = i + 1; }
        ret Some::<&mut int>(firstm(r));
    };
    app(r);
    compare e { Some(v) => { *v = 5; } nothing => {} };
    return 0;
}
`, "r);\n    compare", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, wrappedProjectionPrelude+row.text, row.code, row.snippet)
		})
	}
}

// The wrapper holds what its payload points into, for as long as the wrapper
// lives, and nothing else.
func TestProjectionCarriedByAWrapperControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"wrapper_dropped_before_the_grow", `fn g(k: &mut string[]) -> int {
    {
        let e = Some::<&mut string>(firstsm(k));
        compare e { Some(v) => { print(*v); } nothing => {} };
    }
    apps(k);
    return 0;
}
`},
		{"disjoint_container_grown", `fn g(k: &mut string[], z: &mut string[]) -> int {
    let e = Some::<&mut string>(firstsm(z));
    apps(k);
    compare e { Some(v) => { print(*v); } nothing => {} };
    return 0;
}
`},
		{"copied_value", `fn g(k: &mut int[]) -> int {
    let e = Some(*firstm(k));
    app(k);
    compare e { Some(v) => { print(v to string); } nothing => {} };
    return 0;
}
`},
		{"wrappers_left_a_loop_by_ret_are_one_choice", `fn g(r: &mut int[]) -> int {
    let e = {
        let mut i = 0;
        while i < 3 { if i == 1 { ret Some::<&mut int>(firstm(r)); } i = i + 1; }
        ret Some::<&mut int>(firstm(r));
    };
    compare e { Some(v) => { *v = 5; } nothing => {} };
    return 0;
}
`},
		{"bare_results_left_a_loop_by_ret", `fn g(r: &mut int[]) -> int {
    let e: &mut int = {
        let mut i = 0;
        while i < 3 { if i == 1 { ret firstm(r); } i = i + 1; }
        ret firstm(r);
    };
    *e = 5;
    return 0;
}
`},
		{"cloned_string", `fn g(k: &mut string[]) -> int {
    let e = Some(clone(firstsm(k)));
    apps(k);
    compare e { Some(v) => { print(v); } nothing => {} };
    return 0;
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, wrappedProjectionPrelude+row.text)
		})
	}
}
