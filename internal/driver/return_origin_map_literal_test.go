package driver

import (
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Map literal, expression kind 9 (return_origin_map_literal.go): a fresh map that holds the joined contents of its
// values, keeps no loan in a payload-free value (G6), and records NoBorrowedState for its untracked keys. The symbol
// resolver does not walk map literal entries, so an entry that names a binding keeps the identifier row and an entry
// that opens a scope is refused before it is evaluated. The resolved rows record, per identifier, the symbol the
// resolver would record, and show the transfer itself keeps every origin once such facts exist. Every source is a
// ROOT program against the real core.

const (
	originMapLiteralTyping      = "map literal disagrees with its typed key and value"
	originMapLiteralUnvisited   = "map literal entry needs scopes the resolver did not record"
	originMapLiteralUnresolved  = "identifier has no resolved symbol"
	originMapLiteralLoanElement = "cursor element that can hold storage loans needs its backing loan transfer"
	originMapLiteralTask        = "map literal whose entries may hold a task needs the task check"
	originMapLiteralTagRow      = "tag constructor lacks its original declaration target"
)

// Entries an insert refuses, and tag entries. The task entry lives in task_in_map_test.go: a map
// literal of tasks is refused by SEM3225 before return origins run.
const originMapLiteralInsertRulesSource = `fn tag_value() -> uint {
    let m = { "a" => Some(1) };
    return m.length();
}

fn tag_values() -> uint {
    let m = { "a" => Some("x" * 40), "b" => Some("y" * 40) };
    return m.length();
}

fn range_values() -> uint {
    let m = { 1 => 0..3 };
    return m.length();
}
`

// The shape of vm_maps/map_literal_order.sg and the forms around it.
const originMapLiteralFinishSource = `fn key(label: string) -> string {
    return label;
}

fn val(v: int) -> int {
    return v;
}

fn literal_entries() -> uint {
    let m = { "a" => 1, "b" => 2 };
    return m.length();
}

fn call_entries() -> uint {
    let m = { key("k1") => val(1), key("k2") => val(2) };
    return m.length();
}

fn annotated() -> uint {
    let m: Map<string, int> = { "a" => 1 };
    return m.length();
}

fn returned() -> Map<string, int> {
    return { "x" => 1 };
}

fn array_values() -> uint {
    let m = { 1 => [1, 2], 2 => [3, 4] };
    return m.length();
}

fn nested() -> Map<int, Map<string, int>> {
    return { 1 => { "y" => 1 } };
}
`

// Entries the resolver leaves without facts.
const originMapLiteralUnvisitedSource = `fn named_entries(a: int) -> uint {
    let x: int = 1;
    let m = { a => x };
    return m.length();
}

fn block_value() -> uint {
    let m = { 1 => { ret 2; } };
    return m.length();
}

fn compare_value(o: Option<int>) -> uint {
    let m = { 1 => compare o { Some(v) => v; nothing => 0; } };
    return m.length();
}
`

// Soundness canaries: a value that holds a reference to, or a view of, a dying local, beside forms that keep a
// parameter's origin, a payload-free window, and an entry whose origin is unknown.
const originMapLiteralEntrySource = `fn keep_param(p: &int) -> Map<int, Option<&int>> {
    let o: Option<&int> = Some::<&int>(p);
    return { 1 => o };
}

fn keep_view(s: &string) -> Map<int, BytesView> {
    let v = s.bytes();
    return { 1 => v };
}

fn leak_ref() -> Map<int, Option<&int>> {
    let x: int = 1;
    let o: Option<&int> = Some::<&int>(&x);
    return { 1 => o };
}

fn leak_view() -> Map<int, BytesView> {
    let s: string = "abc";
    let v = s.bytes();
    return { 1 => v };
}

fn leak_second(p: &int) -> Map<int, Option<&int>> {
    let x: int = 1;
    let a: Option<&int> = Some::<&int>(p);
    let b: Option<&int> = Some::<&int>(&x);
    let m = { 1 => a, 2 => b };
    return m;
}

fn window_value() -> uint {
    let a: int[4] = [1, 2, 3, 4];
    let w: int[] = a[[0..2]];
    let m = { 1 => w };
    return m.length();
}

fn through_ref(r: &(Option<&int>, int)) -> Map<int, Option<&int>> {
    return { 1 => r.0 };
}
`

const (
	originMapLiteralFinishDigest      = "2ad20ba692bc6e9ed7fd059bf64b8bacc666d9b61655d97d9082d76668f2c97f"
	originMapLiteralUnvisitedDigest   = "23b0e0141eb1cfd0eb013bd7f09bb81e4f0657f0c88be2a0614bd2d86d5df388"
	originMapLiteralInsertRulesDigest = "febf0627f87d0dc28b820d6ea2e12533e9202ee5ed134c5c4b04de0a78d66b53"
	originMapLiteralEntryDigest       = "1f352d6845400b8eb1f4e97106d455c153de566d47a7764e60c528d628f71c76"
)

// originMapLiteralResolve records, for every identifier inside a map literal of the test source that the resolver
// left without a symbol, the one local or parameter of that name declared nearest before it in the same function,
// the symbol the resolver records for an identifier anywhere else. It answers the number of identifiers recorded.
func originMapLiteralResolve(t *testing.T, f originalGenericFixture, text string) int {
	t.Helper()
	u := f.unit
	file := f.owner.File.ID
	recorded := 0
	for raw := uint32(1); raw <= u.Builder.Exprs.Arena.Len(); raw++ {
		literal := u.Builder.Exprs.Get(ast.ExprID(raw))
		if literal == nil || literal.Kind != ast.ExprMap || literal.Span.File != file {
			continue
		}
		fnStart := strings.LastIndex(text[:literal.Span.Start], "\nfn ")
		for inner := uint32(1); inner <= u.Builder.Exprs.Arena.Len(); inner++ {
			id := ast.ExprID(inner)
			node := u.Builder.Exprs.Get(id)
			if node == nil || node.Kind != ast.ExprIdent || node.Span.File != file ||
				node.Span.Start < literal.Span.Start || node.Span.End > literal.Span.End {
				continue
			}
			if _, present := u.Symbols.ExprSymbols[id]; present {
				continue
			}
			ident, _ := u.Builder.Exprs.Ident(id)
			best := symbols.NoSymbolID
			bestStart := -1
			for i, sym := range u.Symbols.Table.Symbols.Data() {
				start := int(sym.Span.Start)
				if sym.Name != ident.Name || sym.Span.File != file || (sym.Kind != symbols.SymbolLet && sym.Kind != symbols.SymbolParam) ||
					start <= fnStart || start >= int(literal.Span.Start) || start <= bestStart {
					continue
				}
				best, bestStart = symbols.SymbolID(i+1), start
			}
			if best == symbols.NoSymbolID || u.Symbols.Table.Symbols.Get(best).Name != ident.Name {
				t.Fatalf("PRECONDITION: no declaration for the identifier at %d:%d", node.Span.Start, node.Span.End)
			}
			u.Symbols.ExprSymbols[id] = best
			recorded++
		}
	}
	return recorded
}

type originMapLiteralRow struct {
	name, header, body string
	want               []struct{ snippet, reason string }
	allow              []string
	slots              []uint32 // a normal summary with these parameter sources, when non-nil
	escapes            string   // the local whose borrow outlives it, when non-empty
}

func runOriginMapLiteralRows(t *testing.T, stage, text, digest string, resolve int, rows []originMapLiteralRow) {
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
			var prepare func(originalGenericFixture)
			if resolve != 0 {
				prepare = func(f originalGenericFixture) {
					if got := originMapLiteralResolve(t, f, text); got != resolve {
						t.Fatalf("PRECONDITION: recorded %d map literal identifiers, want %d", got, resolve)
					}
				}
			}
			f, analysis := analyzeOriginRoot(t, stage+"_"+row.name, text, true, prepare)
			originExactPending(t, analysis, f.unit.SourceKey, fn, want, row.allow...)
			if row.escapes != "" {
				requireSelectEscape(t, analysis, f.owner.File.ID, fn, row.escapes)
			} else {
				originNoEscape(t, analysis, f.owner.File.ID, fn)
			}
			if row.slots != nil {
				requireOriginSummary(t, analysis, f.owner.File.ID, row.body, false, row.slots)
			}
		})
	}
}

