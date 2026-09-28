package driver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// What the task-payload rule (SEM3223, owner ruling 2026-09-26) took out of the return-origin
// rosters, and what it left there. Seven rows were retired because their programs make a task
// whose result is a task; they were R-i's tripwire (RV2-DEBT-365): NoBorrowedState, the fence on
// a Task handed out of a join, is unsupported for a Task. A fence must not disappear silently, so
// each retired program is kept byte for byte (its old digest) and pinned twice:
//   - first line: the program is refused by SEM3223, at exactly the named spans, and by no
//     other error besides the ones named;
//   - second line: the return-origin analysis, run on the same program with SEM3223 tolerated in
//     the root file only, still leaves the retired row's exact Pending rows. If Task became a
//     counted handle (Wave D4b), whose payload NoBorrowedState would then walk, these go red even
//     though SEM3223 still holds.
// The rule does not reach a task held behind a channel: `Task<Channel<Task<int>>>` is a legal
// type, so R-i keeps a legal form and NoBorrowedState keeps its first-line rows there
// (TestTaskPayloadFenceThroughAChannel), and so does a Task<T> made in a generic body whose T is a task
// (TestTaskPayloadFenceInAGenericBody). The joined-clone drain, retired with its `async fn`
// result, finishes in its legal form, the clone handed on (TestTaskPayloadRetiredDrainHandedOn).

type taskPayloadRetiredRow struct {
	name, text, digest string
	codes              string   // every error code of the program, sorted and comma-joined
	sem3223            []string // the text of every SEM3223 primary span, in order
	fn                 originSpan
	want               []originRefusal // the retired row's Pending rows inside fn, exactly
	// legalFormOnly names the legal-form row that carries the fence when the analysis cannot
	// run on the refused program itself.
	legalFormOnly string
}

// taskPayloadNoBorrowedState is the row NoBorrowedState leaves where it meets a Task.
const taskPayloadNoBorrowedState = "opaque result borrowed-state classification is unsupported"

const taskPayloadRetiredAwaitSource = `async fn read_ref(t: Task<&int>) -> int {
    let _ = t.await();
    return 0;
}

async fn read_array(t: Task<int[]>) -> int {
    let _ = t.await();
    return 0;
}

async fn read_task(t: Task<Task<int>>) -> int {
    let _ = t.await();
    return 0;
}
`

const taskPayloadRetiredNestedAwaitSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn plain(n: int) -> int {
    return n;
}

async fn outer(x: &string) -> Task<int> {
    return worker(x);
}

