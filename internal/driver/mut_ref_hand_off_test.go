package driver

import (
	"testing"

	"surge/internal/diag"
)

// Handing an existing `&mut` reference to a `&mut` parameter -- `r.push(1)`
// through a `&mut self` receiver, `app(r)`, `app(h.s)` for a field reached
// through `h: &mut H`, or spelling it `app(&mut *r)` -- takes no new loan, and
// it was not checked against the views taken through that reference. Each
// refusal row below was accepted before and read freed storage natively
// (valgrind: invalid read after `rt_realloc` or the string's replacement; the
// VM hid it). A `for` loop over a `&mut` reference and a window of a fixed
// array reached through one took no loan at all, so nothing was there to
// conflict with; both now take a shared loan through the reference.

const mutRefHandOffPrelude = `fn app(s: &mut int[]) -> nothing {
    let mut i = 0;
    while i < 200 { s.push(i); i = i + 1; }
    return nothing;
}

fn rd(s: &int[]) -> int {
    return s.__len() to int;
}

fn replace(s: &mut string) -> nothing {
    *s = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz";
    return nothing;
}

fn overwrite(s: &mut string) -> nothing {
    *s = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz";
    return nothing;
}

fn app2(a: &mut int[], b: &mut int[]) -> nothing {
    let e = &b[0];
    app(a);
    print((*e) to string);
    return nothing;
}

fn id(r: &mut int[]) -> &mut int[] {
    return r;
}

fn rd2(a: &int[], b: &mut int[]) -> nothing {
    let e = &a[0];
    app(b);
    print((*e) to string);
    return nothing;
}

fn rd3(b: &mut int[], a: &int[]) -> nothing {
    rd2(a, b);
    return nothing;
}

fn two(a: &int[], b: &int[]) -> int {
    return a[0] + b[0];
}

type H = { s: string, a: int[], b: int[] };

`

func TestMutRefHandOffIsCheckedAgainstViewsThroughIt(t *testing.T) {
	rows := []struct{ name, text, snippet string }{
		{"range_of_a_parameter_then_push", `fn g(r: &mut int[]) -> int {
    let it = r.__range();
    r.push(1);
    let mut t = 0;
    for v in it { t = t + v; }
    return t;
}
`, "r.push(1)"},
		{"range_of_a_parameter_then_a_mut_parameter_call", `fn g(r: &mut int[]) -> int {
    let it = r.__range();
    app(r);
    let mut t = 0;
    for v in it { t = t + v; }
    return t;
}
`, "r);"},
		{"range_of_a_local_reference_then_push", `fn g() -> int {
    let mut ys: int[] = [10, 20, 30, 40];
    let r = &mut ys;
    let it = r.__range();
    r.push(1);
    let mut t = 0;
    for v in it { t = t + v; }
    return t;
}
`, "r.push(1)"},
		{"for_over_a_parameter_then_push", `fn g(r: &mut int[]) -> int {
    let mut t = 0;
    for v in r { t = t + v; r.push(1); }
    return t;
}
`, "r.push(1)"},
		{"for_over_a_parameter_then_explicit_reborrow", `fn g(r: &mut int[]) -> int {
    let mut t = 0;
    for v in r { t = t + v; app(&mut *r); }
    return t;
}
`, "&mut *r)"},
		{"for_over_a_dereferenced_parameter_then_push", `fn g(r: &mut int[]) -> int {
    let mut t = 0;
    for v in *r { t = t + v; r.push(1); }
    return t;
}
`, "r.push(1)"},
		{"for_over_a_dereferenced_local_reference_then_push", `fn g() -> int {
    let mut ys: int[] = [10, 20, 30, 40];
    let mut t = 0;
    {
        let r = &mut ys;
        for v in *r { t = t + v; r.push(1); }
    }
    return t;
}
`, "r.push(1)"},
		{"bytes_of_a_parameter_then_a_mut_parameter_call", `fn g(r: &mut string) -> int {
    let v = r.bytes();
    replace(r);
    return v[0] to int;
}
`, "r);"},
		{"bytes_of_a_field_through_a_parameter_then_a_mut_parameter_call", `fn g(h: &mut H) -> int {
    let v = h.s.bytes();
    replace(h.s);
    return v[0] to int;
}
`, "h.s);"},
		{"element_of_a_parameter_then_push", `fn g(r: &mut int[]) -> int {
    let e = &r[0];
    r.push(1);
    return *e;
}
`, "r.push(1)"},
		{"element_of_a_parameter_then_explicit_reborrow", `fn g(r: &mut int[]) -> int {
    let e = &r[0];
    app(&mut *r);
    return *e;
}
`, "&mut *r)"},
		{"fixed_window_of_a_parameter_then_push", `fn g(r: &mut Array<int[4]>) -> int {
    let v = r[0][[0..2]];
    r.push([1, 2, 3, 4]);
    return v[1];
}
`, "r.push([1"},
		{"fixed_window_of_a_local_reference_then_push", `fn g() -> int {
    let mut xs: Array<int[4]> = [[11, 22, 33, 44]];
    let r = &mut xs;
    let v = r[0][[0..2]];
    r.push([1, 2, 3, 4]);
    return v[1];
}
`, "r.push([1"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, mutRefHandOffPrelude+row.text, diag.SemaBorrowConflict, row.snippet)
		})
	}
}

func TestMutRefHandOffControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"views_dead_before_the_hand_off", `fn g(r: &mut int[]) -> int {
    let mut t = 0;
    { let it = r.__range(); for v in it { t = t + v; } }
    { let e = &r[0]; t = t + *e; }
    r.push(1);
    app(r);
    app(&mut *r);
    for v in r { t = t + v; }
    r.push(2);
    for v in *r { t = t + v; }
    app(r);
    return t;
}
`},
		{"shared_uses_while_views_live", `fn g(r: &mut int[]) -> int {
    let mut t = 0;
    for v in r { t = t + v + rd(r) + (r.__len() to int) + r[0]; }
    let it = r.__range();
    t = t + rd(r) + (r.__len() to int);
    for v in it { t = t + v; }
    return t;
}
`},
		{"hand_off_with_no_view", `fn g(r: &mut int[]) -> int {
    r.push(1);
    app(r);
    app(&mut *r);
    let x = r[0] + 1;
    r.push(x);
    return x;
}
`},
		{"reborrow_chain_hand_off", `fn g(r: &mut int[]) -> int {
    let r2 = &mut *r;
    r2.push(1);
    app(r2);
    r.push(2);
    return 0;
}
`},
		{"local_reference_hand_off_with_no_view", `fn g() -> int {
    let mut ys: int[] = [1, 2];
    let r = &mut ys;
    r.push(1);
    app(r);
    return 0;
}
`},
		{"for_over_a_shared_reference", `fn h(r: &int[]) -> int {
    let mut t = 0;
    for v in r { t = t + v + rd(r); }
    return t;
}

fn g(r: &mut int[]) -> int {
    return h(r);
}
`},
		{"fixed_window_read_in_a_callee", `fn peek(r: &mut Array<int[4]>) -> int {
    let v = r[0][[0..2]];
    return v[1];
}

fn g(r: &mut Array<int[4]>) -> int {
    let t = peek(r);
    r.push([1, 2, 3, 4]);
    return t + peek(r);
}
`},
		{"bytes_dead_before_the_hand_off", `fn g(r: &mut string) -> int {
    let mut t = 0;
    { let v = r.bytes(); t = v[0] to int; }
    replace(r);
    return t;
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, mutRefHandOffPrelude+row.text)
		})
	}
}

// One referent handed to two `&mut` parameters of one call, a shared borrow
// through `r` while an exclusive reborrow `&mut *r` lives, an element reached
// as `(*r)[..]` against a view of `r[..]`, and a `for` loop over an element
// reached through a `&mut` reference: each was accepted and read freed storage
// natively (valgrind: invalid read). `r[0]` and `(*r)[0]` are now one place.
func TestMutRefAliasingAndReferentPathsAreChecked(t *testing.T) {
	rows := []struct{ name, text, snippet string }{
		{"one_reference_handed_to_two_mut_parameters", `fn g(r: &mut int[]) -> int {
    app2(r, r);
    return 0;
}
`, "r);"},
		{"a_reference_and_its_reborrow_handed_to_two_mut_parameters", `fn g(r: &mut int[]) -> int {
    app2(r, &mut *r);
    return 0;
}
`, "&mut *r);"},
		{"a_reborrow_and_its_reference_handed_to_two_mut_parameters", `fn g(r: &mut int[]) -> int {
    app2(&mut *r, r);
    return 0;
}
`, "r);"},
		{"shared_borrow_through_a_reference_its_reborrow_holds", `fn g(r: &mut int[]) -> int {
    let r2 = &mut *r;
    let e = &r[0];
    app(r2);
    return *e;
}
`, "&r[0]"},
		{"shared_borrow_through_a_reference_its_reborrow_pushes_to", `fn g(r: &mut int[]) -> int {
    let r2 = &mut *r;
    let e = &r[0];
    let mut i = 0;
    while i < 200 { r2.push(i); i = i + 1; }
    return *e;
}
`, "&r[0]"},
		{"bytes_of_an_element_then_its_explicit_dereferenced_borrow", `fn h(r: &mut string[]) -> int {
    let v = r[0].bytes();
    replace(&mut (*r)[0]);
    return v[0] to int;
}
`, "&mut (*r)[0]"},
		{"element_of_a_row_then_the_explicit_dereferenced_row", `fn h(r: &mut int[][]) -> int {
    let e = &r[0][0];
    app(&mut (*r)[0]);
    return *e;
}
`, "&mut (*r)[0]"},
		{"explicit_dereferenced_element_then_the_row", `fn h(r: &mut int[][]) -> int {
    let e = &(*r)[0][0];
    app(&mut r[0]);
    return *e;
}
`, "&mut r[0]"},
		{"range_of_a_row_then_the_explicit_dereferenced_row", `fn h(r: &mut int[][]) -> int {
    let it = r[0].__range();
    app(&mut (*r)[0]);
    let mut t = 0;
    for v in it { t = t + v; }
    return t;
}
`, "&mut (*r)[0]"},
		{"element_of_a_row_then_a_bound_explicit_row_borrow", `fn h(r: &mut int[][]) -> int {
    let e = &r[0][0];
    let q = &mut (*r)[0];
    app(q);
    return *e;
}
`, "&mut (*r)[0]"},
		{"for_over_a_row_of_a_parameter_then_the_row", `fn h(r: &mut int[][]) -> int {
    let mut t = 0;
    for v in r[0] { t = t + v; app(&mut r[0]); }
    return t;
}
`, "&mut r[0]"},
		{"function_value_handed_a_reference_under_its_bytes", `fn h(r: &mut string) -> int {
    let f: fn(&mut string) -> nothing = overwrite;
    let v = r.bytes();
    f(r);
    return v[0] to int;
}
`, "r);"},
		// Loans are lexical: a view held by a binding in scope blocks a `&mut`
		// hand-off even after its last read. A parser holding its source's
		// bytes while it advances through a `&mut self` method is refused
		// soundly -- the method could replace the source the view reads.
		{"view_in_scope_blocks_a_mut_self_call_that_could_replace_its_source", `type P = { src: string, pos: int };
extern<P> {
    fn bump(self: &mut P) -> nothing { self.pos = self.pos + 1; return nothing; }
    fn run(self: &mut P) -> int {
        let b = self.src.bytes();
        let mut t = 0;
        while self.pos < 3 { t = t + (b[self.pos] to int); self.bump(); }
        return t;
    }
}
`, "self.bump()"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, mutRefHandOffPrelude+row.text, diag.SemaBorrowConflict, row.snippet)
		})
	}
}

func TestMutRefAliasingControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"two_references_to_different_arrays", `fn g(r: &mut int[], s: &mut int[]) -> int {
    app2(r, s);
    let mut ys: int[] = [1, 2];
    app2(r, &mut ys);
    app2(&mut ys, r);
    return 0;
}
`},
		{"reborrow_ended_before_a_shared_borrow_through_the_reference", `fn g(r: &mut int[]) -> int {
    { let r2 = &mut *r; r2.push(1); }
    let e = &r[0];
    return *e;
}
`},
		{"element_views_dead_before_the_explicit_row_borrow", `fn h(r: &mut int[][]) -> int {
    let mut t = 0;
    { let e = &r[0][0]; t = *e; }
    app(&mut (*r)[0]);
    for v in r[0] { t = t + v + r[1][0]; }
    app(&mut r[0]);
    return t;
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, mutRefHandOffPrelude+row.text)
		})
	}
}

