package driver

import (
	"testing"

	"surge/internal/diag"
)

const argumentOrderPrelude = `fn heap(c: string) -> string { return c * 80; }

fn app(s: &mut int[]) -> nothing {
    let mut i = 0;
    while i < 200 { s.push(i); i = i + 1; }
    return nothing;
}

fn appr(s: &mut int[]) -> int {
    app(s);
    return 0;
}

fn apps(s: &mut string[]) -> nothing {
    let mut i = 0;
    while i < 200 { s.push(heap("x")); i = i + 1; }
    return nothing;
}

fn appsr(s: &mut string[]) -> int {
    apps(s);
    return 0;
}

fn appsv(s: &mut string[]) -> string {
    apps(s);
    return heap("v");
}

fn rd(s: &int[]) -> int {
    return s.__len() to int;
}

fn idam(s: &mut int[]) -> &mut int[] {
    return s;
}

fn use2(a: &int, b: int) -> int {
    return *a + b;
}

fn uses(a: &string, b: int) -> int {
    return (a.__len() to int) + b;
}

extern<int> {
    fn foo(self: &int, b: int) -> int { return *self + b; }
}

type H = { items: int[] };

`

// An element reached through a reference and handed to a `&` parameter -- as
// a method receiver, as an operator's left operand, as one value of a compare
// -- is read by the callee after every later argument ran. A later argument
// that grows the array through the reference, through a new `&mut` borrow of
// it or through an alias a call returned freed what the callee then read
// (valgrind: invalid read). The owned forms (`xs[0].foo(appr(&mut xs))`) are
// refused; these are refused the same way.
func TestLaterArgumentCannotFreeAnEarlierElementThroughAReference(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"receiver_element_of_a_parameter", `fn g(r: &mut int[]) -> int {
    return r[0].foo(appr(r));
}
`, "r));", diag.SemaBorrowConflict},
		{"receiver_element_of_a_local_mut_reference", `fn g() -> int {
    let mut x0: int[] = [1, 2, 3];
    let r = &mut x0;
    let t = r[0].foo(appr(r));
    return t;
}
`, "r));", diag.SemaBorrowConflict},
		{"receiver_element_of_a_field_through_a_reference", `fn g(k: &mut H) -> int {
    return k.items[0].foo(appr(&mut (*k).items));
}
`, "&mut (*k).items", diag.SemaBorrowConflict},
		{"receiver_nested_element_then_row_borrow", `fn g(nr: &mut int[][]) -> int {
    return nr[0][0].foo(appr(&mut nr[0]));
}
`, "&mut nr[0]", diag.SemaBorrowConflict},
		{"string_operator_left_element", `fn g(xr: &mut string[]) -> string {
    return xr[0] + appsv(xr);
}
`, "xr);", diag.SemaBorrowConflict},
		{"compare_value_element_of_a_parameter", `fn g(r: &mut int[], flag: bool) -> int {
    let dA: int[] = [7];
    return use2(compare flag { true => r[0]; finally => dA[0]; }, appr(r));
}
`, "r));", diag.SemaBorrowConflict},
		{"compare_value_elements_of_two_parameters", `fn g(r: &mut int[], s: &mut int[], one: int) -> int {
    return use2(compare one { 1 => r[0]; finally => s[0]; }, appr(r));
}
`, "r));", diag.SemaBorrowConflict},
		{"compare_value_element_of_a_local_mut_reference", `fn g(flag: bool) -> int {
    let dA: int[] = [7];
    let mut x0: int[] = [1, 2, 3];
    let r = &mut x0;
    let t = use2(compare flag { true => r[0]; finally => dA[0]; }, appr(r));
    return t;
}
`, "r));", diag.SemaBorrowConflict},
		{"compare_value_element_of_a_field", `fn g(k: &mut H, flag: bool) -> int {
    let dA: int[] = [7];
    return use2(compare flag { true => k.items[0]; finally => dA[0]; }, appr(&mut (*k).items));
}
`, "&mut (*k).items", diag.SemaBorrowConflict},
		{"compare_value_nested_element", `fn g(nr: &mut int[][], flag: bool) -> int {
    let dA: int[] = [7];
    return use2(compare flag { true => nr[0][0]; finally => dA[0]; }, appr(&mut nr[0]));
}
`, "&mut nr[0]", diag.SemaBorrowConflict},
		{"compare_value_string_element", `fn g(xr: &mut string[], flag: bool) -> int {
    let dS = heap("d");
    return uses(compare flag { true => xr[0]; finally => &dS; }, appsr(xr));
}
`, "xr));", diag.SemaBorrowConflict},
		{"nested_element_then_row_borrow", `fn g(nr: &mut int[][]) -> int {
    return use2(nr[0][0], appr(&mut nr[0]));
}
`, "&mut nr[0]", diag.SemaBorrowConflict},
		{"field_element_then_field_borrow", `fn g(k: &mut H) -> int {
    return use2(k.items[0], appr(&mut (*k).items));
}
`, "&mut (*k).items", diag.SemaBorrowConflict},
		{"element_then_an_alias_a_call_returned", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    return use2(r[0], appr(q));
}
`, "q));", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, argumentOrderPrelude+row.text, row.code, row.snippet)
		})
	}
}

// A `&mut` element or a range cursor taken through a reference -- directly, or
// through an alias a call returned -- pointed into the referent's buffer while
// a grow through the reference was admitted; the store or the read after it
// landed in freed memory (valgrind: invalid read and write). `let e = &mut
// xs[0]; app(&mut xs); *e = 5;` is refused; these are refused the same way.
func TestElementBorrowThroughAReferenceHoldsItsReferent(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"mut_element_through_a_returned_alias_of_a_parameter", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let e = &mut q[0];
    app(r);
    *e = 5;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"mut_element_through_a_returned_alias_of_a_local_mut_reference", `fn g() -> int {
    let mut x: int[] = [1, 2, 3];
    let r = &mut x;
    let q = idam(r);
    let e = &mut q[0];
    app(r);
    *e = 5;
    return q[0];
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"range_through_a_returned_alias", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let mut it = q.__range();
    app(r);
    return compare it.next() { Some(v) => v; nothing => 0; };
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"mut_element_of_a_parameter", `fn g(r: &mut int[]) -> int {
    let e = &mut r[0];
    app(r);
    *e = 5;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"mut_element_of_a_parameter_then_replaced", `fn g(r: &mut int[]) -> int {
    let e = &mut r[0];
    *r = [1, 2];
    *e = 5;
    return 0;
}
`, "*r = [1, 2]", diag.SemaBorrowMutation},
		{"mut_element_of_a_local_mut_reference", `fn g() -> int {
    let mut x: int[] = [1, 2, 3];
    let r = &mut x;
    let e = &mut r[0];
    app(r);
    *e = 5;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"mut_element_of_a_whole_reborrow", `fn g() -> int {
    let mut x: int[] = [1, 2, 3];
    let r = &mut x;
    let r2 = &mut *r;
    let e = &mut r2[0];
    app(r);
    *e = 5;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, argumentOrderPrelude+row.text, row.code, row.snippet)
		})
	}
}

func TestArgumentOrderThroughAReferenceControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		// An int operand is copied out before the right operand runs.
		{"int_operator_over_a_row_borrow", `fn g(nr: &mut int[][]) -> int {
    return nr[0][0] + appr(&mut nr[0]);
}
`},
		{"int_operator_over_a_hand_off", `fn g(r: &mut int[]) -> int {
    return r[0] + appr(r);
}
`},
		{"receiver_element_then_an_unrelated_array", `fn g(r: &mut int[], s: &mut int[]) -> int {
    return r[0].foo(appr(s));
}
`},
		{"compare_value_then_an_unrelated_array", `fn g(r: &mut int[], s: &mut int[], flag: bool) -> int {
    let dA: int[] = [7];
    return use2(compare flag { true => r[0]; finally => dA[0]; }, appr(s));
}
`},
		{"receiver_element_then_a_shared_use", `fn g(r: &mut int[]) -> int {
    return r[0].foo(rd(r));
}
`},
		{"field_element_then_a_shared_use", `fn g(k: &mut H) -> int {
    return use2(k.items[0], rd(&(*k).items));
}
`},
		{"element_then_an_alias_of_another_array", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = idam(s);
    return use2(r[0], appr(q));
}
`},
		{"alias_read_after_a_grow", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    app(r);
    return q[0];
}
`},
		{"mut_element_through_an_alias_dead_before_the_grow", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let mut t = 0;
    { let e = &mut q[0]; *e = 5; t = *e; }
    app(r);
    return t;
}
`},
		{"range_through_an_alias_dead_before_the_grow", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let mut t = 0;
    { let mut it = q.__range(); t = compare it.next() { Some(v) => v; nothing => 0; }; }
    app(r);
    return t;
}
`},
		{"mut_element_dead_before_the_grow", `fn g(r: &mut int[]) -> int {
    { let e = &mut r[0]; *e = 5; }
    app(r);
    return r[0];
}
`},
		{"whole_reborrow_then_the_reference_handed_on", `fn g(r: &mut int[]) -> int {
    let r2 = &mut *r;
    app(r2);
    app(r);
    return r[0];
}
`},
		{"mut_field_then_the_field_handed_on", `fn g(k: &mut H) -> int {
    let it = &mut (*k).items;
    app(it);
    app(k.items);
    return 0;
}
`},
		{"mut_element_arguments_around_a_grow", `fn setp(p: &mut int) -> nothing {
    *p = 1;
    return nothing;
}

fn g(r: &mut int[]) -> int {
    setp(&mut r[0]);
    app(r);
    setp(&mut r[1]);
    return r[0];
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, argumentOrderPrelude+row.text)
		})
	}
}
