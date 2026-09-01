//go:build linux

package vm_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRunCommandWithCancellationSignalFailureReturnsBoundedly(t *testing.T) {
	skipTimeoutTests(t)
	readyFile := filepath.Join(t.TempDir(), "signal-failure.ready")
	cmd := processGroupHelperCommand("ignore-term", readyFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	groupErr := errors.New("injected process-group signal failure")
	directErr := errors.New("injected direct signal failure")
	lifecycle := platformCancellationLifecycle().(linuxCancellationLifecycle)
	lifecycle.signalGroup = func(int, syscall.Signal) error { return groupErr }
	lifecycle.signalProcess = func(*os.Process, syscall.Signal) error { return directErr }

	resultCh := make(chan cancellationCommandResult, 1)
	go func() {
		resultCh <- runCommandWithCancellationLifecycle(ctx, cmd, 0, lifecycle)
	}()
	waitForMarkerFile(t, readyFile, mtScaledTimeout(t, 2*time.Second))
	cleanupArmed := true
	t.Cleanup(func() {
		if !cleanupArmed {
			return
		}
		_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
	})
	cancel()

	var result cancellationCommandResult
	select {
	case result = <-resultCh:
	case <-time.After(mtScaledTimeout(t, 3*time.Second)):
		t.Fatal("signal-failure cancellation did not return within its bounded wait")
	}
	if !errors.Is(result.contextErr, context.Canceled) {
		t.Fatalf("context result: want canceled, got %v", result.contextErr)
	}
	if result.runErr == nil || !strings.Contains(result.runErr.Error(), "reap continues in background") {
		t.Fatalf("run result: want bounded eventual-reap error, got %v", result.runErr)
	}
	if !errors.Is(result.signalErr, groupErr) || !errors.Is(result.signalErr, directErr) {
		t.Fatalf("signal result: want both injected failures, got %v", result.signalErr)
	}

	if err := unix.Kill(-cmd.Process.Pid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		t.Fatalf("cleanup failed-signal process group: %v", err)
	}
	waitForPIDAbsent(t, cmd.Process.Pid, mtScaledTimeout(t, 2*time.Second))
	cleanupArmed = false
}

func TestRunCommandWithCancellationBoundsEscapedInheritedPipe(t *testing.T) {
	skipTimeoutTests(t)
	pidFile := filepath.Join(t.TempDir(), "setsid-tree.pids")
	cmd := processGroupHelperCommand("setsid-leader", pidFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultCh := make(chan cancellationCommandResult, 1)
	go func() {
		resultCh <- runCommandWithCancellation(ctx, cmd, subprocessTerminationGrace)
	}()
	leaderPID, escapedPID := waitForProcessGroupPIDs(t, pidFile, mtScaledTimeout(t, 2*time.Second))
	type reapedProcess struct {
		pid    int
		status syscall.WaitStatus
		err    error
	}
	reapedCh := make(chan reapedProcess, 1)
	go func() {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(escapedPID, &status, 0, nil)
		reapedCh <- reapedProcess{pid: pid, status: status, err: err}
	}()
	reaped := false
	t.Cleanup(func() {
		if reaped {
			return
		}
		_ = unix.Kill(escapedPID, unix.SIGKILL)
		<-reapedCh
	})

	cancel()
	var result cancellationCommandResult
	select {
	case result = <-resultCh:
	case <-time.After(mtScaledTimeout(t, 3*time.Second)):
		t.Fatal("escaped inherited pipe kept command Wait blocked past WaitDelay")
	}
	if leaderPID != cmd.Process.Pid {
		t.Fatalf("leader pid: helper reported %d, command started %d", leaderPID, cmd.Process.Pid)
	}
	if !errors.Is(result.contextErr, context.Canceled) {
		t.Fatalf("context result: want canceled, got %v", result.contextErr)
	}
	if result.runErr != nil || result.signalErr != nil {
		t.Fatalf("cancel leader with escaped pipe holder: run=%v signal=%v stderr=%q",
			result.runErr, result.signalErr, result.stderr)
	}
	if err := unix.Kill(escapedPID, 0); err != nil {
		t.Fatalf("setsid descendant should be outside inherited-group kill contract: %v", err)
	}
	if err := unix.Kill(escapedPID, unix.SIGKILL); err != nil {
		t.Fatalf("kill escaped descendant: %v", err)
	}
	reapedResult := <-reapedCh
	reaped = true
	if reapedResult.err != nil || reapedResult.pid != escapedPID ||
		!reapedResult.status.Signaled() || reapedResult.status.Signal() != syscall.SIGKILL {
		t.Fatalf("escaped descendant wait: pid=%d status=%#v err=%v, want pid=%d SIGKILL",
			reapedResult.pid, reapedResult.status, reapedResult.err, escapedPID)
	}
	requireProcessGone(t, leaderPID, "setsid leader")
	requireProcessGone(t, escapedPID, "setsid descendant")
}

func runSetsidLeaderHelper() {
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	grandchild := processGroupHelperCommand("setsid-grandchild", "")
	grandchild.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_PARENT,
		Setsid:     true,
	}
	grandchild.ExtraFiles = []*os.File{readyWriter}
	grandchild.Stdout = os.Stdout
	grandchild.Stderr = os.Stderr
	if err := grandchild.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	_ = readyWriter.Close()
	var ready [1]byte
	if _, err := io.ReadFull(readyReader, ready[:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	_ = readyReader.Close()
	pidFile := os.Getenv(processGroupPIDFileEnv)
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d %d\n", os.Getpid(), grandchild.Process.Pid)), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	blockCh := make(chan os.Signal, 1)
	signal.Notify(blockCh, syscall.SIGUSR1)
	<-blockCh
}

func runSetsidGrandchildHelper() {
	signal.Ignore(syscall.SIGTERM)
	notifyProcessGroupGrandchildReady()
	blockCh := make(chan os.Signal, 1)
	signal.Notify(blockCh, syscall.SIGUSR1)
	<-blockCh
}

func waitForMarkerFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("marker %q was not created before timeout", path)
		}
	}
}

func waitForPIDAbsent(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		err := unix.Kill(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			return
		}
		if err != nil {
			t.Fatalf("probe process %d: %v", pid, err)
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("process %d was not eventually reaped", pid)
		}
	}
}
