package driver

import (
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
)

// A string rvalue under a shared `&string` formal has no borrow record, because the
// checker materializes it instead of borrowing a place. The obligation that used to
// stand on every such argument is answered only where the call can keep nothing: a
// reference-free result with no loan-carrying and no borrow-hiding part (a raw pointer, a
// Task or another core runtime handle), effects of the same kind, and no `&mut` loan
// sink. Each row is a ROOT program against the real core, so `print` and
// the string methods are the declarations a user program reaches.

const stringTemporaryReason = "implicit borrow lacks an admitted borrow for this expression"

// stringTemporaryRow is one t.Run leaf. kept lists the source text of every argument that
// must still carry the obligation; a row with none must leave its unit with no Pending.
// record names an argument that is given a foreign, reserved borrow record before the
// analysis runs: a record of any kind is the checker's word, and the rule must not speak over it.
type stringTemporaryRow struct {
	name, text string
	kept       []string
	record     string
}

const stringTemporaryWorker = "async fn worker(x: &string) -> int { return 6; }\n"

const stringTemporaryHelpers = `fn id_ref(s: &string) -> &string { return s; }
fn size(s: &string) -> uint { return 1:uint; }
fn label(n: int) -> string { return n to string; }
`

func stringTemporaryRows() []stringTemporaryRow {
	return []stringTemporaryRow{
		{name: "literal", text: "fn f() -> nothing { print(\"Hello world!\"); return nothing; }\n"},
		{name: "concatenation", text: "fn f(t: string) -> nothing { print(\"n=\" + t); return nothing; }\n"},
		{name: "conversion", text: "fn f(n: int) -> nothing { print(n to string); return nothing; }\n"},
		{name: "call_result", text: stringTemporaryHelpers + "fn f(n: int) -> nothing { print(label(n)); return nothing; }\n"},
		{name: "grouped", text: "fn f(t: string) -> nothing { print((\"a\" + t)); return nothing; }\n"},
		{name: "nested_conversion", text: "fn f(n: int) -> nothing { print(\"n=\" + (n to string)); return nothing; }\n"},
		{name: "compare_arm", text: "fn show(n: int) -> nothing { print(n to string); }\n" +
			"fn f(v: int?) -> nothing { compare v { Some(x) => show(x); nothing => print(\"nothing\"); }; }\n"},
		{name: "string_method", text: "fn f(s: string) -> bool { return s.contains(\"zz\"); }\n"},
		{name: "two_string_formals", text: "fn f(s: string) -> string { return s.replace(\"hi\", \"yo\"); }\n"},
		{name: "static_intrinsic", text: "fn f() -> nothing { let _ = int.from_str(\"42\"); return nothing; }\n"},
		// The call hands a reference back: the temporary is what it points into.
		{name: "reference_result", text: stringTemporaryHelpers + "fn f(t: string) -> uint { return size(id_ref(\"a\" + t)); }\n",
			kept: []string{"\"a\" + t"}},
		// An array result is a loan carrier.
		{name: "loan_carrier_result", text: "fn parts(s: &string) -> string[] { let out: string[] = []; return out; }\n" +
			"fn f(t: string) -> nothing { let _ = parts(\"b\" + t); return nothing; }\n", kept: []string{"\"b\" + t"}},
		// A `&mut` formal whose referent holds a reference can be written with the borrow.
		{name: "reference_bearing_effect", text: "@intrinsic fn stash(dst: &mut Option<&string>, s: &string) -> nothing;\n" +
			"fn f(dst: &mut Option<&string>, t: string) -> nothing { stash(dst, \"c\" + t); return nothing; }\n", kept: []string{"\"c\" + t"}},
		// A `&mut` array is a loan sink even though its referent reads reference-free.
		{name: "loan_sink_effect", text: "@intrinsic fn swap_in(dst: &mut uint64[], s: &string) -> nothing;\n" +
			"fn f(dst: &mut uint64[], t: string) -> nothing { swap_in(dst, \"d\" + t); return nothing; }\n", kept: []string{"\"d\" + t"}},
		// A Task holds its callee's borrowed formals; its type names only the result.
		{name: "task_result", text: stringTemporaryWorker + "fn f(t: string) -> Task<int> { let k = worker(\"abc\" + t); return k; }\n",
			kept: []string{"\"abc\" + t"}},
		// `arm` is a declaration: a body that parks the task, as it would have to, is refused by the task
		// check itself (RV2-DEBT-365, R-b(call)), and this row asks only what the signature says.
		{name: "task_effect", text: "@intrinsic fn arm(out: &mut Task<int>, s: &string) -> nothing;\n" +
			"fn f(out: &mut Task<int>, t: string) -> nothing { arm(out, \"abd\" + t); return nothing; }\n", kept: []string{"\"abd\" + t"}},
		// A raw pointer into the temporary's bytes outlives the statement that frees them.
		{name: "pointer_result", text: "fn f(t: string) -> nothing { let _p = rt_string_ptr(\"p\" + t); return nothing; }\n", kept: []string{"\"p\" + t"}},
		// The same refusals on CORE callees, where the core-declaration test passes and one conjunct alone decides.
		{name: "core_loan_carrier_result", text: "fn f(s: string, t: string) -> nothing { let _ = s.split(\",\" + t); return nothing; }\n", kept: []string{"\",\" + t"}},
		{name: "core_loan_sink_effect", text: "fn f(out: &mut byte[], t: string) -> nothing { out.append_string(\"g\" + t); return nothing; }\n", kept: []string{"\"g\" + t"}},
		{name: "core_handle_effect", text: "fn f(r: Range<int>, t: string) -> nothing { let _ = rt_string_slice(\"i\" + t, r); return nothing; }\n", kept: []string{"\"i\" + t"}},
		// A user callee's signature cannot show a task started in its body over its formal. The accepted form
		// awaits that task (dropped where it stands it is SEM3218; handed to a callee that drops it, SEM3021 at
		// the return), and the async callee's own result is a Task, which hides a borrow: the argument is kept.
		{name: "user_callee_starts_task", text: stringTemporaryWorker + "async fn fire(s: &string) -> nothing { let _ = worker(s).await(); return nothing; }\n" +
			"async fn f(t: string) -> nothing { let _ = fire(\"q\" + t).await(); return nothing; }\n", kept: []string{"\"q\" + t"}},
		// The same refusal reaches a harmless user wrapper until the task check can speak for its formal.
		{name: "user_callee_plain", text: "fn note(s: &string) -> nothing { return nothing; }\n" +
			"fn f(t: string) -> nothing { note(\"w\" + t); return nothing; }\n", kept: []string{"\"w\" + t"}},
		// A borrow record that names the argument is never overruled, whatever it says.
		{name: "foreign_record", text: "fn f() -> nothing { print(\"r\"); return nothing; }\n", kept: []string{"\"r\""}, record: "\"r\""},
	}
}

