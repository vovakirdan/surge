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
	runBinaryConsumerModeEnv      = "SURGE_TEST_RUN_BINARY_CONSUMER_MODE"
	runBinaryConsumerArtifactEnv  = "SURGE_TEST_RUN_BINARY_ARTIFACT_DIR"
	runBinaryTimeoutConsumerMode  = "timeout"
	runBinaryRunErrorConsumerMode = "run-error"
	runBinaryTimeoutSentinel      = "sentinel timeout background reap"
	runBinaryRunErrorSentinel     = "sentinel non-timeout wait failure"
)

func TestRunBinaryWithTimeoutReportsCancellationRunError(t *testing.T) {
	artifactsDir := t.TempDir()
	output := runRunBinaryConsumerProcess(t, runBinaryTimeoutConsumerMode, artifactsDir)
	compact := strings.Join(strings.Fields(output), " ")
	want := "cancellation: run/wait: " + runBinaryTimeoutSentinel + " termination: <nil> diagnostics:"
	if !strings.Contains(compact, want) {
		t.Fatalf("runBinaryWithTimeout hid its cancellation run/wait error\nwant segment: %q\noutput:\n%s", want, output)
	}
	requireSavedRunError(t, artifactsDir, runBinaryTimeoutSentinel)
}

func TestRunBinaryWithTimeoutSavesNonTimeoutRunError(t *testing.T) {
	artifactsDir := t.TempDir()
	runRunBinaryConsumerProcess(t, runBinaryRunErrorConsumerMode, artifactsDir)
	requireSavedRunError(t, artifactsDir, runBinaryRunErrorSentinel)
}

func TestRunBinaryCancellationConsumerHelper(t *testing.T) {
	mode := os.Getenv(runBinaryConsumerModeEnv)
	if mode == "" {
		return
	}
	artifactDir := os.Getenv(runBinaryConsumerArtifactEnv)
	if artifactDir == "" {
		t.Fatal("missing run-binary consumer artifact directory")
	}

	sentinel := runBinaryRunErrorSentinel
	contextErr := error(nil)
	if mode == runBinaryTimeoutConsumerMode {
		sentinel = runBinaryTimeoutSentinel
		contextErr = context.DeadlineExceeded
	} else if mode != runBinaryRunErrorConsumerMode {
		t.Fatalf("unknown run-binary consumer mode %q", mode)
	}

	previousRunner := runHarnessCommandWithCancellation
	runHarnessCommandWithCancellation = func(context.Context, *exec.Cmd, time.Duration) cancellationCommandResult {
		return cancellationCommandResult{
			commandResult: commandResult{duration: time.Millisecond},
			runErr:        errors.New(sentinel),
			contextErr:    contextErr,
		}
	}
	defer func() { runHarnessCommandWithCancellation = previousRunner }()

	root := repoRoot(t)
	probePath := filepath.Join(artifactDir, "missing-consumer-probe")
	trackLLVMBuildArtifacts(root, &testArtifacts{Dir: artifactDir}, probePath)
	runBinaryWithTimeout(t, probePath, os.Environ(), time.Second)
	t.Fatal("runBinaryWithTimeout unexpectedly returned from injected failure")
}

func runRunBinaryConsumerProcess(t *testing.T, mode, artifactsDir string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunBinaryCancellationConsumerHelper$")
	env := overrideEnvVar(os.Environ(), runBinaryConsumerModeEnv, mode)
	env = overrideEnvVar(env, runBinaryConsumerArtifactEnv, artifactsDir)
	cmd.Env = overrideEnvVar(env, "SURGE_SKIP_TIMEOUT_TESTS", "0")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("run-binary consumer helper mode %q unexpectedly passed\noutput:\n%s", mode, output)
	}
	return string(output)
}

func requireSavedRunError(t *testing.T, artifactsDir, sentinel string) {
	t.Helper()
	diagnostics, err := os.ReadFile(filepath.Join(artifactsDir, "run.diagnostics"))
	if err != nil {
		t.Fatalf("read saved run diagnostics: %v", err)
	}
	want := "run_error: " + sentinel
	if !strings.Contains(string(diagnostics), want) {
		t.Fatalf("saved run diagnostics missing %q:\n%s", want, diagnostics)
	}
}
