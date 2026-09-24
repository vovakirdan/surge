package driver

import (
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
)

// `select` and `race` (expression kinds 20 and 21): every head's operands are walked in arm order by their own
// transfers, a head's result is never delivered, a sent value is admitted only when it and the channel's element
// are inert, and the value is the join of the arms' values from the state every head left. Every source is a ROOT
// program against the real core, and each row reads one body.

// originSelectSendRefusal is return_origin_select.go's send row, spelled out so this file compiles without it.
const originSelectSendRefusal = "a `select` or `race` send whose payload can hold a reference, a storage loan or a task needs its payload origin"

const originSelectFinishSource = `async fn work(n: int) -> int {
    return n + 1;
}

async fn pick(ch: Channel<int>, a: &int, b: &int) -> &int {
    return select {
        ch.recv() => a;
        default => b;
    };
}

async fn race_pick(ch: Channel<int>, a: &int, b: &int) -> &int {
    return race {
        ch.recv() => b;
        default => a;
    };
}

async fn recv_default() -> int {
    let ch = Channel::<int>::new(0:uint);
    let v = select {
        ch.recv() => 1;
        default => 2;
    };
    return v;
}

async fn send_own(ch: Channel<string>, stop: Channel<int>) -> int {
    let mut job = "job-";
    job = job + "payload";
    return select {
        ch.send(own job) => 1;
        stop.recv() => {
            print(job);
            ret 2;
        };
    };
}

async fn task_timer() -> int {
    let t = spawn work(3);
    let t2 = t.clone();
    let v = select {
        t.await() => 3;
        sleep(100).await() => 0;
    };
    let _ = t2.await();
    return v;
}

async fn race_tasks() -> int {
    let t1 = spawn work(1);
    let t2 = spawn work(2);
    return race {
        t1.await() => 1;
        t2.await() => 2;
    };
}

async fn timed() -> int {
    let t = work(1);
    let u = work(2);
    let r = select {
        timeout(t, 5) => 1;
        u.await() => 2;
    };
    return r;
}

async fn block_heads() -> int {
    let t = work(1);
    let u = work(2);
    let v = select {
        { ret t.await(); } => 1;
        { return u.await(); } => 2;
    };
    return v;
}

async fn spawn_head() -> int {
    return select {
        (spawn work(1)).await() => 1;
        default => 2;
    };
}

async fn send_string(ch: Channel<string>) -> int {
    let mut s = "a";
    s = s + "b";
    return select {
        ch.send(own s) => 1;
        default => 2;
    };
}

async fn head_effect_control(a: &int, ch: Channel<int>, c: bool) -> &int {
    let mut r: &int = a;
    return select {
        (compare c {
            true => {
                r = a;
                ret ch;
            };
            false => ch;
        }).recv() => r;
        default => a;
    };
}
`

const originSelectFarSource = `async fn far_send(ch: far Channel<string>, stop: far Channel<int>) -> int {
    let mut job = "job-";
    job = job + "payload";
    return select {
        ch.send(own job) => 1;
        stop.recv() => {
            print(job);
            ret 2;
        };
    };
}
`

const originSelectTemporarySource = `async fn wk(s: &string) -> int {
    return len(s) to int;
}

async fn head_temp(b: string) -> int {
    return select {
        wk("a" + b).await() => 1;
    };
}
`

const originSelectCaptureSource = `async fn head_capture(r: &int) -> int {
    return select {
        (async { ret *r; }).await() => 1;
        default => 2;
    };
}
`

const originSelectSendTaskSource = `async fn work(n: int) -> int {
    return n + 1;
}

async fn send_task(ch: Channel<Task<int>>) -> int {
    let t = work(1);
    return select {
        ch.send(own t) => 1;
        default => 2;
    };
}
`

const originSelectSendArraySource = `async fn send_array(ch: Channel<int[]>) -> int {
    let xs: int[] = [1, 2, 3];
    return select {
        ch.send(own xs) => 1;
        default => 2;
    };
}
`

