//go:build !golden

package vm_test

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Epic 20 Task 8 row 2: the strict-zero census over the three Epics 16-18
// crossing verticals — share() fan-out, remote select, and migration
// (spawn-on + immediate-on with a heap-bearing capture) — now that
// RV2-DEBT-052 is closed (per-arm compare scrutinee release) and DEBT-040
// is closed (loop-based iteration reclaims). Each vertical's window
// function creates its long-lived runtime handles (channels) ONCE, outside
// the measured HeapStats window, and only the repeated crossing operation
// itself sits inside the window — a channel object has no drop glue
// (RV2-DEBT-048's residual note) and recreating one per iteration would
// make the census confound the vertical under test with that unrelated,
// already-ledgered gap.
//
// Only vertical (c) carries a heap-bearing (runtime-built string field)
// capture: an owned `string` captured directly into an anchored `on`
// block or ferried back as a bare return payload is rejected by sema
// (ON-CAP-N005, "this owned value is not shard-movable") unless the
// crossing type carries `@shard_movable` — verticals (a) and (b) stay on
// plain `int` payloads for that reason, matching the working Epic 16/17
// e2e sources exactly, just wrapped in a counting loop. Every TaskResult /
// union scrutinee is consumed directly by a moving compare arm
// (`Success(x) => x`), never bound and locally discarded (RV2-DEBT-058)
// and never routed through an explicitly-typed intermediate `let` for a
// tag constructor result (RV2-DEBT-057).

