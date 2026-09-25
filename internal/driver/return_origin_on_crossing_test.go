package driver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// An immediate `on dst { ... }` crossing: the destination is an expression of the caller's
// frame, the body is a frame of its own whose `ret` is its only exit, and the value is the
// reply, `TaskResult<T>`, made on the far side. A reply, or a value an anchored channel
// operation moves through the ring, is admitted only when its type can hold no reference, no
// storage loan and no task; `spawn on` is N-TASK-27S's. Every source is a ROOT program against
// the real core, and each row reads one body of one source.

const (
	onCrossingReplyRefusal    = "an `on` crossing reply that can hold a reference, a storage loan or a task needs its reply origin"
	onCrossingAnchoredRefusal = "an anchored channel operation that moves a reference, a storage loan or a task needs its channel crossing contract"
	onCrossingCaptureRefusal  = "an `on` crossing capture that can hold a reference, a storage loan or a task needs its capture origin"
)

// The rows a callable value may raise on its own, allowed where the capture of one is the subject.
const (
	onCrossingCallableIdent     = "callable identifier lacks concrete source facts"
	onCrossingCallableValue     = "callable value needs its concrete original type and alias authority"
	onCrossingCallableAuthority = "selected callable lacks its published callable authority"
)

const onCrossingPlacementSource = `fn double(x: int) -> int {
    return x * 2;
}
fn route_for(user_id: uint64) -> Placement {
    return distributed;
}
fn score(n: int) -> TaskResult<int> {
    return on pool {
        ret double(n);
    };
}
fn routed(n: int, uid: uint64) -> TaskResult<int> {
    return on route_for(uid) {
        ret double(n);
    };
}
fn discard(n: int) -> nothing {
    on pool {
        let _ = double(n);
        ret nothing;
    };
    return nothing;
}
`

const onCrossingAnchoredSource = `@shard_movable
type Message = {
    id: int,
};
fn send_job(ch: far Channel<Message>, msg: own Message) -> TaskResult<nothing> {
    return on ch {
        ch.send(own msg);
        ret nothing;
    };
}
fn take_one(ch: far Channel<int>) -> TaskResult<Option<int>> {
    return on ch {
        ret ch.recv();
    };
}
fn close_remote(conn: far TcpConn) -> TaskResult<nothing> {
    return on conn {
        conn.close();
        ret nothing;
    };
}
`

const onCrossingReferenceSource = `fn peek(n: int) -> int {
    let r = on pool {
        ret &n;
    };
    let _ = r;
    return 0;
}
`

const onCrossingLoanSource = `fn gather() -> int {
    let r = on pool {
        let a: int[] = [1, 2];
        ret a;
    };
    let _ = r;
    return 0;
}
fn push_all(ch: far Channel<int[]>, xs: int[]) -> TaskResult<nothing> {
    return on ch {
        ch.send(own xs);
        ret nothing;
    };
}
fn pull(ch: far Channel<int[]>) -> TaskResult<int> {
    return on ch {
        let got: Option<int[]> = ch.recv();
        ret 0;
    };
}
`

const onCrossingTaskSource = `async fn worker(x: int64) -> int64 {
    return x;
}
fn hand_off() -> int {
    let r = on pool {
        let t = worker(5:int64);
        ret t;
    };
    let _ = r;
    return 0;
}
`

const onCrossingWalkSource = `fn tag_len(label: &string) -> int {
    return 1;
}
fn place_of(label: &string) -> Placement {
    return pool;
}
fn body_row() -> TaskResult<int> {
    return on pool {
        ret tag_len("a" + "b");
    };
}
fn dest_row() -> TaskResult<int> {
    return on place_of("c" + "d") {
        ret 1;
    };
}
fn later(n: int) -> far Task<int> {
    return spawn on pool {
        ret n + 1;
    };
}
`

const onCrossingSpinSource = `fn tag_len(label: &string) -> int {
    return 1;
}
fn after_spin() -> int {
    on pool {
        while true {
        }
    };
    return tag_len("e" + "f");
}
`

const onCrossingLeakSource = `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak() -> int64 {
    let r = on pool {
        let bl: int64 = 5;
        let t = worker(&bl);
        ret t;
    };
    let _ = r;
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`

const onCrossingFnCaptureSource = `fn double(x: int) -> int {
    return x * 2;
}
fn apply(n: int) -> TaskResult<int> {
    let f = double;
    return on pool {
        ret f(n);
    };
}
`

const onCrossingOwnMutRefSource = `fn take_mut(r: own &mut int) -> int {
    return 1;
}
fn bump_own(o: own &mut int) -> TaskResult<int> {
    return on pool {
        ret take_mut(o);
    };
}
`

