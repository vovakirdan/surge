//go:build runtime_v2_pending

package vm_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	remoteHarnessConsumerModeEnv = "SURGE_TEST_REMOTE_HARNESS_CONSUMER_MODE"
	remoteHarnessRunErrSentinel  = "sentinel remote harness background reap"
)

func TestRemotePublicationHarnessReportsCancellationRunError(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestRemotePublicationHarnessCancellationConsumerHelper$")
	cmd.Env = overrideEnvVar(os.Environ(), remoteHarnessConsumerModeEnv, "timeout")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("remote harness consumer helper unexpectedly passed\noutput:\n%s", output)
	}
	compact := strings.Join(strings.Fields(string(output)), " ")
	want := "cancellation: run/wait: " + remoteHarnessRunErrSentinel + " termination: <nil>"
	if !strings.Contains(compact, want) {
		t.Fatalf("remote harness hid its cancellation run/wait error\nwant segment: %q\noutput:\n%s", want, output)
	}
}

func TestRemotePublicationHarnessCancellationConsumerHelper(t *testing.T) {
	if os.Getenv(remoteHarnessConsumerModeEnv) == "" {
		return
	}
	previousRunner := runHarnessCommandWithCancellation
	runHarnessCommandWithCancellation = func(context.Context, *exec.Cmd, time.Duration) cancellationCommandResult {
		return cancellationCommandResult{
			commandResult: commandResult{duration: time.Millisecond},
			runErr:        errors.New(remoteHarnessRunErrSentinel),
			contextErr:    context.DeadlineExceeded,
		}
	}
	defer func() { runHarnessCommandWithCancellation = previousRunner }()

	probePath := filepath.Join(t.TempDir(), "missing-remote-harness-probe")
	runRemotePublicationHarness(t, probePath, "consumer-probe", os.Environ())
	t.Fatal("runRemotePublicationHarness unexpectedly returned from injected timeout")
}
