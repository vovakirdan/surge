package driver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"surge/internal/diag"
)

// Arm and block results that the return-origin check read as escaping while
// nothing escapes, beside the shapes that really do.
//
//   - A compare that only borrows its subject leaves the payload with the
//     subject's owner, so an element read through the binding points into
//     storage that outlives the arm.
//   - A reference result consumed as a scalar is loaded before the arm or block
//     frees what it points into (MIR's loadInsideDropCarryingBlock), when the
//     compare's own type is that scalar, or when a typed `let`, an assignment
//     or a function `return` consumes a block whose last statement is its `ret`.
//   - A binding of an erased type holds a copied value, so what its
//     initializer pointed at is not reported again at every later scope exit.
//   - An `__index` that answers a value takes nothing out of its target.
//
// Every accepted program here builds and runs clean under valgrind; every
// refused one either reads freed storage when the refusal is suppressed or is
// the conservative residue named on its row.
type loadedArmRow struct {
	name string
	// fixture names a golden program instead of text.
	fixture string
	text    string
	// want is the exact sorted list of "CODE line:col" errors; empty accepts.
	want []string
}

var loadedArmRows = []loadedArmRow{
	// Accepted.
	{name: "borrowed_subject_reference_into_payload", fixture: "testdata/golden/sema/valid/arm_reference_into_borrowed_payload.sg"},
	{name: "owned_subject_element_read_as_compare_value", fixture: "testdata/golden/vm_compare/compare_arm_element_read.sg"},
	{name: "borrowed_subject_value_result", text: `fn g(o: &Option<int[]>) -> int {
    return compare *o { Some(inner) => inner[0]; _ => 0; };
}
@entrypoint
fn main() -> int {
    let mut row: int[] = [];
    row.push(6);
    let o: Option<int[]> = Some(row);
    return g(&o);
}
`},
	{name: "borrowed_subject_reference_returned", text: `fn elem(o: &Option<uint64[]>, fb: &uint64) -> &uint64 {
    return compare *o { Some(inner) => &inner[0]; _ => fb; };
}
@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64);
    let held: Option<uint64[]> = Some(row);
    let z: uint64 = 0:uint64;
    let r = elem(&held, &z);
    return (*r) to int;
}
`},
	{name: "compare_value_with_reference_arms", text: `@entrypoint
fn main() -> int {
    let mut values: uint64[][] = [[6:uint64, 7:uint64]];
    let zero: uint64 = 0:uint64;
    let bare: uint64 = compare values.pop() { Some(inner) => inner[0]; _ => &zero; };
    let k: int = { ret 1; };
    return (bare to int) + k;
}
`},
	{name: "plain_block_tail_into_let", text: `@entrypoint
fn main() -> int {
    let bare: uint64 = {
        let inner: uint64[] = [6:uint64, 7:uint64];
        ret inner[0];
    };
    return bare to int;
}
`},
	{name: "plain_block_tail_returned", text: `fn f() -> uint64 {
    return {
        let inner: uint64[] = [6:uint64, 7:uint64];
        ret inner[0];
    };
}
@entrypoint
fn main() -> int {
    return f() to int;
}
`},
	{name: "plain_block_tail_assigned", text: `@entrypoint
fn main() -> int {
    let mut bare: uint64 = 1:uint64;
    bare = {
        let inner: uint64[] = [6:uint64, 7:uint64];
        ret inner[0];
    };
    return bare to int;
}
`},
	{name: "element_assigned_to_an_outer_scalar", text: `@entrypoint
fn main() -> int {
    let mut outer: uint64 = 0:uint64;
    {
        let inner: uint64[] = [6:uint64, 7:uint64];
        outer = inner[1];
    }
    return outer to int;
}
`},
	{name: "user_index_answers_an_owned_value", text: `type Names = { count: int };

extern<Names> {
    fn __index(self: &Names, window: Range<int>) -> string {
        return "window";
    }
}

fn named() -> string {
    let n: Names = Names { count = 2 };
    return n[[0..1]];
}

fn named_let() -> string {
    let n: Names = Names { count = 2 };
    let s: string = n[[0..2]];
    return s;
}

@entrypoint
fn main() -> int {
    print(named());
    print(named_let());
    return 0;
}
`},
	// Refused: each reads freed storage natively when the refusal is suppressed.
	{name: "reference_consumer_of_an_owned_arm", want: []string{"SEM3139 5:60", "SEM3139 6:14"}, text: `@entrypoint
fn main() -> int {
    let mut values: uint64[][] = [[6:uint64, 7:uint64]];
    let zero: uint64 = 0:uint64;
    let r: &uint64 = compare values.pop() { Some(inner) => inner[0]; _ => &zero; };
    return (*r) to int;
}
`},
	{name: "untyped_consumer_of_a_reference_compare", want: []string{"SEM3139 5:54", "SEM3139 6:14"}, text: `@entrypoint
fn main() -> int {
    let mut values: uint64[][] = [[6:uint64, 7:uint64]];
    let zero: uint64 = 0:uint64;
    let bare = compare values.pop() { Some(inner) => inner[0]; _ => &zero; };
    return (*bare) to int;
}
`},
	{name: "owned_subject_reference_escapes_its_arm", want: []string{"SEM3200 5:62"}, text: `@entrypoint
fn main() -> int {
    let mut values: uint64[][] = [[6:uint64, 7:uint64]];
    let zero: uint64 = 0:uint64;
    let got: &uint64 = compare values.pop() { Some(inner) => &inner[0]; _ => &zero; };
    return (*got) to int;
}
`},
	{name: "block_nested_in_an_arm_frees_first", want: []string{"SEM3139 4:94"}, text: `@entrypoint
fn main() -> int {
    let mut values: uint64[][] = [[6:uint64, 7:uint64]];
    let bare: uint64 = compare values.pop() { Some(inner) => { let t: uint64[] = [9:uint64]; ret t[0]; }; _ => 0:uint64; };
    return bare to int;
}
`},
	{name: "reference_consumer_of_a_block", want: []string{"SEM3139 5:9", "SEM3139 7:14"}, text: `@entrypoint
fn main() -> int {
    let r: &uint64 = {
        let inner: uint64[] = [6:uint64, 7:uint64];
        ret inner[0];
    };
    return (*r) to int;
}
`},
	{name: "block_passed_as_an_argument", want: []string{"SEM3139 6:9"}, text: `fn take(x: uint64) -> int { return x to int; }
@entrypoint
fn main() -> int {
    return take({
        let inner: uint64[] = [6:uint64, 7:uint64];
        ret inner[0];
    });
}
`},
	{name: "borrowed_subject_outlived_by_its_owner", want: []string{"SEM3139 7:9", "SEM3139 9:13"}, text: `fn f(z: &uint64) -> uint64 {
    let got: &uint64 = {
        let mut row: uint64[] = [];
        row.push(8:uint64);
        let held: Option<uint64[]> = Some(row);
        let r: &Option<uint64[]> = &held;
        ret compare *r { Some(inner) => &inner[0]; _ => z; };
    };
    return *got;
}
@entrypoint
fn main() -> int {
    let z: uint64 = 0:uint64;
    return f(&z) to int;
}
`},
	// Refused by the checker: a reference taken through a borrowed payload and
	// kept past its block holds the subject owner's loan, as `&p[0]` through
	// `let p = &row` does, so the owner cannot be replaced, moved or mutated
	// while it lives. The owned twin is refused as a reference into a payload
	// the arm frees.
	{name: "owner_reassigned_after_block", want: []string{"SEM3019 11:5"}, text: `@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let z: uint64 = 0:uint64;
    let mut got: &uint64 = &z;
    let mut held: Option<uint64[]> = Some(row);
    {
        got = compare *(&held) { Some(inner) => &inner[0]; _ => &z; };
    }
    held = nothing;
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    print((*got) to string);
    return 0;
}
`},
	{name: "owner_reassigned_through_inner_reference", want: []string{"SEM3019 12:5"}, text: `@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let z: uint64 = 0:uint64;
    let mut got: &uint64 = &z;
    let mut held: Option<uint64[]> = Some(row);
    {
        let p: &Option<uint64[]> = &held;
        got = compare *p { Some(inner) => &inner[0]; _ => &z; };
    }
    held = nothing;
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    print((*got) to string);
    return 0;
}
`},
	{name: "owner_reassigned_before_return", want: []string{"SEM3019 10:5"}, text: `fn f(z: &uint64) -> uint64 {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let mut got: &uint64 = z;
    let mut held: Option<uint64[]> = Some(row);
    {
        let p: &Option<uint64[]> = &held;
        got = compare *p { Some(inner) => &inner[0]; _ => z; };
    }
    held = nothing;
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    return *got;
}
@entrypoint
fn main() -> int { let z: uint64 = 0:uint64; print(f(&z) to string); return 0; }
`},
	{name: "owner_reassigned_after_inner_let", want: []string{"SEM3019 13:5"}, text: `@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let z: uint64 = 0:uint64;
    let mut got: &uint64 = &z;
    let mut held: Option<uint64[]> = Some(row);
    {
        let p: &Option<uint64[]> = &held;
        let g2: &uint64 = compare *p { Some(inner) => &inner[0]; _ => &z; };
        got = g2;
    }
    held = nothing;
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    print((*got) to string);
    return 0;
}
`},
	{name: "owner_reassigned_after_if_body", want: []string{"SEM3019 11:5"}, text: `@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let z: uint64 = 0:uint64;
    let mut got: &uint64 = &z;
    let mut held: Option<uint64[]> = Some(row);
    if z == 0:uint64 {
        got = compare *(&held) { Some(inner) => &inner[0]; _ => &z; };
    }
    held = nothing;
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    print((*got) to string);
    return 0;
}
`},
	{name: "owner_moved_after_block", want: []string{"SEM3020 12:13"}, text: `fn consume(o: Option<uint64[]>) -> int { return 0; }
@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let z: uint64 = 0:uint64;
    let mut got: &uint64 = &z;
    let held: Option<uint64[]> = Some(row);
    {
        got = compare *(&held) { Some(inner) => &inner[0]; _ => &z; };
    }
    consume(held);
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    print((*got) to string);
    return 0;
}
`},
	{name: "owner_mutated_by_call_after_block", want: []string{"SEM3018 12:11"}, text: `fn clear(o: &mut Option<uint64[]>) { *o = nothing; }
@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let z: uint64 = 0:uint64;
    let mut got: &uint64 = &z;
    let mut held: Option<uint64[]> = Some(row);
    {
        got = compare *(&held) { Some(inner) => &inner[0]; _ => &z; };
    }
    clear(&mut held);
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    print((*got) to string);
    return 0;
}
`},
	{name: "owner_reassigned_after_arm_body_store", want: []string{"SEM3019 11:5"}, text: `@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let z: uint64 = 0:uint64;
    let mut got: &uint64 = &z;
    let mut held: Option<uint64[]> = Some(row);
    {
        compare *(&held) { Some(inner) => { got = &inner[0]; }; _ => {}; };
    }
    held = nothing;
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    print((*got) to string);
    return 0;
}
`},
	{name: "owner_reassigned_after_arm_body_call_store", want: []string{"SEM3019 11:5"}, text: `fn ident(r: &uint64) -> &uint64 { return r; }
@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let z: uint64 = 0:uint64;
    let mut got: &uint64 = &z;
    let mut held: Option<uint64[]> = Some(row);
    let mut outer: Option<Option<uint64[]>> = nothing;
    { compare *(&held) { Some(inner) => { got = ident(&inner[1]); }; _ => {}; }; }
    held = nothing;
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    print((*got) to string);
    return 0;
}
`},
	{name: "owned_subject_twin", want: []string{"SEM3200 9:45"}, text: `@entrypoint
fn main() -> int {
    let mut row: uint64[] = [];
    row.push(8:uint64); row.push(9:uint64);
    let z: uint64 = 0:uint64;
    let mut got: &uint64 = &z;
    let mut held: Option<uint64[]> = Some(row);
    {
        got = compare held { Some(inner) => &inner[0]; _ => &z; };
    }
    held = nothing;
    let mut junk: uint64[] = []; junk.push(77:uint64); junk.push(78:uint64);
    print((*got) to string);
    return 0;
}
`},
	// Conservative residue: an earlier `ret` is not the block's last statement,
	// so it stays refused, and only there -- not again at every later exit.
	{name: "early_ret_reported_once", want: []string{"SEM3139 4:16"}, text: `fn pick(c: bool) -> uint64 {
    let bare: uint64 = {
        let inner: uint64[] = [6:uint64, 7:uint64];
        if c { ret inner[1]; }
        ret inner[0];
    };
    let k: uint64 = { ret 1:uint64; };
    return bare + k;
}
@entrypoint
fn main() -> int {
    return (pick(true) + pick(false)) to int;
}
`},
	{name: "real_element_dropped_out", fixture: "testdata/golden/hir_borrow/invalid/element_move_out_forbidden.sg", want: []string{"SEM3143 11:5"}},
	{name: "user_index_window_of_a_fixed_array", fixture: "testdata/golden/sema/invalid/ownership/fixed_array_view_escapes_through_operator.sg",
		want: []string{"SEM3198 29:12", "SEM3198 35:12", "SEM3198 40:12", "SEM3198 45:12", "SEM3198 54:12"}},
}

