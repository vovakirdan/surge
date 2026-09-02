//go:build linux

package vm_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	processGroupHelperModeEnv = "SURGE_TEST_PROCESS_GROUP_HELPER_MODE"
	processGroupPIDFileEnv    = "SURGE_TEST_PROCESS_GROUP_PID_FILE"
)

type reapedProcess struct {
	pid    int
	status syscall.WaitStatus
	err    error
}

func TestProcessGroupCancellationHelper(t *testing.T) {
	switch os.Getenv(processGroupHelperModeEnv) {
	case "":
		return
	case "child":
		runProcessGroupChildHelper()
	case "grandchild":
		runProcessGroupGrandchildHelper()
	case "leader-exits":
		runProcessGroupLeaderExitHelper()
	case "ignore-term-grandchild":
		runProcessGroupIgnoreTermGrandchildHelper()
	case "ignore-term":
		runProcessGroupIgnoreTermHelper()
	case "setsid-leader":
		runSetsidLeaderHelper()
	case "setsid-grandchild":
		runSetsidGrandchildHelper()
	default:
		t.Fatalf("unknown process-group helper mode %q", os.Getenv(processGroupHelperModeEnv))
	}
}

func TestRunCommandWithCancellationReapsDescendantProcessGroup(t *testing.T) {
	skipTimeoutTests(t)
	pidFile := filepath.Join(t.TempDir(), "process-tree.pids")
	cmd := processGroupHelperCommand("child", pidFile)
	timeout := mtScaledTimeout(t, 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result := runCommandWithCancellation(ctx, cmd, subprocessTerminationGrace)
	if !errors.Is(result.contextErr, context.DeadlineExceeded) {
		t.Fatalf("context result: want deadline exceeded, got %v (run=%v, signal=%v, stderr=%q)",
			result.contextErr, result.runErr, result.signalErr, result.stderr)
	}
	if result.runErr != nil || result.signalErr != nil {
		t.Fatalf("cancel process group: run=%v signal=%v stderr=%q", result.runErr, result.signalErr, result.stderr)
	}

	childPID, grandchildPID := readProcessGroupPIDs(t, pidFile)
	if childPID != cmd.Process.Pid {
		t.Fatalf("child pid: helper reported %d, command started %d", childPID, cmd.Process.Pid)
	}
	requireProcessGone(t, childPID, "child")
	requireProcessGone(t, grandchildPID, "grandchild")
}

func TestRunCommandWithCancellationKillsTermResistantDescendantAfterLeaderExit(t *testing.T) {
	skipTimeoutTests(t)
	pidFile := filepath.Join(t.TempDir(), "term-resistant-tree.pids")
	cmd := processGroupHelperCommand("leader-exits", pidFile)
	timeout := mtScaledTimeout(t, 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	lifecycle := platformCancellationLifecycle().(linuxCancellationLifecycle)
	identityHeld := false
	lifecycle.beforeFinalKill = func(cmd *exec.Cmd) error {
		var info unix.Siginfo
		if err := unix.Waitid(
			unix.P_PID,
			cmd.Process.Pid,
			&info,
			unix.WEXITED|unix.WNOWAIT|unix.WNOHANG,
			nil,
		); err != nil {
			return fmt.Errorf("observe unreaped leader before group SIGKILL: %w", err)
		}
		if info.Signo == 0 {
			return errors.New("leader was not waitable before group SIGKILL")
		}
		if err := unix.Kill(cmd.Process.Pid, 0); err != nil {
			return fmt.Errorf("probe unreaped leader before group SIGKILL: %w", err)
		}
		identityHeld = true
		return nil
	}
	resultCh := make(chan cancellationCommandResult, 1)
	go func() {
		resultCh <- runCommandWithCancellationLifecycle(ctx, cmd, subprocessTerminationGrace, lifecycle)
	}()
	childPID, grandchildPID := waitForProcessGroupPIDs(t, pidFile, timeout)
	grandchildExitCh := observeLinuxProcessExit(grandchildPID)
	reaped := false
	t.Cleanup(func() {
		if reaped {
			return
		}
		process, findErr := os.FindProcess(grandchildPID)
		if findErr == nil {
			killErr := process.Kill()
			if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				t.Errorf("cleanup TERM-resistant grandchild %d: %v", grandchildPID, killErr)
			}
		}
		if observeErr := awaitObservedProcessExit(grandchildExitCh); observeErr != nil {
			t.Errorf("observe cleanup of TERM-resistant grandchild %d: %v", grandchildPID, observeErr)
			return
		}
		cleanupResult := reapLinuxChild(grandchildPID)
		reaped = true
		if cleanupResult.err != nil {
			t.Errorf("reap cleanup of TERM-resistant grandchild %d: %v", grandchildPID, cleanupResult.err)
		}
	})

	result := <-resultCh
	if !errors.Is(result.contextErr, context.DeadlineExceeded) {
		t.Fatalf("context result: want deadline exceeded, got %v (run=%v, signal=%v, stderr=%q)",
			result.contextErr, result.runErr, result.signalErr, result.stderr)
	}
	if result.runErr != nil || result.signalErr != nil {
		t.Fatalf("cancel process group after leader exit: run=%v signal=%v stderr=%q",
			result.runErr, result.signalErr, result.stderr)
	}
	if !identityHeld {
		t.Fatal("leader identity was not held through the final process-group signal")
	}
	if result.exitErr == nil {
		t.Fatal("leader exit status: want SIGTERM, command exited successfully")
	}
	leaderStatus, ok := result.exitErr.Sys().(syscall.WaitStatus)
	if !ok || !leaderStatus.Signaled() || leaderStatus.Signal() != syscall.SIGTERM {
		t.Fatalf("leader exit status: want SIGTERM, got %#v", result.exitErr.Sys())
	}
	if childPID != cmd.Process.Pid {
		t.Fatalf("child pid: helper reported %d, command started %d", childPID, cmd.Process.Pid)
	}
	if observeErr := awaitObservedProcessExit(grandchildExitCh); observeErr != nil {
		t.Fatalf("grandchild process %d survived cancellation after leader Wait: %v", grandchildPID, observeErr)
	}
	reapedResult := reapLinuxChild(grandchildPID)
	reaped = true
	if reapedResult.err != nil {
		t.Fatalf("reap TERM-resistant grandchild %d: %v", grandchildPID, reapedResult.err)
	}
	if reapedResult.pid != grandchildPID || !reapedResult.status.Signaled() || reapedResult.status.Signal() != syscall.SIGKILL {
		t.Fatalf("grandchild wait status: pid=%d status=%#v, want pid=%d SIGKILL",
			reapedResult.pid, reapedResult.status, grandchildPID)
	}
	requireProcessGone(t, childPID, "child")
	requireProcessGone(t, grandchildPID, "grandchild")
}

func TestRunCommandWithCancellationEscalatesToKill(t *testing.T) {
	skipTimeoutTests(t)
	readyFile := filepath.Join(t.TempDir(), "ignore-term.ready")
	cmd := processGroupHelperCommand("ignore-term", readyFile)
	timeout := mtScaledTimeout(t, 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result := runCommandWithCancellation(ctx, cmd, 50*time.Millisecond)
	if !errors.Is(result.contextErr, context.DeadlineExceeded) {
		t.Fatalf("context result: want deadline exceeded, got %v", result.contextErr)
	}
	if result.runErr != nil || result.signalErr != nil {
		t.Fatalf("kill escalation: run=%v signal=%v stderr=%q", result.runErr, result.signalErr, result.stderr)
	}
	if _, err := os.Stat(readyFile); err != nil {
		t.Fatalf("TERM-ignore helper was not ready before timeout: %v", err)
	}
	if result.exitErr == nil {
		t.Fatalf("termination status: want SIGKILL, command exited successfully")
	}
	status, ok := result.exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("termination status: want SIGKILL, got %#v", result.exitErr.Sys())
	}
}

func processGroupHelperCommand(mode, pidFile string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessGroupCancellationHelper$")
	env := overrideEnvVar(os.Environ(), processGroupHelperModeEnv, mode)
	cmd.Env = overrideEnvVar(env, processGroupPIDFileEnv, pidFile)
	return cmd
}

func runProcessGroupChildHelper() {
	termCh := make(chan os.Signal, 1)
	signal.Notify(termCh, syscall.SIGTERM)
	grandchild, err := startReadyProcessGroupGrandchild("grandchild", false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	pidFile := os.Getenv(processGroupPIDFileEnv)
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d %d\n", os.Getpid(), grandchild.Process.Pid)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write process ids: %v\n", err)
		os.Exit(2)
	}

	<-termCh
	if err := grandchild.Wait(); err == nil {
		fmt.Fprintln(os.Stderr, "grandchild exited without SIGTERM")
		os.Exit(2)
	}
	os.Exit(0)
}

