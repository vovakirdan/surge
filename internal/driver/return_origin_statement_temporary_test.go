package driver

import (
	"slices"
	"strings"
	"testing"
)

// `&"text"` is a shared borrow of a string literal: the checker admits it with no
// borrow record and MIR takes the address of the literal's string global. Its root
// is a temporary the evaluating statement owns, so a use inside that statement is
// proven and one past it stays refused at the borrow. Each row is a ROOT program
// against the real core.

const statementTemporaryKept = "borrowed temporary is kept past the statement that owns it"

// statementTemporaryRow is one t.Run leaf. kept lists the source text of every borrow
// that must stay refused; a row with none must leave its unit with no Pending at all.
// summary, when set, is the one function whose result sources are then asserted.
type statementTemporaryRow struct {
	name, text string
	kept       []string
	summary    string
	slots      []uint32
}

const statementTemporaryHelpers = `fn id_ref(s: &string) -> &string { return s; }
fn size(s: &string) -> uint { return 1:uint; }
async fn worker(x: &string) -> int { return 6; }
`

func statementTemporaryRows() []statementTemporaryRow {
	return []statementTemporaryRow{
		{name: "argument", text: "fn f() -> uint { return size(&\"a\"); }\n", summary: "f"},
		{name: "grouped", text: "fn f() -> uint { return size(&(\"a\")); }\n", summary: "f"},
		{name: "core_method", text: "fn f(s: string) -> bool { return s.contains(&\"zz\"); }\n", summary: "f"},
		// The call hands the borrow back, and the statement uses it before it ends.
		{name: "reference_result", text: "fn f() -> uint { return size(id_ref(&\"a\")); }\n", summary: "f"},
		// The temporary leaves no trace on a result that names only an input.
		{name: "input_result", text: "fn f(s: &string) -> &string { let _n = size(id_ref(&\"x\")); return s; }\n",
			summary: "f", slots: []uint32{0}},
		{name: "loop_condition", text: "fn f() -> uint { let mut n: uint = 0:uint; while n < size(&\"a\") { n = n + size(&\"b\"); } return n; }\n",
			summary: "f"},
		{name: "if_condition", text: "fn f() -> uint { if size(id_ref(&\"a\")) == 1:uint { return size(&\"b\"); } return 0:uint; }\n",
			summary: "f"},
		// Past the statement: a binding, a reassignment, a result, an external cell.
		{name: "bound", text: "fn f() -> uint { let r = &\"a\"; return size(r); }\n", kept: []string{"&\"a\""}},
		{name: "reassigned", text: "fn f(s: &string) -> uint { let mut r: &string = s; r = &\"b\"; return size(r); }\n",
			kept: []string{"&\"b\""}},
		{name: "loop_carried", text: "fn f(s: &string) -> uint { let mut r: &string = s; let mut n: uint = 0:uint; " +
			"while n < 2:uint { n = n + size(r); r = &\"l\"; } return n; }\n", kept: []string{"&\"l\""}},
		{name: "returned", text: "fn f() -> &string { return &\"c\"; }\n", kept: []string{"&\"c\""}},
		{name: "returned_through_call", text: "fn f() -> &string { return id_ref(&\"d\"); }\n", kept: []string{"&\"d\""}},
		{name: "external_cell", text: "fn f(out: &mut &string) -> nothing { *out = &\"e\"; return nothing; }\n", kept: []string{"&\"e\""}},
		{name: "external_cell_through_call", text: "fn put(out: &mut &string, s: &string) -> nothing { *out = s; return nothing; }\n" +
			"fn f(out: &mut &string) -> nothing { put(out, &\"m\"); return nothing; }\n", kept: []string{"&\"m\""}},
		// A block result leaves the inner statement that computed it.
		{name: "block_result", text: "fn f() -> uint { return size({ let t = 1; ret &\"k\"; }); }\n", kept: []string{"&\"k\""}},
		// A value that hides a borrow keeps the temporary with no root to show for it.
		{name: "task_result", text: "fn f() -> Task<int> { return worker(&\"g\"); }\n", kept: []string{"&\"g\""}},
		{name: "task_awaited", text: "async fn f() -> nothing { let _ = worker(&\"h\").await(); return nothing; }\n", kept: []string{"&\"h\""}},
		{name: "spawned", text: "async fn f() -> nothing { let t = spawn worker(&\"i\"); let _ = t.await(); return nothing; }\n",
			kept: []string{"&\"i\""}},
		// `arm` is a declaration: a formal it writes through can hold a Task, and a Task hides a borrow.
		{name: "task_effect", text: "@intrinsic fn arm(out: &mut Task<int>, s: &string) -> nothing;\n" +
			"fn f(out: &mut Task<int>) -> nothing { arm(out, &\"n\"); return nothing; }\n", kept: []string{"&\"n\""}},
		{name: "pointer_result", text: "fn f() -> nothing { let _p = rt_string_ptr(&\"j\"); return nothing; }\n", kept: []string{"&\"j\""}},
	}
}

func TestAnalyzeStatementTemporaryBorrows(t *testing.T) {
	rows := statementTemporaryRows()
	if len(rows) != 20 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			checkStatementTemporaryRow(t, row)
		})
	}
}

func checkStatementTemporaryRow(t *testing.T, row statementTemporaryRow) {
	t.Helper()
	text := statementTemporaryHelpers + row.text
	for _, kept := range row.kept {
		if strings.Count(text, kept) != 1 {
			t.Fatalf("PRECONDITION: %q is not exact-once in the source", kept)
		}
	}
	f, analysis := analyzeOriginRoot(t, "statement_temporary", text, false, nil)
	var got []string
	for _, pending := range originPendingWithin(analysis, f.unit.SourceKey, 0, len(text)) {
		snippet := text[pending.Span.Start:pending.Span.End]
		switch {
		case pending.Reason == statementTemporaryKept:
			got = append(got, snippet)
		case len(row.kept) == 0:
			t.Errorf("unrelated Pending %q at %q", pending.Reason, snippet)
		}
	}
	t.Logf("STATEMENT_TEMPORARY leaf=%s kept=%q", row.name, got)
	want := slices.Clone(row.kept)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("borrows kept past their statement = %q, want %q", got, want)
	}
	if row.summary != "" {
		requireOriginSummary(t, analysis, f.owner.File.ID, row.summary, false, row.slots)
	}
}