// 7 RUN: 1 parent, 6 leaves.
func TestAnalyzeMapLiterals(t *testing.T) {
	type w = struct{ snippet, reason string }
	rows := []originMapLiteralRow{
		{name: "literal_entries", header: "fn literal_entries(", body: "literal_entries", slots: []uint32{}},
		{name: "call_entries_in_source_order", header: "fn call_entries(", body: "call_entries", slots: []uint32{}},
		{name: "annotated_binding", header: "fn annotated(", body: "annotated", slots: []uint32{}},
		{name: "returned_literal_is_fresh", header: "fn returned(", body: "returned", slots: []uint32{}},
		// As an insert: a value that can carry storage loans needs the backing loan transfer.
		{name: "array_values_keep_the_insert_loan_row", header: "fn array_values(", body: "array_values",
			want: []w{{"{ 1 => [1, 2], 2 => [3, 4] }", originMapLiteralLoanElement}}},
		{name: "nested_literal", header: "fn nested(", body: "nested", slots: []uint32{}},
	}
	runOriginMapLiteralRows(t, "map_literal", originMapLiteralFinishSource, originMapLiteralFinishDigest, 0, rows)
}

// 4 RUN: 1 parent, 3 leaves.
func TestAnalyzeMapLiteralUnvisitedEntries(t *testing.T) {
	type w = struct{ snippet, reason string }
	rows := []originMapLiteralRow{
		{name: "named_entries_keep_the_identifier_row", header: "fn named_entries(", body: "named_entries"},
		{name: "block_value_is_refused_before_evaluation", header: "fn block_value(", body: "block_value",
			want: []w{{"{ 1 => { ret 2; } }", originMapLiteralUnvisited}}},
		{name: "compare_value_is_refused_before_evaluation", header: "fn compare_value(", body: "compare_value",
			want: []w{{"{ 1 => compare o { Some(v) => v; nothing => 0; } }", originMapLiteralUnvisited}}},
	}
	// The identifier rows sit on the single-letter names inside the literal.
	fn := tupleFn(t, originMapLiteralUnvisitedSource, rows[0].header)
	literal := tupleIn(t, originMapLiteralUnvisitedSource, fn, "{ a => x }")
	a := originSpan{literal.start + 2, literal.start + 3, "a"}
	x := originSpan{literal.start + 7, literal.start + 8, "x"}
	t.Run(rows[0].name, func(t *testing.T) {
		checkOriginSource(t, originMapLiteralUnvisitedSource, originMapLiteralUnvisitedDigest, fn, literal, a, x)
		f, analysis := analyzeOriginRoot(t, "map_literal_unvisited_named", originMapLiteralUnvisitedSource, false, nil)
		originExactPending(t, analysis, f.unit.SourceKey, fn,
			[]originRefusal{{span: a, reason: originMapLiteralUnresolved}, {span: x, reason: originMapLiteralUnresolved}})
		originNoEscape(t, analysis, f.owner.File.ID, fn)
	})
	runOriginMapLiteralRows(t, "map_literal_unvisited", originMapLiteralUnvisitedSource, originMapLiteralUnvisitedDigest, 0, rows[1:])
}