async fn leak() -> Task<int> {
    let l: string = "abcdef";
    return compare outer(&l).await() {
        Success(inner) => inner;
        Cancelled() => plain(0);
    };
}
`

const taskPayloadRetiredBlockSource = `fn peek(n: int) -> int {
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

const taskPayloadRetiredSpawnSource = `fn spawn_nested() -> Task<Task<int>> {
    return spawn async {
        let inner: Task<int> = async { ret 7; };
        ret inner;
    };
}
`

const taskPayloadRetiredSpawnOnSource = `async fn plain(x: int) -> int {
    return x;
}

fn start() -> far Task<Task<int>> {
    return spawn on pool {
        ret plain(1);
    };
}
`

const taskPayloadRetiredCanarySource = `async fn read_task(t: Task<Task<int>>) -> int {
    let _ = t.await();
    return 0;
}
`

const taskPayloadRetiredDrainSource = `async fn worker(x: &int64) -> int64 {
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
`

func taskPayloadRetiredRows() []taskPayloadRetiredRow {
	nestedBlock := "async {\n        let inner: Task<int> = async { ret 7; };\n        ret inner;\n    }"
	return []taskPayloadRetiredRow{
		// TestAnalyzeTaskAwaits/task_payload_stays_refused
		{name: "await_task_payload", text: taskPayloadRetiredAwaitSource, digest: "b637c8f95aa3340047a6b401257d903ca06fb12f79a134f39739c2e3cc671c59",
			codes: "SEM3223", sem3223: []string{"Task<Task<int>>"},
			fn: originSpan{167, 253, taskPayloadRetiredAwaitSource[167:253]}, want: []originRefusal{{span: originSpan{227, 236, "t.await()"}, reason: originTaskAwaitUnsupported}, {span: originSpan{227, 236, "t.await()"}, reason: originCalleeSourceRefusal}}},
		// TestAnalyzeTaskAwaits/borrowing_task_payload_stays_refused, R-i's own program
		{name: "await_borrowing_task_payload", text: taskPayloadRetiredNestedAwaitSource, digest: "6beb8a63984df9be83215d3b10b8698a6e9b3c8bf455be4105b7be29c9541f19",
			codes: "SEM3223", sem3223: []string{"-> Task<int>", "-> Task<int>"},
			fn: originSpan{182, 356, taskPayloadRetiredNestedAwaitSource[182:356]}, want: []originRefusal{{span: originSpan{262, 279, "outer(&l).await()"}, reason: originTaskAwaitUnsupported}, {span: originSpan{262, 279, "outer(&l).await()"}, reason: originCalleeSourceRefusal}}},
		// TestAnalyzeTaskBlocks/task_payload_stays_refused
		{name: "block_task_payload", text: taskPayloadRetiredBlockSource, digest: "75a5d3011831d2e43801490139dd27f822438e097ea22f5b4ae06cb614071cd4",
			codes: "SEM3223", sem3223: []string{"Task<Task<int>>", nestedBlock},
			fn: originSpan{226, 354, taskPayloadRetiredBlockSource[226:354]}, want: []originRefusal{{span: originSpan{270, 351, nestedBlock}, reason: originTaskBlockPayloadRefusal}, {span: originSpan{263, 352, "return " + nestedBlock + ";"}, reason: originOutgoingRefusal}, {span: originSpan{238, 256, "-> Task<Task<int>>"}, reason: originResultRefusal}}},
		// TestAnalyzeSpawn/spawn_task_payload_keeps_its_derived_rows
		{name: "spawn_task_payload", text: taskPayloadRetiredSpawnSource, digest: "08ead9d94e4ed8ca4c83da89b9491818fbe915d8679700b9791d0887eff13276",
			codes: "SEM3223", sem3223: []string{"Task<Task<int>>", nestedBlock},
			fn: originSpan{0, 140, taskPayloadRetiredSpawnSource[0:140]}, want: []originRefusal{{span: originSpan{56, 137, nestedBlock}, reason: originTaskBlockPayloadRefusal}, {span: originSpan{43, 138, "return spawn " + nestedBlock + ";"}, reason: originOutgoingRefusal}, {span: originSpan{18, 36, "-> Task<Task<int>>"}, reason: originResultRefusal}}},
		// TestAnalyzeSpawnOn/task_payload_refused
		{name: "spawn_on_task_payload", text: taskPayloadRetiredSpawnOnSource, digest: "d74f2b5f3bc22bf2fb7556eb16ee13a0dec5427ed82358ee50bb07e06a0f74aa",
			codes: "SEM3223", sem3223: []string{"Task<Task<int>>", "spawn on pool {\n        ret plain(1);\n    }"},
			fn: originSpan{49, 142, taskPayloadRetiredSpawnOnSource[49:142]}, want: []originRefusal{{span: originSpan{96, 139, "spawn on pool {\n        ret plain(1);\n    }"}, reason: spawnOnPayloadRefusal}, {span: originSpan{89, 140, "return spawn on pool {\n        ret plain(1);\n    };"}, reason: originOutgoingRefusal}, {span: originSpan{60, 82, "-> far Task<Task<int>>"}, reason: originResultRefusal}},
			// The checker records no crossing for a `spawn on` it refused (the analysis stops with "crossing
			// ... has no checker record"), so this fence is read in its legal form.
			legalFormOnly: "TestTaskPayloadFenceThroughAChannel/spawn_on_channel_of_tasks"},
		// TestReturnOriginHandleDefaultCanariesKeepTheirRows/task_payload_await_reaches_r_i
		{name: "canary_task_payload_await", text: taskPayloadRetiredCanarySource, digest: "852847d285ebaf8548c59371898a104d02f693bc6cb6447e6aa81d4aa9cc9a94",
			codes: "SEM3223", sem3223: []string{"Task<Task<int>>"},
			fn: originSpan{0, len(taskPayloadRetiredCanarySource) - 1, taskPayloadRetiredCanarySource[:len(taskPayloadRetiredCanarySource)-1]}, want: []originRefusal{{span: originSpan{60, 69, "t.await()"}, reason: handleDefaultUnsupported}, {span: originSpan{60, 69, "t.await()"}, reason: originCalleeSourceRefusal}}},
		// TestReturnOriginHandleDefaultsFinish/joined_clone_drain: it finished, so its retired row is no Pending at all
		{name: "joined_clone_drain", text: taskPayloadRetiredDrainSource, digest: "c0ec17f1c547f8cae8274746ab9cfb95160c47991a80e770d629e655bfce2985",
			codes: "SEM3223", sem3223: []string{"-> Task<int64>"},
			fn: originSpan{0, len(taskPayloadRetiredDrainSource) - 1, taskPayloadRetiredDrainSource[:len(taskPayloadRetiredDrainSource)-1]}, want: []originRefusal{}},
	}
}

// First line: each retired program is refused by SEM3223 at exactly its named spans.
func TestTaskPayloadIsTaskRetiredPrograms(t *testing.T) {
	for _, row := range taskPayloadRetiredRows() {
		t.Run(row.name, func(t *testing.T) {
			codes, errs := taskCheckErrorCodes(t, taskCheckProbe{name: row.name, digest: row.digest, text: row.text})
			var got []string
			for _, d := range errs {
				if d.Code == diag.SemaTaskPayloadIsTask {
					got = append(got, row.text[d.Primary.Start:d.Primary.End])
				}
			}
			if codes != row.codes || !slices.Equal(got, row.sem3223) {
				t.Fatalf("errors %q with SEM3223 at %q, want %q at exactly %q", codes, got, row.codes, row.sem3223)
			}
		})
	}
}

// Second line: under SEM3223 the analysis still leaves each retired row's Pending rows.
func TestTaskPayloadIsTaskRetiredFenceRows(t *testing.T) {
	for _, row := range taskPayloadRetiredRows() {
		if row.legalFormOnly != "" {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			spans := []originSpan{row.fn}
			for _, want := range row.want {
				spans = append(spans, want.span)
			}
			checkOriginSource(t, row.text, row.digest, spans...)
			f, analysis := analyzeOriginRootUnderTaskPayloadRule(t, "task_payload_retired_"+row.name, row.text)
			originExactPending(t, analysis, f.unit.SourceKey, row.fn, row.want)
			originNoEscape(t, analysis, f.owner.File.ID, row.fn)
		})
	}
}

