package driver

import (
	"testing"

	"surge/internal/diag"
)

// Three more ways the same holder outlived a grow (valgrind: invalid read and
// write): an alias of several references one of which stands on a loan
// (`pickm(r, a)` with `a = idam(s)`, or `pickm(r, &mut y)`) inherited that one
// loan and forgot r; an alias chosen by a compare or a ternary was not walked
// to its sources; and a `&mut` a call returned INTO the array (`firstm(r)`)
// stood on the whole referent, where a grow through r or through an alias
// was excused as part of its own chain.
func TestAliasHoldsEverySourceAndAnInteriorReferenceHoldsAnElement(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"alias_of_a_parameter_and_a_rooted_alias", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let a = idam(s);
    let q = pickm(r, a);
    let e = &q[0];
    app(r);
    return *e;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"alias_of_a_rooted_alias_and_a_parameter_mut_element", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let a = idam(s);
    let q = pickm(a, r);
    let e = &mut q[0];
    seta(r);
    *e = 5;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"alias_of_a_parameter_and_a_whole_reborrow", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let a = &mut *s;
    let q = pickm(r, a);
    let e = &q[0];
    app(r);
    return *e;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"alias_of_a_parameter_and_a_local_borrow", `fn g(r: &mut int[]) -> int {
    let mut y: int[] = [7];
    let q = pickm(r, &mut y);
    let e = &q[0];
    app(r);
    return *e;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"compare_chosen_alias_mut_element_then_a_source_grown", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = compare r.__len() { 3 => idam(r); _ => idam(s); };
    let e = &mut q[0];
    app(r);
    *e = 5;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"compare_chosen_alias_of_one_source_grown", `fn g(r: &mut int[]) -> int {
    let q = compare r.__len() { 3 => idam(r); _ => idam(r); };
    let e = &mut r[0];
    app(q);
    *e = 5;
    return 0;
}
`, "q);\n    *e", diag.SemaBorrowConflict},
		{"ternary_chosen_alias_grown", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = r.__len() > 1 ? idam(r) : idam(s);
    let e = &mut r[0];
    app(q);
    *e = 5;
    return 0;
}
`, "q);\n    *e", diag.SemaBorrowConflict},
		{"interior_reference_of_a_parameter_then_replaced", `fn g(r: &mut int[]) -> int {
    let e = firstm(r);
    seta(r);
    *e = 3;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"interior_reference_through_an_alias_then_the_source_grown", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let e = firstm(q);
    app(r);
    *e = 3;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"interior_reference_through_an_alias_then_the_alias_grown", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let e = firstm(q);
    app(q);
    *e = 3;
    return 0;
}
`, "q);\n    *e", diag.SemaBorrowConflict},
		{"interior_reference_through_a_nested_alias", `fn g(r: &mut int[]) -> int {
    let e = firstm(idam(r));
    seta(r);
    *e = 3;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"interior_reference_through_a_whole_reborrow", `fn g(r: &mut int[]) -> int {
    let q = &mut *r;
    let e = firstm(q);
    seta(r);
    *e = 3;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"interior_reference_through_a_local_mut_reference", `fn g() -> int {
    let mut x: int[] = [1, 2, 3];
    let q = &mut x;
    let e = firstm(q);
    seta(q);
    *e = 3;
    return 0;
}
`, "q);\n    *e", diag.SemaBorrowConflict},
		{"interior_reference_of_a_call_aliasing_two_sources", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let e = firstm(pickm(r, s));
    app(r);
    *e = 3;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"interior_reference_through_an_alias_of_two_sources", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = pickm(r, s);
    let e = firstm(q);
    app(r);
    *e = 3;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, aliasThroughRefPrelude+row.text, row.code, row.snippet)
		})
	}
}

// Handing a reborrow back is not a move out from under it.
func TestReturningAnAliasOfAReferenceStaysAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"alias_pushed_then_returned", `fn g2(r: &mut int[]) -> &mut int[] {
    let q = idam(r);
    q.push(1);
    return q;
}
`},
		{"alias_returned", `fn g2(r: &mut int[]) -> &mut int[] {
    let q = idam(r);
    return q;
}
`},
		{"whole_reborrow_pushed_then_returned", `fn g2(r: &mut int[]) -> &mut int[] {
    let q = &mut *r;
    q.push(1);
    return q;
}
`},
		{"method_alias_pushed_then_returned", `extern<H> {
    fn itemsm(self: &mut H) -> &mut int[] { return idam(self.items); }
}

fn g2(k: &mut H) -> &mut int[] {
    let q = k.itemsm();
    q.push(4);
    return q;
}
`},
		{"interior_reference_of_an_alias_returned", `fn g2(r: &mut int[]) -> &mut int {
    let q = idam(r);
    return firstm(q);
}
`},
		{"interior_reference_written_then_the_source_read", `fn g(r: &mut int[]) -> int {
    let q = firstm(r);
    *q = 4;
    return r[1];
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, aliasThroughRefPrelude+row.text)
		})
	}
}
