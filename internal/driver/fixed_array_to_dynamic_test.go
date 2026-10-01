package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/diag"
)

// A fixed array `T[N]` never converts implicitly to a dynamic `T[]` (owner's
// ruling 2026-09-30, LANGUAGE.md §2.2). Sema used to treat `T[N]` as assignable
// to `T[]`, and MIR has no such conversion: `let b: int[] = a` with `a: int[2]`
// lowered to a plain move between two layouts, the VM printed 7 and the native
// binary crashed. A call argument already refused it; every other place a value
// lands now does too, with SEM3015 at the value, a help naming the copy and,
// where the value itself is the fixed array, the fix inserting `.to_array()`.
// Each program is a ROOT program over the real core. A refused row names every
// span that carries SEM3015, exactly once, and its message; a control row
// carries no error: an array literal written where a `T[]` is expected is typed
// as that `T[]` (directly, nested, in a tuple), `.to_array()` is the copy, and a
// fixed array still reaches a fixed array of its own length. The reverse
// direction and a different length keep the refusals they always had.
type fixedArrayToDynamicRow struct {
	name, text string
	spans      []string
	message    string
	fix        bool // the value itself is the fixed array, so `.to_array()` is offered
	help       bool
}

func fixedArrayToDynamicRows() []fixedArrayToDynamicRow {
	const direct = "expected [int], got [int; 2]"
	const inSome = "expected Option<[int]>, got Some<[int; 2]>: [int; 2] is not [int]"
	return []fixedArrayToDynamicRow{
		{name: "let_annotation", text: "fn f() -> int {\n    let a: int[2] = [6, 7];\n    let b: int[] = a;\n    return b[1];\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "assignment", text: "fn f() -> int {\n    let a: int[2] = [6, 7];\n    let mut b: int[] = [1];\n    b = a;\n    return b[0];\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "return", text: "fn g(a: int[2]) -> int[] {\n    return a;\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "field_init", text: "type S = { xs: int[] }\n\nfn f(a: int[2]) -> int {\n    let s = S { xs: a };\n    return s.xs[0];\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "option_payload", text: "fn f(a: int[2]) -> int {\n    let o: Option<int[]> = Some(a);\n    return 0;\n}\n",
			spans: []string{"Some(a)"}, message: inSome, help: true},
		{name: "option_payload_returned", text: "fn f(a: int[2]) -> Option<int[]> {\n    return Some(a);\n}\n",
			spans: []string{"Some(a)"}, message: inSome, help: true},
		{name: "option_injection", text: "fn f(a: int[2]) -> int {\n    let o: Option<int[]> = a;\n    return 0;\n}\n",
			spans: []string{"a"}, message: "expected Option<[int]>, got [int; 2]: [int; 2] is not [int]", fix: true, help: true},
		{name: "user_tag_payload", text: "tag W(int[]);\n\ntype U = W | nothing;\n\nfn f(a: int[2]) -> int {\n    let u: U = W(a);\n    return 0;\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "tuple_element", text: "fn f(a: int[2]) -> int {\n    let t: (int[], int) = (a, 1);\n    return t.1;\n}\n",
			spans: []string{"(a, 1)"}, message: "expected ([int], int), got ([int; 2], int): [int; 2] is not [int]", help: true},
		{name: "array_literal_element", text: "fn f(a: int[2]) -> int {\n    let xs: int[][] = [a];\n    return 0;\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "array_index_store", text: "fn f(a: int[2]) -> int {\n    let mut xs: int[][] = [[1]];\n    xs[0] = a;\n    return 0;\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "array_push", text: "fn f(a: int[2]) -> int {\n    let mut xs: int[][] = [[1]];\n    xs.push(a);\n    return 0;\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "map_index_store", text: "fn f(a: int[2]) -> int {\n    let mut m: Map<string, int[]> = Map::<string, int[]>::new();\n    m[\"k\"] = a;\n    return 0;\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "map_insert", text: "fn f(a: int[2]) -> int {\n    let mut m: Map<string, int[]> = Map::<string, int[]>::new();\n    m.insert(\"k\", a);\n    return 0;\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "call_argument", text: "fn h(xs: int[]) -> int {\n    return xs[1];\n}\n\nfn f(a: int[2]) -> int {\n    return h(a);\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "generic_explicit", text: "fn id<T>(x: T) -> T {\n    return x;\n}\n\nfn f(a: int[2]) -> int {\n    let b: int[] = id::<int[]>(a);\n    return 0;\n}\n",
			spans: []string{"a"}, message: direct, fix: true, help: true},
		{name: "generic_inferred_result", text: "fn id<T>(x: T) -> T {\n    return x;\n}\n\nfn f(a: int[2]) -> int {\n    let b: int[] = id(a);\n    return 0;\n}\n",
			spans: []string{"id(a)"}, message: direct, fix: true, help: true},
		{name: "generic_struct_field", text: "type Box<T> = { v: T }\n\nfn f(a: int[2]) -> int {\n    let b: Box<int[]> = Box::<int[]> { v: a };\n    return 0;\n}\n",
			spans: []string{"Box::<int[]> { v: a }"}, message: "expected Box<[int]>, got Box<[int; 2]>: [int; 2] is not [int]", help: true},
		// The refusals that were already there keep their words.
		{name: "reverse_dynamic_to_fixed", text: "fn f(v: int[]) -> int {\n    let b: int[2] = v;\n    return 0;\n}\n",
			spans: []string{"v"}, message: "cannot assign [int] to [int; 2]"},
		{name: "fixed_length_differs", text: "fn f(a: int[2]) -> int {\n    let b: int[3] = a;\n    return 0;\n}\n",
			spans: []string{"a"}, message: "cannot assign [int; 2] to [int; 3]"},
		{name: "control_literal_let_and_assign", text: "fn f() -> int {\n    let mut b: int[] = [6, 7];\n    b = [8, 9, 10];\n    b.push(1);\n    return b[1];\n}\n"},
		{name: "control_literal_return", text: "fn r() -> int[] {\n    return [6, 7];\n}\n"},
		{name: "control_literal_field", text: "type S = { xs: int[] }\n\nfn f() -> int {\n    let s = S { xs: [6, 7] };\n    return s.xs[1];\n}\n"},
		{name: "control_literal_nested_and_tuple", text: "fn f() -> int {\n    let xs: int[][] = [[6, 7], [8, 9]];\n    let t: (int[], int) = ([6, 7], 1);\n    return xs[0][1] + t.0[1];\n}\n"},
		{name: "control_to_array", text: "type S = { xs: int[] }\n\nfn widen(a: &int[2]) -> int[] {\n    return a.to_array();\n}\n\nfn f() -> int {\n    let a: int[2] = [6, 7];\n    let mut b: int[] = a.to_array();\n    b.push(8);\n    let s = S { xs: a.to_array() };\n    let o: Option<int[]> = Some(a.to_array());\n    let c = widen(&a);\n    return b[2] + s.xs[0] + c[1];\n}\n"},
		{name: "control_fixed_to_fixed", text: "fn g(a: int[2]) -> int[2] {\n    return a;\n}\n\nfn f() -> int {\n    let a: int[2] = [6, 7];\n    let c: int[2] = a;\n    let d = g(c);\n    return d[1];\n}\n"},
	}
}

func TestFixedArrayNeverConvertsToDynamic(t *testing.T) {
	for _, row := range fixedArrayToDynamicRows() {
		t.Run(row.name, func(t *testing.T) {
			probe := taskCheckProbe{name: row.name, digest: fmt.Sprintf("%x", sha256.Sum256([]byte(row.text))), text: row.text}
			_, errs := taskCheckErrorCodes(t, probe)
			var got []string
			for _, d := range errs {
				if d.Code != diag.SemaTypeMismatch {
					t.Fatalf("error %s besides SEM3015: %+v", d.Code.ID(), *d)
				}
				got = append(got, row.text[d.Primary.Start:d.Primary.End])
				if d.Message != row.message {
					t.Fatalf("SEM3015 message %q, want %q", d.Message, row.message)
				}
				if row.help != (len(d.Help) > 0) {
					t.Fatalf("SEM3015 help %+v, want help=%v", d.Help, row.help)
				}
				if row.fix != offersToArray(d) {
					t.Fatalf("SEM3015 fixes %+v, want the `.to_array()` insertion=%v", d.Fixes, row.fix)
				}
			}
			if !slices.Equal(got, row.spans) {
				t.Fatalf("SEM3015 at %q, want exactly %q", got, row.spans)
			}
		})
	}
}

// offersToArray reports whether the diagnostic carries the fix that appends
// `.to_array()` right after its primary span.
func offersToArray(d *diag.Diagnostic) bool {
	for _, f := range d.Fixes {
		for _, e := range f.Edits {
			if e.NewText == ".to_array()" && e.Span.Start == d.Primary.End && e.Span.End == d.Primary.End {
				return true
			}
		}
	}
	return false
}