func runProcessGroupLeaderExitHelper() {
	grandchild, err := startReadyProcessGroupGrandchild("ignore-term-grandchild", true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	pidFile := os.Getenv(processGroupPIDFileEnv)
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d %d\n", os.Getpid(), grandchild.Process.Pid)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write process ids: %v\n", err)
		os.Exit(2)
	}
	// Keep SIGTERM at its default disposition: the leader exits immediately
	// while the grandchild remains in the group and ignores the same signal.
	blockCh := make(chan os.Signal, 1)
	signal.Notify(blockCh, syscall.SIGUSR1)
	<-blockCh
}

func startReadyProcessGroupGrandchild(mode string, cloneParent bool) (*exec.Cmd, error) {
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create grandchild ready pipe: %w", err)
	}
	defer readyReader.Close()
	grandchild := processGroupHelperCommand(mode, "")
	if cloneParent {
		grandchild.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_PARENT}
	}
	grandchild.ExtraFiles = []*os.File{readyWriter}
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = readyWriter.Close()
		return nil, fmt.Errorf("open null output: %w", err)
	}
	grandchild.Stdout = devNull
	grandchild.Stderr = devNull
	if err := grandchild.Start(); err != nil {
		_ = readyWriter.Close()
		_ = devNull.Close()
		return nil, fmt.Errorf("start grandchild: %w", err)
	}
	_ = readyWriter.Close()
	_ = devNull.Close()
	var ready [1]byte
	if _, err := io.ReadFull(readyReader, ready[:]); err != nil {
		_ = grandchild.Process.Kill()
		return nil, fmt.Errorf("wait for grandchild readiness: %w", err)
	}
	return grandchild, nil
}

