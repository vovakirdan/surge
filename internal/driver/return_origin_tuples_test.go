package driver

import (
	"strings"
	"testing"
)

// Tuple index, expression kind 12 (return_origin_tuples.go): an element holds nothing when its type can hold no
// reference and no storage loan, and every root of its tuple otherwise; an element reached through a reference keeps
// a named row unless it holds nothing. A tuple pattern in `let` is refused by the checker (SEM3220), so no row here
// destructures. Every source is a ROOT program against the real core, and each row reads one body. The rows are the
// cloud agent's (cloud/n-tuple, 7fbfaa08) index rows and the NTUP review's.

const (
	originTupleCallableRefusal = "tuple element that holds a callable needs its callable transfer"
	originTupleReferentRefusal = "tuple element read through a reference needs its referent's contents"
)

// The shapes of hir/tuples.sg, sema/valid/tuple_access.sg, vm_tuples/tuple_literals.sg and
// vm_async_suite/t15_fairness_round_robin.sg, and the forms around them.
const originTupleFinishSource = `fn swap(a: int, b: int) -> (int, int) {
    return (b, a);
}

fn get_first(t: (int, int)) -> int {
    return t.0;
}

fn get_second() -> string {
    let t: (int, string) = (42, "hello");
    return own t.1;
}

fn nested_access() -> int {
    let t = ((1, 2), 3);
    return t.0.1;
}

fn own_nested() -> int {
    let nested: ((int, int), int) = ((2, 3), 4);
    let inner: (int, int) = own nested.0;
    let single: (int,) = (42,);
    return inner.0 + single.0;
}

fn compare_pair(v: int?) -> int {
    let pair: (bool, int) = compare v {
        Some(x) => (true, x);
        nothing => (false, 0);
    };
    if pair.0 {
        return pair.1;
    }
    return 0;
}

fn ref_elem(r: &(int, int)) -> &int {
    return &r.0;
}

fn first<T>(t: (T, int)) -> T {
    return own t.0;
}

fn use_first() -> int {
    return first::<int>((5, 6));
}

fn store_scalar() -> int {
    let mut t = (1, 2);
    t.0 = 5;
    return t.0 + t.1;
}
`

// Forms that keep a named row.
const originTupleRefusedSource = `fn callable_elem(t: (fn(int) -> int, int)) -> int {
    let h = t.0;
    return t.1;
}

fn window_plain() -> int {
    let a: int[4] = [1, 2, 3, 4];
    let t = (a[[0..2]], 1);
    return t.1;
}

fn window_through_ref(r: &(int, int[])) -> int[] {
    return own r.1;
}

fn leak_window() -> int[] {
    let a: int[4] = [1, 2, 3, 4];
    let t = (1, a[[0..2]]);
    return own t.1;
}
`

const (
	originTupleFinishSourceDigest  = "e33c016cc576c1c754b0c6e0c34a6dc0dbce6cd21573a987dbaaf3cfa1531618"
	originTupleRefusedSourceDigest = "a32ed85f1a1b7447fcc4d81c9197455d8e16108bf905588cd8c5169b5bbda493"
)

// tupleFn is the frozen span of the function whose text starts with header and runs to its closing brace.
func tupleFn(t *testing.T, text, header string) originSpan {
	t.Helper()
	start := strings.Index(text, header)
	if start < 0 || strings.Count(text, header) != 1 {
		t.Fatalf("PRECONDITION: %q is not one function header", header)
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("PRECONDITION: %q has no closing brace", header)
	}
	end += start + len("\n}")
	return originSpan{start, end, text[start:end]}
}

// tupleIn is the frozen span of the one occurrence of snippet inside fn.
func tupleIn(t *testing.T, text string, fn originSpan, snippet string) originSpan {
	t.Helper()
	body := text[fn.start:fn.end]
	at := strings.Index(body, snippet)
	if at < 0 || strings.Count(body, snippet) != 1 {
		t.Fatalf("PRECONDITION: %q is not one occurrence inside %q", snippet, fn.snippet)
	}
	return originSpan{fn.start + at, fn.start + at + len(snippet), snippet}
}

type originTupleRow struct {
	name, header, body string
	want               []struct{ snippet, reason string }
	allow              []string
	slots              []uint32 // a normal summary with these parameter sources, when non-nil
}