// runtimeV2CrossingStrictCensusVerticals holds the three vertical window
// functions shared by both programs below: the shard-1-only pinned census
// (runtimeV2CrossingStrictCensusSource) and the shard-agnostic valgrind-tier
// program (runtimeV2CrossingStrictCensusValgrindSource). The window
// functions' OWN internal correctness checks (acc == expected sum) are
// shard-count-safe application invariants; only the OUTER n=1-vs-n=8
// HeapStats comparisons are shard-1-specific (multi-shard windows race
// transport frees against the snapshot, per the heap-capture census's own
// comment), which is why they live in a separate program instead of a
// shared run().
const runtimeV2CrossingStrictCensusVerticals = `
@shard_movable
type Job = { id: int, note: string };

fn migrate_note(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "m";
        i = i + 1;
    }
    return s;
}

fn describe_job(j: own Job) -> int {
    if j.note != "n-mmmm" { return 0 - 1; }
    return j.id * 100 + 6;
}

// --- Vertical (a): share() fan-out over a far channel (Epic 16 shape) ---
//
// Payloads stay plain int (Copy) deliberately: an owned string capture
// into an anchored "on ch {...}" block is rejected by sema
// (ON-CAP-N005, "this owned value is not shard-movable") unless wrapped in
// an @shard_movable type -- the on-ch e2e's own comment records the same
// gate ("the reply payload must stay plain-copy data ... until unions get
// a by-value wire representation"). Heap-bearing capture is vertical (c)'s
// job, through the @shard_movable Job struct.

async fn share_round(ch: far Channel<int>, value: int) -> int {
    let sent: TaskResult<nothing> = on ch { ch.send(value); ret nothing; };
    return compare sent { Success(_) => 0; Cancelled() => 1; };
}

async fn share_drain(ch: far Channel<int>) -> int {
    let got: TaskResult<int> = on ch {
        let v: Option<int> = ch.recv();
        ret compare v { Some(x) => x; nothing => 0 - 1; };
    };
    return compare got { Success(x) => x; Cancelled() => 0 - 2; };
}

async fn share_window(n: int) -> uint {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 1);
    let c0: HeapStats = rt_heap_stats();
    let mut k = 0;
    let mut acc = 0;
    while k < n {
        let first: Task<int> = spawn share_round(ch.share(), 41);
        let second: Task<int> = spawn share_round(ch.share(), 42);
        let a: int = compare share_drain(ch.share()).await() { Success(x) => x; Cancelled() => 0 - 3; };
        let b: int = compare share_drain(ch.share()).await() { Success(x) => x; Cancelled() => 0 - 4; };
        let p1: int = compare first.await() { Success(x) => x; Cancelled() => 1; };
        let p2: int = compare second.await() { Success(x) => x; Cancelled() => 1; };
        if p1 != 0 { return 999995; }
        if p2 != 0 { return 999994; }
        acc = acc + a + b;
        k = k + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc != 83 * n { return 999993; }
    let closed: TaskResult<nothing> = on ch { ch.close(); ret nothing; };
    let _ = closed;
    return (c1.alloc_count - c0.alloc_count) - (c1.free_count - c0.free_count);
}

// --- Vertical (b): remote select over far channels (Epic 17 shape) ---

async fn feed(ch: far Channel<int>, value: int) -> int {
    let sent: TaskResult<nothing> = on ch { ch.send(value); ret nothing; };
    return compare sent { Success(_) => 0; Cancelled() => 1; };
}

async fn pick(a: far Channel<int>, b: far Channel<int>) -> int {
    let w: int = select { a.recv() => 10; b.recv() => 20; };
    return w;
}

async fn select_window(n: int) -> uint {
    let a: far Channel<int> = channel_on::<int>(shard(0:ShardId), 2);
    let b: far Channel<int> = channel_on::<int>(shard(0:ShardId), 2);
    let c0: HeapStats = rt_heap_stats();
    let mut k = 0;
    let mut acc = 0;
    while k < n {
        let seeded: TaskResult<nothing> = on b { b.send(7); ret nothing; };
        let _ = seeded;
        let first: int = compare pick(a.share(), b.share()).await() { Success(x) => x; Cancelled() => 0 - 1; };
        if first != 20 { return 999999; }
        let feeder: Task<int> = spawn feed(a.share(), 41);
        let second: int = compare pick(a.share(), b.share()).await() { Success(x) => x; Cancelled() => 0 - 2; };
        if second != 10 { return 999998; }
        let fres: int = compare feeder.await() { Success(x) => x; Cancelled() => 1; };
        if fres != 0 { return 999997; }
        acc = acc + first + second;
        k = k + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc != 30 * n { return 999996; }
    let closed_a: TaskResult<nothing> = on a { a.close(); ret nothing; };
    let closed_b: TaskResult<nothing> = on b { b.close(); ret nothing; };
    let _ = closed_a;
    let _ = closed_b;
    return (c1.alloc_count - c0.alloc_count) - (c1.free_count - c0.free_count);
}

// --- Vertical (c): migration, spawn-on AND immediate-on, heap capture (Epic 18 shape) ---

async fn migration_window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut k = 0;
    let mut acc = 0;
    while k < n {
        let j1: own Job = own Job{ id: 4, note: migrate_note("n-") };
        let t: far Task<int> = spawn on distributed { ret describe_job(own j1); };
        let r1: int = compare t.await() { Success(x) => x; Cancelled() => 0 - 5; };
        if r1 != 406 { return 999992; }
        let j2: own Job = own Job{ id: 7, note: migrate_note("n-") };
        let got: TaskResult<int> = on distributed { ret describe_job(own j2); };
        let r2: int = compare got { Success(x) => x; Cancelled() => 0 - 6; };
        if r2 != 706 { return 999991; }
        acc = acc + r1 + r2;
        k = k + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc != 1112 * n { return 999990; }
    return (c1.alloc_count - c0.alloc_count) - (c1.free_count - c0.free_count);
}
`

