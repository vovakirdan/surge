package driver

import (
	"strings"
	"testing"
)

// Tuples (expression kind 12 and `let` destructuring, return_origin_tuples.go): an element holds nothing when its
// type can hold no reference and no storage loan, and every root of its tuple otherwise; an element reached through
// a reference keeps a named row unless it holds nothing; destructuring binds each name as the element read would.
// Every source is a ROOT program against the real core, and each row reads one body.

const (
	originTupleUnknownRefusal     = "tuple element needs its concrete element type"
	originTupleCallableRefusal    = "tuple element that holds a callable needs its callable transfer"
	originTupleReferentRefusal    = "tuple element read through a reference needs its referent's contents"
	originTupleDestructureRefusal = "destructuring needs projected origin facts"
)

// The shapes of hir/tuples.sg, sema/valid/tuple_access.sg, tuple_destructure.sg, tuple_destructure_call.sg,
// vm_tuples/tuple_literals.sg and vm_async_suite/t15_fairness_round_robin.sg, and the forms around them.
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

fn destruct_pair() -> string {
    let pair = (1, "hello");
    let (x, y) = pair;
    return y;
}

fn produce() -> (int, string, bool) {
    return (1, "hello", true);
}

fn destruct_call() -> string {
    let (a, b, c) = produce();
    return b;
}

fn own_nested() -> int {
    let nested: ((int, int), int) = ((2, 3), 4);
    let inner: (int, int) = own nested.0;
    let single: (int,) = (42,);
    return inner.0 + single.0;
}

fn nested_destructure() -> int {
    let ((a, b), c) = ((1, 2), 3);
    return a + b + c;
}

