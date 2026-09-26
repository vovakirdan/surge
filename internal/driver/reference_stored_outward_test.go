package driver

import (
	"testing"

	"surge/internal/diag"
)

// Lifetimes are lexical: a loan lives to the end of its scope. A borrow is
// registered at the scope it is taken in, and a loan stored into a binding
// declared further out used to expire with the INNER block while the binding
// could still be read -- `let mut r = &a; { r = &s; } sink(own s); r.__len()`
// was accepted and read freed memory natively (valgrind: invalid read in
// rt_string_len). The loan now lasts as long as the binding that holds it,
// which is what `let r = &s;` in the binding's own scope already got.

const refOutwardPrelude = `fn sink(s: own string) -> nothing {
    return nothing;
}

fn ident(x: &string) -> &string {
    return x;
}

type ViewHolder = { v: BytesView };

fn vw(x: &string) -> BytesView {
    return x.bytes();
}

`

func TestReferenceStoredOutwardHoldsItsLoan(t *testing.T) {
	rows := []struct {
		name, body, snippet string
		code                diag.Code
	}{
		{"assigned_in_block", `    let mut r = &a;
    { r = &s; }
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"assigned_in_nested_block", `    let mut r = &a;
    { { r = &s; } }
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"assigned_in_if_body", `    let mut r = &a;
    if a.__len() > 0:uint { r = &s; }
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"assigned_in_else_body", `    let mut r = &a;
    if a.__len() == 0:uint { r = &a; } else { r = &s; }
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"assigned_in_while_body", `    let mut r = &a;
    let mut i = 0;
    while i < 1 { r = &s; i = i + 1; }
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"assigned_in_for_body", `    let mut r = &a;
    for i in 0..1 { r = &s; }
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"moved_in_a_later_block", `    let mut r = &a;
    { r = &s; }
    { sink(own s); }
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"mutated_after_block", `    let mut r = &a;
    { r = &s; }
    s = "c" + "d";
    return r.__len();`, `s = "c" + "d"`, diag.SemaBorrowMutation},
		{"copied_from_inner_binding", `    let mut r = &a;
    { let q = &s; r = q; }
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"call_result_in_block", `    let mut r = &a;
    { r = ident(&s); }
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		// The inner binding's own release used to drop the OUTER binding's loan.
		{"outer_loan_copied_into_inner_binding", `    let q = &s;
    { let r2 = q; let n = r2.__len(); }
    sink(own s);
    return q.__len();`, "own s);", diag.SemaBorrowMove},
		// The core BytesView twins: the view's string borrow is the loan.
		{"view_assigned_in_block", `    let mut v = a.bytes();
    { v = s.bytes(); }
    sink(own s);
    return v[0] to uint;`, "own s);", diag.SemaBorrowMove},
		{"view_assigned_in_if_body", `    let mut v = a.bytes();
    if a.__len() > 0:uint { v = s.bytes(); }
    sink(own s);
    return v[0] to uint;`, "own s);", diag.SemaBorrowMove},
		{"view_assigned_in_for_body", `    let mut v = a.bytes();
    for i in 0..1 { v = s.bytes(); }
    sink(own s);
    return v[0] to uint;`, "own s);", diag.SemaBorrowMove},
		{"view_through_inner_reference", `    let mut v = a.bytes();
    { let q = &s; v = q.bytes(); }
    sink(own s);
    return v[0] to uint;`, "own s);", diag.SemaBorrowMove},
		{"view_copied_from_inner_view", `    let mut v = a.bytes();
    { let w = s.bytes(); v = w; }
    sink(own s);
    return v[0] to uint;`, "own s);", diag.SemaBorrowMove},
		// A user function returning a view keeps its argument borrowed; the
		// kept loan is the one stored outward.
		{"view_from_user_function_in_block", `    let mut v = a.bytes();
    { v = vw(&s); }
    sink(own s);
    return v[0] to uint;`, "own s);", diag.SemaBorrowMove},
		{"view_mutated_after_block", `    let mut v = a.bytes();
    { v = s.bytes(); }
    s = "c" + "d";
    return v[0] to uint;`, `s = "c" + "d"`, diag.SemaBorrowMutation},
		// A view stored into part of an outer aggregate is held for the whole
		// aggregate's binding (the root of the written place).
		{"view_stored_into_array_element", `    let mut vs: BytesView[] = [a.bytes()];
    { vs[0] = s.bytes(); }
    sink(own s);
    return vs[0].__len();`, "own s);", diag.SemaBorrowMove},
		{"view_stored_into_option", `    let mut o: Option<BytesView> = nothing;
    { o = Some(s.bytes()); }
    sink(own s);
    let n = compare o { Some(v) => v.__len(); nothing => 0:uint; };
    return n;`, "own s);", diag.SemaBorrowMove},
		{"view_stored_into_struct_field", `    let mut h = ViewHolder { v = a.bytes() };
    { h.v = s.bytes(); }
    sink(own s);
    return h.v.__len();`, "own s);", diag.SemaBorrowMove},
		{"view_read_back_out_of_inner_holder", `    let mut v = a.bytes();
    { let w = ViewHolder { v = s.bytes() }; v = own w.v; }
    sink(own s);
    return v[0] to uint;`, "own s);", diag.SemaBorrowMove},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, refOutwardFixture(row.body), row.code, row.snippet)
		})
	}
}

func TestReferenceStoredOutwardControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, body string }{
		// The loan truly dies in the inner block: its only holder does.
		{"inner_binding_dies_with_block", `    { let q = &s; let n = q.__len(); }
    sink(own s);
    return 0:uint;`},
		{"inner_binding_reassigned_inside", `    { let mut r = &a; r = &s; let n = r.__len(); }
    sink(own s);
    return 0:uint;`},
		{"temporary_call_borrow_in_block", `    { let n = ident(&s).__len(); }
    sink(own s);
    return 0:uint;`},
		{"outer_reference_kept_inner_one_dies", `    let r = &a;
    { let r2 = &s; let n = r2.__len(); }
    sink(own s);
    return r.__len();`},
		// No move: holding the loan longer refuses nothing that reads.
		{"outward_reference_read_only", `    let mut r = &a;
    { r = &s; }
    return r.__len();`},
		// `@drop` still ends a rehomed loan early.
		{"outward_reference_dropped_before_move", `    let mut r = &a;
    { r = &s; }
    let n = r.__len();
    @drop r;
    sink(own s);
    return n;`},
		// Legitimate input of the release guard: an outer loan shared with an
		// inner binding still ends when its outer holder is dropped.
		{"shared_outer_loan_dropped_by_holder", `    let q = &s;
    { let r2 = q; let n = r2.__len(); }
    @drop q;
    sink(own s);
    return 0:uint;`},
		{"loop_over_borrowed_array", `    let mut xs: string[] = [a];
    for x in &xs { let n = x.__len(); }
    xs.push(own s);
    return 0:uint;`},
		{"view_dies_with_block", `    let v = a.bytes();
    { let w = s.bytes(); let f = w[0]; }
    sink(own s);
    return v[0] to uint;`},
		// The resolver opens a scope for each compare arm and block expression
		// that the checker never pushes: a binding declared there holds its
		// loan to the nearest pushed block, not to the whole file.
		{"arm_binding_in_if_body", `    if a.__len() > 0:uint {
        compare 1 {
            1 => { let q = &s; let n = q.__len(); };
            _ => { let m = 0; };
        };
    }
    sink(own s);
    return 0:uint;`},
		{"block_expression_binding_in_block", `    { let n = { let q = &s; return q.__len(); }; }
    sink(own s);
    return 0:uint;`},
		{"arm_block_expression_binding_in_block", `    let k = 1;
    { let n = compare k { 1 => { let q = &s; return q.__len(); }; _ => 0:uint; }; }
    sink(own s);
    return 0:uint;`},
		{"arm_statement_binding_in_block", `    let k = 1;
    { compare k { 1 => { let q = &s; let m = q.__len(); }; _ => {}; }; }
    sink(own s);
    return 0:uint;`},
		{"arm_view_binding_in_block", `    let k = 1;
    { compare k { 1 => { let w = s.bytes(); let n = w.__len(); }; _ => {}; }; }
    sink(own s);
    return 0:uint;`},
		{"inner_view_array_dies_with_block", `    let vs: BytesView[] = [a.bytes()];
    { let ws: BytesView[] = [s.bytes()]; let n = ws[0].__len(); }
    sink(own s);
    return vs[0].__len();`},
		{"inner_view_option_dies_with_block", `    let o: Option<BytesView> = Some(a.bytes());
    { let p: Option<BytesView> = Some(s.bytes()); }
    sink(own s);
    let n = compare o { Some(v) => v.__len(); nothing => 0:uint; };
    return n;`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, refOutwardFixture(row.body))
		})
	}
}

// refOutwardRefusal is bytesViewRefusal plus: the expected refusal is the ONLY
// error, so a fixture that is wrong for another reason cannot pass as a witness.
func refOutwardRefusal(t *testing.T, text string, code diag.Code, snippet string) {
	t.Helper()
	bytesViewRefusal(t, text, code, snippet)
	result, err := bytesViewDiagnose(t, text)
	if result == nil || result.Bag == nil {
		t.Fatalf("no diagnostics bag: %v", err)
	}
	for _, d := range result.Bag.Items() {
		if d.Severity >= diag.SevError && d.Code != code {
			t.Fatalf("PRECONDITION: the fixture has another error:\n%s", diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
		}
	}
}

func refOutwardFixture(body string) string {
	return refOutwardPrelude + "fn f(a: string, t: string) -> uint {\n    let mut s: string = t;\n" + body + "\n}\n"
}
