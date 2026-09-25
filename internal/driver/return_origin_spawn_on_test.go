package driver

import "testing"

// N-TASK-27S: a `spawn on dst { ... }` crossing gets an origin transfer. The destination is an expression of this
// frame; the body is a frame of its own whose `ret` is its only exit, as an `on` body is; this frame goes on from the
// state after the destination. The body runs on its own copies of its captures, so every capture must be
// crossing-inert, and so must the far task's result: then the `far Task<T>` handle carries no origin at all.
// Every other capture or result keeps a named row. Each row is a ROOT program against the real core.

const (
	spawnOnPayloadRefusal = "a `spawn on` task result that can hold a reference, a storage loan or a task needs its payload origin"
	spawnOnCaptureRefusal = "a `spawn on` capture that can hold a reference, a storage loan or a task needs its capture origin"
)

// spawnOnRow is one leaf: the Pending rows inside fn must be exactly want, no diagnostic may point inside it, and a
// row with summary set must publish a normal, source-free summary for body.
type spawnOnRow struct {
	name, text, digest, body string
	fn                       originSpan
	want                     []originRefusal
	summary                  bool
}

func spawnOnRows() []spawnOnRow {
	return []spawnOnRow{
		{name: "returned_far_task_finishes", text: spawnOnSReturnedSource, digest: spawnOnSReturnedSourceDigest, body: "start",
			fn: originSpan{48, 149, "fn start(n: int) -> far Task<int> {\n    return spawn on distributed {\n        ret double(n);\n    };\n}"}, want: []originRefusal{}, summary: true},
		{name: "bound_then_returned_finishes", text: spawnOnSBoundSource, digest: spawnOnSBoundSourceDigest, body: "start",
			fn: originSpan{0, 125, "fn start(n: int) -> far Task<int> {\n    let k: int = n;\n    let t = spawn on pool {\n        ret k + 1;\n    };\n    return t;\n}"}, want: []originRefusal{}, summary: true},
		{name: "awaited_in_frame_finishes", text: spawnOnSAwaitedSource, digest: spawnOnSAwaitedSourceDigest, body: "run",
			fn: originSpan{0, 137, "fn run(dst: Placement) -> TaskResult<int> {\n    let task: far Task<int> = spawn on dst {\n        ret 3;\n    };\n    return task.await();\n}"}, want: []originRefusal{}, summary: true},
		{name: "task_payload_refused", text: spawnOnR01PayloadTaskSource, digest: spawnOnR01PayloadTaskSourceDigest, body: "start",
			fn: originSpan{49, 142, "fn start() -> far Task<Task<int>> {\n    return spawn on pool {\n        ret plain(1);\n    };\n}"}, want: []originRefusal{{span: originSpan{96, 139, "spawn on pool {\n        ret plain(1);\n    }"}, reason: spawnOnPayloadRefusal}, {span: originSpan{89, 140, "return spawn on pool {\n        ret plain(1);\n    };"}, reason: originOutgoingRefusal}, {span: originSpan{60, 82, "-> far Task<Task<int>>"}, reason: originResultRefusal}}, summary: false},
		{name: "array_payload_refused", text: spawnOnR16PayloadArraySource, digest: spawnOnR16PayloadArraySourceDigest, body: "start",
			fn: originSpan{0, 115, "fn start() -> far Task<int[]> {\n    return spawn on pool {\n        let xs: int[] = [1, 2];\n        ret xs;\n    };\n}"}, want: []originRefusal{{span: originSpan{43, 112, "spawn on pool {\n        let xs: int[] = [1, 2];\n        ret xs;\n    }"}, reason: spawnOnPayloadRefusal}, {span: originSpan{36, 113, "return spawn on pool {\n        let xs: int[] = [1, 2];\n        ret xs;\n    };"}, reason: originOutgoingRefusal}, {span: originSpan{11, 29, "-> far Task<int[]>"}, reason: originResultRefusal}}, summary: false},
		{name: "channel_capture_refused", text: spawnOnC05CapChannel64Source, digest: spawnOnC05CapChannel64SourceDigest, body: "start",
			fn: originSpan{0, 124, "fn start(ch: Channel<int64>) -> far Task<int> {\n    return spawn on pool {\n        ch.send(1:int64);\n        ret 1;\n    };\n}"}, want: []originRefusal{{span: originSpan{83, 85, "ch"}, reason: spawnOnCaptureRefusal}}, summary: false},
		{name: "fn_value_capture_refused", text: spawnOnC06CapFnValueSource, digest: spawnOnC06CapFnValueSourceDigest, body: "start",
			fn: originSpan{35, 134, "fn start() -> far Task<int> {\n    let f = one;\n    return spawn on pool {\n        ret f();\n    };\n}"}, want: []originRefusal{{span: originSpan{121, 122, "f"}, reason: spawnOnCaptureRefusal}}, summary: false},
		{name: "far_task_capture_refused", text: spawnOnR14CapFarTaskSource, digest: spawnOnR14CapFarTaskSourceDigest, body: "start",
			fn: originSpan{0, 225, "fn start() -> far Task<int> {\n    let inner = spawn on pool {\n        ret 1;\n    };\n    return spawn on pool {\n        ret compare inner.await() {\n            Success(n) => n;\n            Cancelled() => 0;\n        };\n    };\n}"}, want: []originRefusal{{span: originSpan{131, 136, "inner"}, reason: spawnOnCaptureRefusal}}, summary: false},
		{name: "body_rows_are_reported", text: spawnOnR09BodyStringTempSource, digest: spawnOnR09BodyStringTempSourceDigest, body: "start",
			fn: originSpan{62, 243, "async fn start() -> far Task<int> {\n    return spawn on pool {\n        ret compare wk(\"abc\").await() {\n            Success(n) => n;\n            Cancelled() => 0;\n        };\n    };\n}"}, want: []originRefusal{{span: originSpan{148, 153, "\"abc\""}, reason: stringTemporaryReason}}, summary: false},
		{name: "destination_rows_are_reported", text: spawnOnR18DestStringTempSource, digest: spawnOnR18DestStringTempSourceDigest, body: "start",
			fn: originSpan{63, 151, "fn start() -> far Task<int> {\n    return spawn on route(\"abc\") {\n        ret 1;\n    };\n}"}, want: []originRefusal{{span: originSpan{119, 124, "\"abc\""}, reason: stringTemporaryReason}}, summary: false},
		{name: "continuation_rows_are_reported", text: spawnOnR17AfterRowSource, digest: spawnOnR17AfterRowSourceDigest, body: "start",
			fn: originSpan{47, 175, "fn start() -> int {\n    let ft = spawn on distributed {\n        ret 1;\n    };\n    let _ = ft.await();\n    return route(\"abc\");\n}"}, want: []originRefusal{{span: originSpan{166, 171, "\"abc\""}, reason: stringTemporaryReason}}, summary: false},
	}
}

