package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// RV2-DEBT-370, native: a call of an `async fn` creates a task that does not run until it
// is spawned or awaited (docs/RUNTIME_MODEL_EXPLAINED.ru.md 6.3), and the last handle
// dropped before that ends it without running it. The stand drives the runtime's own entry
// points -- __task_create_cold, rt_task_wake, rt_task_poll, rt_task_await, rt_task_cancel,
// rt_task_clone, rt_task_handle_drop, rt_scope_join_all -- with a start frame whose
// descriptor counts its own destruction, so every row can say both "the body ran" and "the
// frame was given back", each exactly once or not at all.

func TestRuntimeV2ColdTaskDroppedNeverRuns(t *testing.T) {
	runColdTaskStand(t, "drop", "1", "SURGE_THREADS=1")
}

func TestRuntimeV2ColdTaskSpawnedRuns(t *testing.T) {
	runColdTaskStand(t, "spawn", "1", "SURGE_THREADS=1")
}

func TestRuntimeV2ColdTaskAwaitedRuns(t *testing.T) {
	runColdTaskStand(t, "await", "1", "SURGE_THREADS=1")
}

func TestRuntimeV2ColdTaskCloneKeepsItAlive(t *testing.T) {
	runColdTaskStand(t, "clone", "1", "SURGE_THREADS=1")
}

func TestRuntimeV2ColdTaskCancelPublishesIt(t *testing.T) {
	runColdTaskStand(t, "cancel", "1", "SURGE_THREADS=1")
}

// A member dropped cold leaves its fail-fast scope drained and not fail-fast, and the join
// that follows does not wait.
func TestRuntimeV2ColdTaskDroppedMemberDoesNotHoldTheJoin(t *testing.T) {
	runColdTaskStand(t, "scope-drop", "1", "SURGE_THREADS=1")
}

// A member whose handle outlives the body is published by the join and joined, as it was
// when every task was published at creation.
func TestRuntimeV2ColdTaskJoinPublishesAnEscapedMember(t *testing.T) {
	runColdTaskStand(t, "scope-escape", "1", "SURGE_THREADS=1")
}

// The carrier pin is written at creation, before any publication, and holds whether the
// task is claimed inline by its creator or published later from a thread that is no worker.
func TestRuntimeV2ColdTaskAffinePinHolds(t *testing.T) {
	for _, mode := range []string{"affine-inline", "affine-handoff"} {
		for _, threads := range []string{"2", "4"} {
			t.Run(mode+"/threads-"+threads, func(t *testing.T) {
				runColdTaskStand(t, mode, "1", "SURGE_SHARDS=1", "SURGE_THREADS="+threads)
			})
		}
	}
}

// A publication and a discard made on a lane that is not the scope's owner lane reach the
// scope only through the scope event (ruling 2026-09-02, Р6): the owner lane's next turn
// finds the hint applied, and the transport carried the event.
func TestRuntimeV2ColdTaskForeignLaneReachesTheScopeByEvent(t *testing.T) {
	for _, mode := range []string{"scope-foreign-publish", "scope-foreign-drop"} {
		t.Run(mode, func(t *testing.T) {
			runColdTaskStand(t, mode, "1", "SURGE_THREADS=1")
		})
	}
}

// A discard and a cancel by id: after the discard the cancel finds nothing to revive; a
// cancel that took the gate first is owed its answer, so the last drop publishes the task and
// its first poll answers Cancelled() without entering the body; the row reads the publication
// word inside the window, so a discard cannot pass for that publication.
func TestRuntimeV2ColdTaskDiscardAndCancelRace(t *testing.T) {
	runColdTaskStand(t, "drop-then-cancel", "1", "SURGE_THREADS=1")
	bin := buildColdTaskStandWithFlags(t, "-DRT_TEST_SYNC_POINTS")
	runColdTaskBinary(t, bin, "cancel-race", "1", "SURGE_THREADS=1")
}