// R-i's legal form: a task behind a channel in an awaited, block or `spawn on` payload. SEM3223
// does not walk a channel's payload, so these programs build as far as the checker is concerned,
// and NoBorrowedState (or the payload row) is their first and only fence.
func TestTaskPayloadFenceThroughAChannel(t *testing.T) {
	for _, row := range []struct {
		name, text string
		fn         string
		want       []struct{ snippet, reason string }
	}{
		{name: "await_channel_of_tasks", fn: "read_channel", want: []struct{ snippet, reason string }{{"t.await()", taskPayloadNoBorrowedState}, {"t.await()", originCalleeSourceRefusal}},
			text: "async fn read_channel(t: Task<Channel<Task<int>>>) -> int {\n    let _ = t.await();\n    return 0;\n}\n"},
		{name: "block_channel_of_tasks", fn: "relay", want: []struct{ snippet, reason string }{{"async {\n        let ch = Channel::<Task<int>>::new(1:uint);\n        ret ch;\n    }", originTaskBlockPayloadRefusal}, {"Channel::<Task<int>>::new(1:uint)", taskPayloadNoBorrowedState}, {"Channel::<Task<int>>::new(1:uint)", originCalleeSourceRefusal}},
			text: "fn relay() -> int {\n    let t = async {\n        let ch = Channel::<Task<int>>::new(1:uint);\n        ret ch;\n    };\n    let _ = t;\n    return 0;\n}\n"},
		{name: "spawn_channel_of_tasks", fn: "spawn_relay", want: []struct{ snippet, reason string }{{"-> Task<Channel<Task<int>>>", originResultRefusal}, {"return spawn async {\n        let ch = Channel::<Task<int>>::new(1:uint);\n        ret ch;\n    };", originOutgoingRefusal}, {"async {\n        let ch = Channel::<Task<int>>::new(1:uint);\n        ret ch;\n    }", originTaskBlockPayloadRefusal}, {"Channel::<Task<int>>::new(1:uint)", taskPayloadNoBorrowedState}, {"Channel::<Task<int>>::new(1:uint)", originCalleeSourceRefusal}},
			text: "fn spawn_relay() -> Task<Channel<Task<int>>> {\n    return spawn async {\n        let ch = Channel::<Task<int>>::new(1:uint);\n        ret ch;\n    };\n}\n"},
		{name: "spawn_on_channel_of_tasks", fn: "start", want: []struct{ snippet, reason string }{{"-> far Task<Channel<Task<int>>>", originResultRefusal}, {"return spawn on pool {\n        let ch = Channel::<Task<int>>::new(1:uint);\n        ret ch;\n    };", originOutgoingRefusal}, {"spawn on pool {\n        let ch = Channel::<Task<int>>::new(1:uint);\n        ret ch;\n    }", spawnOnPayloadRefusal}, {"Channel::<Task<int>>::new(1:uint)", taskPayloadNoBorrowedState}, {"Channel::<Task<int>>::new(1:uint)", originCalleeSourceRefusal}},
			text: "fn start() -> far Task<Channel<Task<int>>> {\n    return spawn on pool {\n        let ch = Channel::<Task<int>>::new(1:uint);\n        ret ch;\n    };\n}\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			start, end := taskNarrowFunction(t, row.text, row.fn)
			fn := originSpan{start, end, row.text[start:end]}
			var want []originRefusal
			for _, w := range row.want {
				want = append(want, originRefusal{span: handleDefaultAt(t, row.text, w.snippet), reason: w.reason})
			}
			if _, errs := taskCheckErrorCodes(t, taskCheckProbe{name: row.name, digest: sha256Hex(row.text), text: row.text}); len(errs) != 0 {
				t.Fatalf("PRECONDITION: the legal form has errors %+v", errs)
			}
			f, analysis := analyzeOriginRoot(t, "task_payload_channel_"+row.name, row.text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, fn, want)
			originNoEscape(t, analysis, f.owner.File.ID, fn)
		})
	}
}