const originSelectEscapeSource = `async fn first_arm(ch: Channel<int>, a: &int) -> &int {
    let l: int = 1;
    return select {
        ch.recv() => &l;
        default => a;
    };
}

async fn second_arm(ch: Channel<int>, a: &int) -> &int {
    let l: int = 1;
    return race {
        ch.recv() => a;
        default => &l;
    };
}

async fn head_effect(a: &int, ch: Channel<int>, c: bool) -> &int {
    let l: int = 1;
    let mut r: &int = a;
    return select {
        (compare c {
            true => {
                r = &l;
                ret ch;
            };
            false => ch;
        }).recv() => r;
        default => a;
    };
}
`

const (
	originSelectFinishSourceDigest    = "fe6ceec3efb8b15f378d129e0a6b904f813b7666892ae04e0df29933d6e3e14b"
	originSelectFarSourceDigest       = "378efa1a2fa9c6239adb28a08af611b07bb29058d02071a80fc7f82477e989e3"
	originSelectTemporarySourceDigest = "11038505debf90eaa9e5e4e301cffbfb0751ed6da571d9eedc7f92d844e4a66b"
	originSelectCaptureSourceDigest   = "6a5cc6d0e8b4c3017876b9525973c7bf648acef00ddbd8ed86b4c45a10b10d30"
	originSelectSendTaskSourceDigest  = "ab3b5bcb1b483993fceae686ccfb49bfa57a16b4664f14269a1398d64a1237c8"
	originSelectSendArraySourceDigest = "29fab95e981c3204376bad8ac79e3fa6c4a8bb359b270ff8c79904fc32f6ea6d"
	originSelectEscapeSourceDigest    = "d05c3e6c28abf75b11ba5775b326c6ea816bc9f33375da4d03735c0010a2d9cd"
)

// originSelectRow is one t.Run leaf: the Pending rows inside fn must be exactly want, no diagnostic may point inside
// it, and a row with summary set must publish a normal summary for body whose parameter sources are slots.
type originSelectRow struct {
	name, text, digest, body string
	fn                       originSpan
	want                     []originRefusal
	summary                  bool
	slots                    []uint32
}

func originSelectRows() []originSelectRow {
	return []originSelectRow{
		{name: "select_recv_default_finishes", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "recv_default",
			fn: originSpan{327, 491, originSelectFinishSource[327:491]}, want: []originRefusal{}, summary: true, slots: []uint32{}},
		{name: "select_send_own_binding_finishes", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "send_own",
			fn: originSpan{493, 752, originSelectFinishSource[493:752]}, want: []originRefusal{}, summary: true, slots: []uint32{}},
		{name: "select_far_send_finishes", text: originSelectFarSource, digest: originSelectFarSourceDigest, body: "far_send",
			fn: originSpan{0, 267, originSelectFarSource[0:267]}, want: []originRefusal{}, summary: true, slots: []uint32{}},
		{name: "select_task_and_timer_finishes", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "task_timer",
			fn: originSpan{754, 960, originSelectFinishSource[754:960]}, want: []originRefusal{}, summary: true, slots: []uint32{}},
		{name: "race_task_arms_finish", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "race_tasks",
			fn: originSpan{962, 1125, originSelectFinishSource[962:1125]}, want: []originRefusal{}, summary: true, slots: []uint32{}},
		{name: "select_timeout_head_finishes", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "timed",
			fn: originSpan{1127, 1290, originSelectFinishSource[1127:1290]}, want: []originRefusal{}, summary: true, slots: []uint32{}},
		{name: "select_value_joins_both_arms", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "pick",
			fn: originSpan{52, 186, originSelectFinishSource[52:186]}, want: []originRefusal{}, summary: true, slots: []uint32{1, 2}},
		{name: "race_value_joins_both_arms", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "race_pick",
			fn: originSpan{188, 325, originSelectFinishSource[188:325]}, want: []originRefusal{}, summary: true, slots: []uint32{1, 2}},
		{name: "select_head_block_forms_finish", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "block_heads",
			fn: originSpan{1292, 1478, originSelectFinishSource[1292:1478]}, want: []originRefusal{}, summary: true, slots: []uint32{}},
		{name: "select_head_spawn_is_transparent", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "spawn_head",
			fn: originSpan{1480, 1599, originSelectFinishSource[1480:1599]}, want: []originRefusal{}, summary: true, slots: []uint32{}},
		{name: "select_send_string_finishes", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "send_string",
			fn: originSpan{1601, 1769, originSelectFinishSource[1601:1769]}, want: []originRefusal{}, summary: true, slots: []uint32{}},
		{name: "head_effect_control", text: originSelectFinishSource, digest: originSelectFinishSourceDigest, body: "head_effect_control",
			fn: originSpan{1771, 2075, originSelectFinishSource[1771:2075]}, want: []originRefusal{}, summary: true, slots: []uint32{0}},
		{name: "select_head_string_temporary_row_stands", text: originSelectTemporarySource, digest: originSelectTemporarySourceDigest, body: "head_temp",
			fn: originSpan{62, 163, originSelectTemporarySource[62:163]}, want: []originRefusal{{span: originSpan{132, 139, "\"a\" + b"}, reason: stringTemporaryReason}}, summary: false, slots: nil},
		{name: "select_head_capture_row_stands", text: originSelectCaptureSource, digest: originSelectCaptureSourceDigest, body: "head_capture",
			fn: originSpan{0, 132, originSelectCaptureSource[0:132]}, want: []originRefusal{{span: originSpan{69, 86, "async { ret *r; }"}, reason: originTaskBlockCaptureRefusal}}, summary: false, slots: nil},
		{name: "select_send_task_payload_refused", text: originSelectSendTaskSource, digest: originSelectSendTaskSourceDigest, body: "send_task",
			fn: originSpan{52, 204, originSelectSendTaskSource[52:204]}, want: []originRefusal{{span: originSpan{161, 166, "own t"}, reason: originSelectSendRefusal}}, summary: false, slots: nil},
		{name: "select_send_array_payload_refused", text: originSelectSendArraySource, digest: originSelectSendArraySourceDigest, body: "send_array",
			fn: originSpan{0, 160, originSelectSendArraySource[0:160]}, want: []originRefusal{{span: originSpan{116, 122, "own xs"}, reason: originSelectSendRefusal}}, summary: false, slots: nil},
	}
}