const onCrossingOwnRefArraySource = `fn take_arr(r: own &int[]) -> int {
    return 1;
}
fn first_own(o: own &int[]) -> TaskResult<int> {
    return on pool {
        ret take_arr(o);
    };
}
`

const (
	onCrossingPlacementSourceDigest   = "654966b3e8684aa418bd0572b12ac283b98cb716b04f8425279813d6bf71f624"
	onCrossingAnchoredSourceDigest    = "af8f6a4e4e6a06d1f039f9187297ded22e3584ba5f8ea5d4f7b8a335379ef959"
	onCrossingReferenceSourceDigest   = "96190a3b8f7ec90a066f933477107f1c2af32d84c6c41929ee7aa1192c523c12"
	onCrossingLoanSourceDigest        = "8ab9b61d042696b838f413af4be1295044ca9d3276c244407cdca08aa92a13d6"
	onCrossingTaskSourceDigest        = "b57358e80e9a0f99246f0d97b4fca664d098da101c8c1030ac744faa12d57f47"
	onCrossingWalkSourceDigest        = "a5dad5e2ffb5faf276224b03b2b0d8f426fa4d6676b307d8824010277f1fdfab"
	onCrossingSpinSourceDigest        = "d19f6ee9feedb6d92d84735b541d87602d3024f2f7a1ae84526054766e624104"
	onCrossingLeakSourceDigest        = "a5393f612a8f4fa9c600faf714cca8f4da92aaaecf9c3105550562c1642b8368"
	onCrossingFnCaptureSourceDigest   = "36bf93b1fdc66dcf9f846cf6b1ba41ef9808a8ee17d763913f96d08b84424def"
	onCrossingOwnMutRefSourceDigest   = "a9774ab9c361d29be4c8e614de676eb36246525595bd17287bf8147db3636344"
	onCrossingOwnRefArraySourceDigest = "1ee3eebb1549e436ec37aad62de82188928bc0c6787ab75c405d5ce130c74f96"
)

// onCrossingRow is one t.Run leaf: the Pending rows inside fn must be exactly want, no
// diagnostic may point inside it, and a row with summary set must publish a normal,
// source-free summary for body.
type onCrossingRow struct {
	name, text, digest, body string
	fn                       originSpan
	want                     []originRefusal
	summary                  bool
	allow                    []string
}

