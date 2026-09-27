package driver

import (
	"testing"

	"surge/internal/diag"
)

const elementThroughRefPrelude = `fn app(s: &mut int[]) -> nothing {
    let mut i = 0;
    while i < 200 { s.push(i); i = i + 1; }
    return nothing;
}

fn rd(s: &int[]) -> int {
    return s.__len() to int;
}

fn first(s: &int[]) -> &int {
    return &s[0];
}

fn firstm(s: &mut int[]) -> &int {
    return &s[0];
}

fn pick(a: &int[], b: &int[]) -> &int {
    return &a[0];
}

fn id(s: &mut int[]) -> &mut int[] {
    return s;
}

type H = { items: int[] };

fn hm(k: &mut H) -> nothing {
    app(k.items);
    return nothing;
}

type Bx = { xs: int[] };

extern<Bx> {
    fn get(self: &Bx, i: int) -> &int { return &self.xs[i]; }
    fn grow(self: &mut Bx) -> nothing { app(self.xs); return nothing; }
    fn xsm(self: &mut Bx) -> &mut int[] { return id(self.xs); }
}

fn appr(s: &mut int[]) -> int {
    app(s);
    return 0;
}

fn use2(a: &int, b: int) -> int {
    return *a + b;
}

`

// An element reference taken through a reference -- `let x = r[0]` with
// `r: &mut int[]` binds `&int`, since `__index` returns a reference -- and a
// reference a call returned from a reference handed on to it pointed into the
// referent while holding no loan on it. The grow, replace or store through the
// reference that followed was accepted and the read after it read the freed
// buffer natively (valgrind: invalid read). `let x = xs[0]; xs.push(1)` is
// refused; these are refused the same way.
func TestElementReferenceThroughAReferenceHoldsItsReferent(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"index_of_a_parameter_then_hand_off", `fn g(r: &mut int[]) -> int {
    let x = r[0];
    app(r);
    return x;
}
`, "r);", diag.SemaBorrowConflict},
		{"index_of_a_parameter_then_push", `fn g(r: &mut int[]) -> int {
    let x = r[0];
    r.push(1);
    return x;
}
`, "r.push(1)", diag.SemaBorrowConflict},
		{"index_of_a_parameter_then_store", `fn g(r: &mut int[]) -> int {
    let x = r[0];
    *r = [1, 2];
    return x;
}
`, "*r = [1, 2]", diag.SemaBorrowMutation},
		{"index_of_a_parameter_then_element_store", `fn g(r: &mut int[]) -> int {
    let x = r[0];
    r[1] = 5;
    return x;
}
`, "r[1] = 5", diag.SemaBorrowMutation},
		{"index_by_variable_then_hand_off", `fn g(r: &mut int[]) -> int {
    let i = 2;
    let x = r[i];
    app(r);
    return x;
}
`, "r);", diag.SemaBorrowConflict},
		{"index_of_a_local_mut_reference_then_hand_off", `fn g() -> int {
    let mut v: int[] = [10, 20, 30, 40];
    let r = &mut v;
    let x = r[0];
    app(r);
    return x + 0;
}
`, "r);", diag.SemaBorrowConflict},
		{"index_of_a_field_through_a_reference_then_hand_off", `fn g(h: &mut H) -> int {
    let x = h.items[0];
    hm(h);
    return x;
}
`, "h);", diag.SemaBorrowConflict},
		{"index_of_a_field_through_a_reference_then_field_hand_off", `fn g(h: &mut H) -> int {
    let x = h.items[0];
    app(h.items);
    return x;
}
`, "h.items);", diag.SemaBorrowConflict},
		{"nested_index_then_row_hand_off", `fn g(r: &mut int[][]) -> int {
    let x = r[0][1];
    app(&mut r[0]);
    return x;
}
`, "&mut r[0]", diag.SemaBorrowConflict},
		{"index_stored_from_an_inner_block", `fn g(r: &mut int[]) -> int {
    let mut x = r[1];
    { x = r[0]; }
    app(r);
    return x;
}
`, "r);", diag.SemaBorrowConflict},
		{"index_copied_to_another_binding", `fn g(r: &mut int[]) -> int {
    let x = r[0];
    let y = x;
    app(r);
    return y;
}
`, "r);", diag.SemaBorrowConflict},
		{"index_as_a_compare_arm_binding", `fn g(r: &mut int[]) -> int {
    return compare r[0] {
        v => { app(r); ret v; }
    };
}
`, "r); ret", diag.SemaBorrowConflict},
		{"index_as_a_ternary_value", `fn g(r: &mut int[]) -> int {
    let c = r.__len() > 2;
    let x = c ? r[0] : r[1];
    app(r);
    return x;
}
`, "r);", diag.SemaBorrowConflict},
		{"ternary_value_stored_from_an_inner_block", `fn g(r: &mut int[], s: &int[]) -> int {
    let c = r.__len() > 2;
    let mut x = s[0];
    { x = c ? r[0] : r[1]; }
    app(r);
    return x;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"returned_reference_then_hand_off", `fn g(r: &mut int[]) -> int {
    let e = first(r);
    app(r);
    return *e;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"returned_reference_then_store", `fn g(r: &mut int[]) -> int {
    let e = first(r);
    *r = [1, 2];
    return *e;
}
`, "*r = [1, 2]", diag.SemaBorrowMutation},
		{"returned_reference_from_a_mut_parameter", `fn g(r: &mut int[]) -> int {
    let e = firstm(r);
    app(r);
    return *e;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"returned_reference_that_may_alias_two_arguments", `fn g(r: &mut int[]) -> int {
    let e = pick(r, r);
    app(r);
    return *e;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"reference_that_may_alias_two_arguments_stored_from_an_inner_block", `fn g(r: &mut int[], s: &int[]) -> int {
    let mut e = first(s);
    { e = pick(r, r); }
    app(r);
    return *e;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"returned_reference_through_a_returned_reference", `fn g(r: &mut int[]) -> int {
    let e = first(id(r));
    app(r);
    return *e;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"returned_reference_from_a_local_mut_reference", `fn g() -> int {
    let mut v: int[] = [10, 20, 30, 40];
    let r = &mut v;
    let e = first(r);
    app(r);
    return *e;
}
`, "r);\n    return", diag.SemaBorrowConflict},
		{"method_result_through_a_reference_receiver", `fn g(b: &mut Bx) -> int {
    let e = b.get(0);
    b.grow();
    return *e;
}
`, "b.grow()", diag.SemaBorrowConflict},
		{"copied_element_then_hand_off_with_it", `fn add(s: &mut int[], x: int) -> nothing {
    app(s);
    s.push(x);
    return nothing;
}

fn g(r: &mut int[]) -> int {
    let x = r[0];
    add(r, x);
    return x;
}
`, "r, x)", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, elementThroughRefPrelude+row.text, row.code, row.snippet)
		})
	}
}

func TestElementValueThroughAReferenceStaysAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"element_value_copied_out", `fn g(r: &mut int[]) -> int {
    let n: int = r[0];
    let m = *r[1];
    let k = r[2] + 1;
    app(r);
    return n + m + k;
}
`},
		{"element_reference_dead_before_the_hand_off", `fn g(r: &mut int[]) -> int {
    let mut t = 0;
    { let x = r[0]; t = x; }
    { let e = first(r); t = t + *e; }
    app(r);
    return t;
}
`},
		{"element_reference_with_only_shared_uses", `fn g(r: &mut int[]) -> int {
    let x = r[0];
    let y = r[1];
    let e = first(r);
    return x + y + *e + rd(r) + (r.__len() to int);
}
`},
		{"element_of_a_shared_reference", `fn g(r: &int[]) -> int {
    let x = r[0];
    let e = first(r);
    return x + *e + rd(r);
}
`},
		{"element_read_as_a_statement_temporary", `fn g(r: &mut int[]) -> int {
    print(r[0] to string);
    app(r);
    return 0;
}
`},
		{"element_written_then_read", `fn g(r: &mut int[]) -> int {
    r[1] = 5;
    let x = r[0];
    return x;
}
`},
		{"element_of_an_aliased_parameter_returned_by_value", `fn g(r: &mut int[]) -> int {
    let q = r;
    let x = q[0];
    return x;
}
`},
		{"mut_reference_returned_then_written_through", `fn g(r: &mut int[]) -> int {
    let q = id(r);
    q.push(1);
    *q = [1, 2];
    return rd(q);
}
`},
		{"method_result_copied_out_before_the_mut_call", `fn g(b: &mut Bx) -> int {
    let n: int = b.get(0);
    b.grow();
    return n;
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, elementThroughRefPrelude+row.text)
		})
	}
}

// A reference a `&mut Map` method returns is held against the map the same
// way. The first row is the replace-effects fixture as it was first written:
// `old` is held across `get_mut` and `remove` of the map, which the owned form
// `let mut m = Map...; let old = m.insert(..); m.get_mut(..)` already refused.
func TestMapReferenceThroughAReferenceHoldsTheMap(t *testing.T) {
	rows := []struct{ name, text, snippet string }{
		{"inserted_old_value_held_across_get_mut", `fn read_old(m: &mut Map<string, &string>, key: string, value: &string) -> Option<&string> {
    let old: Option<&string> = m.insert(clone(&key), value);
    let _ = m.contains(&key);
    let _ = m.length();
    let _ = m.get_ref(&key);
    let _ = m.get_mut(&key);
    return old;
}
`, "m.get_mut"},
		{"get_ref_held_across_remove", `fn g(m: &mut Map<string, string>, k: string) -> int {
    let r = m.get_ref(&k);
    let _ = m.remove(&k);
    return compare r { Some(v) => v.__len() to int; nothing => 0; };
}
`, "m.remove"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, row.text, diag.SemaBorrowConflict, row.snippet)
		})
	}
}

// Element references reached through a returned reference or a local
// reference that was given one, and element references handed to a call whose
// later argument grows the array: each was accepted and read freed storage
// natively (valgrind: invalid read); the owned forms are refused.
func TestElementReferenceThroughAGivenReferenceHoldsItsSource(t *testing.T) {
	rows := []struct{ name, text, snippet string }{
		{"index_of_a_returned_reference", `fn g(r: &mut int[]) -> int {
    let x = id(r)[0];
    app(r);
    return x;
}
`, "r);\n    return"},
		{"index_of_a_method_returned_reference", `fn g(b: &mut Bx) -> int {
    let x = b.xsm()[0];
    b.grow();
    return x;
}
`, "b.grow()"},
		{"index_through_a_local_given_a_returned_reference", `fn g(r: &mut int[]) -> int {
    let q = id(r);
    let x = q[0];
    app(r);
    return x;
}
`, "r);\n    return"},
		{"explicit_element_borrow_through_a_local_given_a_returned_reference", `fn g(r: &mut int[]) -> int {
    let q = id(r);
    let e = &q[0];
    r.push(1);
    return *e;
}
`, "r.push(1)"},
		{"element_argument_then_a_growing_argument", `fn g(r: &mut int[]) -> int {
    return use2(r[0], appr(r));
}
`, "r));"},
		{"returned_reference_argument_then_a_growing_argument", `fn g(r: &mut int[]) -> int {
    return use2(first(r), appr(r));
}
`, "r));"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, elementThroughRefPrelude+row.text, diag.SemaBorrowConflict, row.snippet)
		})
	}
}

// Rebinding a local reference frees nothing it pointed at: an element
// reference read through it before the rebind still holds what the reference
// was given, and the reference itself is free to point elsewhere and be used.
func TestRebindingAReferenceWithALiveElementStaysAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"shared_reference_rebound", `fn g(a: &int[], b: &int[]) -> int {
    let mut q = a;
    let x = q[0];
    q = b;
    return x + q[0];
}
`},
		{"shared_reference_rebound_in_a_loop", `fn g(a: &int[], b: &int[]) -> int {
    let mut q = a;
    let mut t = 0;
    let mut i = 0;
    while i < 2 {
        let e = first(q);
        t = t + *e;
        q = b;
        i = i + 1;
    }
    return t;
}
`},
		{"element_argument_then_an_unrelated_growing_argument", `fn g(r: &mut int[], s: &mut int[]) -> int {
    return use2(r[0], appr(s));
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, elementThroughRefPrelude+row.text)
		})
	}
}