func TestLoadedArmAndBlockResults(t *testing.T) {
	root := repoRootFromDriverTest(t)
	t.Setenv("SURGE_STDLIB", root)
	for _, row := range loadedArmRows {
		t.Run(row.name, func(t *testing.T) {
			text := row.text
			if row.fixture != "" {
				data, err := os.ReadFile(filepath.Join(root, row.fixture))
				if err != nil {
					t.Fatalf("PRECONDITION: %v", err)
				}
				text = string(data)
			}
			got := loadedArmErrors(t, text)
			if !slices.Equal(got, row.want) {
				t.Fatalf("errors = %q, want %q", got, row.want)
			}
		})
	}
}

// loadedArmErrors diagnoses one program through the public path and answers
// its sorted errors; an unfinished return-origin analysis fails the row.
func loadedArmErrors(t *testing.T, text string) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "origin.sg")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := DiagnoseOptions{Stage: DiagnoseStageSema, BaseDir: dir, MaxDiagnostics: 64, IgnoreWarnings: true}
	res, err := DiagnoseWithOptions(t.Context(), path, &opts)
	var unfinished *returnOriginUnfinishedError
	if errors.As(err, &unfinished) {
		t.Fatalf("return-origin analysis unfinished: %v", err)
	}
	if err != nil {
		t.Fatalf("PRECONDITION: diagnosis failed: %v", err)
	}
	if res == nil || res.Bag == nil {
		return nil
	}
	var out []string
	for _, d := range res.Bag.Items() {
		if d.Severity < diag.SevError {
			continue
		}
		line, col := loadedArmPosition(text, d.Primary.Start)
		out = append(out, fmt.Sprintf("%s %d:%d", d.Code.ID(), line, col))
		t.Logf("%s %d:%d %s", d.Code.ID(), line, col, d.Message)
	}
	slices.Sort(out)
	return out
}

func loadedArmPosition(text string, offset uint32) (line, col int) {
	if int(offset) > len(text) {
		return 0, 0
	}
	before := text[:offset]
	return strings.Count(before, "\n") + 1, int(offset) - strings.LastIndex(before, "\n")
}