// 12 RUN: 1 parent, 11 leaves.
func TestAnalyzeSpawnOn(t *testing.T) {
	rows := spawnOnRows()
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
			f, analysis := analyzeOriginRoot(t, "spawn_on_"+row.name, row.text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, row.fn, row.want)
			originNoEscape(t, analysis, f.owner.File.ID, row.fn)
			if row.summary {
				requireOriginSummary(t, analysis, f.owner.File.ID, row.body, false, nil)
			}
		})
	}
}

// A borrow of a body local that outlives the local inside the body is the return-origin analysis's own SEM3139,
// reported inside the body: the body is walked as a frame (CF-S27a removes the walk and the program builds and reads
// a freed string natively).
//
// 2 RUN: 1 parent, 1 leaf.
func TestSpawnOnBodyEscapeIsReported(t *testing.T) {
	for _, row := range []struct {
		name, text, digest string
		fn                 originSpan
		n                  int
	}{
		{name: "body_local_escape_reported", text: spawnOnR10BodyLocalEscapeSource, digest: spawnOnR10BodyLocalEscapeSourceDigest, fn: originSpan{0, 203, "fn start() -> far Task<int> {\n    return spawn on pool {\n        let v: int = 1;\n        let mut p: &int = &v;\n        {\n            let w: int = 2;\n            p = &w;\n        }\n        ret *p;\n    };\n}"}, n: 2},
	} {
		t.Run(row.name, func(t *testing.T) {
			checkOriginSource(t, row.text, row.digest, row.fn)
			f, analysis := analyzeOriginRoot(t, "spawn_on_"+row.name, row.text, true, nil)
			got := 0
			for _, d := range analysis.Diagnostics {
				if d.Primary.File == f.owner.File.ID && int(d.Primary.Start) >= row.fn.start && int(d.Primary.End) <= row.fn.end {
					t.Logf("SPAWN_ON_ESCAPE %s %q", d.Code.ID(), d.Message)
					if d.Code.ID() != "SEM3139" {
						t.Errorf("diagnostic %s %q, want only SEM3139", d.Code.ID(), d.Message)
					}
					got++
				}
			}
			if got != row.n {
				t.Errorf("%d SEM3139 inside the body's function, want %d: the body is not walked as a frame", got, row.n)
			}
			for _, p := range originPendingWithin(analysis, f.unit.SourceKey, row.fn.start, row.fn.end) {
				t.Errorf("unexpected pending %q at %d:%d", p.Reason, p.Span.Start, p.Span.End)
			}
		})
	}
}
