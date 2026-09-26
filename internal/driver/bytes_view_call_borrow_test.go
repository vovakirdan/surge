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
