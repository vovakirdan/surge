package driver

import (
	"strings"
	"testing"
)

// `async { ... }` and `blocking { ... }`: the value is a Task handle made in this frame, the body
// is a frame of its own whose `ret` is its only exit, and the frame that makes the task goes on
// with what it had. The handle is fresh only when its payload can hold no reference, no storage
// loan and no task, and every capture the checker recorded for the block must pass the same
// test. Every source is a ROOT program against the real core, and each row reads one body.

const (
	originTaskBlockPayloadRefusal = "an `async` or `blocking` block whose value can hold a reference, a storage loan or a task needs its payload origin"
	originTaskBlockCaptureRefusal = "an `async` or `blocking` block that captures a value which can hold a reference, a storage loan or a task needs its capture origin"
	originTaskBlockSpawnKind      = "expression kind 16 needs an origin transfer"
	originTaskBlockAwaitG1        = "generic original call argument disagrees with its substituted source signature"
)

const originTaskBlockFinishSource = `fn score(n: int) -> Task<int> {
    return async {
        ret n + 1;
    };
}

fn offload(n: int) -> Task<int> {
    return blocking {
        ret n * 2;
    };
}

fn bound(n: int) -> int {
    let job: Task<int> = blocking {
        ret n;
    };
    let _ = job;
    return n;
}

fn pick(c: bool) -> Task<int> {
    return async {
        if c {
            ret 1;
        }
        let v: int = { ret 5; };
        let mut i: int = 0;
        while i < 10 {
            if i == v {
                ret i * 10;
            }
            i = i + 1;
        }
        ret v + 1;
    };
}
`

const originTaskBlockPayloadSource = `fn peek(n: int) -> int {
    let t = async {
        ret &n;
    };
    let _ = t;
    return 0;
}

fn gather() -> int {
    let t = async {
        let a: int[] = [1, 2];
        ret a;
    };
    let _ = t;
    return 0;
}

fn nested() -> Task<Task<int>> {
    return async {
        let inner: Task<int> = async { ret 7; };
        ret inner;
    };
}
`

const originTaskBlockCaptureSource = `fn read_through(r: &int) -> Task<int> {
    return async {
        ret *r;
    };
}
`

const originTaskBlockWalkSource = `fn tag_len(label: &string) -> int {
    return 1;
}

fn body_row() -> Task<int> {
    return async {
        ret tag_len("a" + "b");
    };
}

fn after_spin() -> int {
    let job = blocking {
        while true {
        }
    };
    let _ = job;
    return tag_len("e" + "f");
}

fn later(n: int) -> Task<int> {
    return spawn async {
        ret n + 1;
    };
}

async fn joined(n: int) -> int {
    let t: Task<int> = async { ret n + 1; };
    return compare t.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}
`

const originTaskBlockChannelSource = `fn relay(ch: Channel<int64>) -> Task<nothing> {
    return blocking {
        ch.send(1:int64);
        ret nothing;
    };
}
`

const (
	originTaskBlockFinishSourceDigest  = "fb99996ed30855e326dcaa1c2b7cb87b4ec7bce2c7553fdf47ca25d27bfc9a23"
	originTaskBlockPayloadSourceDigest = "75a5d3011831d2e43801490139dd27f822438e097ea22f5b4ae06cb614071cd4"
	originTaskBlockCaptureSourceDigest = "2099c74e4200e9b4eaa225fac27a15030b67f1fa25b3a8334601d562ffc1048e"
	originTaskBlockWalkSourceDigest    = "7d51a7fca41c0380ae19cb8c947c3a9454118398a108a91b26906fa114389036"
	originTaskBlockChannelSourceDigest = "a70115b4aca7358d56a8af7fcb9f194c1d3b21a9846804241fb435650b46de70"
)

