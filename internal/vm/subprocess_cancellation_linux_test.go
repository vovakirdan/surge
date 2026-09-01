//go:build linux

package vm_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type linuxCancellationLifecycle struct {
	signalGroup     func(int, syscall.Signal) error
	signalProcess   func(*os.Process, syscall.Signal) error
	beforeFinalKill func(*exec.Cmd) error
}

func platformCancellationLifecycle() commandCancellationLifecycle {
	return linuxCancellationLifecycle{
		signalGroup: unix.Kill,
		signalProcess: func(process *os.Process, signal syscall.Signal) error {
			return process.Signal(signal)
		},
	}
}

func configureCancellationTarget(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
}

func (lifecycle linuxCancellationLifecycle) wait(
	ctx context.Context,
	cmd *exec.Cmd,
	terminationGrace time.Duration,
) cancellationWaitResult {
	exitCh := observeLinuxProcessExit(cmd.Process.Pid)
	select {
	case observeErr := <-exitCh:
		if observeErr != nil {
			eventuallyWaitCommand(cmd, nil)
			return cancellationWaitResult{waitErr: observeErr}
		}
		return cancellationWaitResult{waitErr: cmd.Wait()}
	case <-ctx.Done():
		return lifecycle.terminate(ctx.Err(), cmd, exitCh, terminationGrace)
	}
}

func (lifecycle linuxCancellationLifecycle) terminate(
	contextErr error,
	cmd *exec.Cmd,
	exitCh <-chan error,
	terminationGrace time.Duration,
) cancellationWaitResult {
	termErr := lifecycle.signalTarget(cmd, syscall.SIGTERM)
	var (
		observeErr error
		observed   bool
	)
	if terminationGrace > 0 {
		timer := time.NewTimer(terminationGrace)
		select {
		case observeErr = <-exitCh:
			observed = true
			<-timer.C
		case <-timer.C:
		}
		timer.Stop()
	}

	var hookErr error
	if lifecycle.beforeFinalKill != nil {
		hookErr = lifecycle.beforeFinalKill(cmd)
	}
	killErr := lifecycle.signalTarget(cmd, syscall.SIGKILL)
	signalErr := errors.Join(termErr, hookErr, killErr)

	if !observed {
		timer := time.NewTimer(subprocessKillWait)
		select {
		case observeErr = <-exitCh:
			observed = true
		case <-timer.C:
		}
		timer.Stop()
	}
	if !observed {
		eventuallyWaitCommand(cmd, exitCh)
		return cancellationWaitResult{
			waitErr: fmt.Errorf(
				"process-group leader %d did not exit within %s after SIGKILL; reap continues in background",
				cmd.Process.Pid,
				subprocessKillWait,
			),
			contextErr: contextErr,
			signalErr:  signalErr,
		}
	}
	if observeErr != nil {
		eventuallyWaitCommand(cmd, nil)
		return cancellationWaitResult{
			waitErr:    fmt.Errorf("observe process-group leader %d exit: %w", cmd.Process.Pid, observeErr),
			contextErr: contextErr,
			signalErr:  signalErr,
		}
	}
	return cancellationWaitResult{
		waitErr:    cmd.Wait(),
		contextErr: contextErr,
		signalErr:  signalErr,
	}
}

func observeLinuxProcessExit(pid int) <-chan error {
	exitCh := make(chan error, 1)
	go func() {
		var info unix.Siginfo
		for {
			err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			exitCh <- err
			return
		}
	}()
	return exitCh
}

func (lifecycle linuxCancellationLifecycle) signalTarget(cmd *exec.Cmd, signal syscall.Signal) error {
	err := lifecycle.signalGroup(-cmd.Process.Pid, signal)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	directErr := lifecycle.signalProcess(cmd.Process, signal)
	if errors.Is(directErr, os.ErrProcessDone) {
		directErr = nil
	}
	return errors.Join(
		fmt.Errorf("signal process group %d: %w", cmd.Process.Pid, err),
		directErr,
	)
}

// eventuallyWaitCommand is the one Wait owner after a bounded exceptional
// return. If every signal failed, immediate reap and bounded return cannot both
// be guaranteed; the child remains wait-owned and is reaped when it exits.
func eventuallyWaitCommand(cmd *exec.Cmd, exitCh <-chan error) {
	go func() {
		if exitCh != nil {
			<-exitCh
		}
		_ = cmd.Wait()
	}()
}
