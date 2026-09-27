package driver

import (
	"testing"

	"surge/internal/diag"
)

// `xs[1].__len()` borrows `xs` for the element reference `xs[1]`, and that
// reference is a statement temporary: the method reads it and returns a value
// that carries no borrow. Its loan ends with its statement (an `if` or `while`
// condition's before the body), so the owner can be dropped, moved or
// reassigned after it; a loan some binding holds still lives to its lexical end.

func TestStatementTemporaryLoanEndsWithItsStatement(t *testing.T) {
	rows := []struct{ name, body string }{
		{"method_on_an_element_then_drop", `    let xs: string[] = [a + "x", a + "y"];
    let n = xs[1].__len();
    @drop xs;
    return n;`},
		{"method_on_an_element_then_move", `    let xs: string[] = [a + "x", a + "y"];
    let n = xs[1].__len();
    let z = own xs;
    return n;`},
		{"method_on_an_element_then_reassign", `    let mut xs: string[] = [a + "x", a + "y"];
    let n = xs[1].__len();
    xs = [a + "z"];
    return n;`},
		{"slice_element_then_drop_the_slice", `    let mut arr: string[] = [];
    arr.push(a + "p");
    arr.push(a + "q");
    let v: string[] = arr.slice(0..2);
    let n = v[1].__len();
    @drop v;
    return n;`},
		{"element_passed_by_reference_then_drop", `    let xs: string[] = [a + "x", a + "y"];
    print(xs[1]);
    @drop xs;
    return 0:uint;`},
		{"element_in_a_loop_then_drop", `    let xs: string[] = [a + "x", a + "y"];
    let mut i = 0;
    let mut n: uint = 0:uint;
    while i < 2 { n = n + xs[1].__len(); i = i + 1; }
    @drop xs;
    return n;`},
		{"element_in_an_if_condition_then_drop_in_the_body", `    let xs: string[] = [a + "x", a + "y"];
    if xs[1].__len() > 0:uint { @drop xs; }
    return 0:uint;`},
		{"element_in_a_while_condition_then_reassign_in_the_body", `    let mut xs: string[] = [a + "x", a + "y"];
    let mut i = 0;
    while xs[1].__len() > 0:uint && i < 1 { xs = [a + "z", a + "w"]; i = i + 1; }
    return 0:uint;`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, dropBorrowFixture(row.body))
		})
	}
}

func TestStatementTemporaryLoanHeldByABindingStaysBorrowed(t *testing.T) {
	rows := []struct {
		name, body, snippet string
	}{
		{"element_reference_bound", `    let xs: string[] = [a + "x", a + "y"];
    let r = xs[1];
    @drop xs;
    return r.__len();`, "@drop xs;"},
		{"view_of_an_element", `    let xs: string[] = [a + "x", a + "y"];
    let v = xs[1].bytes();
    @drop xs;
    return v.__len();`, "@drop xs;"},
		{"temporary_then_a_bound_element", `    let xs: string[] = [a + "x", a + "y"];
    let n = xs[1].__len();
    let r = xs[0];
    @drop xs;
    return r.__len();`, "@drop xs;"},
		{"call_result_carries_the_element", `    let xs: string[] = [a + "x", a + "y"];
    let q = ident(&xs[1]);
    @drop xs;
    return q.__len();`, "@drop xs;"},
		{"compare_arm_binds_the_element", `    let xs: string[] = [a + "x", a + "y"];
    let n = compare xs[1] {
        x => { @drop xs; ret x.__len(); };
    };
    return n;`, "@drop xs;"},
		{"bound_element_outlives_a_loop_of_temporaries", `    let xs: string[] = [a + "x", a + "y"];
    let r = xs[0];
    let mut i = 0;
    while i < 2 { let n = xs[1].__len(); i = i + 1; }
    @drop xs;
    return r.__len();`, "@drop xs;"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, dropBorrowFixture(row.body), diag.SemaBorrowMove, row.snippet)
		})
	}
}

// A call whose result can point into the element, or a parameter the callee
// could store it through, keeps the index temporary's loan to the block's end:
// `Range<T>` is `{ __state: *byte }`, a raw pointer is a raw pointer, and a
// `&mut` parameter counts behind an alias too.
func TestStatementTemporaryLoanKeptWhenTheCallCanKeepIt(t *testing.T) {
	rangeTail := `
    let mut total: int = 0;
    while true {
        let v: int = compare it.next() { Some(v) => v; _ => -1; };
        if v < 0 { break; }
        total = total + v;
    }
    return total to uint;`
	rows := []struct {
		name, decls, body, snippet string
		code                       diag.Code
	}{
		{"range_of_an_element_then_reassign", "", `    let mut yss: int[][] = [[1, 2], [10, 20]];
    let mut it: Range<int> = yss[1].__range();
    yss = [[7, 7, 7]];` + rangeTail, "yss = [[7, 7, 7]];", diag.SemaBorrowMutation},
		{"range_of_an_element_then_move", "", `    let mut yss: int[][] = [[1, 2], [10, 20]];
    let mut it: Range<int> = yss[1].__range();
    let z = own yss;` + rangeTail, "own yss;", diag.SemaBorrowMove},
		{"range_of_an_element_then_store_into_it", "", `    let mut yss: int[][] = [[1, 2], [10, 20]];
    let mut it: Range<int> = yss[1].__range();
    yss[1] = [9, 9];` + rangeTail, "yss[1] = [9, 9];", diag.SemaBorrowMutation},
		{"raw_pointer_of_an_element_then_move", "", `    let xs: string[] = [a + "x", a + "y"];
    let p = rt_string_ptr(xs[1]);
    let z = own xs;
    return 0:uint;`, "own xs;", diag.SemaBorrowMove},
		{"mut_parameter_behind_an_alias", "type MS = &mut string;\nfn put(v: MS, x: &string) -> nothing { return nothing; }\n", `    let xs: string[] = [a + "x", a + "y"];
    let mut u = a + "u";
    put(&mut u, xs[1]);
    let z = own xs;
    return 0:uint;`, "own xs;", diag.SemaBorrowMove},
		{"mut_array_parameter_behind_an_alias", "type MA = &mut Array<&string>;\nfn put(v: MA, x: &string) -> nothing { return nothing; }\n", `    let xs: string[] = [a + "x", a + "y"];
    let mut rs: Array<&string> = [];
    put(&mut rs, xs[1]);
    let z = own xs;
    return 0:uint;`, "own xs;", diag.SemaBorrowMove},
		{"reference_through_two_calls_then_reassign", "", `    let mut xs: string[] = [a + "x", a + "y"];
    let q = ident(ident(xs[1]));
    xs = [a + "p", a + "q"];
    return q.__len();`, "xs = [a + \"p\", a + \"q\"];", diag.SemaBorrowMutation},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			text := refOutwardPrelude + row.decls + "fn f(a: string) -> uint {\n" + row.body + "\n}\n"
			refOutwardRefusal(t, text, row.code, row.snippet)
		})
	}
}