// originTaskBlockRow is one t.Run leaf: the Pending rows inside fn must be exactly want, no
// diagnostic may point inside it, and a row with summary set must publish a normal,
// source-free summary for body.
type originTaskBlockRow struct {
	name, text, digest, body string
	fn                       originSpan
	want                     []originRefusal
	summary                  bool
}

func originTaskBlockRows() []originTaskBlockRow {
	return []originTaskBlockRow{
		{name: "async_payload_finishes", text: originTaskBlockFinishSource, digest: originTaskBlockFinishSourceDigest, body: "score",
			fn: originSpan{0, 78, originTaskBlockFinishSource[0:78]}, want: []originRefusal{}, summary: true},
		{name: "blocking_payload_finishes", text: originTaskBlockFinishSource, digest: originTaskBlockFinishSourceDigest, body: "offload",
			fn: originSpan{80, 163, originTaskBlockFinishSource[80:163]}, want: []originRefusal{}, summary: true},
		{name: "bound_block_continues", text: originTaskBlockFinishSource, digest: originTaskBlockFinishSourceDigest, body: "bound",
			fn: originSpan{165, 281, originTaskBlockFinishSource[165:281]}, want: []originRefusal{}, summary: true},
		{name: "body_exits_finish", text: originTaskBlockFinishSource, digest: originTaskBlockFinishSourceDigest, body: "pick",
			fn: originSpan{283, 588, originTaskBlockFinishSource[283:588]}, want: []originRefusal{}, summary: true},
		{name: "reference_payload_stays_refused", text: originTaskBlockPayloadSource, digest: originTaskBlockPayloadSourceDigest, body: "peek",
			fn: originSpan{0, 98, originTaskBlockPayloadSource[0:98]}, want: []originRefusal{{span: originSpan{37, 66, "async {\n        ret &n;\n    }"}, reason: originTaskBlockPayloadRefusal}, {span: originSpan{80, 81, "t"}, reason: originOutgoingRefusal}}, summary: false},
		{name: "loan_carrier_payload_stays_refused", text: originTaskBlockPayloadSource, digest: originTaskBlockPayloadSourceDigest, body: "gather",
			fn: originSpan{100, 224, originTaskBlockPayloadSource[100:224]}, want: []originRefusal{{span: originSpan{133, 192, "async {\n        let a: int[] = [1, 2];\n        ret a;\n    }"}, reason: originTaskBlockPayloadRefusal}}, summary: false},
		{name: "task_payload_stays_refused", text: originTaskBlockPayloadSource, digest: originTaskBlockPayloadSourceDigest, body: "nested",
			fn: originSpan{226, 354, originTaskBlockPayloadSource[226:354]}, want: []originRefusal{{span: originSpan{270, 351, "async {\n        let inner: Task<int> = async { ret 7; };\n        ret inner;\n    }"}, reason: originTaskBlockPayloadRefusal}, {span: originSpan{263, 352, "return async {\n        let inner: Task<int> = async { ret 7; };\n        ret inner;\n    };"}, reason: originOutgoingRefusal}, {span: originSpan{238, 256, "-> Task<Task<int>>"}, reason: originResultRefusal}}, summary: false},
		{name: "reference_capture_stays_refused", text: originTaskBlockCaptureSource, digest: originTaskBlockCaptureSourceDigest, body: "read_through",
			fn: originSpan{0, 83, originTaskBlockCaptureSource[0:83]}, want: []originRefusal{{span: originSpan{51, 80, "async {\n        ret *r;\n    }"}, reason: originTaskBlockCaptureRefusal}}, summary: false},
		{name: "body_rows_are_reported", text: originTaskBlockWalkSource, digest: originTaskBlockWalkSourceDigest, body: "body_row",
			fn: originSpan{53, 141, originTaskBlockWalkSource[53:141]}, want: []originRefusal{{span: originSpan{121, 130, "\"a\" + \"b\""}, reason: stringTemporaryReason}}, summary: false},
		{name: "continuation_after_a_body_that_never_finishes", text: originTaskBlockWalkSource, digest: originTaskBlockWalkSourceDigest, body: "after_spin",
			fn: originSpan{143, 280, originTaskBlockWalkSource[143:280]}, want: []originRefusal{{span: originSpan{267, 276, "\"e\" + \"f\""}, reason: stringTemporaryReason}}, summary: false},
		{name: "spawn_keeps_its_row", text: originTaskBlockWalkSource, digest: originTaskBlockWalkSourceDigest, body: "later",
			fn: originSpan{282, 366, originTaskBlockWalkSource[282:366]}, want: []originRefusal{{span: originSpan{325, 363, "spawn async {\n        ret n + 1;\n    }"}, reason: originTaskBlockSpawnKind}, {span: originSpan{318, 364, "return spawn async {\n        ret n + 1;\n    };"}, reason: originOutgoingRefusal}, {span: originSpan{299, 311, "-> Task<int>"}, reason: originResultRefusal}}, summary: false},
		{name: "await_keeps_its_g1_row", text: originTaskBlockWalkSource, digest: originTaskBlockWalkSourceDigest, body: "joined",
			fn: originSpan{368, 536, originTaskBlockWalkSource[368:536]}, want: []originRefusal{{span: originSpan{465, 474, "t.await()"}, reason: originTaskBlockAwaitG1}}, summary: false},
	}
}

