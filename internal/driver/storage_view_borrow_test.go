package driver

import (
	"testing"

	"surge/internal/diag"
)

// A range cursor and a window of a FIXED array are views into storage, and the
// checker holds them as it holds a BytesView: a view is a borrow of what it
// reads. Each refusal row below was accepted by the checker before; the
// compare-arm row was then stopped only by return-origin analysis, and every
// other row but the two over a local fixed array (a frame slot the
// reassignment overwrites in place) read freed storage natively -- valgrind:
// invalid read after the reassignment or `rt_realloc`; the VM hid it. A dynamic
// array's window retains its base at runtime and stays accepted.

const storageViewPrelude = `fn cur(xs: &int[]) -> Range<int> {
    return xs.__range();
}

fn first(c: &uint64[4]) -> uint64[] {
    return c[[0..2]];
}

fn window(c: &int[]) -> int[] {
    return c[[0..2]];
}

fn ident(x: &string) -> &string {
    return x;
}

type Win = { cells: uint64[4] };

type Bag = { items: int[] };

`

// throughMutRef assigns through a `&mut` reference while a range it made lives.
const throughMutRef = `fn through(r: &mut int[]) -> int {
    let it = r.__range();
    *r = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16];
    let mut t = 0;
    for v in it { t = t + v; }
    return t;
}

`

func storageViewFixture(body string) string {
	return storageViewPrelude + "fn f(a: string) -> int {\n" + body + "\n}\n"
}

func TestStorageViewKeepsItsSourceBorrowed(t *testing.T) {
	rows := []struct {
		name, body, snippet string
		code                diag.Code
	}{
		{"range_then_reassign", `    let mut ys: int[] = [10, 20, 30, 40];
    let it = ys.__range();
    ys = [7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7];
    let mut t = 0;
    for v in it { t = t + v; }
    return t;`, "ys = [7, 7", diag.SemaBorrowMutation},
		{"range_then_push", `    let mut ys: int[] = [10, 20, 30, 40];
    let it = ys.__range();
    ys.push(5);
    let mut t = 0;
    for v in it { t = t + v; }
    return t;`, "ys.push(5)", diag.SemaBorrowConflict},
		{"range_through_a_user_function", `    let mut ys: int[] = [10, 20, 30, 40];
    let it = cur(&ys);
    ys = [7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7];
    let mut t = 0;
    for v in it { t = t + v; }
    return t;`, "ys = [7, 7", diag.SemaBorrowMutation},
		{"range_stored_from_an_inner_block", `    let mut ys: int[] = [10, 20, 30, 40];
    let zs: int[] = [1];
    let mut it = zs.__range();
    { it = ys.__range(); }
    ys = [7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7];
    let mut t = 0;
    for v in it { t = t + v; }
    return t;`, "ys = [7, 7", diag.SemaBorrowMutation},
		{"range_of_an_element_stored_from_an_inner_block", `    let mut yss: int[][] = [];
    let row: int[] = [10, 20, 30, 40];
    yss.push(row);
    let zs: int[] = [1];
    let mut it = zs.__range();
    { it = yss[0].__range(); }
    yss = [];
    let mut t = 0;
    for v in it { t = t + v; }
    return t;`, "yss = [];", diag.SemaBorrowMutation},
		{"range_of_a_fixed_field_then_reassign", `    let mut b: Win = Win { cells = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };
    let it = b.cells.__range();
    b = Win { cells = [5:uint64, 6:uint64, 7:uint64, 8:uint64] };
    let mut t: uint64 = 0:uint64;
    for v in it { t = t + v; }
    return t to int;`, "b = Win", diag.SemaBorrowMutation},
		{"range_handed_out_of_a_compare_arm", `    let mut ys: int[] = [10, 20, 30, 40];
    let zs: int[] = [1];
    let mut out = zs.__range();
    {
        let o: Option<Range<int>> = Some(ys.__range());
        compare o { Some(it) => { out = it; }; nothing => {}; };
    }
    ys = [7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7];
    let mut t = 0;
    for v in out { t = t + v; }
    return t;`, "ys = [7, 7", diag.SemaBorrowMutation},
		{"for_loop_pushes_onto_its_iterable", `    let mut ys: int[] = [10, 20, 30, 40];
    let mut total = 0;
    for v in ys { total = total + v; ys.push(1); }
    return total;`, "ys.push(1)", diag.SemaBorrowConflict},
		{"for_loop_reassigns_its_iterable", `    let mut ys: int[] = [10, 20, 30, 40];
    let mut total = 0;
    for v in ys { total = total + v; ys = [1, 2, 3, 4, 5, 6, 7, 8]; }
    return total;`, "ys = [1, 2", diag.SemaBorrowMutation},
		{"for_loop_pushes_onto_its_iterable_field", `    let mut b = Bag { items = [10, 20, 30, 40] };
    let mut total = 0;
    for v in b.items { total = total + v; b.items.push(v); }
    return total;`, "b.items.push(v)", diag.SemaBorrowConflict},
		{"element_reference_through_a_call_in_an_inner_block", `    let mut xs: string[] = [a + "x", a + "y"];
    let mut r: &string = &a;
    { r = ident(xs[1]); }
    xs = [a + "p", a + "q"];
    return r.__len() to int;`, "xs = [a + \"p\"", diag.SemaBorrowMutation},
		{"fixed_window_through_a_call_then_realloc", `    let mut xs: Array<uint64[4]> = [];
    xs.push([11:uint64, 22:uint64, 33:uint64, 44:uint64]);
    let v = first(&xs[0]);
    xs.push([1:uint64, 2:uint64, 3:uint64, 4:uint64]);
    return v[1] to int;`, "xs.push([1:uint64", diag.SemaBorrowConflict},
		{"fixed_window_of_an_element_stored_from_an_inner_block", `    let mut xs: Array<uint64[4]> = [];
    xs.push([11:uint64, 22:uint64, 33:uint64, 44:uint64]);
    let d: uint64[] = [0:uint64, 0:uint64];
    let mut v = d[[0..2]];
    { v = xs[0][[0..2]]; }
    xs.push([1:uint64, 2:uint64, 3:uint64, 4:uint64]);
    return v[1] to int;`, "xs.push([1:uint64", diag.SemaBorrowConflict},
		{"fixed_window_of_a_local_stored_from_an_inner_block", `    let mut c: int[4] = [11, 22, 33, 44];
    let d: int[] = [0, 0];
    let mut v = d[[0..2]];
    { v = c[[0..2]]; }
    c = [1, 2, 3, 4];
    return v[1];`, "c = [1, 2, 3, 4];", diag.SemaBorrowMutation},
		{"fixed_window_through_an_index_temporary_then_realloc", `    let mut xs: Array<uint64[4]> = [];
    xs.push([11:uint64, 22:uint64, 33:uint64, 44:uint64]);
    let v = first(xs[0]);
    xs.push([1:uint64, 2:uint64, 3:uint64, 4:uint64]);
    return v[1] to int;`, "xs.push([1:uint64", diag.SemaBorrowConflict},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, storageViewFixture(row.body), row.code, row.snippet)
		})
	}
	t.Run("range_through_a_mut_reference_then_assign_through_it", func(t *testing.T) {
		text := throughMutRef + storageViewFixture(`    let mut ys: int[] = [10, 20, 30, 40];
    return through(&mut ys);`)
		refOutwardRefusal(t, text, diag.SemaBorrowMutation, "*r = [1, 2")
	})
}

func TestStorageViewControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, body string }{
		{"for_loops_that_do_not_mutate_their_iterable", `    let mut ys: int[] = [10, 20, 30, 40];
    let mut zs: int[] = [];
    let mut total = 0;
    for v in ys { total = total + v; zs.push(v); }
    for v in &ys { total = total + v; }
    ys.push(5);
    for v in ys { total = total + v; }
    ys = [1];
    return total + (zs.__len() to int);`},
		{"numeric_loop_mutates_the_array_it_measured", `    let mut ys: int[] = [1];
    for i in 0..(ys.__len() to int) { ys.push(i); }
    return ys.__len() to int;`},
		{"range_consumed_in_a_block_before_the_mutation", `    let mut ys: int[] = [10, 20, 30, 40];
    let mut total = 0;
    {
        let it = ys.__range();
        for v in it { total = total + v; }
    }
    ys = [7];
    ys.push(1);
    return total;`},
		{"dynamic_window_through_a_call_then_push", `    let mut ys: int[] = [10, 20, 30, 40];
    let v = window(&ys);
    ys.push(1);
    return v[1];`},
		{"windows_that_die_in_a_block", `    let mut ys: int[] = [10, 20, 30, 40];
    let mut t = 0;
    { let v = ys[[0..2]]; t = t + (v.__len() to int); }
    ys.push(1);
    let mut c: int[4] = [1, 2, 3, 4];
    { let w = c[[1..3]]; t = t + (w.__len() to int); }
    c = [5, 6, 7, 8];
    return t + c[0];`},
		{"fixed_window_through_a_call_in_a_block", `    let mut xs: Array<uint64[4]> = [];
    xs.push([11:uint64, 22:uint64, 33:uint64, 44:uint64]);
    let mut n: uint = 0:uint;
    { let v = first(&xs[0]); n = v.__len(); }
    xs.push([1:uint64, 2:uint64, 3:uint64, 4:uint64]);
    return n to int;`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, storageViewFixture(row.body))
		})
	}
}