// R-i's other remaining form: a Task<T> made inside a generic body whose T is instantiated with a
// task. SEM3223 does not reach it -- the declaration sees only T, and the call's result holds no
// task -- so the checker accepts the program, and return origins keep it unfinished: the block's
// capture and payload rows inside the generic body, and NoBorrowedState at the instantiating call.
func TestTaskPayloadFenceInAGenericBody(t *testing.T) {
	text := `async fn relay<T>(x: T) -> int {
    let t = async { ret x; };
    let r = t.await();
    return 0;
}

async fn work() -> int {
    return 1;
}

async fn use_relay() -> int {
    return compare relay(work()).await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}
`
	if _, errs := taskCheckErrorCodes(t, taskCheckProbe{name: "generic_body", digest: sha256Hex(text), text: text}); len(errs) != 0 {
		t.Fatalf("PRECONDITION: the generic-body form has checker errors %+v", errs)
	}
	f, analysis := analyzeOriginRoot(t, "task_payload_generic_body", text, false, nil)
	for _, want := range []struct{ snippet, reason string }{
		{"async { ret x; }", originTaskBlockCaptureRefusal},
		{"async { ret x; }", originTaskBlockPayloadRefusal},
		{"relay(work())", taskPayloadNoBorrowedState},
	} {
		if span := handleDefaultAt(t, text, want.snippet); !originPendingAt(analysis, f.unit.SourceKey, span, want.reason) {
			t.Errorf("missing %q at %q: %+v", want.reason, want.snippet, originPendingWithin(analysis, f.unit.SourceKey, 0, len(text)))
		}
	}
}