// A `for` loop over a field reached through a `&mut` reference -- a parameter
// or a local `let h = &mut b;` -- walks the field's storage while the body
// grows or replaces it: accepted, and natively an invalid read (valgrind).
func TestForOverAFieldThroughAMutRefKeepsItBorrowed(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"for_over_a_field_of_a_parameter_then_push", `type Bag = { items: int[] };
fn g(h: &mut Bag) -> int {
    let mut t = 0;
    for v in h.items { t = t + v; h.items.push(1); }
    return t;
}
`, "h.items.push(1)", diag.SemaBorrowConflict},
		{"for_over_a_field_of_a_parameter_then_replace", `type Bag = { items: int[] };
fn g(h: &mut Bag) -> int {
    let mut t = 0;
    for v in h.items { t = t + v; h.items = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]; }
    return t;
}
`, "h.items = [1", diag.SemaBorrowMutation},
		{"for_over_a_field_of_a_local_reference_then_push", `type Bag = { items: int[] };
fn g() -> int {
    let mut b = Bag { items = [10, 20, 30, 40] };
    let mut t = 0;
    {
        let h = &mut b;
        for v in h.items { t = t + v; h.items.push(1); }
    }
    return t;
}
`, "h.items.push(1)", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, mutRefHandOffPrelude+row.text, row.code, row.snippet)
		})
	}
}
