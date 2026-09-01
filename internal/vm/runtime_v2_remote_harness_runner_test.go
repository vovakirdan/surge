//go:build runtime_v2_pending

package vm_test

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

const remotePublicationHarnessTimeout = 30 * time.Second

func runRemotePublicationHarness(t *testing.T, bin, mode string, env []string) (string, string, int) {
	t.Helper()
	timeout := mtScaledTimeout(t, remotePublicationHarnessTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.Command(bin, mode)
	cmd.Env = env
	result := runCommandWithCancellation(ctx, cmd, subprocessTerminationGrace)
	if result.contextErr != nil {
		t.Fatalf("remote publication harness mode %q timed out after %s\nstdout:\n%s\nstderr:\n%s\ncancellation:\n%s",
			mode, timeout, result.stdout, result.stderr, formatCancellationDiagnostics(result))
	}
	if result.runErr != nil {
		t.Fatalf("run remote publication harness mode %q: %v\nstderr:\n%s", mode, result.runErr, result.stderr)
	}
	return result.stdout, result.stderr, result.exitCode
}