func onCrossingRows() []onCrossingRow {
	return []onCrossingRow{
		{name: "placement_reply_finishes", text: onCrossingPlacementSource, digest: onCrossingPlacementSourceDigest, body: "score",
			fn: originSpan{118, 208, onCrossingPlacementSource[118:208]}, want: []originRefusal{}, summary: true},
		{name: "destination_call_finishes", text: onCrossingPlacementSource, digest: onCrossingPlacementSourceDigest, body: "routed",
			fn: originSpan{209, 323, onCrossingPlacementSource[209:323]}, want: []originRefusal{}, summary: true},
		{name: "statement_crossing_finishes", text: onCrossingPlacementSource, digest: onCrossingPlacementSourceDigest, body: "discard",
			fn: originSpan{324, 446, onCrossingPlacementSource[324:446]}, want: []originRefusal{}, summary: false},
		{name: "anchored_send_finishes", text: onCrossingAnchoredSource, digest: onCrossingAnchoredSourceDigest, body: "send_job",
			fn: originSpan{48, 203, onCrossingAnchoredSource[48:203]}, want: []originRefusal{}, summary: true},
		{name: "anchored_recv_finishes", text: onCrossingAnchoredSource, digest: onCrossingAnchoredSourceDigest, body: "take_one",
			fn: originSpan{204, 317, onCrossingAnchoredSource[204:317]}, want: []originRefusal{}, summary: true},
		{name: "anchored_close_finishes", text: onCrossingAnchoredSource, digest: onCrossingAnchoredSourceDigest, body: "close_remote",
			fn: originSpan{318, 450, onCrossingAnchoredSource[318:450]}, want: []originRefusal{}, summary: true},
		{name: "reference_reply_stays_refused", text: onCrossingReferenceSource, digest: onCrossingReferenceSourceDigest, body: "peek",
			fn: originSpan{0, 100, onCrossingReferenceSource[0:100]}, want: []originRefusal{{span: originSpan{37, 68, "on pool {\n        ret &n;\n    }"}, reason: onCrossingReplyRefusal}, {span: originSpan{82, 83, "r"}, reason: originOutgoingRefusal}}, summary: false},
		{name: "loan_carrier_reply_stays_refused", text: onCrossingLoanSource, digest: onCrossingLoanSourceDigest, body: "gather",
			fn: originSpan{0, 126, onCrossingLoanSource[0:126]}, want: []originRefusal{{span: originSpan{33, 94, "on pool {\n        let a: int[] = [1, 2];\n        ret a;\n    }"}, reason: onCrossingReplyRefusal}}, summary: false},
		{name: "task_reply_stays_refused", text: onCrossingTaskSource, digest: onCrossingTaskSourceDigest, body: "hand_off",
			fn: originSpan{53, 183, onCrossingTaskSource[53:183]}, want: []originRefusal{{span: originSpan{88, 151, "on pool {\n        let t = worker(5:int64);\n        ret t;\n    }"}, reason: onCrossingReplyRefusal}}, summary: false},
		{name: "anchored_loan_carrier_send_stays_refused", text: onCrossingLoanSource, digest: onCrossingLoanSourceDigest, body: "push_all",
			fn: originSpan{127, 272, onCrossingLoanSource[127:272]}, want: []originRefusal{{span: originSpan{226, 241, "ch.send(own xs)"}, reason: onCrossingAnchoredRefusal}, {span: originSpan{238, 240, "xs"}, reason: onCrossingCaptureRefusal}}, summary: false},
		{name: "anchored_loan_carrier_recv_stays_refused", text: onCrossingLoanSource, digest: onCrossingLoanSourceDigest, body: "pull",
			fn: originSpan{273, 412, onCrossingLoanSource[273:412]}, want: []originRefusal{{span: originSpan{378, 387, "ch.recv()"}, reason: onCrossingAnchoredRefusal}}, summary: false},
		{name: "body_rows_are_reported", text: onCrossingWalkSource, digest: onCrossingWalkSourceDigest, body: "body_row",
			fn: originSpan{114, 210, onCrossingWalkSource[114:210]}, want: []originRefusal{{span: originSpan{190, 199, "\"a\" + \"b\""}, reason: stringTemporaryReason}}, summary: false},
		{name: "destination_rows_are_reported", text: onCrossingWalkSource, digest: onCrossingWalkSourceDigest, body: "dest_row",
			fn: originSpan{211, 305, onCrossingWalkSource[211:305]}, want: []originRefusal{{span: originSpan{269, 278, "\"c\" + \"d\""}, reason: stringTemporaryReason}}, summary: false},
		{name: "continuation_after_a_body_that_never_finishes", text: onCrossingSpinSource, digest: onCrossingSpinSourceDigest, body: "after_spin",
			fn: originSpan{52, 161, onCrossingSpinSource[52:161]}, want: []originRefusal{{span: originSpan{148, 157, "\"e\" + \"f\""}, reason: stringTemporaryReason}}, summary: false},
		{name: "spawn_on_finishes", text: onCrossingWalkSource, digest: onCrossingWalkSourceDigest, body: "later",
			fn: originSpan{306, 396, onCrossingWalkSource[306:396]}, want: []originRefusal{}, summary: false},
		{name: "fn_value_capture_stays_refused", text: onCrossingFnCaptureSource, digest: onCrossingFnCaptureSourceDigest, body: "apply",
			fn: originSpan{47, 152, onCrossingFnCaptureSource[47:152]}, want: []originRefusal{{span: originSpan{138, 139, "f"}, reason: onCrossingCaptureRefusal}}, summary: false, allow: []string{onCrossingCallableIdent, onCrossingCallableValue, originCallRefusal, onCrossingCallableAuthority}},
	}
}

// 17 RUN: 1 parent, 16 leaves.
func TestAnalyzeOnCrossing(t *testing.T) {
	rows := onCrossingRows()
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
			f, analysis := analyzeOriginRoot(t, "on_crossing_"+row.name, row.text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, row.fn, row.want, row.allow...)
			originNoEscape(t, analysis, f.owner.File.ID, row.fn)
			if row.summary {
				requireOriginSummary(t, analysis, f.owner.File.ID, row.body, false, nil)
			}
		})
	}
}

// onCrossingOwnRefRow is one capture of `own` over a reference into an `on` body, in the two
// forms P1u-TC2's own rows do not spell (`TestTaskCheckRefusesOwnedReferenceCaptures` has `own
// &int` under `on` and `spawn on`): `own &mut int` and `own &int[]`. The capture is passed on
// whole, so nothing but the capture can refuse it (a read through it is SEM3143). The gate reads
// the type under `own` since P1u-TC2 and must refuse it alone: exactly one error, SEM3165, at the
// capture.
type onCrossingOwnRefRow struct {
	name, text, digest string
	capture            originSpan
}

