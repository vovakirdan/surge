package driver

import (
	"testing"

	"surge/internal/diag"
)

const bytesViewCallHelpers = `fn view(s: &string) -> BytesView {
    return s.bytes();
}

fn window(xs: &int[]) -> int[] {
    return xs[[0..2]];
}

`

// A result type that carries a borrow keeps every shared-reference argument's
// implicit loan alive.  These rows exercise a user function (rather than the
// core constructor) and both the direct place and write-through-&mut paths.
func TestBytesViewReturnedByCallKeepsArgumentBorrowed(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"move", bytesViewCallHelpers + bytesViewSink + `fn f(s: string) -> uint8 {
    let v = view(&s);
    sink(own s);
    return v[0];
}
`, "own s);", diag.SemaBorrowMove},
		{"reassignment", bytesViewCallHelpers + `fn f(t: string) -> uint8 {
    let mut s: string = t;
    let v = view(&s);
    s = "new";
    return v[0];
}
`, `s = "new"`, diag.SemaBorrowMutation},
		{"assignment_through_mut_ref", bytesViewCallHelpers + `fn f(s: &mut string) -> uint8 {
    let v = view(s);
    *s = "new";
    return v[0];
}
`, `*s = "new"`, diag.SemaBorrowMutation},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, row.text, row.code, row.snippet)
		})
	}
}

func TestBytesViewReturnedByCallControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"view_dead_in_earlier_block", bytesViewCallHelpers + bytesViewSink + `fn f(s: string) -> uint8 {
    let mut first: uint8 = 0:uint8;
    { let v = view(&s); first = v[0]; }
    sink(own s);
    return first;
}
`},
		{"copy_argument", bytesViewCallHelpers + `fn copied(n: &int) -> int { return *n; }
fn f() -> int {
    let n: int = 7;
    let copy = copied(&n);
    return n + copy;
}
`},
		{"view_rederived_after_move", bytesViewCallHelpers + `fn f(s: string) -> uint8 {
    let t: string = s;
    let v = view(&t);
    return v[0];
}
`},
		{"plain_string_result", bytesViewCallHelpers + `fn copy(s: &string) -> string { return "copy"; }
fn f(s: string) -> uint { let c = copy(&s); return s.__len() + c.__len(); }
`},
		{"plain_int_result", bytesViewCallHelpers + `fn size(s: &string) -> uint { return s.__len(); }
fn f(s: string) -> uint { let n = size(&s); return s.__len() + n; }
`},
		// Windows retain their allocation at runtime.  A window returned by a
		// call therefore does not turn the call's `&` argument into a live loan.
		{"window_move", bytesViewCallHelpers + `fn sinka(xs: own int[]) -> nothing { return nothing; }
fn f(xs: int[]) -> int { let w = window(&xs); sinka(own xs); return w[0]; }
`},
		{"window_reassignment", bytesViewCallHelpers + `fn f(t: int[]) -> int {
    let mut xs: int[] = t; let w = window(&xs); xs = [7, 8]; return w[0];
}
`},
		{"window_assignment_through_mut_ref", bytesViewCallHelpers + `fn f(xs: &mut int[]) -> int {
    let w = window(xs); *xs = [7, 8]; return w[0];
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { bytesViewAccepted(t, row.text) })
	}
}

const bytesViewHolderHelpers = `type Holder = { s: string };

fn getr(h: &Holder) -> &string {
    return &(*h).s;
}

fn sinkh(h: own Holder) -> nothing {
    return nothing;
}

`

// A reference produced by a call names no place, so the view taken from it
// has no child loan of its own to record. It needs none: `getr` returns a
// reference, so the call keeps its own `&h` argument borrowed, and `h` stays
// held while the view lives. The first rows pin that such a view is accepted
// (it used to be refused as "not addressable"); the last ones pin that the
// source behind the call is still protected.
func TestBytesViewOfACallReturnedReference(t *testing.T) {
	accepted := []struct{ name, text string }{
		{"core_receiver", bytesViewHolderHelpers + `fn f(t: string) -> uint {
    let h = Holder { t };
    let v = getr(&h).bytes();
    return v.__len();
}
`},
		{"user_argument", bytesViewCallHelpers + bytesViewHolderHelpers + `fn f(t: string) -> uint8 {
    let h = Holder { t };
    let v = view(getr(&h));
    return v[0];
}
`},
	}
	for _, row := range accepted {
		t.Run(row.name, func(t *testing.T) { bytesViewAccepted(t, row.text) })
	}
	refused := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"core_receiver_source_moved", bytesViewHolderHelpers + `fn f(t: string) -> uint {
    let h = Holder { t };
    let v = getr(&h).bytes();
    sinkh(own h);
    return v.__len();
}
`, "own h);", diag.SemaBorrowMove},
		{"user_argument_source_overwritten", bytesViewCallHelpers + bytesViewHolderHelpers + `fn f(t: string) -> uint8 {
    let mut h = Holder { t };
    let v = view(getr(&h));
    h.s = "new";
    return v[0];
}
`, `h.s = "new"`, diag.SemaBorrowMutation},
	}
	for _, row := range refused {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, row.text, row.code, row.snippet)
		})
	}
}

// A reborrow chain stacks one exclusive loan per level: `let r2 = &mut *r`
// leaves r's loan on the place and adds r2's. A write through r2 is authorized
// by the whole chain, not only by its first link, so plain int code that
// writes through a reborrow stays accepted. A live shared loan -- a view taken
// through the chain -- still freezes the place whichever link writes.
func TestWriteThroughAReborrowChain(t *testing.T) {
	accepted := []struct{ name, text string }{
		{"local_chain", `fn f() -> int {
    let mut s: int = 1;
    let r = &mut s;
    let r2 = &mut *r;
    *r2 = 5;
    return 0;
}
`},
		{"local_chain_then_parent_write", `fn f() -> int {
    let mut s: int = 1;
    let r = &mut s;
    let r2 = &mut *r;
    *r2 = 5;
    *r = 6;
    return s;
}
`},
		{"param_chain", `fn f(p: &mut int) -> nothing {
    let r2 = &mut *p;
    *r2 = 5;
    *p = 6;
    return nothing;
}
`},
	}
	for _, row := range accepted {
		t.Run(row.name, func(t *testing.T) { bytesViewAccepted(t, row.text) })
	}
	refused := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"view_blocks_write_through_chain", bytesViewCallHelpers + `fn f(t: string) -> uint8 {
    let mut s: string = t;
    let r = &mut s;
    let r2 = &mut *r;
    let v = view(r2);
    *r2 = "new";
    return v[0];
}
`, `*r2 = "new"`, diag.SemaBorrowMutation},
		{"view_blocks_write_through_parent", bytesViewCallHelpers + `fn f(t: string) -> uint8 {
    let mut s: string = t;
    let r = &mut s;
    let r2 = &mut *r;
    let v = view(r2);
    *r = "new";
    return v[0];
}
`, `*r = "new"`, diag.SemaBorrowMutation},
		{"view_blocks_write_through_param_chain", bytesViewCallHelpers + `fn f(p: &mut string) -> uint8 {
    let r2 = &mut *p;
    let v = view(r2);
    *r2 = "new";
    return v[0];
}
`, `*r2 = "new"`, diag.SemaBorrowMutation},
	}
	for _, row := range refused {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, row.text, row.code, row.snippet)
		})
	}
}

// Writes through `&mut` are checked against the referent's live loans, so a
// shared reference to a field of a `&mut` parameter freezes that field. And a
// call returning a BytesView keeps EVERY `&` argument borrowed, not only the
// one the view reads: the rule asks the result's type, not its origin.
func TestBytesViewCallConservativeRows(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"shared_field_ref_of_mut_param", `type Pair = { n: int, s: string };
fn f(h: &mut Pair) -> int {
    let r = &h.n;
    h.n = 5;
    return *r;
}
`, "h.n = 5", diag.SemaBorrowMutation},
		{"two_args_first_moved", bytesViewSink + `fn two(a: &string, b: &string) -> BytesView { return a.bytes(); }
fn f(a: string, b: string) -> uint8 {
    let v = two(&a, &b);
    sink(own a);
    return v[0];
}
`, "own a);", diag.SemaBorrowMove},
		{"two_args_second_moved", bytesViewSink + `fn two(a: &string, b: &string) -> BytesView { return a.bytes(); }
fn f(a: string, b: string) -> uint8 {
    let v = two(&a, &b);
    sink(own b);
    return v[0];
}
`, "own b);", diag.SemaBorrowMove},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, row.text, row.code, row.snippet)
		})
	}
}
