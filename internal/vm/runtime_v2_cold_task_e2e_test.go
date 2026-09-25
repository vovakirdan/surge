package vm_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// RV2-DEBT-370 end to end. These rows land with W4-G1's re-landing: before it, every
// non-own `.await()` keeps a return-origin row, so none of these programs builds on the D2
// line. The runtime half (RT-COLD) is pinned before that by runtime_v2_cold_task_test.go
// (native stand) and task_cold_internal_test.go / internal/asyncrt (VM).
//
// Each dropped program's worker ends the process with exit 46 the moment it runs: a dropped task
// that never runs is exit 0 with nothing printed, on both backends. Since the dropped-task rule
// (SEM3218) a task cannot be dropped where it stands, and a borrowing task dropped any other way is
// refused at the frame's exit, so the handle goes as an unused binding or through a by-value
// parameter, on a task that borrows nothing.

const coldDroppedCallSource = `async fn worker(n: int) -> int {
    rt_exit(n + 40);
    return n;
}

fn leak() -> int {
    let t = worker(6);
    return 0;
}

@entrypoint
fn main() -> int {
    let r = leak();
    let _ = checkpoint().await();
    let _ = checkpoint().await();
    return r;
}
`

const coldForwardedCallSource = `async fn worker(n: int) -> int {
    rt_exit(n + 40);
    return n;
}

fn fwd(n: int) -> Task<int> {
    return worker(n);
}

fn leak() -> int {
    let t = fwd(6);
    return 0;
}

@entrypoint
fn main() -> int {
    let r = leak();
    let _ = checkpoint().await();
    let _ = checkpoint().await();
    return r;
}
`

// The member form: the dropped task is a member of outer's scope, so this is also the row
// that a dropped member does not hold the join at the end of outer's body.
const coldDroppedMemberSource = `async fn worker(n: int) -> int {
    rt_exit(n + 40);
    return n;
}

fn sink(t: Task<int>) -> nothing {
    return nothing;
}

async fn outer() -> int {
    sink(worker(6));
    let _ = checkpoint().await();
    return 0;
}

@entrypoint
fn main() -> int {
    return compare outer().await() {
        Success(n) => n;
        Cancelled() => 41;
    };
}
`

// The control: the same task awaited runs, exactly as it did when it was published at
// creation.
const coldAwaitedControlSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let l: string = "abcdef";
    return compare worker(&l).await() {
        Success(n) => n + 40;
        Cancelled() => 41;
    };
}
`

// A chain in which each level creates its child, then a leaf, then awaits the child: the
// child is not the awaiter's most recent creation, so it is published through the queue and
// polled at its own turn, as it was when the base's inline poll took only the top of the
// local queue. 100000 levels would overflow the C stack if each await polled its child
// inline (review F1); it must answer exactly 100000 on both backends.
const coldDeepChainSource = `async fn leaf() -> int {
    return 1;
}

async fn walk(n: int) -> int {
    if n == 0 {
        return 0;
    }
    let a = walk(n - 1);
    let b = leaf();
    let x = compare a.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
    let y = compare b.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
    return x + y;
}

@entrypoint
fn main() -> int {
    return compare walk(100000).await() {
        Success(v) => v - 100000;
        Cancelled() => 41;
    };
}
`

func TestRuntimeV2ColdDeepChainRunsThroughTheQueue(t *testing.T) {
	skipTimeoutTests(t)
	res := runProgramFromSource(t, coldDeepChainSource, runOptions{captureStdout: true})
	if res.exitCode != 0 || res.stderr != "" {
		t.Fatalf("deep chain: exit %d stderr %q, want 0 and no output", res.exitCode, res.stderr)
	}
}

func TestRuntimeV2ColdDroppedCallNeverRuns(t *testing.T) {
	for _, row := range []struct {
		name, source string
		exit         int
	}{
		{"dropped_call", coldDroppedCallSource, 0},
		{"dropped_forwarded_call", coldForwardedCallSource, 0},
		{"dropped_member_in_async_body", coldDroppedMemberSource, 0},
		{"awaited_call_control", coldAwaitedControlSource, 46},
	} {
		t.Run(row.name, func(t *testing.T) {
			skipTimeoutTests(t)
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != row.exit || res.stdout != "" || res.stderr != "" {
				t.Fatalf("exit %d stdout %q stderr %q, want exit %d and no output", res.exitCode, res.stdout, res.stderr, row.exit)
			}
		})
	}
}

// RV2-DEBT-369 end to end: the program of that ledger row, verbatim. A task that borrows a
// local of an async frame and is joined in the same frame. The VM stopped with VM1999
// ("storage: type#4 is not an inline aggregate") while building the task, because its
// state-pin walk read the borrowed resident `string` as an inline aggregate (P369); the
// native backend ran it. Both backends must exit 0 with nothing printed.
const coldBorrowedLocalJoinedSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn joined() -> int {
    let l: string = "abcdef";
    let t = worker(&l);
    return compare t.await() {
        Success(n) => n;
        Cancelled() => 100;
    };
}

@entrypoint
fn main() -> int {
    return compare joined().await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
`

