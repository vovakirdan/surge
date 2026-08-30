package vm_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This file exists because of RV2-DEBT-311, and because of what that row
// taught: `runBinaryUnderValgrind` used to kill the process on timeout and
// only then format the failure, so a guest that overran its budget was
// reported as four lines of Memcheck banner and nothing else. That text says
// the program did not finish. It does not say what the program was DOING,
// which is the only question worth asking of a run that overran, and the
// evidence to answer it died with the process.
//
// Two probes are asked BEFORE the kill, and they answer different halves.
//
// The /proc half is always available and cannot fail: per-thread run state
// plus a CPU-tick delta across a short window. That delta alone separates the
// two shapes a wedged runtime can have -- every thread blocked (a lost wakeup)
// against a core pinned at 100% (a spin) -- and it needs no tooling.
//
// The vgdb half is the one that names the code. `vgdb --pid=N v.info
// scheduler` ptraces valgrind into answering even when every guest thread sits
// inside a syscall, and prints each thread's scheduler status and stack in the
// GUEST program's own symbols. A native debugger attached to the same pid
// cannot do that: it sees valgrind's synthetic CPU, not the program running on
// it. Measured against valgrind 3.22.0, the version the self-hosted lane pins.
//
// A probe that fails silently would be worse than no probe, because the next
// occurrence would again teach nothing and this time look instrumented. So
// every branch below records either its answer or the reason it has none.

// valgrindWedgeBudgets bounds the probe itself. A wedged guest must not be
// replaced in the report by a wedged probe: the whole point is that the
// failure text arrives.
const (
	valgrindWedgeSampleWindow = 1 * time.Second
	valgrindWedgeVgdbBudget   = 20 * time.Second
)

// valgrindWedgeReport photographs the valgrind process pid while it is still
// alive and returns the text to fold into the timeout failure. childEnv is the
// environment valgrind itself was started with, and it is REQUIRED rather than
// convenient -- see vgdbTmpdir. It never fails and never blocks longer than
// valgrindWedgeSampleWindow + valgrindWedgeVgdbBudget: a report that can
// itself hang is not a report.
func valgrindWedgeReport(pid int, budget time.Duration, childEnv []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "wedge probe: valgrind pid %d overran %s; state BEFORE the kill\n", pid, budget)
	b.WriteString(procThreadPhotograph(pid, valgrindWedgeSampleWindow))
	b.WriteString(vgdbGuestStacks(pid, valgrindWedgeVgdbBudget, vgdbTmpdir(childEnv)))
	return b.String()
}

// vgdbTmpdir returns the temporary directory valgrind will have used for its
// gdbserver FIFOs, read out of the environment VALGRIND was given rather than
// the one this test process happens to hold.
//
// Both sides derive the default --vgdb-prefix from TMPDIR, falling back to
// /tmp. They are separate processes and the helper hands valgrind an explicit
// env, so the two can disagree -- and when they do, vgdb answers `no FIFO
// found matching pid N` and the probe degrades into a report that the probe
// did not work. Measured: a valgrind started under one TMPDIR is invisible to
// a vgdb run under another, with exactly that message. Inheriting was the
// first thing tried here and it was wrong.
func vgdbTmpdir(childEnv []string) string {
	for _, entry := range childEnv {
		if value, ok := strings.CutPrefix(entry, "TMPDIR="); ok && value != "" {
			return value
		}
	}
	// Not inherited: valgrind with no TMPDIR uses /tmp, so vgdb must too,
	// whatever this process's own TMPDIR says.
	return "/tmp"
}

// procThreadPhotograph reports each thread's run state and the process-wide
// CPU-tick delta over window. The delta is the discriminator: ~0 ticks means
// every thread is parked and the runtime lost a wakeup, while ticks
// accumulating at roughly one core's worth per second means it is spinning.
func procThreadPhotograph(pid int, window time.Duration) string {
	var b strings.Builder
	b.WriteString("  -- /proc thread state --\n")

	before, beforeErr := procCPUTicks(pid)
	time.Sleep(window)
	after, afterErr := procCPUTicks(pid)
	switch {
	case beforeErr != nil:
		fmt.Fprintf(&b, "  cpu ticks: unreadable (%v)\n", beforeErr)
	case afterErr != nil:
		fmt.Fprintf(&b, "  cpu ticks: process left between samples (%v)\n", afterErr)
	default:
		fmt.Fprintf(&b,
			"  cpu ticks: %d -> %d (delta %d over %s; ~0 = every thread parked, ~%d = one core spinning)\n",
			before, after, after-before, window, int64(window/time.Second)*100)
	}

	tasks, err := filepath.Glob(fmt.Sprintf("/proc/%d/task/*", pid))
	if err != nil || len(tasks) == 0 {
		fmt.Fprintf(&b, "  threads: unreadable (%v)\n", err)
		return b.String()
	}
	sort.Strings(tasks)
	for _, task := range tasks {
		tid := filepath.Base(task)
		fmt.Fprintf(&b, "  tid=%s state=%s wchan=%s syscall=%s\n",
			tid,
			procField(filepath.Join(task, "status"), "State:"),
			procOneLine(filepath.Join(task, "wchan")),
			firstToken(procOneLine(filepath.Join(task, "syscall"))))
	}
	return b.String()
}

