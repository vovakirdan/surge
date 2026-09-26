package driver

import (
	"testing"

	"surge/internal/diag"
)

// Two exclusive reborrows of one reference alias each other, so the second is
// refused as `&mut s` twice is. Which exclusive loan the table reported first
// used to depend on map iteration order, and the same program was accepted on
// most runs and refused on some. The row asks many times, so an answer that
// depends on that order cannot pass.
func TestSiblingReborrowsAreRefusedOnEveryRun(t *testing.T) {
	const text = `fn f() -> int {
    let mut s: int = 1;
    let r = &mut s;
    let a = &mut *r;
    let b = &mut *r;
    *b = 2;
    *a = 3;
    return 0;
}
`
	for range 16 {
		bytesViewRefusal(t, text, diag.SemaBorrowConflict, "&mut *r;\n    *b")
	}
}

// A reborrow chain is authorized by every link, so a write through its third
// link is accepted -- on every run, not only when a map range happens to yield
// the parent's loan first.
func TestThreeLinkReborrowChainIsAcceptedOnEveryRun(t *testing.T) {
	const text = `fn f() -> int {
    let mut s: int = 1;
    let r = &mut s;
    let r2 = &mut *r;
    let r3 = &mut *r2;
    *r3 = 5;
    return 0;
}
`
	for range 16 {
		bytesViewAccepted(t, text)
	}
}

// A reborrow that ended with its block does not hold the reference, so the
// next reborrow of it is accepted.
func TestSiblingReborrowAfterItsBlockStaysAccepted(t *testing.T) {
	bytesViewAccepted(t, `fn f() -> int {
    let mut s: int = 1;
    let r = &mut s;
    { let a = &mut *r; *a = 1; }
    let b = &mut *r;
    *b = 2;
    return 0;
}
`)
}

// The shapes a view reaches its string through besides a direct core call: a
// `&mut string` parameter, a user function taking `&mut`, a user method, and a
// view nested in a struct, an Option or a tuple result. Each keeps the string
// borrowed while the result lives.
func TestBytesViewThroughAMutRefOrAUserResultKeepsItsString(t *testing.T) {
	const helpers = bytesViewSink + `type Holder = { s: string };

extern<Holder> {
    fn hv(self: &Holder) -> BytesView { return self.s.bytes(); }
}

fn sinkh(h: own Holder) -> nothing {
    return nothing;
}

`
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"core_view_of_mut_param_assigned", `fn f(s: &mut string) -> uint8 {
    let v = s.bytes();
    *s = "new";
    return v[0];
}
`, `*s = "new"`, diag.SemaBorrowMutation},
		{"core_view_of_field_of_mut_param_assigned", `type Holder = { s: string };
fn f(h: &mut Holder) -> uint8 {
    let v = h.s.bytes();
    h.s = "new";
    return v[0];
}
`, `h.s = "new"`, diag.SemaBorrowMutation},
		{"user_view_of_mut_ref_reassigned", `fn view_m(s: &mut string) -> BytesView {
    return s.bytes();
}

fn f(t: string) -> uint8 {
    let mut s: string = t;
    let v = view_m(&mut s);
    s = "new";
    return v[0];
}
`, `s = "new"`, diag.SemaBorrowMutation},
		{"user_method_view_source_moved", helpers + `fn f(t: string) -> uint8 {
    let h = Holder { t };
    let v = h.hv();
    sinkh(own h);
    return v[0];
}
`, "own h);", diag.SemaBorrowMove},
		{"view_in_struct_result_moved", bytesViewSink + `type VH = { v: BytesView };
fn mk(s: &string) -> VH { return VH { s.bytes() }; }
fn f(s: string) -> uint8 {
    let h = mk(&s);
    sink(own s);
    return h.v[0];
}
`, "own s);", diag.SemaBorrowMove},
		{"view_in_option_result_moved", bytesViewSink + `fn ov(s: &string) -> Option<BytesView> { return Some(s.bytes()); }
fn f(s: string) -> uint8 {
    let o = ov(&s);
    sink(own s);
    return compare o { Some(v) => v[0]; nothing => 0:uint8; };
}
`, "own s);", diag.SemaBorrowMove},
		{"view_in_tuple_result_moved", bytesViewSink + `fn tv(s: &string) -> (BytesView, int) { return (s.bytes(), 1); }
fn f(s: string) -> uint8 {
    let p = tv(&s);
    sink(own s);
    return p.0[0];
}
`, "own s);", diag.SemaBorrowMove},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, row.text, row.code, row.snippet)
		})
	}
}