// The two select-head rows SEM3223 retired from TestSelectRaceRuntime (internal/vm, S-RI-SELECT):
// leak2 returns a task from an async fn, which a select head awaited without delivering it.
// Each program is kept byte for byte (it ran, exit 46, at the base) and is refused by SEM3223 at
// leak2's result. The property had no legal form left: handing the same borrowing task out
// through a channel instead is refused by the task check at leak2's return (SEM3021).
const taskPayloadRetiredSelectSource = `async fn worker(x: &string) -> int {
    checkpoint().await();
    checkpoint().await();
    print("worker read " + x);
    return len(x) to int;
}

async fn leak2(x: &string) -> Task<int> {
    let t = worker(x);
    return t;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = select { leak2(&bl).await() => 46; };
        ret v;
    }).await();
    print("after");
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

const taskPayloadRetiredSelectSpawnSource = `async fn worker(x: &string) -> int {
    checkpoint().await();
    checkpoint().await();
    print("worker read " + x);
    return len(x) to int;
}

async fn leak2(x: &string) -> Task<int> {
    let t = spawn worker(x);
    return t;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = select { leak2(&bl).await() => 46; };
        ret v;
    }).await();
    print("after");
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

const taskPayloadSelectChannelSource = `async fn worker(x: &string) -> int {
    checkpoint().await();
    checkpoint().await();
    print("worker read " + x);
    return len(x) to int;
}

async fn leak2(x: &string) -> Channel<Task<int>> {
    let ch = Channel::<Task<int>>::new(1:uint);
    let t = worker(x);
    ch.send(own t);
    return ch;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = select { leak2(&bl).await() => 46; };
        ret v;
    }).await();
    print("after");
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

const taskPayloadSelectChannelSpawnSource = `async fn worker(x: &string) -> int {
    checkpoint().await();
    checkpoint().await();
    print("worker read " + x);
    return len(x) to int;
}

async fn leak2(x: &string) -> Channel<Task<int>> {
    let ch = Channel::<Task<int>>::new(1:uint);
    let t = spawn worker(x);
    ch.send(own t);
    return ch;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = select { leak2(&bl).await() => 46; };
        ret v;
    }).await();
    print("after");
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