// originMapLiteralDerived are the rows that only carry a refused source on to a result or an outgoing reference.
var originMapLiteralDerived = []string{"function result contains an unproved source", "outgoing reference has unresolved or captured provenance"}

// 8 RUN: 1 parent, 7 leaves. Without resolver facts no body is clean: each keeps the identifier row (the window body
// also the insert's loan row).
func TestAnalyzeMapLiteralEntriesWithoutSymbols(t *testing.T) {
	for _, header := range []string{"fn keep_param(", "fn keep_view(", "fn leak_ref(", "fn leak_view(", "fn leak_second(", "fn window_value(", "fn through_ref("} {
		t.Run(strings.TrimSuffix(strings.TrimPrefix(header, "fn "), "("), func(t *testing.T) {
			fn := tupleFn(t, originMapLiteralEntrySource, header)
			checkOriginSource(t, originMapLiteralEntrySource, originMapLiteralEntryDigest, fn)
			f, analysis := analyzeOriginRoot(t, "map_literal_without_symbols", originMapLiteralEntrySource, false, nil)
			unresolved := 0
			for _, row := range originPendingWithin(analysis, f.unit.SourceKey, fn.start, fn.end) {
				switch row.Reason {
				case originMapLiteralUnresolved:
					unresolved++
				case originMapLiteralDerived[0], originMapLiteralDerived[1], originTupleReferentRefusal, originMapLiteralLoanElement:
				default:
					t.Errorf("unexpected %q at %d:%d", row.Reason, row.Span.Start, row.Span.End)
				}
			}
			if unresolved == 0 {
				t.Errorf("an entry naming a binding was admitted without its symbol inside %q", fn.snippet)
			}
			originNoEscape(t, analysis, f.owner.File.ID, fn)
		})
	}
}

