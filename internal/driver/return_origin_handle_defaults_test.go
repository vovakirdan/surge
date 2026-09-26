package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// N-DEFHANDLE: the default of a core runtime handle (Task, Channel, Range, and so
// Mutex, whose one field is a Channel) is the null handle (owner ruling 2026-09-25,
// packet VMDH), so it meets Defaultable with no requirement. The rule answers
// Defaultable only: a handle obtained any other way still meets NoBorrowedState and
// keeps its row, which is R-i's fence (RV2-DEBT-365). Every source is a ROOT program
// against the real core.

const (
	handleDefaultUnsupported   = "opaque result borrowed-state classification is unsupported"
	handleDefaultNotProven     = "default result is not proven Defaultable"
	handleDefaultNoOperation   = "generic use lacks its original typed operation"
	handleDefaultLoanDiscarded = "storage loan would be discarded by a payload-free value"
)

// The shapes of the five golden programs this packet frees, and the declaration
// and return forms of the same default.
var handleDefaultFinishRows = []struct{ name, text string }{
	{"popped_task_drain", `async fn work() -> int {
    return 1;
}

pub async fn drain() -> int {
    let mut q: Task<int>[] = [];
    q.push(spawn work());
    q.push(spawn work());
    let mut sum: int = 0;
    while q.__len() != 0:uint {
        let t = q.pop().safe();
        sum = sum + compare t.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
    }
    return sum;
}
`},
	{"joined_clone_drain", `async fn worker(x: &int64) -> int64 {
    return *x;
}

async fn drained_then_clone_returned() -> Task<int64> {
    let l: int64 = 5;
    let t = spawn worker(&l);
    let c = t.clone();
    let mut tasks: Task<int64>[] = [];
    tasks.push(t);
    while tasks.__len() > 0:uint {
        let x = tasks.pop().safe();
        let _ = x.await();
    }
    return c;
}
`},
	{"declared_defaults", `fn declared() -> int {
    let ch: Channel<int>;
    let r: Range<int>;
    let t: Task<int>;
    let m: Mutex;
    return 0;
}
`},
	{"returned_defaults", `fn task_default() -> Task<int> {
    let t: Task<int>;
    return t;
}

fn channel_from_empty() -> Channel<int> {
    let mut cs: Channel<int>[] = [];
    return cs.pop().safe();
}

fn range_from_nothing() -> Range<int> {
    let o: Option<Range<int>> = nothing;
    return o.safe();
}
`},
}

func TestReturnOriginHandleDefaultsFinish(t *testing.T) {
	for _, row := range handleDefaultFinishRows {
		t.Run(row.name, func(t *testing.T) {
			f, analysis := analyzeOriginRoot(t, "handle_default_"+row.name, row.text, false, nil)
			for _, pending := range analysis.Pending {
				t.Errorf("unexpected pending %q at %s %v", pending.Reason, pending.SourceKey, pending.Span)
			}
			originNoEscape(t, analysis, f.owner.File.ID, originSpan{0, len(row.text), row.name})
		})
	}
}

// handleDefaultAt names the one occurrence of snippet in text.
func handleDefaultAt(t *testing.T, text, snippet string) originSpan {
	t.Helper()
	start := strings.Index(text, snippet)
	if start < 0 || strings.Count(text, snippet) != 1 {
		t.Fatalf("PRECONDITION: %q is not unique in the fixture", snippet)
	}
	return originSpan{start, start + len(snippet), snippet}
}