func TestTaskPayloadIsTaskRetiredSelectPrograms(t *testing.T) {
	for _, row := range []struct{ name, text, digest, codes string }{
		{"ri_select_head_over_a_borrowing_payload", taskPayloadRetiredSelectSource, "eea32cfbed434092751539fcc87902d21fe2f0a9603a9628e32fa6ad29f4301e", "SEM3223"},
		{"ri_select_head_over_a_spawned_borrowing_payload", taskPayloadRetiredSelectSpawnSource, "69cdb7565b9a18952ef75cf5c00c73cb86482925350339be9f487724b0dd48ac", "SEM3223"},
		{"channel_form_refused_by_the_task_check", taskPayloadSelectChannelSource, "e7682ec19d69fa72b8a472ecab431cae8aee618bb41a7513c6959600e6e73313", "SEM3021"},
		{"spawned_channel_form_refused_by_the_task_check", taskPayloadSelectChannelSpawnSource, "4c94c16ffc34075dd7db4d343309593f571478590863adb9fcc0452c6977b854", "SEM3021"},
	} {
		t.Run(row.name, func(t *testing.T) {
			codes, errs := taskCheckErrorCodes(t, taskCheckProbe{name: row.name, digest: row.digest, text: row.text})
			var at []string
			for _, d := range errs {
				at = append(at, row.text[d.Primary.Start:d.Primary.End])
			}
			want := []string{"-> Task<int>"}
			if row.codes == "SEM3021" {
				want = []string{"x"} // the borrow the task still holds at leak2's return
			}
			if codes != row.codes || !slices.Equal(at, want) {
				t.Fatalf("errors %q at %q, want %q at exactly %q", codes, at, row.codes, want)
			}
		})
	}
}

// The rule concerns a local Task<T> only (owner ruling 2026-09-26): a `far Task<T>`, the handle
// `spawn on` returns, in a task's result is allowed -- as an `async fn` result and as a payload.
func TestTaskPayloadIsTaskAllowsAFarTask(t *testing.T) {
	for _, row := range []struct{ name, text string }{
		{"async_fn_returns_a_far_task", "async fn start(dst: Placement) -> far Task<int> {\n    return spawn on dst {\n        ret 7;\n    };\n}\n"},
		{"task_of_a_far_task", "fn keep(t: Task<far Task<int>>) -> int {\n    return 0;\n}\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if _, errs := taskCheckErrorCodes(t, taskCheckProbe{name: row.name, digest: sha256Hex(row.text), text: row.text}); len(errs) != 0 {
				t.Fatalf("a far task in a task's result is refused: %+v", errs)
			}
		})
	}
}

// The joined-clone drain in its legal form (the golden task_clone_borrow_joined_then_handed_on):
// the clone is handed on after the drain joined its original, and the program finishes.
func TestTaskPayloadRetiredDrainHandedOn(t *testing.T) {
	text := `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn hand_off(t: Task<int64>) -> nothing {
    return nothing;
}

async fn drained_then_clone_handed_on() -> int64 {
    let l: int64 = 5;
    let t = spawn worker(&l);
    let c = t.clone();
    let mut tasks: Task<int64>[] = [];
    tasks.push(t);
    while tasks.__len() > 0:uint {
        let x = tasks.pop().safe();
        let _ = x.await();
    }
    hand_off(c);
    return 0;
}
`
	if _, errs := taskCheckErrorCodes(t, taskCheckProbe{name: "drain_handed_on", digest: sha256Hex(text), text: text}); len(errs) != 0 {
		t.Fatalf("the legal drain has errors %+v", errs)
	}
	f, analysis := analyzeOriginRoot(t, "task_payload_drain_handed_on", text, false, nil)
	for _, pending := range analysis.Pending {
		t.Errorf("unexpected pending %q at %s %v", pending.Reason, pending.SourceKey, pending.Span)
	}
	originNoEscape(t, analysis, f.owner.File.ID, originSpan{0, len(text), "drained_then_clone_handed_on"})
}

func sha256Hex(text string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
}

// analyzeOriginRootUnderTaskPayloadRule is analyzeOriginRoot over a program whose only errors are
// SEM3223 in the root file (at least one): the fixture, the closure and the analysis are the same.
func analyzeOriginRootUnderTaskPayloadRule(t *testing.T, stage, text string) (originalGenericFixture, *sema.ReturnOriginAnalysis) {
	t.Helper()
	return analyzeOriginRootUnderRule(t, stage, text, diag.SemaTaskPayloadIsTask)
}