const runtimeV2CrossingStrictCensusSource = runtimeV2CrossingStrictCensusVerticals + `
fn fail_growth(label: string, one: uint, eight: uint) -> int {
    print("FAIL census growth ");
    print(label);
    print(" one=");
    print(one to string);
    print(" eight=");
    print(eight to string);
    return 0 - 1;
}

async fn run() -> int {
    print("crossing-strict-census-witness-start");
    let sh1: TaskResult<uint> = share_window(1).await();
    let sh8: TaskResult<uint> = share_window(8).await();
    let se1: TaskResult<uint> = select_window(1).await();
    let se8: TaskResult<uint> = select_window(8).await();
    let mg1: TaskResult<uint> = migration_window(1).await();
    let mg8: TaskResult<uint> = migration_window(8).await();

    let sh1v: uint = compare sh1 { Success(x) => x; Cancelled() => 888888; };
    let sh8v: uint = compare sh8 { Success(x) => x; Cancelled() => 888888; };
    let se1v: uint = compare se1 { Success(x) => x; Cancelled() => 888888; };
    let se8v: uint = compare se8 { Success(x) => x; Cancelled() => 888888; };
    let mg1v: uint = compare mg1 { Success(x) => x; Cancelled() => 888888; };
    let mg8v: uint = compare mg8 { Success(x) => x; Cancelled() => 888888; };

    print("census share one=");
    print(sh1v to string);
    print(" eight=");
    print(sh8v to string);
    print("census select one=");
    print(se1v to string);
    print(" eight=");
    print(se8v to string);
    print("census migration one=");
    print(mg1v to string);
    print(" eight=");
    print(mg8v to string);

    if sh1v >= 888888 || sh8v >= 888888 || se1v >= 888888 || se8v >= 888888 || mg1v >= 888888 || mg8v >= 888888 {
        print("FAIL result");
        return 1;
    }

    // MIGRATION reaches exact zero, and is pinned at zero rather than merely
    // flat: the far Task<int> handle's per-operation allocation is reclaimed
    // when the caller consumes it via await, and the fixed window-edge unit
    // that used to sit alongside it was a composite's heap box, which went
    // away when a composite got its own storage.
    if mg1v != 0 { return fail_growth("migration-baseline", mg1v, mg8v); }
    if mg8v != 0 { return fail_growth("migration-growth", mg1v, mg8v); }

    // SHARE and SELECT are not zero, and what remains is now exactly one unit
    // per .share() call plus two at the window edge, in both verticals.
    //
    // The per-call unit is the dispatch-side sibling lease struct. Leases are
    // freed together, but only once the registry entry's last lease releases,
    // and that is triggered by the channel itself — created outside the
    // measured window — so within the window every .share() accumulates one
    // still-reachable struct. share_window makes 4 such calls per iteration
    // and select_window 5, which is exactly the growth pinned below:
    // (34 - 6) / 7 = 4 and (42 - 7) / 7 = 5.
    //
    // Two earlier costs are gone. The caller-side box each channel_on(...)
    // and .share() allocated to hold the returned far Channel<T> value now
    // frees at the binding's ordinary scope exit. And a composite stopped
    // being a heap box, which removed one window-edge unit from every vertical
    // and one further per-iteration unit from select — select used to grow by
    // 6 per iteration against 5 .share() calls, and now the two agree.
    //
    // The figures are pinned exactly, not asserted flat, so the next real
    // reduction collapses this check loudly instead of passing quietly.
    if sh1v != 6 { return fail_growth("share-baseline", sh1v, sh8v); }
    if sh8v != 34 { return fail_growth("share-growth", sh1v, sh8v); }
    if se1v != 7 { return fail_growth("select-baseline", se1v, se8v); }
    if se8v != 42 { return fail_growth("select-growth", se1v, se8v); }

    print("crossing-strict-census-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

// runtimeV2CrossingStrictCensusValgrindSource exercises the SAME three
// vertical window functions once each, at a shard-count-agnostic n, with
// only the shard-safe application-level correctness check each window
// function already does internally (acc == expected sum) -- it carries
// none of the n=1-vs-n=8 HeapStats comparisons above, which are pinned to
// exact values that only hold at SURGE_SHARDS=1 (see the comment on those
// checks). This is the program TestRuntimeV2CrossingStrictCensusValgrindBounded
// runs under valgrind at SURGE_SHARDS=1, 2, and 8: valgrind's own
// definitely-lost accounting doesn't care about the wrapped program's exit
// code, but the program still needs to reach an honest completion marker
// at every shard count for the test to trust the leak numbers it captured.
const runtimeV2CrossingStrictCensusValgrindSource = runtimeV2CrossingStrictCensusVerticals + `
async fn run() -> int {
    print("crossing-strict-census-valgrind-witness-start");
    let sh: uint = compare share_window(4).await() { Success(x) => x; Cancelled() => 888888; };
    let se: uint = compare select_window(4).await() { Success(x) => x; Cancelled() => 888888; };
    let mg: uint = compare migration_window(4).await() { Success(x) => x; Cancelled() => 888888; };
    if sh >= 888888 || se >= 888888 || mg >= 888888 {
        print("FAIL result");
        return 1;
    }
    print("crossing-strict-census-valgrind-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2CrossingStrictCensusBalanced(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CrossingStrictCensusSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	duration, result := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
	if result.exitCode != 0 {
		t.Fatalf(
			"crossing strict census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode,
			duration,
			result.stdout,
			result.stderr,
		)
	}
	if !strings.Contains(result.stdout, "crossing-strict-census-witness-start") {
		t.Fatalf("crossing strict census missing execution witness; stdout=%q", result.stdout)
	}
	if !strings.Contains(result.stdout, "crossing-strict-census-ok") {
		t.Fatalf("crossing strict census missing completion marker; stdout=%q", result.stdout)
	}
}

var valgrindDefinitelyLostRE = regexp.MustCompile(`definitely lost:\s*([\d,]+)\s*bytes? in\s*([\d,]+)\s*blocks?`)

// parseValgrindDefinitelyLost extracts the "definitely lost" byte and block
// counts from a `valgrind --leak-check=full` stderr report. Absent the
// summary line entirely (e.g. valgrind aborted before the leak-check
// summary), it returns an error rather than silently reporting zero.
func parseValgrindDefinitelyLost(stderr string) (bytesLost, blocksLost int, err error) {
	m := valgrindDefinitelyLostRE.FindStringSubmatch(stderr)
	if m == nil {
		// Valgrind prints the LEAK SUMMARY breakdown only when something was
		// still reachable at exit. A run that freed everything replaces it with
		// this line, which is the strongest possible answer to "did anything
		// leak" — read it as a clean zero rather than a parse failure.
		if strings.Contains(stderr, "All heap blocks were freed -- no leaks are possible") {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("no \"definitely lost\" summary line found in valgrind output")
	}
	b, convErr := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	if convErr != nil {
		return 0, 0, fmt.Errorf("parse definitely-lost bytes %q: %w", m[1], convErr)
	}
	blocks, convErr := strconv.Atoi(strings.ReplaceAll(m[2], ",", ""))
	if convErr != nil {
		return 0, 0, fmt.Errorf("parse definitely-lost blocks %q: %w", m[2], convErr)
	}
	return b, blocks, nil
}

// runBinaryUnderValgrind runs outputPath wrapped in `valgrind --leak-check=full`
// and returns the combined stdout/stderr text plus the WRAPPED PROGRAM's own
// exit code (not valgrind's). Deliberately does NOT pass --error-exitcode:
// valgrind's default `--errors-for-leak-kinds=definite,possible` folds leak
// loss records into its own error count, which would make an accepted,
// documented leak residual (see TestRuntimeV2CrossingStrictCensusValgrindBounded)
// indistinguishable from a genuine memory-safety bug (invalid free,
// use-after-free, uninitialized read). Callers that need to catch real
// memcheck errors should scan stderr with hasValgrindMemcheckError instead,
// which matches only Memcheck's own error-report headers, never its leak
// summary lines.
func runBinaryUnderValgrind(t *testing.T, outputPath string, env []string, timeout time.Duration) (stdout, stderr string, exitCode int) {
	t.Helper()
	skipTimeoutTests(t)
	root := repoRoot(t)
	timeout = mtScaledTimeout(t, timeout)
	args := []string{
		"--leak-check=full",
		outputPath,
	}
	// #nosec G204 -- fixed valgrind invocation over a test-built binary
	cmd := exec.Command("valgrind", args...)
	cmd.Dir = root
	cmd.Env = env
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start valgrind: %v", err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		exitCode = 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				t.Fatalf("run valgrind %s: %v\nstderr:\n%s", outputPath, err, errBuf.String())
			}
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("valgrind run timed out after %s\nstdout:\n%s\nstderr:\n%s", timeout, outBuf.String(), errBuf.String())
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// valgrindMemcheckErrorRE matches Memcheck's own error-report headers
// ("N bytes ... blocks are definitely lost" is a LEAK line and never
// matches this) so a real memory-safety bug is distinguishable from the
// accepted leak residual this file's valgrind test pins.
var valgrindMemcheckErrorRE = regexp.MustCompile(
	`Invalid (free|read|write)|Mismatched free|Source and destination overlap|Use of uninitialised value|Conditional jump depends on uninitialised value|Invalid or double-free`,
)

func hasValgrindMemcheckError(stderr string) bool {
	return valgrindMemcheckErrorRE.MatchString(stderr)
}

// TestRuntimeV2CrossingStrictCensusValgrindBounded is the timing-independent
// multi-shard witness, run against runtimeV2CrossingStrictCensusValgrindSource
// rather than the pinned SHARDS=1-only program above (that program's own
// sh1v/sh8v/se1v/se8v exit-code checks are NOT shard-agnostic -- reusing it
// here made the wrapped program itself fail at SHARDS=2/8, which is a
// different failure than a real memcheck/leak regression and must not be
// conflated with one). This test does NOT assert definitely-lost == 0.
// The larger of two per-call costs the share/select verticals used to
// carry is now closed (see the
// runtimeV2CrossingStrictCensusSource comment above the pinned
// sh1v/sh8v/se1v/se8v checks for the full writeup): every far Channel<T>
// value returned by channel_on(...) or .share() used to allocate a
// caller-side handle box (rt_far_channel_handle_alloc) that no generated
// code path ever freed -- it now frees at the binding's ordinary scope
// exit. Measured directly (manual valgrind, not derived) against THIS
// leaner program (each window function called once, at n=4, no HeapStats
// comparisons): definitely-lost dropped from 1,280 bytes/52 blocks to 344
// bytes/13 blocks, identical byte-for-byte at SURGE_SHARDS=1, 2, and 8 --
// still shard-topology independent. A later census audit corrected the
// remaining attribution: the seven direct allocations are generated
// tag-only `TaskResult<nothing>` outcome boxes, not runtime lease structs.
// The exact tag-only layout reduced each unchanged allocation from 8 bytes
// to 4, hence 56/7 -> 28/7 with identical allocation count and stacks.
// Wave D replaces this boxed task-result carrier; that migration must tighten
// the row to strict zero rather than recalibrate it again.
func TestRuntimeV2CrossingStrictCensusValgrindBounded(t *testing.T) {
	if _, err := exec.LookPath("valgrind"); err != nil {
		t.Skip("valgrind not installed; skipping valgrind leak-check census")
	}
	// This allowance is gone. It stood at 344 bytes in 13 blocks, was
	// recalibrated to 28 in 7 once a boxed composite with no heap-owning field
	// began freeing its box, and reached zero when a task result stopped being
	// the first awaiter's property and a payload the runtime holds but never
	// delivers started being released. Each step was a real reduction rather
	// than balanced churn, so the row asserts none rather than a smaller
	// number: a residual that comes back is a regression, and there is no
	// longer a documented amount for it to hide inside.
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CrossingStrictCensusValgrindSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
			if hasValgrindMemcheckError(stderr) {
				t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if exitCode != 0 {
				t.Fatalf("crossing strict census under valgrind failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
			}
			if !strings.Contains(stdout, "crossing-strict-census-valgrind-ok") {
				t.Fatalf("crossing strict census under valgrind missing completion marker; stdout=%q", stdout)
			}
			bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
			if err != nil {
				t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
			}
			if bytesLost != 0 || blocksLost != 0 {
				t.Fatalf(
					"crossing strict census leaked %d bytes in %d blocks at shards=%d, want none\nstderr:\n%s",
					bytesLost, blocksLost, shardCount, stderr,
				)
			}
		})
	}
}