// 13 RUN: 1 parent, 12 leaves.
func TestAnalyzeTaskBlocks(t *testing.T) {
	rows := originTaskBlockRows()
	if len(rows) != 12 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			spans := []originSpan{row.fn}
			for _, want := range row.want {
				spans = append(spans, want.span)
			}
			checkOriginSource(t, row.text, row.digest, spans...)
			f, analysis := analyzeOriginRoot(t, "task_block_"+row.name, row.text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, row.fn, row.want)
			originNoEscape(t, analysis, f.owner.File.ID, row.fn)
			if row.summary {
				requireOriginSummary(t, analysis, f.owner.File.ID, row.body, false, nil)
			}
		})
	}
}

// originTaskBlockCaptureRow is a blocking body that captures a channel, the shape of
// stdlib/term's read_event_async, in a source of its own so a checker refusal of it cannot
// stop another leaf. `Channel<int>` is SEM3168 into `blocking` (an arbitrary-precision payload,
// coordinator's measurement of 2026-09-22), so the payload is `int64`. The capture row must
// stand at the block; the rows the body's own `send` raises are logged and not judged.
type originTaskBlockCaptureRow struct {
	name        string
	fn, capture originSpan
}

// 2 RUN: 1 parent, 1 leaf.
func TestTaskBlockCapturesAreChecked(t *testing.T) {
	for _, row := range []originTaskBlockCaptureRow{
		{name: "blocking_channel_capture_stays_refused", fn: originSpan{0, 125, originTaskBlockChannelSource[0:125]}, capture: originSpan{59, 122, "blocking {\n        ch.send(1:int64);\n        ret nothing;\n    }"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			checkOriginSource(t, originTaskBlockChannelSource, originTaskBlockChannelSourceDigest, row.fn, row.capture)
			f, analysis := analyzeOriginRoot(t, "task_block_"+row.name, originTaskBlockChannelSource, false, nil)
			for _, p := range originPendingWithin(analysis, f.unit.SourceKey, row.fn.start, row.fn.end) {
				t.Logf("TASK_BLOCK_CAPTURE %s pending %q at %d:%d", row.name, p.Reason, p.Span.Start, p.Span.End)
			}
			if !originPendingAt(analysis, f.unit.SourceKey, row.capture, originTaskBlockCaptureRefusal) {
				t.Errorf("no capture row at %q: a channel captured by the block is refused by nothing that names it",
					strings.SplitN(row.capture.snippet, "\n", 2)[0])
			}
			originNoEscape(t, analysis, f.owner.File.ID, row.fn)
		})
	}
}
