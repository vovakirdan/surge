package driver

import "testing"

// A Task receiver of `.await()` written without `own`: a binding, a call's result, a block, a
// parameter. `await` takes `self: own Task<T>`, and the handle is moved into the call; the
// result TaskResult<T> is then read like any body-less generic result: fresh when T can hold no
// reference, no array and no task, and otherwise a named row at the call. What a running task
// borrows stays the task check's. Every source is a ROOT program against the real core, and each
// row reads one body.

const (
	originTaskAwaitBorrowedState = "opaque result type may carry borrowed state"
	originTaskAwaitUnsupported   = "opaque result borrowed-state classification is unsupported"
	// A reference-bearing payload also fails the finalized use and taints through the receiver's effect.
	originTaskAwaitGenericUse = "generic opaque use requires its type-dependent effect transfer"
	originTaskAwaitEffect     = "opaque call may change reference-bearing or callable contents"
)

const originTaskAwaitFinishSource = `async fn one() -> int {
    return 1;
}

async fn call_awaited() -> int {
    return compare one().await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

async fn bound_awaited() -> int {
    let t = one();
    return compare t.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

async fn block_awaited(n: int) -> int {
    return compare (async { ret n + 1; }).await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

async fn parameter_awaited(t: Task<int>) -> int {
    return compare t.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

async fn checkpoint_awaited() -> int {
    checkpoint().await();
    return 0;
}
`

const originTaskAwaitPayloadSource = `async fn read_ref(t: Task<&int>) -> int {
    let _ = t.await();
    return 0;
}

async fn read_array(t: Task<int[]>) -> int {
    let _ = t.await();
    return 0;
}
`

const (
	originTaskAwaitFinishSourceDigest  = "5f433fe494ca7f57dc443580df0977911c217f2f3f55c302b23073e2ac39de92"
	originTaskAwaitPayloadSourceDigest = "d96c44b2d31900e90ba8f35c319363ccdd1aa9dde4b18b2347fe8d08ba29b197"
)

// originTaskAwaitRow is one t.Run leaf: the Pending rows inside fn must be exactly want -- for a
// refusal, the named row and the "callee returned an unproved source" row the call's unknown value
// derives at the same span (coordinator's measurement, 2026-09-22) -- no diagnostic may point inside
// it, and a row with summary set must publish a normal, source-free summary for body.
type originTaskAwaitRow struct {
	name, text, digest, body string
	fn                       originSpan
	want                     []originRefusal
	summary                  bool
}

func originTaskAwaitRows() []originTaskAwaitRow {
	return []originTaskAwaitRow{
		{name: "call_awaited_finishes", text: originTaskAwaitFinishSource, digest: originTaskAwaitFinishSourceDigest, body: "call_awaited",
			fn: originSpan{41, 168, originTaskAwaitFinishSource[41:168]}, want: []originRefusal{}, summary: true},
		{name: "bound_task_awaited_finishes", text: originTaskAwaitFinishSource, digest: originTaskAwaitFinishSourceDigest, body: "bound_awaited",
			fn: originSpan{170, 313, originTaskAwaitFinishSource[170:313]}, want: []originRefusal{}, summary: true},
		{name: "block_awaited_finishes", text: originTaskAwaitFinishSource, digest: originTaskAwaitFinishSourceDigest, body: "block_awaited",
			fn: originSpan{315, 466, originTaskAwaitFinishSource[315:466]}, want: []originRefusal{}, summary: true},
		{name: "parameter_awaited_finishes", text: originTaskAwaitFinishSource, digest: originTaskAwaitFinishSourceDigest, body: "parameter_awaited",
			fn: originSpan{468, 608, originTaskAwaitFinishSource[468:608]}, want: []originRefusal{}, summary: true},
		{name: "checkpoint_awaited_finishes", text: originTaskAwaitFinishSource, digest: originTaskAwaitFinishSourceDigest, body: "checkpoint_awaited",
			fn: originSpan{610, 690, originTaskAwaitFinishSource[610:690]}, want: []originRefusal{}, summary: true},
		{name: "reference_payload_stays_refused", text: originTaskAwaitPayloadSource, digest: originTaskAwaitPayloadSourceDigest, body: "read_ref",
			fn: originSpan{0, 80, originTaskAwaitPayloadSource[0:80]}, want: []originRefusal{{span: originSpan{54, 63, "t.await()"}, reason: originTaskAwaitBorrowedState}, {span: originSpan{54, 63, "t.await()"}, reason: originCalleeSourceRefusal}, {span: originSpan{54, 63, "t.await()"}, reason: originTaskAwaitGenericUse}, {span: originSpan{54, 63, "t.await()"}, reason: originTaskAwaitEffect}}, summary: false},
		{name: "array_payload_stays_refused", text: originTaskAwaitPayloadSource, digest: originTaskAwaitPayloadSourceDigest, body: "read_array",
			fn: originSpan{82, 165, originTaskAwaitPayloadSource[82:165]}, want: []originRefusal{{span: originSpan{139, 148, "t.await()"}, reason: originTaskAwaitUnsupported}, {span: originSpan{139, 148, "t.await()"}, reason: originCalleeSourceRefusal}}, summary: false},
	}
}

// 8 RUN: 1 parent, 7 leaves. A payload that is itself a task -- a Task<Task<int>> parameter,
// an `async fn` whose result is a task -- is no longer a row: it is a compile error (SEM3223,
// task_payload_is_task_test.go).
func TestAnalyzeTaskAwaits(t *testing.T) {
	rows := originTaskAwaitRows()
	if len(rows) != 7 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			spans := []originSpan{row.fn}
			for _, want := range row.want {
				spans = append(spans, want.span)
			}
			checkOriginSource(t, row.text, row.digest, spans...)
			f, analysis := analyzeOriginRoot(t, "task_await_"+row.name, row.text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, row.fn, row.want)
			originNoEscape(t, analysis, f.owner.File.ID, row.fn)
			if row.summary {
				requireOriginSummary(t, analysis, f.owner.File.ID, row.body, false, nil)
			}
		})
	}
}