func runProcessGroupGrandchildHelper() {
	notifyProcessGroupGrandchildReady()
	blockCh := make(chan os.Signal, 1)
	signal.Notify(blockCh, syscall.SIGUSR1)
	<-blockCh
}

func runProcessGroupIgnoreTermGrandchildHelper() {
	signal.Ignore(syscall.SIGTERM)
	notifyProcessGroupGrandchildReady()
	blockCh := make(chan os.Signal, 1)
	signal.Notify(blockCh, syscall.SIGUSR1)
	<-blockCh
}

func notifyProcessGroupGrandchildReady() {
	ready := os.NewFile(3, "grandchild-ready")
	if ready == nil {
		os.Exit(2)
	}
	if _, err := ready.Write([]byte{1}); err != nil {
		os.Exit(2)
	}
	_ = ready.Close()
}

func runProcessGroupIgnoreTermHelper() {
	signal.Ignore(syscall.SIGTERM)
	if err := os.WriteFile(os.Getenv(processGroupPIDFileEnv), []byte("ready\n"), 0o600); err != nil {
		os.Exit(2)
	}
	blockCh := make(chan os.Signal, 1)
	signal.Notify(blockCh, syscall.SIGUSR1)
	<-blockCh
}

func readProcessGroupPIDs(t *testing.T, path string) (int, int) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read process ids (helper did not become ready before timeout): %v", err)
	}
	childPID, grandchildPID, err := parseProcessGroupPIDs(raw)
	if err != nil {
		t.Fatal(err)
	}
	return childPID, grandchildPID
}

func waitForProcessGroupPIDs(t *testing.T, path string, timeout time.Duration) (int, int) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			childPID, grandchildPID, parseErr := parseProcessGroupPIDs(raw)
			if parseErr == nil {
				return childPID, grandchildPID
			}
			lastErr = parseErr
		} else {
			lastErr = err
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("wait for process ids: %v", lastErr)
		}
	}
}

func parseProcessGroupPIDs(raw []byte) (int, int, error) {
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("process id file: want two fields, got %q", raw)
	}
	childPID, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse child pid %q: %w", fields[0], err)
	}
	grandchildPID, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse grandchild pid %q: %w", fields[1], err)
	}
	return childPID, grandchildPID, nil
}

func requireProcessGone(t *testing.T, pid int, role string) {
	t.Helper()
	err := syscall.Kill(pid, 0)
	if err == nil {
		t.Fatalf("%s process %d survived process-group timeout", role, pid)
	}
	if !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("probe %s process %d: %v", role, pid, err)
	}
}

// awaitObservedProcessExit waits for a WNOWAIT observation of the process to
// land. The budget is subprocessKillWait for every caller on purpose: it is the
// same bound the supervisor gives its own final group signal, so a test that
// waited longer would report a pass the gate would not.
func awaitObservedProcessExit(exitCh <-chan error) error {
	timer := time.NewTimer(subprocessKillWait)
	defer timer.Stop()
	select {
	case err := <-exitCh:
		return err
	case <-timer.C:
		return fmt.Errorf("process exit was not observable within %s", subprocessKillWait)
	}
}

func reapLinuxChild(pid int) reapedProcess {
	var status syscall.WaitStatus
	reapedPID, err := syscall.Wait4(pid, &status, 0, nil)
	return reapedProcess{pid: reapedPID, status: status, err: err}
}
