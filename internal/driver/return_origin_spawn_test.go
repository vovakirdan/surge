package driver

import "testing"

// `spawn X` (expression kind 16): X is walked by its own transfer and the spawned value is X's handle unchanged --
// the runtime wakes the task the handle names and stores the same handle. What the running task borrows is the
// task check's; what the operand carries keeps its own rows: an `async { }` block's payload and capture rows, a lent
// temporary's RV2-DEBT-368 row (including a member left to an implicit join, which nothing pins); `spawn on` is
// N-TASK-27S's. Every source is a ROOT program against the real core, and each row reads one body.

const originSpawnFinishSource = `async fn work(n: int) -> int {
    return n + 1;
}

fn spawn_call(n: int) -> Task<int> {
    return spawn work(n);
}

fn spawn_name(n: int) -> Task<int> {
    let t = work(n);
    let s = spawn t;
    return s;
}

fn spawn_block(n: int) -> Task<int> {
    return spawn async {
        ret n + 1;
    };
}

fn spawn_value_block(n: int) -> Task<int> {
    return {
        ret spawn work(n);
    };
}
`

const originSpawnWrappedSource = `async fn work(n: int) -> int {
    return n + 1;
}

fn spawn_wrapped(n: int) -> Option<Task<int>> {
    return Some(spawn work(n));
}
`

const originSpawnPayloadSource = `fn spawn_nested() -> Task<Task<int>> {
    return spawn async {
        let inner: Task<int> = async { ret 7; };
        ret inner;
    };
}
`

const originSpawnCaptureSource = `fn spawn_read_through(r: &int) -> Task<int> {
    return spawn async {
        ret *r;
    };
}
`

const originSpawnTemporarySource = `async fn wk(s: &string) -> int {
    return len(s) to int;
}

fn spawn_temporary(b: string) -> Task<int> {
    return spawn wk("a" + b);
}
`

const originSpawnImplicitJoinTemporarySource = `async fn wk(s: &string) -> int {
    return len(s) to int;
}

@entrypoint
fn main() -> int {
    let b: string = "c";
    let r = (async {
        let _s = spawn wk("a" + b);
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`

const originSpawnImplicitJoinCaptureSource = `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let bl: string = "abc";
        let r = &bl;
        let _s = spawn async {
            ret len(r) to int;
        };
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`

const (
	originSpawnFinishSourceDigest                = "b308b0e83622cc6197cd137b849ed5b99fa2590c3ad937d0c889ccf6f04e02ce"
	originSpawnWrappedSourceDigest               = "0cee108217de9546808715e8d56218016e616ce60d3c544d3089aee75317ffa0"
	originSpawnPayloadSourceDigest               = "08ead9d94e4ed8ca4c83da89b9491818fbe915d8679700b9791d0887eff13276"
	originSpawnCaptureSourceDigest               = "88bb7316c5ff53887871b2fcf2c3e5180d8b3018ee314e4b845542c2c7f9a384"
	originSpawnTemporarySourceDigest             = "a5e8b75740732335f3c2805f62221ff5f8f1a8c6dfd44d56cb4696b4b0987146"
	originSpawnImplicitJoinTemporarySourceDigest = "64359b706d5cc77c56cec3f323dc7ec3346f842d729958b60c8915631e593b9f"
	originSpawnImplicitJoinCaptureSourceDigest   = "452f7983da0fb8a1f8108ae94871a7bd7b9fb0804427d3ec515483acfd98b70e"
)

// originSpawnRow is one t.Run leaf: the Pending rows inside fn must be exactly want, no diagnostic may point inside
// it, and a row with summary set must publish a normal, source-free summary for body.
type originSpawnRow struct {
	name, text, digest, body string
	fn                       originSpan
	want                     []originRefusal
	summary                  bool
}

