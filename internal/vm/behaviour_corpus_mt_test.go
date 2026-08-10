package vm_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The multi-worker lane.
//
// The behavioural corpus compares the two backends with one worker, because
// that is the only configuration in which the question "did the two backends
// answer the same" has a single right answer: the VM is single-worker, and a
// recorded interleaving from a thirty-two-worker run is not a fact about the
// program. That comparison is necessary and it is not sufficient. The native
// binary is multi-worker in production, and a defect that only appears once the
// work is split across workers is invisible to a lane that never splits it.
//
// So this lane runs the same recorded programs on the native backend at several
// worker counts, and asserts what multiple workers actually promise. What they
// promise is in docs/CONCURRENCY.md: a task is never polled by two workers at
// once, suspension points park tasks rather than blocking workers, cancellation
// is observed at suspension points. What they explicitly do NOT promise, in the
// same document, is ordering — "Determinism in parallel mode" and "Global FIFO
// ordering or fairness across multiple workers" are listed as non-guarantees.
//
// This lane therefore never compares output text. Asserting a recorded
// interleaving here would be asserting something the language refuses to
// promise, which is how the native lane's failure list came to change between
// two runs of the same tree. What it asserts instead is the class of failure
// that multiple workers actually produce and one worker hides:
//
//   - the program TERMINATES, so a lost wakeup or a deadlock is a failure and
//     not a quiet timeout at the end of the package;
//   - it exits with the code it is recorded to exit with, so a task that
//     silently stops producing a result is a failure;
//   - it does not die by a signal, and prints no allocator or sanitizer
//     complaint, so a race that corrupts the heap is a failure even when the
//     exit code happens to survive it.
//
// Stronger per-fixture assertions — "these lines in any order", "this value
// exactly once" — belong to whichever fixtures can state them, and are not
// invented here for fixtures that never made the claim.
type mtLaneConfig struct {
	workers []string
	repeats int
}

// mtCrashSignatures are the things a runtime prints on its way down that an
// exit code can hide. glibc reports allocator corruption on stderr and then
// aborts; a sanitizer prints a report before exiting; an assertion names
// itself. None of these is ordering, so none of them is excused by running on
// more than one worker.
var mtCrashSignatures = []string{
	"free(): ",
	"double free",
	"corrupted",
	"malloc(): ",
	"munmap_chunk",
	"stack smashing",
	"Assertion",
	"AddressSanitizer",
	"ThreadSanitizer",
	"UndefinedBehaviorSanitizer",
	"Segmentation fault",
}

// behaviourMTConfig reads the lane's shape from the environment so the same
// test serves a quick local run and a longer CI sweep without a second copy of
// the walk.
//
// The lane is OFF unless asked for, and asking is the whole point: it costs a
// native compile per fixture and then one process per worker count per repeat,
// so it belongs at an important step rather than in the pre-commit hook. When
// it is asked for and the toolchain is missing, behaviouralBackends already
// fails rather than skipping.
func behaviourMTConfig(t *testing.T) (mtLaneConfig, bool) {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("SURGE_BEHAVIOUR_MT"))
	if raw == "" {
		return mtLaneConfig{}, false
	}

	cfg := mtLaneConfig{workers: []string{"2", "8"}, repeats: 2}
	if raw != "1" {
		var workers []string
		for _, field := range strings.Split(raw, ",") {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			n, err := strconv.Atoi(field)
			if err != nil || n < 1 {
				t.Fatalf("SURGE_BEHAVIOUR_MT names %q, which is not a worker count", field)
			}
			workers = append(workers, strconv.Itoa(n))
		}
		if len(workers) == 0 {
			t.Fatal("SURGE_BEHAVIOUR_MT is set but names no worker count")
		}
		cfg.workers = workers
	}
	if repeats := strings.TrimSpace(os.Getenv("SURGE_BEHAVIOUR_MT_REPEATS")); repeats != "" {
		n, err := strconv.Atoi(repeats)
		if err != nil || n < 1 {
			t.Fatalf("SURGE_BEHAVIOUR_MT_REPEATS is %q, which is not a repeat count", repeats)
		}
		cfg.repeats = n
	}
	return cfg, true
}

// TestBehaviourCorpusMT runs the async corpus on the native backend at more
// than one worker.
//
// Only the async directories are walked. A program with no task in it produces
// the same bytes whatever the worker count, so running it here would buy
// nothing and cost a native compile.
func TestBehaviourCorpusMT(t *testing.T) {
	cfg, on := behaviourMTConfig(t)
	if !on {
		t.Skip("SURGE_BEHAVIOUR_MT is not set; the multi-worker lane runs from make behaviour-check-mt")
	}
	skipTimeoutTests(t)

	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	// Asking for the native lane through the shared reader keeps one rule about
	// a missing toolchain: it fails, it does not skip.
	t.Setenv("SURGE_BEHAVIOUR_BACKENDS", "llvm")
	if backends := behaviouralBackends(t); len(backends) != 1 || backends[0] != "llvm" {
		t.Fatalf("multi-worker lane resolved backends %v, want [llvm]", backends)
	}

	for _, dir := range []string{"vm_async", "vm_async_suite"} {
		absDir := filepath.Join(root, "testdata", "golden", dir)
		entries, err := os.ReadDir(absDir)
		if err != nil {
			t.Fatalf("read %s dir: %v", dir, err)
		}
		for _, ent := range entries {
			if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".sg") || strings.HasPrefix(ent.Name(), "_") {
				continue
			}
			name := strings.TrimSuffix(ent.Name(), ".sg")
			if !fixtureRunsOn(t, absDir, name, "llvm") {
				continue
			}
			t.Run(name, func(t *testing.T) {
				runBehaviourMTCase(t, root, surge, absDir, name, cfg)
			})
		}
	}
}