// Rebinding a reference does not end the loan an element read through it
// holds on it: the store cannot tell which of the reference's values a reader
// holds -- a rebind in one branch, before a `break`, or on one turn of a loop
// leaves the other -- so an exclusive use through the reference stays refused
// while the element lives, and so does one through what it was given first.
// Each witness was accepted when the rebind ended the loan and read freed
// storage natively (valgrind: invalid read). The straight-line
// `q = b; q.push(1)` is refused with them, conservatively.
func TestRebindingAReferenceKeepsItsElementLoan(t *testing.T) {
	const head = `fn g(r: &mut int[], s: &mut int[], c: bool) -> int {
`
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"rebind_in_a_branch_then_hand_off", head + `    let mut q = r;
    let x = q[0];
    if c { q = s; }
    app(q);
    return x;
}
`, "q);\n    return", diag.SemaBorrowConflict},
		{"rebind_in_a_branch_else_hand_off", head + `    let mut q = r;
    let x = q[0];
    if c { q = s; } else { app(q); }
    return x;
}
`, "q); }", diag.SemaBorrowConflict},
		{"rebind_in_a_branch_then_push", head + `    let mut q = r;
    let x = q[0];
    if c { q = s; }
    q.push(1);
    return x;
}
`, "q.push(1)", diag.SemaBorrowConflict},
		{"rebind_in_a_branch_then_store", head + `    let mut q = r;
    let x = q[0];
    if c { q = s; }
    *q = [1];
    return x;
}
`, "*q = [1]", diag.SemaBorrowMutation},
		{"returned_reference_then_rebind_in_a_branch", head + `    let mut q = r;
    let x = first(q);
    if c { q = s; }
    app(q);
    return *x;
}
`, "q);\n    return", diag.SemaBorrowConflict},
		{"given_returned_references_rebound_in_a_branch", head + `    let mut q = id(r);
    let x = q[0];
    if c { q = id(s); }
    app(q);
    return x;
}
`, "q);\n    return", diag.SemaBorrowConflict},
		{"rebind_on_each_turn_of_a_loop", head + `    let mut q = r;
    let mut t = 0;
    let mut i = 0;
    while i < 2 {
        let x = q[0];
        q = s;
        app(q);
        t = t + x;
        i = i + 1;
    }
    return t;
}
`, "q);\n        t", diag.SemaBorrowConflict},
		{"rebind_skipped_by_a_break", head + `    let mut q = r;
    let x = q[0];
    let mut i = 0;
    while i < 2 {
        if i == 0 { break; }
        q = s;
        i = i + 1;
    }
    app(q);
    return x;
}
`, "q);\n    return", diag.SemaBorrowConflict},
		{"straight_rebind_then_push_stays_refused", head + `    let mut q = r;
    let x = q[0];
    q = s;
    q.push(1);
    return x + 0;
}
`, "q.push(1)", diag.SemaBorrowConflict},
		{"former_source_grown_after_the_rebind", head + `    let mut q = id(r);
    let x = q[0];
    q = s;
    app(r);
    return x;
}
`, "r);\n    return", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, elementThroughRefPrelude+row.text, row.code, row.snippet)
		})
	}
}

// The later argument handed on itself is refused once, by the call's check of
// its reference arguments.
func TestElementArgumentThenTheArrayHandedOnIsRefusedOnce(t *testing.T) {
	bytesViewRefusal(t, elementThroughRefPrelude+`fn usem(a: &int, b: &mut int[]) -> int {
    return *a;
}

fn g(r: &mut int[]) -> int {
    return usem(r[0], r);
}
`, diag.SemaBorrowConflict, "r);\n}")
}