func originSpawnRows() []originSpawnRow {
	return []originSpawnRow{
		{name: "spawn_call_finishes", text: originSpawnFinishSource, digest: originSpawnFinishSourceDigest, body: "spawn_call",
			fn: originSpan{52, 116, originSpawnFinishSource[52:116]}, want: []originRefusal{}, summary: true},
		{name: "spawn_name_finishes", text: originSpawnFinishSource, digest: originSpawnFinishSourceDigest, body: "spawn_name",
			fn: originSpan{118, 212, originSpawnFinishSource[118:212]}, want: []originRefusal{}, summary: true},
		{name: "spawn_block_finishes", text: originSpawnFinishSource, digest: originSpawnFinishSourceDigest, body: "spawn_block",
			fn: originSpan{214, 304, originSpawnFinishSource[214:304]}, want: []originRefusal{}, summary: true},
		{name: "spawn_value_block_finishes", text: originSpawnFinishSource, digest: originSpawnFinishSourceDigest, body: "spawn_value_block",
			fn: originSpan{306, 398, originSpawnFinishSource[306:398]}, want: []originRefusal{}, summary: true},
		{name: "spawn_wrapped_finishes", text: originSpawnWrappedSource, digest: originSpawnWrappedSourceDigest, body: "spawn_wrapped",
			fn: originSpan{52, 133, originSpawnWrappedSource[52:133]}, want: []originRefusal{}, summary: true},
		{name: "spawn_task_payload_keeps_its_derived_rows", text: originSpawnPayloadSource, digest: originSpawnPayloadSourceDigest, body: "spawn_nested",
			fn: originSpan{0, 140, originSpawnPayloadSource[0:140]}, want: []originRefusal{{span: originSpan{56, 137, "async {\n        let inner: Task<int> = async { ret 7; };\n        ret inner;\n    }"}, reason: originTaskBlockPayloadRefusal}, {span: originSpan{43, 138, "return spawn async {\n        let inner: Task<int> = async { ret 7; };\n        ret inner;\n    };"}, reason: originOutgoingRefusal}, {span: originSpan{18, 36, "-> Task<Task<int>>"}, reason: originResultRefusal}}, summary: false},
		{name: "spawn_async_capture_row_stands", text: originSpawnCaptureSource, digest: originSpawnCaptureSourceDigest, body: "spawn_read_through",
			fn: originSpan{0, 95, originSpawnCaptureSource[0:95]}, want: []originRefusal{{span: originSpan{63, 92, "async {\n        ret *r;\n    }"}, reason: originTaskBlockCaptureRefusal}}, summary: false},
		{name: "spawn_string_temporary_row_stands", text: originSpawnTemporarySource, digest: originSpawnTemporarySourceDigest, body: "spawn_temporary",
			fn: originSpan{62, 138, originSpawnTemporarySource[62:138]}, want: []originRefusal{{span: originSpan{127, 134, "\"a\" + b"}, reason: stringTemporaryReason}}, summary: false},
		{name: "s_ij_temp", text: originSpawnImplicitJoinTemporarySource, digest: originSpawnImplicitJoinTemporarySourceDigest, body: "main",
			fn: originSpan{74, 288, originSpawnImplicitJoinTemporarySource[74:288]}, want: []originRefusal{{span: originSpan{165, 172, "\"a\" + b"}, reason: stringTemporaryReason}}, summary: false},
		{name: "s_ij_capture", text: originSpawnImplicitJoinCaptureSource, digest: originSpawnImplicitJoinCaptureSourceDigest, body: "main",
			fn: originSpan{105, 384, originSpawnImplicitJoinCaptureSource[105:384]}, want: []originRefusal{{span: originSpan{221, 269, "async {\n            ret len(r) to int;\n        }"}, reason: originTaskBlockCaptureRefusal}}, summary: false},
		{name: "spawn_on_finishes", text: onCrossingWalkSource, digest: onCrossingWalkSourceDigest, body: "later",
			fn: originSpan{306, 396, onCrossingWalkSource[306:396]}, want: []originRefusal{}, summary: false},
	}
}

// 12 RUN: 1 parent, 11 leaves.
func TestAnalyzeSpawn(t *testing.T) {
	rows := originSpawnRows()
	if len(rows) != 11 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			spans := []originSpan{row.fn}
			for _, want := range row.want {
				spans = append(spans, want.span)
			}
			checkOriginSource(t, row.text, row.digest, spans...)
			f, analysis := analyzeOriginRoot(t, "spawn_"+row.name, row.text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, row.fn, row.want)
			originNoEscape(t, analysis, f.owner.File.ID, row.fn)
			if row.summary {
				requireOriginSummary(t, analysis, f.owner.File.ID, row.body, false, nil)
			}
		})
	}
}