func originTupleFinishRows() []originTupleRow {
	return []originTupleRow{
		{name: "swap_builds_a_tuple", header: "fn swap(", body: "swap", slots: []uint32{}},
		{name: "index_of_a_parameter", header: "fn get_first(", body: "get_first", slots: []uint32{}},
		{name: "own_index_of_a_local", header: "fn get_second(", body: "get_second", slots: []uint32{}},
		{name: "nested_index", header: "fn nested_access(", body: "nested_access", slots: []uint32{}},
		{name: "own_nested_and_single", header: "fn own_nested(", body: "own_nested", slots: []uint32{}},
		{name: "compare_built_pair", header: "fn compare_pair(", body: "compare_pair", slots: []uint32{}},
		{name: "borrow_of_an_element_through_a_reference", header: "fn ref_elem(", body: "ref_elem", slots: []uint32{0}},
		{name: "generic_element_keeps_its_tuple", header: "fn first<T>(", body: "first", slots: []uint32{0}},
		{name: "generic_use", header: "fn use_first(", body: "use_first", slots: []uint32{}},
		{name: "store_into_a_scalar_element", header: "fn store_scalar(", body: "store_scalar", slots: []uint32{}},
	}
}

// originTupleDerived are the rows that only carry a refused source on to a result or an outgoing reference.
var originTupleDerived = []string{"function result contains an unproved source", "outgoing reference has unresolved or captured provenance"}

func originTupleRefusedRows() []originTupleRow {
	type w = struct{ snippet, reason string }
	return []originTupleRow{
		{name: "callable_element_keeps_its_row", header: "fn callable_elem(", body: "callable_elem",
			// The parameter row is the pre-existing refusal of a function-typed parameter slot (return_origin_summary.go).
			want:  []w{{"t.0", originTupleCallableRefusal}},
			allow: append([]string{"parameter requires concrete type or callable provenance"}, originTupleDerived...)},
		{name: "window_in_a_reference_free_tuple_keeps_g6", header: "fn window_plain(", body: "window_plain",
			want: []w{{"(a[[0..2]], 1)", "storage loan would be discarded by a payload-free value"}}},
		{name: "window_through_a_reference_keeps_its_row", header: "fn window_through_ref(", body: "window_through_ref",
			want: []w{{"r.1", originTupleReferentRefusal}}, allow: originTupleDerived},
		// Was an escape canary beside a reference element; with no reference beside it the window keeps the G6 row,
		// so the body is still never clean.
		{name: "index_of_a_window_into_a_local_keeps_g6", header: "fn leak_window(", body: "leak_window",
			want: []w{{"(1, a[[0..2]])", "storage loan would be discarded by a payload-free value"}}, allow: originTupleDerived},
	}
}

func runOriginTupleRows(t *testing.T, stage, text, digest string, rows []originTupleRow) {
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			fn := tupleFn(t, text, row.header)
			var want []originRefusal
			spans := []originSpan{fn}
			for _, refusal := range row.want {
				span := tupleIn(t, text, fn, refusal.snippet)
				want = append(want, originRefusal{span: span, reason: refusal.reason})
				spans = append(spans, span)
			}
			checkOriginSource(t, text, digest, spans...)
			f, analysis := analyzeOriginRoot(t, stage+"_"+row.name, text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, fn, want, row.allow...)
			originNoEscape(t, analysis, f.owner.File.ID, fn)
			if row.slots != nil {
				requireOriginSummary(t, analysis, f.owner.File.ID, row.body, false, row.slots)
			}
		})
	}
}

// 11 RUN: 1 parent, 10 leaves.
func TestAnalyzeTuples(t *testing.T) {
	rows := originTupleFinishRows()
	if len(rows) != 10 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	runOriginTupleRows(t, "tuple", originTupleFinishSource, originTupleFinishSourceDigest, rows)
}

// 5 RUN: 1 parent, 4 leaves.
func TestAnalyzeTupleRefusals(t *testing.T) {
	rows := originTupleRefusedRows()
	if len(rows) != 4 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	runOriginTupleRows(t, "tuple_refused", originTupleRefusedSource, originTupleRefusedSourceDigest, rows)
}