// vgdbGuestStacks asks valgrind's own gdbserver for the guest-side scheduler
// state. Its failures are reported rather than swallowed, because "vgdb is
// not installed on this host" and "vgdb answered and the guest was idle" are
// opposite conclusions that must not share an empty section.
func vgdbGuestStacks(pid int, budget time.Duration, tmpdir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  -- vgdb v.info scheduler (guest threads, budget %s, TMPDIR=%s) --\n", budget, tmpdir)

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	// #nosec G204 -- fixed vgdb invocation over a pid this process started
	cmd := exec.CommandContext(ctx, "vgdb", "--pid="+strconv.Itoa(pid), "v.info", "scheduler")
	cmd.Env = append(envWithoutTmpdir(os.Environ()), "TMPDIR="+tmpdir)
	out, err := cmd.CombinedOutput()
	text := strings.TrimRight(string(out), "\n")
	switch {
	case ctx.Err() != nil:
		fmt.Fprintf(&b, "  vgdb did not answer within %s -- valgrind is wedged below its own gdbserver\n", budget)
	case err != nil:
		fmt.Fprintf(&b, "  vgdb failed: %v\n", err)
	}
	if text == "" {
		b.WriteString("  (no output)\n")
		return b.String()
	}
	for _, line := range strings.Split(text, "\n") {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

// procCPUTicks returns utime+stime for pid. It parses from the LAST ')'
// because field 2 is the executable name in parentheses and may itself
// contain spaces and parentheses, which splitting the whole line would
// misread.
func procCPUTicks(pid int) (int64, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	commEnd := strings.LastIndex(string(raw), ")")
	if commEnd < 0 {
		return 0, fmt.Errorf("no comm field in /proc/%d/stat", pid)
	}
	// After "pid (comm) " the remaining fields start at state, which is
	// field 3, so utime (14) and stime (15) are offsets 11 and 12 here.
	fields := strings.Fields(string(raw)[commEnd+1:])
	if len(fields) < 13 {
		return 0, fmt.Errorf("short /proc/%d/stat: %d fields after comm", pid, len(fields))
	}
	utime, err := strconv.ParseInt(fields[11], 10, 64)
	if err != nil {
		return 0, err
	}
	stime, err := strconv.ParseInt(fields[12], 10, 64)
	if err != nil {
		return 0, err
	}
	return utime + stime, nil
}

func procField(path, prefix string) string {
	raw, err := os.ReadFile(path) // #nosec G304 -- a /proc path this process built
	if err != nil {
		return "unreadable"
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return "absent"
}

func procOneLine(path string) string {
	raw, err := os.ReadFile(path) // #nosec G304 -- a /proc path this process built
	if err != nil {
		// wchan and syscall are privileged on current kernels; an
		// unprivileged reader gets a refusal here rather than a wrong answer.
		return "unreadable"
	}
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return "empty"
	}
	return line
}

func envWithoutTmpdir(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "TMPDIR=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func firstToken(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

// TestRuntimeV2ValgrindWedgeProbeSeparatesParkedFromSpinning exercises the
// probe against the two shapes it exists to tell apart, using ordinary
// processes rather than a wedged valgrind: a parked one must photograph as
// no CPU movement and a spinning one as roughly a core's worth, or the
// delta in the failure text means nothing and the next occurrence of
// RV2-DEBT-311 is again undiagnosable. The vgdb section is asserted to be
// PRESENT and non-empty rather than to contain guest stacks -- these pids
// are not valgrind, so the honest answer here is vgdb's refusal, and a
// probe that omitted the section instead of recording the refusal is the
// silent-failure mode this test rules out.
func TestRuntimeV2ValgrindWedgeProbeSeparatesParkedFromSpinning(t *testing.T) {
	t.Parallel()

	parked := startProbeSubject(t, "sleep 30")
	spinning := startProbeSubject(t, "while :; do :; done")

	parkedReport := valgrindWedgeReport(parked, 42*time.Second, nil)
	spinningReport := valgrindWedgeReport(spinning, 42*time.Second, nil)

	for name, report := range map[string]string{"parked": parkedReport, "spinning": spinningReport} {
		for _, want := range []string{"cpu ticks:", "tid=", "state=", "vgdb v.info scheduler"} {
			if !strings.Contains(report, want) {
				t.Errorf("%s report is missing %q; a probe that omits a section teaches nothing:\n%s", name, want, report)
			}
		}
		if strings.Contains(report, "-- vgdb v.info scheduler") &&
			strings.Contains(report, "(no output)") &&
			!strings.Contains(report, "vgdb failed") &&
			!strings.Contains(report, "vgdb did not answer") {
			t.Errorf("%s report has an empty vgdb section with no stated reason:\n%s", name, report)
		}
	}

	parkedDelta := probeReportedDelta(t, parkedReport)
	spinningDelta := probeReportedDelta(t, spinningReport)
	if parkedDelta > 5 {
		t.Errorf("a parked process photographed as %d ticks; the delta cannot mean 'lost wakeup' if it moves while nothing runs", parkedDelta)
	}
	if spinningDelta < 20 {
		t.Errorf("a spinning process photographed as %d ticks over %s; the delta cannot mean 'spin' if it stays near zero while a core burns", spinningDelta, valgrindWedgeSampleWindow)
	}
}

// TestRuntimeV2ValgrindWedgeProbeReachesTheGuestUnderItsOwnTmpdir is the arm
// that would have caught the defect this probe shipped with on its first
// draft. valgrind is started under a TMPDIR of this test's choosing -- the
// helper hands the child an explicit environment, so this is the real
// arrangement and not a contrived one -- and the probe must still come back
// with the GUEST's frames. Reverting to an inherited environment turns the
// vgdb section into `no FIFO found matching pid`, which reads as "the guest
// was unreachable" when the truth is "the probe looked in the wrong
// directory".
func TestRuntimeV2ValgrindWedgeProbeReachesTheGuestUnderItsOwnTmpdir(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("valgrind"); err != nil {
		t.Skipf("valgrind not on PATH: %v", err)
	}
	if _, err := exec.LookPath("vgdb"); err != nil {
		t.Skipf("vgdb not on PATH: %v", err)
	}

	tmpdir := t.TempDir()
	childEnv := append(envWithoutTmpdir(os.Environ()), "TMPDIR="+tmpdir)
	// #nosec G204 -- a fixed invocation over a stock system binary
	cmd := exec.Command("valgrind", "--leak-check=full", "sleep", "60")
	cmd.Env = childEnv
	if err := cmd.Start(); err != nil {
		t.Fatalf("start valgrind: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	waitForVgdbFifo(t, tmpdir, cmd.Process.Pid)

	report := valgrindWedgeReport(cmd.Process.Pid, 42*time.Second, childEnv)
	if !strings.Contains(report, "sched status:") {
		t.Fatalf("vgdb did not reach valgrind's gdbserver; the probe cannot name what a wedged guest is doing:\n%s", report)
	}
	// VgTs_ prefixes valgrind's own thread-state names, and they appear only
	// on the guest-side listing -- their presence is what distinguishes
	// reaching the gdbserver from reaching the guest's threads through it.
	if !strings.Contains(report, "VgTs_") {
		t.Fatalf("vgdb answered but reported no guest thread state:\n%s", report)
	}
}

// waitForVgdbFifo blocks until valgrind has published the gdbserver FIFO for
// pid under tmpdir. Probing before it exists would measure the startup race
// rather than the probe.
func waitForVgdbFifo(t *testing.T, tmpdir string, pid int) {
	t.Helper()
	pattern := filepath.Join(tmpdir, fmt.Sprintf("vgdb-pipe-*-%d-*", pid))
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("valgrind published no gdbserver FIFO matching %q within 30s", pattern)
}

// TestRuntimeV2ValgrindWedgeProbeTmpdirFallsBackToTmp pins the other half of
// the same rule: an environment that does not carry TMPDIR is not a licence to
// read this process's, because valgrind with no TMPDIR uses /tmp.
func TestRuntimeV2ValgrindWedgeProbeTmpdirFallsBackToTmp(t *testing.T) {
	t.Parallel()
	for name, row := range map[string]struct {
		env  []string
		want string
	}{
		"carried":  {env: []string{"PATH=/usr/bin", "TMPDIR=/run/user/1000/x"}, want: "/run/user/1000/x"},
		"absent":   {env: []string{"PATH=/usr/bin"}, want: "/tmp"},
		"empty":    {env: []string{"TMPDIR="}, want: "/tmp"},
		"no-env":   {env: nil, want: "/tmp"},
		"prefixed": {env: []string{"NOTTMPDIR=/wrong", "TMPDIR=/right"}, want: "/right"},
	} {
		if got := vgdbTmpdir(row.env); got != row.want {
			t.Errorf("%s: vgdbTmpdir = %q, want %q", name, got, row.want)
		}
	}
}

// startProbeSubject runs script under sh, registers its teardown, and returns
// its pid.
func startProbeSubject(t *testing.T, script string) int {
	t.Helper()
	// #nosec G204 -- fixed literal scripts from this test
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start probe subject %q: %v", script, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// probeReportedDelta reads the delta back out of the report text, so the test
// asserts what a reader of a failure would actually see rather than
// recomputing it from the same sampler.
func probeReportedDelta(t *testing.T, report string) int64 {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		_, rest, found := strings.Cut(line, "(delta ")
		if !found {
			continue
		}
		token, _, _ := strings.Cut(rest, " ")
		delta, err := strconv.ParseInt(token, 10, 64)
		if err != nil {
			t.Fatalf("delta %q in report is not a number: %v", token, err)
		}
		return delta
	}
	t.Fatalf("no cpu-tick delta in report:\n%s", report)
	return 0
}