fn wildcard() -> int {
    let (_, b) = (1, 2);
    return b;
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

fn ref_destructure(r: &(int, int)) -> int {
    let (a, b) = r;
    return a + b;
}

fn keep_index(a: &int) -> Option<&int> {
    let t = (Some::<&int>(a), 2);
    return own t.0;
}

fn keep_destructure(a: &int) -> Option<&int> {
    let (o, n) = (Some::<&int>(a), 2);
    return o;
}

fn keep_nested(a: &int) -> Option<&int> {
    let ((o, k), n) = ((Some::<&int>(a), 1), 2);
    return o;
}

fn keep_param(t: (Option<&int>, int)) -> Option<&int> {
    return own t.0;
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
const originTupleRefusedSource = `fn through_ref(r: &(Option<&int>, int)) -> int {
    let (o, n) = r;
    return n;
}

fn through_ref_index(r: &(Option<&int>, int)) -> Option<&int> {
    return own r.0;
}

fn callable_elem(t: (fn(int) -> int, int)) -> int {
    let h = t.0;
    return t.1;
}

fn window_plain() -> int {
    let a: int[4] = [1, 2, 3, 4];
    let t = (a[[0..2]], 1);
    return t.1;
}

fn store_reference(a: &int) -> Option<&int> {
    let x: int = 1;
    let mut t = (Some::<&int>(a), 1);
    t.0 = Some::<&int>(&x);
    return own t.0;
}
`

// Soundness canaries: an element that holds a reference to, or a window into, a dying local leaves its frame.
const originTupleEscapeSource = `fn leak_index() -> Option<&int> {
    let x: int = 1;
    let t = (Some::<&int>(&x), 2);
    return own t.0;
}

fn leak_destructure() -> Option<&int> {
    let x: int = 1;
    let (o, n) = (Some::<&int>(&x), 2);
    return o;
}

fn leak_nested_index() -> Option<&int> {
    let x: int = 1;
    let t = ((Some::<&int>(&x), 1), 2);
    return own t.0.0;
}

fn leak_nested_destructure() -> Option<&int> {
    let x: int = 1;
    let ((o, k), n) = ((Some::<&int>(&x), 1), 2);
    return o;
}

fn leak_window(p: &int) -> int[] {
    let a: int[4] = [1, 2, 3, 4];
    let t = (Some::<&int>(p), a[[0..2]]);
    return own t.1;
}

fn leak_window_destructure(p: &int) -> int[] {
    let a: int[4] = [1, 2, 3, 4];
    let (o, w) = (Some::<&int>(p), a[[0..2]]);
    return w;
}
`

const (
	originTupleFinishSourceDigest  = "3f2131888fd19a89adfb2a7ccfbfdc69c2492b2dad9e6c6ef0346aa7eff16fb0"
	originTupleRefusedSourceDigest = "227d6eda0aa5b9304c188b15992a7986e07df9f1198f128dbba79d0985c10d25"
	originTupleEscapeSourceDigest  = "b5c45a2104257de3df1466a4f9d7e3e0e9101ce6b7b91f9c1fed02acc8d9846c"
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
		{name: "destructure_a_binding", header: "fn destruct_pair(", body: "destruct_pair", slots: []uint32{}},
		{name: "destructure_a_call_result", header: "fn destruct_call(", body: "destruct_call", slots: []uint32{}},
		{name: "own_nested_and_single", header: "fn own_nested(", body: "own_nested", slots: []uint32{}},
		{name: "nested_destructure", header: "fn nested_destructure(", body: "nested_destructure", slots: []uint32{}},
		{name: "wildcard_binds_nothing", header: "fn wildcard(", body: "wildcard", slots: []uint32{}},
		{name: "compare_built_pair", header: "fn compare_pair(", body: "compare_pair", slots: []uint32{}},
		{name: "borrow_of_an_element_through_a_reference", header: "fn ref_elem(", body: "ref_elem", slots: []uint32{0}},
		{name: "destructure_through_a_reference_holds_nothing", header: "fn ref_destructure(", body: "ref_destructure", slots: []uint32{}},
		{name: "index_keeps_the_reference_origin", header: "fn keep_index(", body: "keep_index", slots: []uint32{0}},
		{name: "destructure_keeps_the_reference_origin", header: "fn keep_destructure(", body: "keep_destructure", slots: []uint32{0}},
		{name: "nested_destructure_keeps_the_reference_origin", header: "fn keep_nested(", body: "keep_nested", slots: []uint32{0}},
		{name: "index_of_a_reference_holding_parameter", header: "fn keep_param(", body: "keep_param", slots: []uint32{0}},
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
		{name: "destructure_through_a_reference_keeps_its_row", header: "fn through_ref(", body: "through_ref",
			want: []w{{"let (o, n) = r;", originTupleReferentRefusal}}},
		{name: "index_through_a_reference_keeps_its_row", header: "fn through_ref_index(", body: "through_ref_index",
			want: []w{{"r.0", originTupleReferentRefusal}}, allow: originTupleDerived},
		{name: "callable_element_keeps_its_row", header: "fn callable_elem(", body: "callable_elem",
			// The parameter row is the pre-existing refusal of a function-typed parameter slot (return_origin_summary.go).
			want:  []w{{"t.0", originTupleCallableRefusal}},
			allow: append([]string{"parameter requires concrete type or callable provenance"}, originTupleDerived...)},
		{name: "window_in_a_reference_free_tuple_keeps_g6", header: "fn window_plain(", body: "window_plain",
			want: []w{{"(a[[0..2]], 1)", "storage loan would be discarded by a payload-free value"}}},
		// A store of a reference into an element keeps the store row; the read after it is not clean.
		{name: "store_of_a_reference_into_an_element_keeps_its_row", header: "fn store_reference(", body: "store_reference",
			want: []w{{"t.0 = Some::<&int>(&x)", "store through a place needs reference-content transfer"}}, allow: originTupleDerived},
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

// 20 RUN: 1 parent, 19 leaves.
func TestAnalyzeTuples(t *testing.T) {
	rows := originTupleFinishRows()
	if len(rows) != 19 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	runOriginTupleRows(t, "tuple", originTupleFinishSource, originTupleFinishSourceDigest, rows)
}

// 6 RUN: 1 parent, 5 leaves.
func TestAnalyzeTupleRefusals(t *testing.T) {
	rows := originTupleRefusedRows()
	if len(rows) != 5 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	runOriginTupleRows(t, "tuple_refused", originTupleRefusedSource, originTupleRefusedSourceDigest, rows)
}

// 7 RUN: 1 parent, 6 leaves. Each leak is SEM3139 for its local, never a clean body.
func TestTupleEscapeIsReported(t *testing.T) {
	rows := []struct{ name, header, owner string }{
		{"index_of_a_reference_to_a_local", "fn leak_index(", "x"},
		{"destructure_of_a_reference_to_a_local", "fn leak_destructure(", "x"},
		{"nested_index_of_a_reference_to_a_local", "fn leak_nested_index(", "x"},
		{"nested_destructure_of_a_reference_to_a_local", "fn leak_nested_destructure(", "x"},
		{"index_of_a_window_into_a_local", "fn leak_window(", "a"},
		{"destructure_of_a_window_into_a_local", "fn leak_window_destructure(", "a"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			fn := tupleFn(t, originTupleEscapeSource, row.header)
			checkOriginSource(t, originTupleEscapeSource, originTupleEscapeSourceDigest, fn)
			f, analysis := analyzeOriginRoot(t, "tuple_escape_"+row.name, originTupleEscapeSource, true, nil)
			requireSelectEscape(t, analysis, f.owner.File.ID, fn, row.owner)
		})
	}
}