// 17 RUN: 1 parent, 16 leaves.
func TestAnalyzeSelect(t *testing.T) {
	rows := originSelectRows()
	if len(rows) != 16 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			spans := []originSpan{row.fn}
			for _, want := range row.want {
				spans = append(spans, want.span)
			}
			checkOriginSource(t, row.text, row.digest, spans...)
			f, analysis := analyzeOriginRoot(t, "select_"+row.name, row.text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, row.fn, row.want)
			originNoEscape(t, analysis, f.owner.File.ID, row.fn)
			if row.summary {
				requireOriginSummary(t, analysis, f.owner.File.ID, row.body, false, row.slots)
			}
		})
	}
}

// requireSelectEscape finds the SEM3139 return origins report for owner inside fn: the select's value carried a
// borrow of a local out of its frame.
func requireSelectEscape(t *testing.T, analysis *sema.ReturnOriginAnalysis, file source.FileID, fn originSpan, owner string) {
	t.Helper()
	for _, d := range analysis.Diagnostics {
		if d.Code == diag.SemaBorrowEscapesReturn && d.Severity == diag.SevError && d.Primary.File == file &&
			int(d.Primary.Start) >= fn.start && int(d.Primary.End) <= fn.end &&
			d.Message == "borrow of '"+owner+"' outlives its owner when this scope exits" {
			return
		}
	}
	t.Errorf("missing SEM3139 for %q inside %q: %+v", owner, fn.snippet, analysis.Diagnostics)
}

// 4 RUN: 1 parent, 3 leaves.
func TestSelectValueEscapeIsReported(t *testing.T) {
	rows := []struct {
		name, body, owner string
		fn                originSpan
	}{
		{name: "select_first_arm_local_escapes", body: "first_arm", owner: "l", fn: originSpan{0, 151, originSelectEscapeSource[0:151]}},
		{name: "race_second_arm_local_escapes", body: "second_arm", owner: "l", fn: originSpan{153, 303, originSelectEscapeSource[153:303]}},
		{name: "head_effect_reaches_the_arm", body: "head_effect", owner: "l", fn: originSpan{305, 622, originSelectEscapeSource[305:622]}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			checkOriginSource(t, originSelectEscapeSource, originSelectEscapeSourceDigest, row.fn)
			f, analysis := analyzeOriginRoot(t, "select_escape_"+row.name, originSelectEscapeSource, true, nil)
			requireSelectEscape(t, analysis, f.owner.File.ID, row.fn, row.owner)
		})
	}
}