// 8 RUN: 1 parent, 7 leaves. With the symbols the resolver would record, each origin is kept.
func TestAnalyzeMapLiteralResolvedEntries(t *testing.T) {
	type w = struct{ snippet, reason string }
	rows := []originMapLiteralRow{
		{name: "value_keeps_a_parameter_reference", header: "fn keep_param(", body: "keep_param", slots: []uint32{0}},
		{name: "view_value_keeps_its_parameter", header: "fn keep_view(", body: "keep_view", slots: []uint32{0}},
		{name: "reference_to_a_dying_local_escapes", header: "fn leak_ref(", body: "leak_ref", escapes: "x"},
		{name: "view_of_a_dying_local_escapes", header: "fn leak_view(", body: "leak_view", escapes: "s"},
		{name: "second_entry_joins_its_origin", header: "fn leak_second(", body: "leak_second", escapes: "x"},
		{name: "window_in_a_payload_free_value_keeps_g6", header: "fn window_value(", body: "window_value",
			want: []w{{"{ 1 => w }", "storage loan would be discarded by a payload-free value"}, {"{ 1 => w }", originMapLiteralLoanElement}}},
		{name: "unknown_entry_keeps_its_row", header: "fn through_ref(", body: "through_ref",
			want: []w{{"r.0", "tuple element read through a reference needs its referent's contents"}}, allow: originMapLiteralDerived},
	}
	runOriginMapLiteralRows(t, "map_literal_resolved", originMapLiteralEntrySource, originMapLiteralEntryDigest, 8, rows)
}

// 2 RUN: 1 parent, 1 leaf. A literal whose recorded type is another map than its entries' is refused.
func TestAnalyzeMapLiteralTypedAuthority(t *testing.T) {
	t.Run("literal_typed_as_another_map", func(t *testing.T) {
		text := originMapLiteralFinishSource
		fn := tupleFn(t, text, "fn returned(")
		site := tupleIn(t, text, fn, `{ "x" => 1 }`)
		other := tupleIn(t, text, tupleFn(t, text, "fn array_values("), "{ 1 => [1, 2], 2 => [3, 4] }")
		checkOriginSource(t, text, originMapLiteralFinishDigest, fn, site, other)
		f, analysis := analyzeOriginRoot(t, "map_literal_typing", text, false, func(f originalGenericFixture) {
			literal := originExprAt(t, f.unit, f.owner.File.ID, site, ast.ExprMap)
			forged := f.unit.Sema.ExprTypes[originExprAt(t, f.unit, f.owner.File.ID, other, ast.ExprMap)]
			if forged == types.NoTypeID || forged == f.unit.Sema.ExprTypes[literal] {
				t.Fatal("PRECONDITION: the two literals do not have two map types")
			}
			f.unit.Sema.ExprTypes[literal] = forged
		})
		if !originPendingAt(analysis, f.unit.SourceKey, site, originMapLiteralTyping) {
			t.Errorf("a literal typed as another map kept its transfer: %+v", originPendingWithin(analysis, f.unit.SourceKey, fn.start, fn.end))
		}
	})
}

// 4 RUN: 1 parent, 3 leaves. A tag constructor the resolver never saw is refused before the tag transfer reads its
// untyped target; a range value keeps the insert's loan row.
func TestAnalyzeMapLiteralInsertRules(t *testing.T) {
	type w = struct{ snippet, reason string }
	rows := []originMapLiteralRow{
		{name: "tag_value_is_refused_before_evaluation", header: "fn tag_value(", body: "tag_value",
			want: []w{{`{ "a" => Some(1) }`, originMapLiteralUnvisited}}, allow: []string{originMapLiteralTagRow}},
		{name: "tag_values_are_refused_before_evaluation", header: "fn tag_values(", body: "tag_values",
			want:  []w{{`{ "a" => Some("x" * 40), "b" => Some("y" * 40) }`, originMapLiteralUnvisited}},
			allow: []string{originMapLiteralTagRow}},
		{name: "range_value_keeps_the_insert_loan_row", header: "fn range_values(", body: "range_values",
			want: []w{{"{ 1 => 0..3 }", originMapLiteralLoanElement}}},
	}
	runOriginMapLiteralRows(t, "map_literal_insert_rules", originMapLiteralInsertRulesSource, originMapLiteralInsertRulesDigest, 0, rows)
}