// The inline claim keeps the base's scope (review F1): only the awaiter's most recent creation
// is polled inline on the awaiting thread; an older child goes through the queue.
func TestRuntimeV2ColdTaskInlineClaimTakesOnlyTheLatest(t *testing.T) {
	runColdTaskStand(t, "inline-scope", "1", "SURGE_SHARDS=1", "SURGE_THREADS=2")
}

// The join's walk holds the control lane between get_task and the member's publication word;
// a last drop on another thread inside that window discards the member, the walk wakes
// nothing, and the free waits for the walk (packet section 00, re-check 2).
// Falsifiable twice (review D1): the plain build asks the task table, 50 ms after the discard,
// whether the member is still there; the AddressSanitizer build aborts if the walk reads it
// freed. CF8, a free that does not wait for control, must turn both leaves red.
func TestRuntimeV2ColdTaskJoinWalkRacesALastDrop(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		bin := buildColdTaskStandWithFlags(t, "-DRT_TEST_SYNC_POINTS")
		runColdTaskBinary(t, bin, "join-walk-drop", "1", "SURGE_THREADS=1")
	})
	t.Run("asan", func(t *testing.T) {
		requireUnlimitedAddressSpace(t) // AddressSanitizer reserves its shadow up front, as TSan does
		bin := buildColdTaskStandWithFlags(t, "-DRT_TEST_SYNC_POINTS", "-fsanitize=address,undefined",
			"-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-O1")
		runColdTaskBinary(t, bin, "join-walk-drop", "1", "SURGE_THREADS=1",
			"ASAN_OPTIONS=abort_on_error=1:detect_leaks=0")
	})
}

// The cold paths under ThreadSanitizer: concurrent last drops, a join that publishes an
// escaped member with worker threads running, and a publication from a thread that is not
// the scope's owner lane.
func TestRuntimeV2ColdTaskUnderThreadSanitizer(t *testing.T) {
	skipTimeoutTests(t)
	requireUnlimitedAddressSpace(t)
	bin := buildColdTaskStandWithFlags(t, "-fsanitize=thread", "-O1")
	for _, row := range []struct{ mode, rounds, threads string }{
		{"race-drop", "200", "4"},
		{"scope-escape", "1", "4"},
		{"scope-foreign-publish", "1", "1"},
	} {
		t.Run(row.mode, func(t *testing.T) {
			cmd := exec.Command(bin, row.mode, row.rounds)
			cmd.Env = append(os.Environ(), "SURGE_BLOCKING_THREADS=1", "SURGE_THREADS="+row.threads)
			stdout, stderr, code := runCommand(t, cmd, "")
			if strings.Contains(stderr, "ThreadSanitizer") || code != 0 ||
				!strings.Contains(stdout, "COLD_STAND_OK "+row.mode) {
				t.Fatalf("cold task %s under ThreadSanitizer (code=%d)\nstdout:\n%s\nstderr:\n%s", row.mode, code, stdout, stderr)
			}
		})
	}
}

// The fail-fast window: a cancel that reaches a cold member --
// here the cancel-all a cancelled sibling raises, landing while the owner still holds the
// member's handle -- linearizes before the member could start, so the member answers Cancelled()
// without its body ever being entered (docs/RUNTIME_MODEL_EXPLAINED.ru.md 6.3: a cold task runs
// only when spawned or awaited; docs/RUNTIME_V2.md, owner ruling 2026-08-29), although the owner
// then drops the handle, releases the local the member borrows and joins. The owner waits for
// the member's gate before its drop, so that order is fixed by construction.
// Which road published the member is logged from the stand's OK line: recorded, not required.
func TestRuntimeV2ColdTaskFailfastCancelsAColdMemberUnrun(t *testing.T) {
	skipTimeoutTests(t)
	bin := buildColdTaskStand(t)
	for _, threads := range []string{"2", "4"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			cmd := exec.Command(bin, "failfast-cold-member", "50")
			cmd.Env = append(os.Environ(), "SURGE_BLOCKING_THREADS=1", "SURGE_SHARDS=1", "SURGE_THREADS="+threads)
			stdout, stderr, code := runCommand(t, cmd, "")
			if code != 0 || !strings.Contains(stdout, "COLD_STAND_OK failfast-cold-member") {
				t.Fatalf("cold task stand failfast-cold-member failed (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			t.Log(strings.TrimSpace(stdout))
		})
	}
}