func TestRuntimeV2BorrowedLocalTaskJoinedInItsFrame(t *testing.T) {
	skipTimeoutTests(t)
	res := runProgramFromSource(t, coldBorrowedLocalJoinedSource, runOptions{captureStdout: true})
	if res.exitCode != 0 || res.stdout != "" || res.stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q, want exit 0 and no output", res.exitCode, res.stdout, res.stderr)
	}
}

// Native, under Valgrind: no error and no allocation per dropped call, at one call and at
// eight -- the start frame with an owned string it captured, the task and its scope
// membership are all given back. This also exercises the compiler-generated start-frame
// descriptor, which the C stand replaces with a hand-made one. The far Task leg of review F8
// is still not written. Since N-TASK-27S a far Task from `spawn on` passes return origins,
// but the leg needs the far Task moved into a cold call that is then dropped, and the
// lifecycle check refuses that move (SEM3107: the handle is neither awaited nor returned).
// rt_far_task_release_owned in cold_discard is therefore covered only by reading.
func TestRuntimeV2ColdDroppedCallValgrindZero(t *testing.T) {
	if testBackend(t) != backendLLVM {
		t.Skip("a native row: SURGE_BACKEND=llvm")
	}
	skipTimeoutTests(t)
	if _, err := exec.LookPath("valgrind"); err != nil {
		t.Skip("valgrind not installed")
	}
	low := coldDroppedCallOutstanding(t, 1)
	high := coldDroppedCallOutstanding(t, 8)
	if high-low >= 2 {
		t.Errorf("a dropped call leaks: %d outstanding at 1 call, %d at 8", low, high)
	}
}

func coldDroppedCallOutstanding(t *testing.T, calls int) int {
	t.Helper()
	source := strings.Replace(`async fn worker2(owned: string) -> int {
    rt_exit(len(owned) to int + 40);
    return len(owned) to int;
}

fn leak() -> int {
    let l: string = "abcdef";
    let owned: string = "q" + l;
    let t = worker2(owned);
    return 0;
}

@entrypoint
fn main() -> int {
    let mut i: int = 0;
    while i < CALLS {
        let _ = leak();
        i = i + 1;
    }
    let _ = checkpoint().await();
    return 0;
}
`, "CALLS", strconv.Itoa(calls), 1)
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	src := artifactSourcePath(artifacts)
	if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if res := runProgram(t, root, src, runOptions{}, artifacts); res.exitCode != 0 {
		t.Fatalf("%d dropped calls: exit %d stderr %q, want 0", calls, res.exitCode, res.stderr)
	}
	cmd := exec.Command("valgrind", "--error-exitcode=99", "--leak-check=full",
		"--errors-for-leak-kinds=definite,indirect", llvmOutputPath(root, src))
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 || !strings.Contains(stderr, "ERROR SUMMARY: 0 errors") {
		t.Fatalf("valgrind at %d dropped calls (code=%d)\nstdout:\n%s\nstderr:\n%s", calls, code, stdout, stderr)
	}
	outstanding, err := parseValgrindOutstandingAllocations(stderr)
	if err != nil {
		t.Fatalf("parse valgrind allocation ledger: %v\nstderr:\n%s", err, stderr)
	}
	return outstanding
}

// RV2-DEBT-365/370 tripwire, runtime line: the dropped-call form. The task check exempts a
// Task-valued call whose value is dropped where it stands (TC-1d) because the runtime
// creates that task cold and ends it unrun when the value goes. The program is the leaking
// one itself, not a twin: if its task ran it would exit 46 or 40, or stop VM3301.
func TestH2TripwireDroppedCallNeverRuns(t *testing.T) {
	checkH2TripwireBarrier(t, "dropped_call", coldDroppedCallSource, coldDroppedCallDigest(), h2DroppedCallExit)
}

var h2DroppedCallExit = h2TripwireBarrier{exit: 0}

func coldDroppedCallDigest() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(coldDroppedCallSource)))
}

// The dropped-call matcher refuses every recorded outcome of a task that ran, and a
// compile-time refusal.
func TestH2TripwireDroppedCallMatcher(t *testing.T) {
	for name, row := range map[string]struct {
		backend string
		out     h2TripwireOutcome
	}{
		"vm_use_after_free":   {backendVM, h2TripwireOutcome{vmError: `VM3301: use-after-free: local "l" used after drop`, exit: 1}},
		"vm_task_ran":         {backendVM, h2TripwireOutcome{exit: 46}},
		"native_task_ran":     {backendLLVM, h2TripwireOutcome{exit: 46}},
		"native_silent_wrong": {backendLLVM, h2TripwireOutcome{exit: 40}},
		"refused_by_origins":  {backendLLVM, h2TripwireOutcome{refused: "return-origin analysis unfinished", buildFailed: true}},
	} {
		t.Run(name, func(t *testing.T) {
			if h2DroppedCallExit.holds(row.backend, row.out) == "" {
				t.Errorf("the dropped-call row accepted %+v", row.out)
			}
		})
	}
	for _, backend := range []string{backendVM, backendLLVM} {
		if verdict := h2DroppedCallExit.holds(backend, h2TripwireOutcome{}); verdict != "" {
			t.Errorf("the dropped-call row refused a clean exit 0 on %s: %s", backend, verdict)
		}
	}
}
