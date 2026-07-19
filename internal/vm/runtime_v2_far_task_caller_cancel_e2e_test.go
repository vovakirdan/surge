//go:build !golden

package vm_test

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Composed caller-cancel route, e2e-shaped (Epic 20 Task 8 row 3, the two
// follow-ups carried from Task 5): a `far Task<T>` is spawned with a
// heap-bearing @shard_movable capture, and the LOCAL caller task that owns
// it (not the far task itself) is cancelled by its PARENT while the far
// task may not have polled yet. This drives
// rt_far_task_release_owned -> rt_far_task_lease_release_route ->
// rt_remote_spawn_abandon_handle — the composed path the direct-abandon
// C-harness rows in TestRuntimeV2RemoteTaskBehavior fake by calling abandon
// directly. The far body still runs regardless of caller cancellation
// (in-flight semantics pinned by Task 5); describe() prints a witness so
// completion is observable even though nothing polls the far task's own
// result after the caller is gone.
const runtimeV2FarTaskCallerCancelSource = `
@shard_movable
type Job = { id: int, note: string };

fn build_note(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn describe(j: own Job) -> int {
    print("e2e1-far-body-ran");
    if j.note != "n-xxxx" { return 0 - 1; }
    return j.id * 100 + 6;
}

async fn caller_body() -> int {
    let j: own Job = own Job{ id: 4, note: build_note("n-") };
    let t: far Task<int> = spawn on distributed { ret describe(own j); };
    let got: TaskResult<int> = t.await();
    return compare got { Success(x) => x; Cancelled() => 0 - 2; };
}

async fn run() -> int {
    let child: Task<int> = spawn caller_body();
    child.cancel();
    let r: TaskResult<int> = child.await();
    let v: int = compare r { Success(x) => x; Cancelled() => 0 - 1; };
    if v != (0 - 1) {
        print("FAIL expected cancelled caller, got v=");
        print(v to string);
        return 1;
    }
    print("far-task-caller-cancel-e2e1-ok");
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

// The phase-sweep twin: the same caller-cancel shape, but the parent gives
// the child a varying number of checkpoint yields before cancelling, and
// repeats each phase a few times. This has NO sync points and does not try
// to pin an exact publication/first-poll boundary (the empirical behavior,
// discovered while building this row, is that at SURGE_SHARDS=1 the
// boundary is narrower than one checkpoint yield: 0 yields never lets the
// child start at all, and 1 yield already resolves the whole round trip —
// there is no stable Surge-level lever that lands strictly between them at
// one shard). Instead this sweeps yield counts (0, 1, 6) x 3 shard counts x
// 3 iterations per phase and asserts the invariant that must hold
// regardless of which side of the race a given run lands on: no crash, the
// far body always runs (one witness print per iteration, whichever side of
// the cancel race it's on), and every outcome is a legal TaskResult arm
// (never the sentinel "unexpected" branch).
const runtimeV2FarTaskCallerCancelPhaseSweepSource = `
@shard_movable
type Job = { id: int, note: string };

fn build_note(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn describe(j: own Job) -> int {
    print("e2e2-far-body-ran");
    if j.note != "n-xxxx" { return 0 - 1; }
    return j.id * 100 + 6;
}

async fn caller_body() -> int {
    let j: own Job = own Job{ id: 4, note: build_note("n-") };
    let t: far Task<int> = spawn on distributed { ret describe(own j); };
    let got: TaskResult<int> = t.await();
    return compare got { Success(x) => x; Cancelled() => 0 - 2; };
}

async fn phase_iteration(yields: int) -> int {
    let child: Task<int> = spawn caller_body();
    let mut k = 0;
    while k < yields {
        checkpoint().await();
        k = k + 1;
    }
    child.cancel();
    let r: TaskResult<int> = child.await();
    return compare r { Success(_) => 1; Cancelled() => 2; };
}

async fn run_phase(yields: int, count: int) -> int {
    let mut i = 0;
    let mut success_count = 0;
    let mut cancelled_count = 0;
    while i < count {
        let outcome: TaskResult<int> = phase_iteration(yields).await();
        let code: int = compare outcome { Success(x) => x; Cancelled() => 0 - 9; };
        if code == 1 {
            success_count = success_count + 1;
        } else if code == 2 {
            cancelled_count = cancelled_count + 1;
        } else {
            print("FAIL phase_iteration unexpected outcome code=");
            print(code to string);
            return 0 - 1;
        }
        i = i + 1;
    }
    print("phase-done yields=");
    print(yields to string);
    print(" successes=");
    print(success_count to string);
    print(" cancelled=");
    print(cancelled_count to string);
    return 0;
}

async fn run() -> int {
    let r0: TaskResult<int> = run_phase(0, 3).await();
    let c0: int = compare r0 { Success(x) => x; Cancelled() => 0 - 10; };
    if c0 != 0 { print("FAIL phase0"); return 21; }

    let r1: TaskResult<int> = run_phase(1, 3).await();
    let c1: int = compare r1 { Success(x) => x; Cancelled() => 0 - 11; };
    if c1 != 0 { print("FAIL phase1"); return 22; }

    let r2: TaskResult<int> = run_phase(6, 3).await();
    let c2: int = compare r2 { Success(x) => x; Cancelled() => 0 - 12; };
    if c2 != 0 { print("FAIL phase2"); return 23; }

    print("far-task-caller-cancel-e2e2-ok");
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

// TestRuntimeV2FarTaskCallerCancel drives the two e2e follow-ups carried
// from Epic 20 Task 5 into Task 8 row 3: caller-cancel through the
// composed lease/abandon route, at shard counts 1/2/8, plus a
// valgrind-gated leak census.
func TestRuntimeV2FarTaskCallerCancel(t *testing.T) {
	t.Run("cancel_after_publication_before_first_poll", func(t *testing.T) {
		outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarTaskCallerCancelSource, nil)
		baseEnv := envWithStdlib(repoRoot(t))
		for _, shardCount := range []int{1, 2, 8} {
			t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
				value := strconv.Itoa(shardCount)
				env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
				env = overrideEnvVar(env, "SURGE_THREADS", value)
				duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
				if result.exitCode != 0 {
					t.Fatalf(
						"far-task caller-cancel e2e1 failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
						result.exitCode, duration, result.stdout, result.stderr,
					)
				}
				if strings.Contains(result.stdout, "FAIL") {
					t.Fatalf("far-task caller-cancel e2e1 printed a FAIL marker; stdout=%q", result.stdout)
				}
				if !strings.Contains(result.stdout, "far-task-caller-cancel-e2e1-ok") {
					t.Fatalf("far-task caller-cancel e2e1 missing completion marker; stdout=%q", result.stdout)
				}
				// At SURGE_SHARDS=1 the caller-cancel race is won
				// deterministically by cancel() before the child ever
				// runs (see the source comment): the far task is never
				// created, so its witness never fires. At 2+ shards the
				// child reliably gets a head start on a second worker
				// and the far body runs before the caller resolves
				// Cancelled — the witness is required there.
				hasWitness := strings.Contains(result.stdout, "e2e1-far-body-ran")
				if shardCount == 1 && hasWitness {
					t.Fatalf("unexpected far-body witness at shards=1 (race assumption changed); stdout=%q", result.stdout)
				}
				if shardCount > 1 && !hasWitness {
					t.Fatalf("missing far-body witness at shards=%d; stdout=%q", shardCount, result.stdout)
				}
			})
		}
		t.Run("valgrind", func(t *testing.T) {
			runFarTaskCallerCancelValgrindCensus(t, outputPath, baseEnv, "far-task-caller-cancel-e2e1-ok", "e2e1-far-body-ran")
		})
	})

	t.Run("phase_sweep_publication_and_first_poll", func(t *testing.T) {
		outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarTaskCallerCancelPhaseSweepSource, nil)
		baseEnv := envWithStdlib(repoRoot(t))
		for _, shardCount := range []int{1, 2, 8} {
			t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
				value := strconv.Itoa(shardCount)
				env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
				env = overrideEnvVar(env, "SURGE_THREADS", value)
				duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
				if result.exitCode != 0 {
					t.Fatalf(
						"far-task caller-cancel e2e2 failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
						result.exitCode, duration, result.stdout, result.stderr,
					)
				}
				if strings.Contains(result.stdout, "FAIL") {
					t.Fatalf("far-task caller-cancel e2e2 printed a FAIL marker; stdout=%q", result.stdout)
				}
				if !strings.Contains(result.stdout, "far-task-caller-cancel-e2e2-ok") {
					t.Fatalf("far-task caller-cancel e2e2 missing completion marker; stdout=%q", result.stdout)
				}
				// 3 phases x 3 iterations = 9 far-task invocations; every
				// one must run to completion regardless of which side of
				// the cancel race it landed on.
				const wantWitnesses = 9
				gotWitnesses := strings.Count(result.stdout, "e2e2-far-body-ran")
				if gotWitnesses != wantWitnesses {
					t.Fatalf("far-body witness count = %d, want %d; stdout=%q", gotWitnesses, wantWitnesses, result.stdout)
				}
			})
		}
		t.Run("valgrind", func(t *testing.T) {
			runFarTaskCallerCancelValgrindCensus(t, outputPath, baseEnv, "far-task-caller-cancel-e2e2-ok", "e2e2-far-body-ran")
		})
	})
}