func TestAnalyzeStringTemporaryArguments(t *testing.T) {
	rows := stringTemporaryRows()
	if len(rows) != 23 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			checkStringTemporaryRow(t, row)
		})
	}
}

// checkStringTemporaryRow analyzes one root program and compares the arguments that carry
// the implicit-borrow obligation with the row's list, by source text.
func checkStringTemporaryRow(t *testing.T, row stringTemporaryRow) {
	t.Helper()
	for _, text := range row.kept {
		if strings.Count(row.text, text) != 1 {
			t.Fatalf("PRECONDITION: %q is not exact-once in the source", text)
		}
	}
	f, analysis := analyzeOriginRoot(t, "string_temporary", row.text, false, func(f originalGenericFixture) {
		if row.record == "" {
			return
		}
		at := strings.Index(row.text, row.record)
		id := originExprAt(t, f.unit, f.owner.File.ID, originSpan{at, at + len(row.record), row.record}, ast.ExprLit)
		f.unit.Sema.Borrows = append(f.unit.Sema.Borrows, sema.BorrowInfo{ID: 1 << 30, Kind: sema.BorrowShared, Reserved: true, Life: sema.Interval{FromExpr: id}})
	})
	var got []string
	for _, pending := range originPendingWithin(analysis, f.unit.SourceKey, 0, len(row.text)) {
		if pending.Reason == stringTemporaryReason {
			got = append(got, row.text[pending.Span.Start:pending.Span.End])
		} else if len(row.kept) == 0 {
			t.Errorf("unrelated Pending %q at %q", pending.Reason, row.text[pending.Span.Start:pending.Span.End])
		}
	}
	t.Logf("STRING_TEMPORARY leaf=%s class_rows=%q", row.name, got)
	want := slices.Clone(row.kept)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("arguments keeping the obligation = %q, want %q", got, want)
	}
}
