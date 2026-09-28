package driver

import (
	"testing"

	"surge/internal/diag"
)

const aliasThroughRefPrelude = `fn heap(c: string) -> string { return c * 80; }

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

fn seta(s: &mut int[]) -> nothing { *s = [4, 5, 6]; return nothing; }
fn setstr(s: &mut string) -> nothing { *s = heap("z"); return nothing; }
fn seti(p: &mut int) -> int { *p = 9; return 0; }
fn rdi(p: &int[]) -> int { return p.__len() to int; }
fn idam(s: &mut int[]) -> &mut int[] { return s; }
fn pickm(a: &mut int[], b: &mut int[]) -> &mut int[] { return a; }
fn pickms(a: &mut string[], b: &mut string[]) -> &mut string[] { return a; }
fn idsm(s: &mut string) -> &mut string { return s; }
fn idsam(s: &mut string[]) -> &mut string[] { return s; }
fn first(s: &int[]) -> &int { return &s[0]; }
fn firstm(s: &mut int[]) -> &mut int { return &mut s[0]; }
fn vw(s: &string) -> BytesView { return s.bytes(); }
fn use2(a: &int, b: int) -> int { return *a + b; }
fn uses(a: &string, b: int) -> int { return (a.__len() to int) + b; }

type H = { s: string, items: int[], ss: string[] };

fn mkH() -> H { return H{ s: heap("s"), items: [1, 2, 3], ss: [heap("e")] }; }
fn setH(s: &mut H) -> nothing { *s = mkH(); return nothing; }

extern<H> {
    fn grow(self: &mut H) -> nothing { app(self.items); self.s = heap("z"); return nothing; }
}

`