// An element holding a reference -- `Option<&int>` in a tuple type or literal -- is refused by SEM3138 (the
// containment rule holds at any depth), so these programs, which the index, refusal and escape rows above used to
// read, never reach return-origin analysis. Each row keeps its old name and pins the refusal; the escape rows' point,
// that a reference to a dying local never leaves through a tuple, still holds, now by the refusal. A refused tuple
// type or literal has no type, so three rows also carry what that leaves behind: the checker's SEM3020 for the moved
// element, a result type of nothing (SEM3015), and no overload of `first` for an untyped argument (SEM3046).
func TestTupleStoredReferenceIsRefused(t *testing.T) {
	runStoredReferenceRows(t, []storedReferenceRow{
		{name: "index_keeps_the_reference_origin", want: "SEM3138",
			text: "fn keep_index(a: &int) -> Option<&int> {\n    let t = (Some::<&int>(a), 2);\n    return own t.0;\n}\n"},
		{name: "index_of_a_reference_holding_parameter", want: "SEM3138",
			text: "fn keep_param(t: (Option<&int>, int)) -> Option<&int> {\n    return own t.0;\n}\n"},
		{name: "index_of_an_owned_parameter", want: "SEM3138",
			text: "fn index_own(t: own (Option<&int>, int)) -> Option<&int> {\n    return own t.0;\n}\n"},
		{name: "scalar_store_keeps_the_reference_origin", want: "SEM3138",
			text: "fn keep_after_store(p: &int) -> Option<&int> {\n    let mut t = (Some::<&int>(p), 1);\n    t.1 = 5;\n    return own t.0;\n}\n"},
		{name: "index_through_a_reference_keeps_its_row", want: "SEM3138",
			text: "fn through_ref_index(r: &(Option<&int>, int)) -> Option<&int> {\n    return own r.0;\n}\n"},
		{name: "store_of_a_reference_into_an_element_keeps_its_row", want: "SEM3138",
			text: "fn store_reference(a: &int) -> Option<&int> {\n    let x: int = 1;\n    let mut t = (Some::<&int>(a), 1);\n    t.0 = Some::<&int>(&x);\n    return own t.0;\n}\n"},
		{name: "store_through_a_mutable_element_borrow_keeps_its_row", want: "SEM3020,SEM3138",
			text: "fn set(o: &mut Option<&int>, a: &int) {\n    *o = Some::<&int>(a);\n}\n\nfn store_through_element(p: &int) -> Option<&int> {\n    let x: int = 7;\n    let mut t = (Some::<&int>(p), 1);\n    set(&mut t.0, &x);\n    return own t.0;\n}\n"},
		{name: "tuple_field_of_a_struct_keeps_the_member_row", want: "SEM3138",
			text: "type Holder = { t: (Option<&int>, int) };\n\nfn field_tuple(p: &int) -> Option<&int> {\n    let h: Holder = { t = (Some::<&int>(p), 1) };\n    return own h.t.0;\n}\n"},
		{name: "index_of_a_reference_to_a_local", want: "SEM3138",
			text: "fn leak_index() -> Option<&int> {\n    let x: int = 1;\n    let t = (Some::<&int>(&x), 2);\n    return own t.0;\n}\n"},
		{name: "nested_index_of_a_reference_to_a_local", want: "SEM3138",
			text: "fn leak_nested_index() -> Option<&int> {\n    let x: int = 1;\n    let t = ((Some::<&int>(&x), 1), 2);\n    return own t.0.0;\n}\n"},
		{name: "index_of_a_call_result", want: "SEM3015,SEM3138",
			text: "fn mk(a: &int) -> (Option<&int>, int) {\n    return (Some::<&int>(a), 1);\n}\n\nfn leak_call_index() -> Option<&int> {\n    let x: int = 7;\n    let t = mk(&x);\n    return own t.0;\n}\n"},
		{name: "generic_instantiation", want: "SEM3046,SEM3138",
			text: "fn first<T>(t: (T, int)) -> T {\n    return own t.0;\n}\n\nfn leak_generic() -> Option<&int> {\n    let x: int = 7;\n    let o: Option<&int> = Some::<&int>(&x);\n    return first::<Option<&int>>((o, 1));\n}\n"},
		{name: "whole_reassignment", want: "SEM3138",
			text: "fn leak_reassign(p: &int) -> Option<&int> {\n    let x: int = 7;\n    let mut t = (Some::<&int>(p), 1);\n    t = (Some::<&int>(&x), 2);\n    return own t.0;\n}\n"},
		{name: "compare_join", want: "SEM3138",
			text: "fn leak_join(p: &int, c: bool) -> Option<&int> {\n    let x: int = 7;\n    let t = compare c {\n        true => (Some::<&int>(p), 1);\n        false => (Some::<&int>(&x), 2);\n    };\n    return own t.0;\n}\n"},
		{name: "scalar_store_beside_the_reference", want: "SEM3138",
			text: "fn leak_after_store() -> Option<&int> {\n    let x: int = 7;\n    let mut t = (Some::<&int>(&x), 1);\n    t.1 = 5;\n    return own t.0;\n}\n"},
	})
}