// Canaries the analysis itself must keep refusing, each by its exact rows.
func TestReturnOriginHandleDefaultCanariesKeepTheirRows(t *testing.T) {
	type want struct{ snippet, reason string }
	rows := []struct {
		name, text string
		want       []want
	}{
		{
			// A Range over a local array holds a storage loan; out of a container it keeps
			// the G6 row where it is stored. Only the default path of safe() is answered.
			name: "borrowing_range_out_of_container", text: `fn leak_range() -> Range<int> {
    let xs: int[] = [1, 2, 3];
    let mut rs: Range<int>[] = [];
    rs.push(xs.__range());
    return rs.pop().safe();
}
`, want: []want{{"rs.push(xs.__range())", handleDefaultLoanDiscarded}},
		},
		{
			name: "borrowing_range_through_option", text: `fn leak_range_opt() -> Range<int> {
    let xs: int[] = [1, 2, 3];
    let o: Option<Range<int>> = Some(xs.__range());
    return o.safe();
}
`, want: []want{{"Some(xs.__range())", handleDefaultLoanDiscarded}},
		},
		{
			// A handle popped out of a borrowed container: the element's storage loan is
			// still refused once the default path is answered.
			name: "task_out_of_borrowed_container", text: `fn pop_task_param(q: &mut Task<int64>[]) -> Task<int64> {
    return q.pop().safe();
}
`, want: []want{{"q.pop().safe()", handleDefaultLoanDiscarded}},
		},
		{
			// R-i: NoBorrowedState on the result of await over a Task payload is untouched.
			name: "task_payload_await_reaches_r_i", text: `async fn read_task(t: Task<Task<int>>) -> int {
    let _ = t.await();
    return 0;
}
`, want: []want{{"t.await()", handleDefaultUnsupported}, {"t.await()", originCalleeSourceRefusal}},
		},
		{
			// @intrinsic but not a runtime handle: the rule is keyed by core's handle
			// marks, not by the attribute, so these defaults stay unproven.
			name: "intrinsic_rwlock_default", text: `fn lock_default() -> RwLock {
    let l: RwLock;
    return l;
}
`, want: []want{{"let l: RwLock;", handleDefaultNotProven}, {"let l: RwLock;", handleDefaultNoOperation}},
		},
		{
			name: "intrinsic_file_default", text: `fn file_default() -> File {
    let f: File;
    return f;
}
`, want: []want{{"let f: File;", handleDefaultNotProven}, {"let f: File;", handleDefaultNoOperation}},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f, analysis := analyzeOriginRoot(t, "handle_default_"+row.name, row.text, false, nil)
			var refusals []originRefusal
			for _, w := range row.want {
				refusals = append(refusals, originRefusal{span: handleDefaultAt(t, row.text, w.snippet), reason: w.reason})
			}
			originExactPending(t, analysis, f.unit.SourceKey, originSpan{0, len(row.text), row.name}, refusals)
		})
	}
}

// Canaries the checker refuses before the analysis runs: a Task obtained from a
// borrowing call keeps SEM3139 or the task check's own refusal.
func TestReturnOriginHandleDefaultTaskCanariesStayRefused(t *testing.T) {
	rows := []struct {
		name, text string
		code       diag.Code
	}{
		{"borrowing_task_returned", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak_direct() -> Task<int> {
    let l: string = "abcdef";
    return worker(&l);
}
`, diag.SemaBorrowEscapesReturn},
		{"borrowing_task_through_option", `async fn worker(x: &int64) -> int64 {
    return *x;
}

async fn leak_task_opt() -> Task<int64> {
    let l: int64 = 5;
    let o: Option<Task<int64>> = Some(spawn worker(&l));
    return o.safe();
}
`, diag.SemaBorrowThreadEscape},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdlib := detectStdlibRootFrom(".")
			if stdlib == "" {
				t.Fatal("PRECONDITION: real stdlib unavailable")
			}
			t.Setenv("SURGE_STDLIB", stdlib)
			root := t.TempDir()
			path := filepath.Join(root, "main.sg")
			if err := os.WriteFile(path, []byte(row.text), 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := DiagnoseWithOptions(context.Background(), path, &DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64})
			if err != nil || result == nil || result.Bag == nil {
				t.Fatalf("diagnose: result=%v err=%v", result != nil, err)
			}
			for _, d := range result.Bag.Items() {
				if d.Code == row.code && d.Severity >= diag.SevError {
					return
				}
			}
			t.Fatalf("expected %s, got:\n%s", row.code.ID(), diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
		})
	}
}