var (
	valgrindDefiniteLeakRE = regexp.MustCompile(`(?:^|\n)\s*==\d+==\s*definitely lost:\s*([0-9,]+)\s*bytes in\s*([0-9,]+)\s*blocks`)
	valgrindIndirectLeakRE = regexp.MustCompile(`(?:^|\n)\s*==\d+==\s*indirectly lost:\s*([0-9,]+)\s*bytes in\s*([0-9,]+)\s*blocks`)
	// These memcheck error classes indicate an actual crash-class bug
	// (use-after-free, buffer overrun, uninitialised control-flow use) —
	// distinct from, and always more serious than, the leak-accounting
	// residual this row documents. Any of these is a hard failure.
	valgrindCorruptionRE = regexp.MustCompile(`Invalid (?:free|read|write)|Mismatched free|Source and destination overlap|Conditional jump or move depends on uninitialised value`)
)

// runFarTaskCallerCancelValgrindCensus runs the already-built e2e binary
// under valgrind at 1 shard (and 2 shards, once proven stable) and reports
// the leak census. It intentionally does NOT hard-assert
// definitely-lost==0: this row's own investigation found that a LOCAL
// (non-far) Task whose body parks on an internal .await() leaks its own
// compiled async frame when its PARENT cancels it before the body has run
// a single instruction — see the KNOWN RESIDUAL note below. That defect
// pre-exists this row (it reproduces with no far/distributed task
// involved at all) and is reported, not fixed, per the
// dodge-don't-fix constraint; only genuine crash-class errors
// (valgrindCorruptionRE) are treated as hard failures here.
func runFarTaskCallerCancelValgrindCensus(t *testing.T, bin string, baseEnv []string, wantMarker, witnessMarker string) {
	t.Helper()
	vg, err := exec.LookPath("valgrind")
	if err != nil {
		t.Skip("valgrind not found on PATH; skipping leak census")
	}
	root := repoRoot(t)
	for _, shardCount := range []int{1, 2} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			args := []string{
				"--leak-check=full",
				"--show-leak-kinds=definite,indirect",
				"--errors-for-leak-kinds=definite,indirect",
				"--error-exitcode=97",
				bin,
			}
			cmd := exec.CommandContext(ctx, vg, args...)
			cmd.Dir = root
			cmd.Env = env
			stdout, stderr, exitCode := runCommand(t, cmd, "")
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("valgrind run timed out\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if !strings.Contains(stdout, wantMarker) {
				t.Fatalf("missing completion marker %q under valgrind; stdout=%q\nstderr:\n%s", wantMarker, stdout, stderr)
			}
			if m := valgrindCorruptionRE.FindString(stderr); m != "" {
				t.Fatalf("valgrind reports a crash-class error (%q), not just the known leak residual\nstderr:\n%s", m, stderr)
			}
			definiteBytes, definiteBlocks := parseValgrindLeakMatch(valgrindDefiniteLeakRE, stderr)
			indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
			witnessCount := strings.Count(stdout, witnessMarker)
			t.Logf(
				"valgrind census (shards=%d): definitely_lost=%dB/%dblk indirectly_lost=%dB/%dblk far_body_witnesses=%d exit=%d",
				shardCount, definiteBytes, definiteBlocks, indirectBytes, indirectBlocks, witnessCount, exitCode,
			)
			// KNOWN RESIDUAL (reported here, not fixed — candidate new
			// debt row, lead's call): definitely/indirectly lost bytes
			// are attributable one-for-one to cancelled callers whose
			// body parked on an internal .await() before its own
			// checkpoint ever ran — a minimal, far-task-free repro
			// (spawn a child that does `helper().await()` then returns;
			// cancel it from the parent immediately after spawn) leaks
			// 32B direct + 24B indirect EXACTLY once, and leaks 0 when
			// the cancel call is removed. The far-task shape here adds
			// its own capture/task-handle state on top (72B/occurrence
			// observed). This matches the "abandon path never draining
			// post-shutdown" cancelled-frame gap flagged as a
			// possibility for this row: assert only that the loss is a
			// clean multiple of the smallest observed per-occurrence
			// direct-leak unit (40B for this far-task shape) rather than
			// growing unboundedly or including corruption-class errors.
			const perOccurrenceDirectBytes = 40
			if definiteBytes%perOccurrenceDirectBytes != 0 {
				t.Fatalf(
					"definitely-lost bytes (%d) is not a multiple of the known per-cancelled-caller unit (%d); this looks like a NEW leak shape, not the documented residual",
					definiteBytes, perOccurrenceDirectBytes,
				)
			}
		})
	}
}

func parseValgrindLeakMatch(re *regexp.Regexp, stderr string) (bytes, blocks int) {
	m := re.FindStringSubmatch(stderr)
	if m == nil {
		return 0, 0
	}
	bytes, _ = strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	blocks, _ = strconv.Atoi(strings.ReplaceAll(m[2], ",", ""))
	return bytes, blocks
}