// mtRunTimeout bounds ONE execution of an already-built binary. These programs
// finish in milliseconds - native timers run on executor time, so even a
// recorded sleep(100) is not wall-clock - so anything near this bound is not
// slow, it is stuck.
//
// Bounding each run is what makes a hang readable. The first run of this lane
// found one and reported it as the whole package timing out after an hour, with
// the fixture name recoverable only from a goroutine dump. A hang is the most
// interesting thing a multi-worker lane can find; it has to arrive as a named
// failure.
const mtRunTimeout = 20 * time.Second

func runBehaviourMTCase(t *testing.T, root, surge, absDir, name string, cfg mtLaneConfig) {
	t.Helper()

	// A run-time sidecar cannot be honoured by a prebuilt binary, and quietly
	// ignoring one would run a different program than the corpus recorded.
	if _, err := os.Stat(filepath.Join(absDir, name+".flags")); err == nil {
		t.Fatalf("%s carries a .flags sidecar, which this lane cannot pass to a prebuilt binary", name)
	}

	wantCode := 0
	if b, err := os.ReadFile(filepath.Join(absDir, name+".code")); err == nil {
		n, parseErr := strconv.Atoi(strings.TrimSpace(string(b)))
		if parseErr != nil {
			t.Fatalf("parse %s.code: %v", name, parseErr)
		}
		wantCode = n
	}

	// Built once, run many. The compile is the expensive part - it is clang
	// linking the whole runtime - and it does not vary with the worker count,
	// which is the only thing this lane changes between runs.
	binary := buildFixtureBinary(t, root, surge, absDir, name)

	for _, workers := range cfg.workers {
		for repeat := 1; repeat <= cfg.repeats; repeat++ {
			stdout, stderr, code, timedOut := runBinaryWithWorkers(t, binary, workers)

			if timedOut {
				t.Fatalf("workers=%s run %d: did not finish within %s - a task that never wakes looks exactly like this\nstdout:\n%s\nstderr:\n%s",
					workers, repeat, mtRunTimeout, stdout, stderr)
			}
			// A process killed by a signal reports -1 here rather than a code,
			// so it cannot be confused with a program that chose to exit.
			if code < 0 {
				t.Fatalf("workers=%s run %d: killed by a signal\nstdout:\n%s\nstderr:\n%s",
					workers, repeat, stdout, stderr)
			}
			if code != wantCode {
				t.Fatalf("workers=%s run %d: exit code want %d, got %d\nstdout:\n%s\nstderr:\n%s",
					workers, repeat, wantCode, code, stdout, stderr)
			}
			for _, signature := range mtCrashSignatures {
				if strings.Contains(stderr, signature) || strings.Contains(stdout, signature) {
					t.Fatalf("workers=%s run %d: runtime reported %q\nstdout:\n%s\nstderr:\n%s",
						workers, repeat, signature, stdout, stderr)
				}
			}
		}
	}
}

// buildFixtureBinary compiles one recorded program into a throwaway project and
// returns the executable. `surge build` wants a manifest, so the fixture is
// copied beside a minimal one rather than built in place - the corpus directory
// stays read-only, which matters because this lane runs beside others.
func buildFixtureBinary(t *testing.T, root, surge, absDir, name string) string {
	t.Helper()
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join(absDir, name+".sg"))
	if err != nil {
		t.Fatalf("read %s.sg: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sg"), source, 0o600); err != nil {
		t.Fatalf("write fixture source: %v", err)
	}
	manifest := "[package]\nname = \"mt\"\nroot = \".\"\nversion = \"0.1.0\"\n\n[run]\nmain = \"main.sg\"\n"
	if err := os.WriteFile(filepath.Join(dir, "surge.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write fixture manifest: %v", err)
	}

	cmd := exec.Command(surge, "build", "--backend=llvm")
	cmd.Dir = dir
	cmd.Env = envWithStdlib(root)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("build %s for the multi-worker lane: %v\n%s", name, err, out.String())
	}
	return filepath.Join(dir, "target", "debug", "mt")
}

func runBinaryWithWorkers(t *testing.T, binary, workers string) (stdout, stderr string, code int, timedOut bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), mtRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(), "SURGE_THREADS="+workers)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()

	stdout, stderr = outBuf.String(), errBuf.String()
	if ctx.Err() != nil {
		return stdout, stderr, 0, true
	}
	if err == nil {
		return stdout, stderr, 0, false
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run %s: %v\nstderr:\n%s", binary, err, stderr)
	}
	return stdout, stderr, exitErr.ExitCode(), false
}