// analyzeOriginRootUnderRule is analyzeOriginRootUnderTaskPayloadRule for the one refusing rule named.
func analyzeOriginRootUnderRule(t *testing.T, stage, text string, rule diag.Code) (originalGenericFixture, *sema.ReturnOriginAnalysis) {
	t.Helper()
	stdlib := detectStdlibRootFrom(".")
	if stdlib == "" {
		t.Fatal("PRECONDITION: real stdlib unavailable")
	}
	t.Setenv("SURGE_STDLIB", stdlib)
	root := t.TempDir()
	path := filepath.Join(root, "origin.sg")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	files := source.NewFileSetWithBase(root)
	id, err := files.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	file, bag := files.Get(id), diag.NewBag(64)
	strs, interner := source.NewInterner(), types.NewInterner()
	opts := &DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64, KeepArtifacts: true}
	if mapErr := ensureModuleMapping(opts, root); mapErr != nil {
		t.Fatal(mapErr)
	}
	diagnoseTokenize(file, bag)
	builder, fileID := diagnoseParseWithStrings(t.Context(), files, file, bag, strs, parser.DirectiveModeOff)
	exports, rec, records, err := runModuleGraph(t.Context(), files, file, builder, fileID, bag, opts, NewModuleCache(256), interner, strs)
	if err != nil || rec == nil {
		t.Fatalf("PRECONDITION: real stdlib module graph: %v", err)
	}
	resolveModuleRecord(t.Context(), rec, root, exports, interner, opts, nil)
	resolved, ok := rec.Symbols[fileID]
	res := &DiagnoseResult{FileSet: files, File: file, FileID: fileID, Builder: rec.Builder, Bag: rec.Bag,
		Symbols: &resolved, Sema: rec.Sema[fileID], rootRecord: rec, moduleRecords: records}
	if !ok || res.Sema == nil || res.Sema.TypeInterner == nil || len(res.Sema.ExprTypes) == 0 {
		t.Fatal("PRECONDITION: real stdlib source did not retain original typed artifacts")
	}
	requireOnlyRule(t, res, rule)
	if closeErr := FinalizeInstantiationClosure(t.Context(), res, 64); closeErr != nil {
		t.Fatalf("PRECONDITION: source closure failed: %v", closeErr)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil || len(inputs.units) != 11 {
		t.Fatalf("PRECONDITION: full eleven-unit input missing: units=%d error=%v", len(inputs.units), err)
	}
	requireOnlyRule(t, res, rule)
	f := originalGenericFixture{owner: res, authority: res.Sema, inputs: inputs}
	owners := 0
	for _, unit := range inputs.units {
		if unit.Builder.Files.Get(unit.FileID).Span.File == res.File.ID {
			f.unit, owners = unit, owners+1
		}
	}
	if owners != 1 {
		t.Fatalf("PRECONDITION: the test source has %d owning units", owners)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": stage, "source_key": f.unit.SourceKey,
		"pending": originPendingWithin(analysis, f.unit.SourceKey, 0, len(text)), "diagnostics": analysis.Diagnostics})
	return f, analysis
}

// requireOnlyRule: every error of every original bag is the rule's in the root file, and there is one.
func requireOnlyRule(t *testing.T, res *DiagnoseResult, code diag.Code) {
	t.Helper()
	bags := []*diag.Bag{res.Bag}
	for _, rec := range res.moduleRecords {
		if rec != nil {
			bags = append(bags, rec.Bag)
		}
	}
	rule := 0
	for _, bag := range bags {
		if bag == nil {
			t.Fatal("PRECONDITION: original module bag is missing")
		}
		for _, d := range bag.Items() {
			if d == nil || d.Severity < diag.SevError {
				continue
			}
			if d.Code != code || d.Primary.File != res.File.ID {
				t.Fatalf("PRECONDITION: the retired program has an error besides %s in its own file: %+v", code.ID(), *d)
			}
			rule++
		}
	}
	if rule == 0 {
		t.Fatalf("the retired program is no longer refused by %s: the first line this row stands behind is gone", code.ID())
	}
}
