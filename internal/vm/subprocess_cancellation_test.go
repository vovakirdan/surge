package vm_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

const (
	subprocessTerminationGrace = 250 * time.Millisecond
	subprocessKillWait         = time.Second
)

type cancellationCommandResult struct {
	commandResult
	runErr     error
	contextErr error
	signalErr  error
}

func formatCancellationDiagnostics(result cancellationCommandResult) string {
	return fmt.Sprintf("run/wait:\n%v\ntermination:\n%v", result.runErr, result.signalErr)
}

type cancellationWaitResult struct {
	waitErr    error
	contextErr error
	signalErr  error
}

type commandCancellationLifecycle interface {
	wait(context.Context, *exec.Cmd, time.Duration) cancellationWaitResult
}

// synchronizedBuffer permits a bounded signal-failure return while the sole
// eventual Wait still owns command I/O cleanup in the background.
type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *synchronizedBuffer) snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// runCommandWithCancellation owns the command's sole Wait responsibility. On
// Linux, cancellation also terminates descendants that remain in the inherited
// process group. A descendant that creates a new session or process group is
// outside that contract. If every kill attempt fails, the bounded return hands
// that sole Wait to an eventual background reap.
func runCommandWithCancellation(
	ctx context.Context,
	cmd *exec.Cmd,
	terminationGrace time.Duration,
) cancellationCommandResult {
	return runCommandWithCancellationLifecycle(
		ctx,
		cmd,
		terminationGrace,
		platformCancellationLifecycle(),
	)
}

func runCommandWithCancellationLifecycle(
	ctx context.Context,
	cmd *exec.Cmd,
	terminationGrace time.Duration,
	lifecycle commandCancellationLifecycle,
) cancellationCommandResult {
	var outBuf, errBuf synchronizedBuffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	configureCancellationTarget(cmd)
	if cmd.WaitDelay == 0 || cmd.WaitDelay > subprocessKillWait {
		cmd.WaitDelay = subprocessKillWait
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return cancellationCommandResult{
			commandResult: commandResult{
				stdout:   outBuf.snapshot(),
				stderr:   errBuf.snapshot(),
				exitCode: -1,
				duration: time.Since(start),
			},
			runErr: err,
		}
	}

	waitResult := lifecycle.wait(ctx, cmd, terminationGrace)

	result := cancellationCommandResult{
		commandResult: commandResult{
			stdout:   outBuf.snapshot(),
			stderr:   errBuf.snapshot(),
			duration: time.Since(start),
		},
		contextErr: waitResult.contextErr,
		signalErr:  waitResult.signalErr,
	}
	if waitResult.waitErr == nil {
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(waitResult.waitErr, &exitErr) {
		result.exitCode = exitErr.ExitCode()
		result.exitErr = exitErr
		return result
	}
	result.exitCode = -1
	result.runErr = waitResult.waitErr
	return result
}
