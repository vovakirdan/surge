//go:build !linux

package vm_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

func configureCancellationTarget(_ *exec.Cmd) {}

type directChildCancellationLifecycle struct{}

func platformCancellationLifecycle() commandCancellationLifecycle {
	return directChildCancellationLifecycle{}
}

func (directChildCancellationLifecycle) wait(
	ctx context.Context,
	cmd *exec.Cmd,
	terminationGrace time.Duration,
) cancellationWaitResult {
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	select {
	case waitErr := <-waitCh:
		return cancellationWaitResult{waitErr: waitErr}
	case <-ctx.Done():
		return terminateDirectChild(ctx.Err(), cmd, waitCh, terminationGrace)
	}
}

func terminateDirectChild(
	contextErr error,
	cmd *exec.Cmd,
	waitCh <-chan error,
	terminationGrace time.Duration,
) cancellationWaitResult {
	termErr := cmd.Process.Signal(os.Interrupt)
	if errors.Is(termErr, os.ErrProcessDone) {
		termErr = nil
	}
	if terminationGrace > 0 {
		timer := time.NewTimer(terminationGrace)
		select {
		case waitErr := <-waitCh:
			timer.Stop()
			return cancellationWaitResult{
				waitErr:    waitErr,
				contextErr: contextErr,
				signalErr:  termErr,
			}
		case <-timer.C:
		}
	}
	killErr := cmd.Process.Kill()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	timer := time.NewTimer(subprocessKillWait)
	defer timer.Stop()
	select {
	case waitErr := <-waitCh:
		return cancellationWaitResult{
			waitErr:    waitErr,
			contextErr: contextErr,
			signalErr:  errors.Join(termErr, killErr),
		}
	case <-timer.C:
		return cancellationWaitResult{
			waitErr: fmt.Errorf(
				"direct child %d did not exit within %s after kill; reap continues in background",
				cmd.Process.Pid,
				subprocessKillWait,
			),
			contextErr: contextErr,
			signalErr:  errors.Join(termErr, killErr),
		}
	}
}
