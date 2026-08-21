package goldencheck

import (
	"bytes"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// signalDeadlockGuard bounds the two waits the signal test performs. Neither
// bound is an assertion. The script answers a trapped signal by running
// `exit 128+n`, so the handler running IS the process exiting - there is no
// earlier event to wait on, and the wait can only ever be a wall clock. The
// guard exists to turn a genuine hang into a named failure instead of a
// package-timeout panic, so it is set far above any scheduling delay these
// waits can legitimately suffer.
//
// Measured on a 32-core machine with a full `go test ./...` running alongside
// 128 CPU spinners (load average 147): start-to-marker peaked at 326ms over 496
// runs and kill-to-reap at 158ms over 1339 signal deliveries. A 5s bound leaves
// margins of only 15x and 32x under that load, thin enough that it has run out
// in a pre-commit run and failed a commit that had nothing to do with this
// test.
//
// Re-measured independently 2026-08-21 at 03cba4d8, 32 spinners plus a full
// tagged `internal/vm` run alongside (load average 20.6): 40 consecutive runs,
// 120 subtest executions, zero failures, slowest subtest 0.19s end to end.
// Two measurements on different loads agreeing on the same order of magnitude
// is why the bound is set against a peak rather than against an average.
//
// THE BUDGET ARITHMETIC, because the previous note got it wrong: each subtest
// carries TWO of these bounds, not one - waitForMarker below, then the
// post-kill guard - so the worst case is 3 x 2 x the bound. At 60s that was
// 360s against a 300s package timeout (Makefile:93), i.e. a hang would have
// killed the package before this test could name itself, which is the opposite
// of what the bound is for. At 45s the worst case is 270s and the property
// holds. The margin is still 236x over the slowest subtest measured.
const signalDeadlockGuard = 45 * time.Second

func TestGoldenScriptSignalAfterInstallRestoresLiveCorpus(t *testing.T) {
	fixture := newScriptFixture(t, "valid.sg")
	goldenRoot := filepath.Join(fixture.root, "testdata", "golden")
	before, err := Scan(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	cmd := commandWithMoveFault(t, fixture, "signal-after-install")
	output, runErr := cmd.CombinedOutput()
	if runErr == nil {
		t.Fatalf("script accepted post-install TERM\n%s", output)
	}
	after, err := Scan(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	if changes := Diff(before, after); len(changes) != 0 {
		t.Fatalf("post-install TERM changed live corpus: %#v\n%s", changes, output)
	}
	assertNoGoldenStaging(t, fixture.root)
}

func TestGoldenScriptSignalsPreserveLiveCorpus(t *testing.T) {
	// status is the exit code the script's own trap installs for each signal.
	// Asserting it is what makes this test able to fail: a signal the script
	// does NOT trap still stops the shell and still runs the EXIT trap, so
	// "the script stopped" alone holds either way. What only the trap achieves
	// is a CONTROLLED exit - the untrapped shell dies by the signal and its
	// cleanup then observes $? == 0, which skips the rollback that sets aside a
	// half-installed corpus and puts the backup back.
	tests := []struct {
		name   string
		signal syscall.Signal
		status int
	}{
		{name: "INT", signal: syscall.SIGINT, status: 130},
		{name: "TERM", signal: syscall.SIGTERM, status: 143},
		{name: "HUP", signal: syscall.SIGHUP, status: 129},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScriptFixture(t, "valid.sg")
			goldenRoot := filepath.Join(fixture.root, "testdata", "golden")
			before, err := Scan(goldenRoot)
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(fixture.root, "blocked")
			cmd := fixture.command(t, "", nil, nil, nil)
			cmd.Env = append(cmd.Env, "BLOCK_PHASE=diagnostics", "BLOCK_MARKER="+marker)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if startErr := cmd.Start(); startErr != nil {
				t.Fatal(startErr)
			}
			waitForMarker(t, marker, cmd.Process.Pid)
			if signalErr := syscall.Kill(-cmd.Process.Pid, test.signal); signalErr != nil {
				t.Fatalf("send %s: %v", test.name, signalErr)
			}
			var guardFired atomic.Bool
			guard := time.AfterFunc(signalDeadlockGuard, func() {
				guardFired.Store(true)
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			})
			waitErr := cmd.Wait()
			guard.Stop()
			if guardFired.Load() {
				t.Fatalf("script did not stop within %v of %s\n%s", signalDeadlockGuard, test.name, output.String())
			}
			if waitErr == nil {
				t.Fatalf("script accepted %s\n%s", test.name, output.String())
			}
			if status := cmd.ProcessState.ExitCode(); status != test.status {
				t.Fatalf("script answered %s with exit status %d, want %d: the script must TRAP the signal and exit with it, not die by it - a shell that dies by the signal reports no exit status and its cleanup sees $? == 0, which disarms the corpus rollback\n%s",
					test.name, status, test.status, output.String())
			}
			after, err := Scan(goldenRoot)
			if err != nil {
				t.Fatal(err)
			}
			if changes := Diff(before, after); len(changes) != 0 {
				t.Fatalf("%s changed live corpus: %#v", test.name, changes)
			}
			assertNoGoldenStaging(t, fixture.root)
		})
	}
}

func waitForMarker(t *testing.T, marker string, processGroup int) {
	t.Helper()
	deadline := time.Now().Add(signalDeadlockGuard)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(-processGroup, syscall.SIGKILL)
	t.Fatalf("fake generator did not reach blocking phase within %v", signalDeadlockGuard)
}
