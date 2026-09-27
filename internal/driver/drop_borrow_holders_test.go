package driver

import (
	"testing"

	"surge/internal/diag"
)

// `@drop x` ends a binding before its block does (LANGUAGE.md §Lexical
// Lifetimes and Early Drop). Two ways it let a live reference outlive what it
// points at:
//
//   - A reference binding's drop released its loan even when another binding
//     held a copy of it: `let q = &s; let r = q; @drop q; sink(own s);
//     r.__len()` was accepted and natively read freed memory (valgrind: invalid
//     read in rt_string_len). The loan is now released only when no other
//     binding in scope holds it, and the dropped binding itself cannot be read
//     afterwards.
//   - Dropping the OWNER never asked the borrow table: `let r = &s; @drop s;`
//     was refused only by the return-origin analysis, or not at all. A drop is
//     now checked as a move is.

func dropBorrowFixture(body string) string {
	return refOutwardPrelude + "type Named = { name: string, k: int, k32: int32 };\n\n" +
		"fn first(xs: &string[]) -> &string {\n    return xs[0];\n}\n\n" +
		"fn f(a: string, t: string) -> uint {\n    let mut s: string = t;\n" + body + "\n}\n"
}

func TestDropOfAReferenceKeepsALoanAnotherBindingHolds(t *testing.T) {
	rows := []struct {
		name, body, snippet string
		code                diag.Code
	}{
		{"let_copy", `    let q = &s;
    let r = q;
    @drop q;
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"assignment_copy", `    let mut r = &a;
    let q = &s;
    r = q;
    @drop q;
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"assignment_copy_in_inner_block", `    let mut r = &a;
    let q = &s;
    { r = q; }
    @drop q;
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"copy_through_a_call", `    let q = &s;
    let r = ident(q);
    @drop q;
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		// The copy lives inside an aggregate that carries a reference.
		{"copy_inside_an_option", `    let mut m = Map::<string, string>::new();
    m.insert(a + "k", a + "v");
    let k = a + "k";
    let q = &m;
    let o = q.get_ref(&k);
    @drop q;
    let z = own m;
    return compare o { Some(x) => x.__len(); nothing => 0:uint; };`, "own m;", diag.SemaBorrowMove},
		{"copy_declared_in_inner_block", `    let q = &s;
    {
        let r = q;
        @drop q;
        sink(own s);
        let n = r.__len();
    }
    return 0:uint;`, "own s);", diag.SemaBorrowMove},
		{"copy_dropped_original_read", `    let q = &s;
    let r = q;
    @drop r;
    sink(own s);
    return q.__len();`, "own s);", diag.SemaBorrowMove},
		// Indexing binds a reference into the array; its copy holds the loan too.
		{"copy_of_an_element_reference", `    let xs: string[] = [a + "x"];
    let e = xs[0];
    let c = e;
    @drop e;
    let z = own xs;
    return c.__len();`, "own xs;", diag.SemaBorrowMove},
		// A compare arm's pattern binding copies what its scrutinee carries.
		{"arm_binding_of_the_reference", `    let q = &s;
    let n = compare q {
        x => { @drop q; sink(own s); ret x.__len(); };
    };
    return n;`, "own s);", diag.SemaBorrowMove},
		{"arm_binding_of_an_element_reference", `    let xs: string[] = [a + "x"];
    let b = xs[0];
    let n = compare b {
        x => { @drop b; let z = own xs; ret x.__len(); };
    };
    return n;`, "own xs;", diag.SemaBorrowMove},
		{"arm_binding_of_a_call_result", `    let mut m = Map::<string, string>::new();
    m.insert(a + "k", a + "v");
    let k = a + "k";
    let q = &m;
    let n = compare q.get_ref(&k) {
        Some(x) => { @drop q; let z = own m; ret x.__len(); };
        nothing => 0:uint;
    };
    return n;`, "own m;", diag.SemaBorrowMove},
		// A field that owns no heap is not refused at its drop (the gate a whole
		// binding has); reading it through the reference is.
		{"storage_free_field_read_after_its_drop", `    let mut o = Named { name = a + "n", k = 1, k32 = 2:int32 };
    let r = &o.k32;
    @drop o.k32;
    let n = *r;
    return 0:uint;`, "*r;", diag.SemaUseAfterMove},
		// A compare used as a value hands out its chosen arm's value, so the
		// binding it initializes holds every arm value's loans.
		{"compare_value_of_an_arm_binding", `    let q = &s;
    let r = compare q { x => x; };
    @drop q;
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"compare_value_of_either_arm", `    let q = &s;
    let k = 0;
    let r = compare k { 0 => q; _ => &a; };
    @drop q;
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"compare_value_from_a_block_arm", `    let q = &s;
    let r = compare q { x => { let n = x.__len(); ret x; }; };
    @drop q;
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"compare_value_assigned", `    let q = &s;
    let mut r = &a;
    r = compare q { x => x; };
    @drop q;
    sink(own s);
    return r.__len();`, "own s);", diag.SemaBorrowMove},
		{"compare_value_through_an_option", `    let mut m = Map::<string, string>::new();
    m.insert(a + "k", a + "v");
    let k = a + "k";
    let q = &m;
    let r = compare q.get_ref(&k) { Some(x) => x; nothing => &a; };
    @drop q;
    let z = own m;
    return r.__len();`, "own m;", diag.SemaBorrowMove},
		// An arm binding is a reference like any other: dropped, it is gone.
		{"dropped_arm_binding_read", `    let q = &s;
    let n = compare q { x => { @drop x; ret x.__len(); }; };
    return n;`, "x.__len()", diag.SemaUseAfterMove},
		// The dropped binding is gone: reading it would read the moved `s`.
		{"dropped_reference_read_after_move", `    let q = &s;
    @drop q;
    sink(own s);
    return q.__len();`, "q.__len()", diag.SemaUseAfterMove},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, dropBorrowFixture(row.body), row.code, row.snippet)
		})
	}
}

func TestDropOfABorrowedOwnerIsRefused(t *testing.T) {
	rows := []struct {
		name, body, snippet string
	}{
		{"shared_reference", `    let r = &s;
    @drop s;
    return r.__len();`, "@drop s;"},
		{"mutable_reference", `    let m = &mut s;
    @drop s;
    return m.__len();`, "@drop s;"},
		{"view", `    let v = s.bytes();
    @drop s;
    return v.__len();`, "@drop s;"},
		{"view_from_user_function", `    let v = vw(&s);
    @drop s;
    return v.__len();`, "@drop s;"},
		{"view_in_array", `    let vs: BytesView[] = [s.bytes()];
    @drop s;
    return vs[0].__len();`, "@drop s;"},
		{"view_in_struct", `    let h = ViewHolder { v = s.bytes() };
    @drop s;
    return h.v.__len();`, "@drop s;"},
		{"reference_stored_outward", `    let mut r = &a;
    { r = &s; }
    @drop s;
    return r.__len();`, "@drop s;"},
		{"inside_the_block_that_stored_it", `    let mut r = &a;
    { r = &s; @drop s; }
    return r.__len();`, "@drop s;"},
		{"borrowed_field", `    let mut o = Named { name = a + "n", k = 1, k32 = 2:int32 };
    let r = &o.name;
    @drop o.name;
    return r.__len();`, "@drop o.name;"},
		{"array_under_an_element_reference", `    let xs: string[] = [a + "x"];
    let e = xs[0];
    @drop xs;
    return e.__len();`, "@drop xs;"},
		// Kept refused on purpose: `@drop v` does not release a view's loan,
		// because a view can be copied where no binding records it
		// (`vs.push(v)`), so the owner stays borrowed to the view's lexical end
		// -- as `@drop v; sink(own s);` already was.
		{"owner_after_its_view_was_dropped", `    let v = s.bytes();
    let n = v.__len();
    @drop v;
    @drop s;
    return n;`, "@drop s;"},
		{"scalar_owner", `    let x = 5;
    let r = &x;
    @drop x;
    let n = *r;
    return 0:uint;`, "@drop x;"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, dropBorrowFixture(row.body), diag.SemaBorrowMove, row.snippet)
		})
	}
}

// A reference an `@drop` ends inside a loop body, but declared outside it, is
// read dead on the next turn -- the rule a heap owner already gets.
func TestDropOfAReferenceInsideALoop(t *testing.T) {
	rows := []struct{ name, body, snippet string }{
		{"while_read_on_the_next_turn", `    let q = &s;
    let mut i = 0;
    while i < 2 { let n = q.__len(); @drop q; i = i + 1; }
    return 0:uint;`, "@drop q;"},
		{"element_reference_then_owner_reassigned", `    let mut xs: string[] = [a + "x"];
    let b = xs[0];
    let mut i = 0;
    while i < 2 { let n = b.__len(); @drop b; xs = [a + "y"]; i = i + 1; }
    return 0:uint;`, "@drop b;"},
		{"call_result_then_owner_reassigned", `    let mut xs: string[] = [a + "x"];
    let b = first(&xs);
    let mut i = 0;
    while i < 2 { let n = b.__len(); @drop b; xs = [a + "y"]; i = i + 1; }
    return 0:uint;`, "@drop b;"},
		{"for_in_body", `    let q = &s;
    for x in [1, 2] { let n = q.__len(); @drop q; }
    return 0:uint;`, "@drop q;"},
		{"outer_loop_around_an_inner_one", `    let q = &s;
    let mut i = 0;
    while i < 2 { let mut j = 0; while j < 1 { j = j + 1; } let n = q.__len(); @drop q; i = i + 1; }
    return 0:uint;`, "@drop q;"},
		// A `continue` re-enters with the state it has, not the body's end's.
		{"continue_edge_carries_the_drop", `    let mut xs: string[] = [a + "x"];
    let mut b = xs[0];
    let mut i = 0;
    while i < 3 { i = i + 1; let n = b.__len(); @drop b; xs = [a + "y"]; if i == 1 { continue; } b = &a; }
    return 0:uint;`, "@drop b;"},
		{"continue_edge_carries_a_heap_owner_move", `    let mut t2 = a + "t";
    let mut i = 0;
    while i < 3 { i = i + 1; sink(own t2); if i == 1 { continue; } t2 = a + "u"; }
    return 0:uint;`, "own t2)"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, dropBorrowFixture(row.body), diag.SemaUseAfterMove, row.snippet)
		})
	}
}

// Each guard on a legitimate input: the loan is released when its last holder
// goes, and an owner whose borrows are over may be dropped.
func TestDropOfBorrowsControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, body string }{
		{"drop_reference_then_move", `    let q = &s;
    let n = q.__len();
    @drop q;
    sink(own s);
    return n;`},
		{"drop_every_alias_then_move", `    let q = &s;
    let r = q;
    @drop q;
    @drop r;
    sink(own s);
    return 0:uint;`},
		{"copy_died_with_its_block", `    let q = &s;
    { let r = q; let n = r.__len(); }
    @drop q;
    sink(own s);
    return 0:uint;`},
		{"copy_died_with_its_arm", `    let q = &s;
    compare 1 { 1 => { let r = q; let n = r.__len(); }; _ => {}; };
    @drop q;
    sink(own s);
    return 0:uint;`},
		{"copy_rebound_elsewhere", `    let q = &s;
    let mut r = q;
    r = &a;
    @drop q;
    sink(own s);
    return r.__len();`},
		{"mutable_reference_dropped_then_move", `    let m = &mut s;
    let n = m.__len();
    @drop m;
    sink(own s);
    return n;`},
		{"owner_dropped_after_its_borrow_block", `    { let r = &s; let n = r.__len(); }
    @drop s;
    return 0:uint;`},
		{"owner_dropped_after_its_view_block", `    { let v = s.bytes(); let n = v.__len(); }
    @drop s;
    return 0:uint;`},
		{"owner_dropped_after_its_reference", `    let r = &s;
    let n = r.__len();
    @drop r;
    @drop s;
    return n;`},
		// An element reference holds the loan its index keeps, so dropping it
		// ends that loan (testdata/golden/vm_rc/vm_rc_chain.sg).
		{"array_dropped_after_its_element_references", `    let xs: string[] = [a + "x"];
    let e = xs[0];
    let f2 = xs[0];
    @drop e;
    @drop f2;
    @drop xs;
    return 0:uint;`},
		// A value that owns no heap is not released by its drop, so nothing is
		// freed under the borrow (return-origin leaf drop_copy_then_read).
		{"storage_free_owner_under_a_reference", `    let x: int32 = 5;
    let r = &x;
    @drop x;
    let y: int32 = *r;
    return 0:uint;`},
		{"arm_binding_died_before_the_drop", `    let q = &s;
    let n = compare q {
        x => { ret x.__len(); };
    };
    @drop q;
    sink(own s);
    return n;`},
		{"element_reference_dropped_then_move", `    let xs: string[] = [a + "x"];
    let b = xs[0];
    let n = b.__len();
    @drop b;
    let z = own xs;
    return n;`},
		{"loop_drop_then_reassign", `    let mut q = &a;
    let mut i = 0;
    while i < 2 { let n = q.__len(); @drop q; q = &s; i = i + 1; }
    return 0:uint;`},
		{"drop_then_reassign", `    let mut q = &s;
    @drop q;
    q = &a;
    return q.__len();`},
		{"reference_declared_in_the_loop_body", `    for x in [1, 2] { let q = &s; let n = q.__len(); @drop q; }
    let mut i = 0;
    while i < 2 { let q = &s; let n = q.__len(); @drop q; i = i + 1; }
    sink(own s);
    return 0:uint;`},
		{"drop_after_the_loop", `    let q = &s;
    let mut i = 0;
    while i < 2 { let n = q.__len(); i = i + 1; }
    @drop q;
    sink(own s);
    return 0:uint;`},
		// A place that owns no heap frees nothing under the borrow, so its drop
		// is not refused; the read through the reference is (a moved place).
		{"storage_free_field_under_a_reference_until_read", `    let mut o = Named { name = a + "n", k = 1, k32 = 2:int32 };
    let r = &o.k32;
    @drop o.k32;
    return 0:uint;`},
		{"continue_after_a_rebind_on_every_path", `    let mut q = &a;
    let mut i = 0;
    while i < 3 { i = i + 1; let n = q.__len(); @drop q; if i == 2 { q = &a; continue; } q = &s; }
    return 0:uint;`},
		{"continue_before_the_drop", `    let mut q = &a;
    let mut i = 0;
    while i < 3 { i = i + 1; if i == 1 { continue; } let n = q.__len(); @drop q; q = &s; }
    return 0:uint;`},
		{"heap_owner_restored_before_continue", `    let mut t2 = a + "t";
    let mut i = 0;
    while i < 3 { i = i + 1; sink(own t2); t2 = a + "u"; if i == 1 { continue; } }
    return 0:uint;`},
		{"compare_value_owned", `    let q = &s;
    let r = compare q { x => x.__len(); };
    @drop q;
    sink(own s);
    return r;`},
		{"compare_value_of_a_live_owner", `    let q = &s;
    let k = 0;
    let r = compare k { 0 => q; _ => &a; };
    @drop q;
    return r.__len();`},
		{"unborrowed_owner", `    @drop s;
    return 0:uint;`},
		{"other_field_borrowed", `    let mut o = Named { name = a + "n", k = 1, k32 = 2:int32 };
    let r = &o.k;
    @drop o.name;
    let n = *r;
    return 0:uint;`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, dropBorrowFixture(row.body))
		})
	}
}