// A `&mut` reference a call returned from an existing reference that holds no
// loan of this function -- `let q = idam(r)` with `r: &mut int[]` a parameter
// -- pointed at r's referent while holding nothing on it. A grow through q
// freed what a holder taken through r read (`let e = &mut r[0]; app(q); *e =
// 5;`), and a shared view taken through r was not checked against an
// exclusive element borrow taken through q (`let e = &mut q[0]; let v =
// xr[0].bytes(); *e = ..; v[5]`); natively each read or wrote freed memory
// (valgrind). q now holds an exclusive reborrow of r's referent, as `let q =
// idam(&mut x)` holds its borrow, and each form is refused as its owned
// analog is.
func TestAliasReturnedFromAReferenceHoldsItsSource(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"mut_element_through_the_alias_then_a_view_through_the_source", `fn g(xr: &mut string[]) -> int {
    let q = idsam(xr);
    let e = &mut q[0];
    let v = xr[0].bytes();
    *e = heap("z");
    let b = v[5];
    return b to int;
}
`, "xr[0].bytes()", diag.SemaBorrowConflict},
		{"mut_element_through_the_alias_then_a_view_of_the_source_element", `fn g(xr: &mut string[]) -> int {
    let q = idsam(xr);
    let e = &mut q[0];
    let v = vw(&xr[0]);
    setstr(e);
    let b = v[5];
    return b to int;
}
`, "&xr[0]", diag.SemaBorrowConflict},
		{"mut_element_through_the_source_then_a_grow_through_the_alias", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let e = &mut r[0];
    app(q);
    *e = 5;
    return 0;
}
`, "&mut r[0]", diag.SemaBorrowConflict},
		{"element_read_through_the_source_then_a_grow_through_the_alias", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let e = r[0];
    app(q);
    let t = e;
    return t;
}
`, "r[0];", diag.SemaBorrowConflict},
		{"view_of_the_source_string_then_a_store_through_the_alias", `fn g(s: &mut string) -> int {
    let q = idsm(s);
    let v = s.bytes();
    setstr(q);
    let b = v[5];
    return b to int;
}
`, "s.bytes();\n    setstr(q)", diag.SemaBorrowConflict},
		{"mut_element_through_the_source_then_a_replace_through_the_alias", `fn g(xr: &mut string[]) -> int {
    let q = idsam(xr);
    let e = &mut xr[0];
    *q = [heap("z")];
    *e = heap("w");
    return 0;
}
`, "&mut xr[0]", diag.SemaBorrowConflict},
		{"shared_element_of_the_source_then_a_grow_through_the_alias", `fn g(xr: &mut string[]) -> int {
    let q = idsam(xr);
    let e = &xr[0];
    apps(q);
    return e.__len() to int;
}
`, "&xr[0]", diag.SemaBorrowConflict},
		{"range_of_the_source_then_a_grow_through_the_alias", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let mut it = r.__range();
    app(q);
    return compare it.next() { Some(v) => v; nothing => 0; };
}
`, "r.__range()", diag.SemaBorrowConflict},
		{"mut_element_through_the_source_then_a_push_through_the_alias", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let e = &mut r[0];
    q.push(1);
    *e = 5;
    return 0;
}
`, "&mut r[0]", diag.SemaBorrowConflict},
		{"returned_element_of_the_source_then_a_replace_through_the_alias", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let e = first(r);
    seta(q);
    return *e;
}
`, "r);\n    seta(q)", diag.SemaBorrowConflict},
		{"two_aliases_of_one_source", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let p = idam(r);
    let e = &mut q[0];
    app(p);
    *e = 5;
    return 0;
}
`, "idam(r);\n    let e", diag.SemaBorrowConflict},
		{"alias_of_an_alias", `fn g(r: &mut int[]) -> int {
    let q = idam(idam(r));
    let e = &mut r[0];
    app(q);
    *e = 5;
    return 0;
}
`, "&mut r[0]", diag.SemaBorrowConflict},
		{"alias_of_two_sources_mut_element_then_a_source_grown", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = pickm(r, s);
    let e = &mut q[0];
    app(r);
    *e = 5;
    return 0;
}
`, "r);\n    *e", diag.SemaBorrowConflict},
		{"alias_of_two_sources_grown_by_a_later_argument", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = pickm(r, s);
    return use2(r[0], appr(q));
}
`, "q));", diag.SemaBorrowConflict},
		{"alias_of_two_sources_mut_element_then_a_view_through_a_source", `fn g(xr: &mut string[], ys: &mut string[]) -> int {
    let q = pickms(xr, ys);
    let e = &mut q[0];
    let v = xr[0].bytes();
    *e = heap("z");
    let b = v[5];
    return b to int;
}
`, "xr[0].bytes()", diag.SemaBorrowConflict},
		{"mut_element_of_a_source_then_an_alias_of_two_sources_grown", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = pickm(r, s);
    let e = &mut r[0];
    app(q);
    *e = 5;
    return 0;
}
`, "q);\n    *e", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, aliasThroughRefPrelude+row.text, row.code, row.snippet)
		})
	}
}

func TestAliasReturnedFromAReferenceControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"mut_field_then_the_record_replaced_then_pushed", `fn g(k: &mut H) -> int {
    let f = &mut (*k).items;
    setH(k);
    f.push(1);
    return 0;
}
`},
		{"mut_field_then_the_record_assigned", `fn g(k: &mut H) -> int {
    let f = &mut (*k).items;
    *k = mkH();
    f.push(1);
    return 0;
}
`},
		{"mut_string_field_then_the_record_replaced", `fn g(k: &mut H) -> int {
    let f = &mut (*k).s;
    setH(k);
    *f = heap("q");
    return f.__len() to int;
}
`},
		{"mut_field_then_a_grow_method", `fn g(k: &mut H) -> int {
    let f = &mut (*k).items;
    k.grow();
    f.push(1);
    return 0;
}
`},
		{"disjoint_field_grown_by_a_later_argument", `fn g(k: &mut H) -> int {
    return use2(k.items[0], appsr(&mut (*k).ss));
}
`},
		{"disjoint_field_borrowed_by_a_later_argument", `fn g(k: &mut H) -> int {
    return uses(k.ss[0], appr(&mut (*k).items));
}
`},
		{"two_mut_disjoint_fields", `fn g(k: &mut H) -> int {
    let a = &mut (*k).items;
    let b = &mut (*k).ss;
    app(a);
    b.push(heap("n"));
    return 0;
}
`},
		{"alias_of_another_source_grown_by_a_later_argument", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let p = idam(s);
    return use2(r[0], appr(p));
}
`},
		{"shared_later_argument", `fn g(r: &mut int[]) -> int {
    return use2(r[0], rdi(r));
}
`},
		{"nested_alias_of_another_source", `fn g(r: &mut int[], s: &mut int[]) -> int {
    return use2(r[0], appr(idam(s)));
}
`},
		{"element_of_another_array_set_by_a_later_argument", `fn g(r: &mut int[], s: &mut int[]) -> int {
    return use2(r[0], seti(&mut s[0]));
}
`},
		{"alias_grown_while_another_array_is_read", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = idam(r);
    let t = use2(s[0], appr(q));
    return t + q[0];
}
`},
		{"mut_element_then_another_field_read", `fn g(k: &mut H) -> int {
    let e = &mut (*k).items[0];
    let n = k.ss.__len() to int;
    *e = n;
    return 0;
}
`},
		{"mut_element_then_another_array_grown", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let e = &mut r[0];
    app(s);
    *e = 1;
    return 0;
}
`},
		{"mut_element_through_the_alias_then_the_source_read_by_a_call", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let e = &mut q[0];
    let n = rdi(r);
    *e = n;
    return 0;
}
`},
		{"element_copied_out_then_a_grow_through_the_alias", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    let e: int = r[0];
    let f = r[1] + 0;
    app(q);
    return e + f;
}
`},
		{"aliases_of_two_sources", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = idam(r);
    let p = idam(s);
    let e = &mut q[0];
    app(p);
    *e = 5;
    return 0;
}
`},
		{"alias_used_then_the_source_used", `fn g(r: &mut int[]) -> int {
    let q = idam(r);
    q.push(1);
    r.push(2);
    return rdi(q) + rdi(r);
}
`},
		{"alias_of_two_sources_grown_after_its_element_is_dead", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = pickm(r, s);
    { let e = &mut q[0]; *e = 1; }
    app(q);
    app(r);
    return q[0];
}
`},
		{"alias_of_two_sources_pushed_and_replaced", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let q = pickm(r, s);
    q.push(1);
    *q = [5];
    return 0;
}
`},
		{"alias_of_two_sources_grown_while_another_array_is_held", `fn g(r: &mut int[], s: &mut int[], t: &mut int[]) -> int {
    let q = pickm(r, s);
    let e = &mut t[0];
    app(q);
    *e = 2;
    return 0;
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, aliasThroughRefPrelude+row.text)
		})
	}
}