// The cancelled-cold paths under ThreadSanitizer: the fail-fast member with four workers, and a
// cancel of a cold task answered by an await from a thread that is no worker.
func TestRuntimeV2ColdTaskCancelledColdUnderThreadSanitizer(t *testing.T) {
	skipTimeoutTests(t)
	requireUnlimitedAddressSpace(t)
	bin := buildColdTaskStandWithFlags(t, "-fsanitize=thread", "-O1")
	for _, row := range []struct{ mode, rounds string }{
		{"failfast-cold-member", "100"},
		{"cancel", "200"},
	} {
		t.Run(row.mode, func(t *testing.T) {
			cmd := exec.Command(bin, row.mode, row.rounds)
			cmd.Env = append(os.Environ(), "SURGE_BLOCKING_THREADS=1", "SURGE_SHARDS=1", "SURGE_THREADS=4")
			stdout, stderr, code := runCommand(t, cmd, "")
			if strings.Contains(stderr, "ThreadSanitizer") || code != 0 ||
				!strings.Contains(stdout, "COLD_STAND_OK "+row.mode) {
				t.Fatalf("cancelled cold task %s under ThreadSanitizer (code=%d)\nstdout:\n%s\nstderr:\n%s", row.mode, code, stdout, stderr)
			}
		})
	}
}

// A task cancelled while cold, under Valgrind: no error and no allocation per task, at one and at
// eight rounds -- its start frame goes back through mark_done's reclaim pair, not through a poll.
func TestRuntimeV2ColdTaskCancelledColdValgrindZero(t *testing.T) {
	skipTimeoutTests(t)
	if _, err := exec.LookPath("valgrind"); err != nil {
		t.Skip("valgrind not installed")
	}
	bin := buildColdTaskStand(t)
	low := coldTaskValgrind(t, bin, "cancel", "1")
	high := coldTaskValgrind(t, bin, "cancel", "8")
	if high-low >= 2 {
		t.Errorf("a task cancelled while cold leaks: %d outstanding at 1, %d at 8", low, high)
	}
}

// Two handles of one cold task dropped at the same time from two threads: exactly one
// discard, no leak, no double free.
func TestRuntimeV2ColdTaskConcurrentLastDrops(t *testing.T) {
	runColdTaskStand(t, "race-drop", "2000", "SURGE_THREADS=1")
}

// The drop row and the discarding scope row under Valgrind: no error, and no allocation per
// dropped task (the slope between one and eight drops), which is where a start frame, a
// captured value or the task itself left behind would show.
func TestRuntimeV2ColdTaskDropValgrindZero(t *testing.T) {
	skipTimeoutTests(t)
	if _, err := exec.LookPath("valgrind"); err != nil {
		t.Skip("valgrind not installed")
	}
	bin := buildColdTaskStand(t)
	for _, mode := range []string{"drop", "scope-drop", "clone"} {
		t.Run(mode, func(t *testing.T) {
			low := coldTaskValgrind(t, bin, mode, "1")
			high := coldTaskValgrind(t, bin, mode, "8")
			if high-low >= 2 {
				t.Errorf("%s leaks per dropped task: %d outstanding at 1, %d at 8", mode, low, high)
			}
		})
	}
}