func onCrossingOwnRefRows() []onCrossingOwnRefRow {
	return []onCrossingOwnRefRow{
		{name: "own_mut_ref_capture_stays_refused", text: onCrossingOwnMutRefSource, digest: onCrossingOwnMutRefSourceDigest, capture: originSpan{146, 147, "o"}},
		{name: "own_ref_array_capture_stays_refused", text: onCrossingOwnRefArraySource, digest: onCrossingOwnRefArraySourceDigest, capture: originSpan{143, 144, "o"}},
	}
}

// 3 RUN: 1 parent, 2 leaves.
func TestOnCrossingOwnReferenceCaptureStaysRefused(t *testing.T) {
	for _, row := range onCrossingOwnRefRows() {
		t.Run(row.name, func(t *testing.T) {
			checkOriginSource(t, row.text, row.digest, row.capture)
			t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
			dir := t.TempDir()
			path := filepath.Join(dir, "origin.sg")
			if err := os.WriteFile(path, []byte(row.text), 0o600); err != nil {
				t.Fatal(err)
			}
			opts := DiagnoseOptions{Stage: DiagnoseStageSema, BaseDir: dir, MaxDiagnostics: 64, IgnoreWarnings: true}
			res, err := DiagnoseWithOptions(t.Context(), path, &opts)
			var unfinished *returnOriginUnfinishedError
			if err != nil && !errors.As(err, &unfinished) {
				t.Fatalf("PRECONDITION: the program did not reach the checker's verdict: %v", err)
			}
			if unfinished != nil {
				for _, p := range unfinished.Pending {
					if !strings.HasPrefix(p.SourceKey, "core/") {
						t.Logf("ON_OWN_REF %s pending %q at %d:%d", row.name, p.Reason, p.Span.Start, p.Span.End)
					}
				}
			}
			var errs []string
			for _, d := range h2TripwireBagItems(res) {
				if d.Severity < diag.SevError {
					continue
				}
				at := ""
				if int(d.Primary.End) <= len(row.text) {
					at = row.text[d.Primary.Start:d.Primary.End]
				}
				errs = append(errs, fmt.Sprintf("%s@%d:%d:%s", d.Code.ID(), d.Primary.Start, d.Primary.End, at))
			}
			want := fmt.Sprintf("SEM3165@%d:%d:%s", row.capture.start, row.capture.end, row.capture.snippet)
			if len(errs) != 1 || errs[0] != want {
				t.Errorf("errors %q, want exactly [%q]: the capture gate must refuse `own` over a reference by itself", errs, want)
			}
		})
	}
}

// A task made in a crossing body over a body-local `let` and handed out by `ret` is refused by
// the task check at the body's `ret`, which judges the task it hands out (SEM3139), before return
// origins run (RV2-DEBT-365, probe P-ON; TC-XB, RV2-DEBT-378: the body is a frame for task borrows).
// This row is a control: no line of the crossing's origin transfer can change it.
//
// 2 RUN: 1 parent, 1 leaf.
func TestOnCrossingTaskLeakStaysWithTheTaskCheck(t *testing.T) {
	t.Run("body_local_task_reply", func(t *testing.T) {
		checkOriginSource(t, onCrossingLeakSource, onCrossingLeakSourceDigest)
		t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
		dir := t.TempDir()
		path := filepath.Join(dir, "origin.sg")
		if err := os.WriteFile(path, []byte(onCrossingLeakSource), 0o600); err != nil {
			t.Fatal(err)
		}
		opts := DiagnoseOptions{Stage: DiagnoseStageSema, BaseDir: dir, MaxDiagnostics: 64, IgnoreWarnings: true}
		res, err := DiagnoseWithOptions(t.Context(), path, &opts)
		var unfinished *returnOriginUnfinishedError
		if err != nil && !errors.As(err, &unfinished) {
			t.Fatalf("PRECONDITION: the probe did not reach the task check: %v", err)
		}
		var errs []string
		for _, d := range h2TripwireBagItems(res) {
			if d.Severity >= diag.SevError {
				errs = append(errs, d.Code.ID()+" "+d.Message)
			}
		}
		want := "SEM3139 cannot return this task: it borrows 'bl', which is freed when this body finishes while the task may still be running"
		if len(errs) != 1 || errs[0] != want {
			t.Errorf("errors %q, want exactly [%q]: the crossing body's `ret` hands out a task over its own local", errs, want)
		}
	})
}