// ThreadSanitizer and AddressSanitizer map their shadow memory up front and refuse a finite RLIMIT_AS
// ("setrlimit() failed 22", measured on the dedicated host 2026-09-22). A raisable soft limit is
// raised for the test and restored after; a hard limit cannot be, and the row says so instead of
// failing on the environment. The host script runs these rows without `ulimit -v`.
func requireUnlimitedAddressSpace(t *testing.T) {
	t.Helper()
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &old); err != nil {
		t.Fatalf("read RLIMIT_AS: %v", err)
	}
	const unlimited = ^uint64(0)
	if old.Cur == unlimited {
		return
	}
	if old.Max != unlimited {
		t.Skipf("ThreadSanitizer needs an unlimited address space (RLIMIT_AS soft %d, hard %d); run this row outside ulimit -v", old.Cur, old.Max)
	}
	raised := syscall.Rlimit{Cur: unlimited, Max: old.Max}
	if err := syscall.Setrlimit(syscall.RLIMIT_AS, &raised); err != nil {
		t.Skipf("ThreadSanitizer needs an unlimited address space and RLIMIT_AS could not be raised: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Setrlimit(syscall.RLIMIT_AS, &old) })
}

func runColdTaskStand(t *testing.T, mode, rounds string, env ...string) {
	t.Helper()
	skipTimeoutTests(t)
	runColdTaskBinary(t, buildColdTaskStand(t), mode, rounds, env...)
}

func runColdTaskBinary(t *testing.T, bin, mode, rounds string, env ...string) {
	t.Helper()
	skipTimeoutTests(t)
	cmd := exec.Command(bin, mode, rounds)
	cmd.Env = append(os.Environ(), append([]string{"SURGE_BLOCKING_THREADS=1"}, env...)...)
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 || !strings.Contains(stdout, "COLD_STAND_OK "+mode) {
		t.Fatalf("cold task stand %s failed (code=%d)\nstdout:\n%s\nstderr:\n%s", mode, code, stdout, stderr)
	}
}

func coldTaskValgrind(t *testing.T, bin, mode, rounds string) int {
	t.Helper()
	// The runtime's own blocking-pool thread leaves one "possibly lost" TLS block (ensure_exec ->
	// rt_blocking_init -> pthread_create); like the repo's other valgrind stands, only definite and
	// indirect leaks are errors. What proves the task's own blocks are released is the stand's
	// per-round frame counter (exactly one release per dropped task) and the allocs-minus-frees
	// slope between one and eight rounds below, neither of which that flag relaxes.
	cmd := exec.Command("valgrind", "--error-exitcode=99", "--leak-check=full",
		"--errors-for-leak-kinds=definite,indirect", bin, mode, rounds)
	cmd.Env = append(os.Environ(), "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 || !strings.Contains(stderr, "ERROR SUMMARY: 0 errors") {
		t.Fatalf("valgrind %s %s (code=%d)\nstdout:\n%s\nstderr:\n%s", mode, rounds, code, stdout, stderr)
	}
	outstanding, err := parseValgrindOutstandingAllocations(stderr)
	if err != nil {
		t.Fatalf("parse valgrind allocation ledger: %v\nstderr:\n%s", err, stderr)
	}
	return outstanding
}

func buildColdTaskStand(t *testing.T) string {
	t.Helper()
	return buildColdTaskStandWithFlags(t)
}

func buildColdTaskStandWithFlags(t *testing.T, flags ...string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the cold task stand")
	}
	root := repoRoot(t)
	dir := t.TempDir()
	harness := filepath.Join(dir, "cold_task_stand.c")
	bin := filepath.Join(dir, "cold_task_stand")
	if writeErr := os.WriteFile(harness, []byte(coldTaskStand+coldTaskStandFailfast+coldTaskStandMain), 0o600); writeErr != nil {
		t.Fatalf("write stand: %v", writeErr)
	}
	sources, globErr := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if globErr != nil {
		t.Fatalf("glob runtime sources: %v", globErr)
	}
	sort.Strings(sources)
	args := append([]string{"-std=c11", "-g", "-Wall", "-Wextra", "-Werror", "-pthread"}, flags...)
	args = append(args, "-I"+filepath.Join(root, "runtime", "native"), "-o", bin, harness)
	for _, src := range sources {
		if filepath.Base(src) != "rt_entry.c" {
			args = append(args, src)
		}
	}
	build := exec.Command(clang, args...)
	build.Dir = root
	out, errOut, code := runCommand(t, build, "")
	if code != 0 {
		t.Fatalf("build cold task stand (code=%d)\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	return bin
}
