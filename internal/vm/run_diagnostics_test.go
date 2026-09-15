package vm_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeRunDiagnostics(t *testing.T, artifactsDir, diagnostics string) {
	t.Helper()
	if artifactsDir == "" || diagnostics == "" {
		return
	}
	writeArtifact(t, artifactsDir, "run.diagnostics", diagnostics)
}

func writeRunOutputArtifacts(t *testing.T, artifactsDir, stdout, stderr string, exitCode int) {
	t.Helper()
	if artifactsDir == "" {
		return
	}
	writeArtifact(t, artifactsDir, "run.stdout", stdout)
	writeArtifact(t, artifactsDir, "run.stderr", stderr)
	writeArtifact(t, artifactsDir, "run.exit_code", fmt.Sprintf("%d\n", exitCode))
}

func readRunDiagnostics(artifactsDir string) string {
	if artifactsDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(artifactsDir, "run.diagnostics"))
	if err != nil || len(data) == 0 {
		return ""
	}
	return string(data)
}

func formatBinaryStat(path string) string {
	if path == "" {
		return "<unknown>"
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("%s (stat error: %v)", path, err)
	}
	return fmt.Sprintf("%s (mode=%s size=%d modtime=%s executable=%t)",
		path,
		info.Mode(),
		info.Size(),
		info.ModTime().Format(time.RFC3339Nano),
		info.Mode().Perm()&0o111 != 0,
	)
}

func exitSignal(exitErr *exec.ExitError) string {
	if exitErr == nil {
		return ""
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return status.Signal().String()
}

type runDiagnostics struct {
	cmd          *exec.Cmd
	artifactsDir string
	outputPath   string
	tmpDir       string
	stdout       string
	stderr       string
	exitCode     int
	exitErr      *exec.ExitError
	duration     time.Duration
	timeout      time.Duration
	ctxErr       error
	runErr       error
}

func formatRunDiagnostics(diag runDiagnostics) string {
	var b strings.Builder
	b.WriteString("run diagnostics:\n")
	if diag.cmd != nil {
		fmt.Fprintf(&b, "command: %s\n", diag.cmd.String())
		fmt.Fprintf(&b, "dir: %s\n", diag.cmd.Dir)
	}
	fmt.Fprintf(&b, "artifact_dir: %s\n", diag.artifactsDir)
	fmt.Fprintf(&b, "binary: %s\n", formatBinaryStat(diag.outputPath))
	fmt.Fprintf(&b, "tmp_dir: %s\n", diag.tmpDir)
	fmt.Fprintf(&b, "exit_code: %d\n", diag.exitCode)
	if signal := exitSignal(diag.exitErr); signal != "" {
		fmt.Fprintf(&b, "signal: %s\n", signal)
	}
	fmt.Fprintf(&b, "duration: %s\n", diag.duration)
	if diag.timeout > 0 {
		fmt.Fprintf(&b, "timeout: %s\n", diag.timeout)
	}
	if diag.ctxErr != nil {
		fmt.Fprintf(&b, "context_error: %v\n", diag.ctxErr)
	}
	if diag.runErr != nil {
		fmt.Fprintf(&b, "run_error: %v\n", diag.runErr)
	}
	fmt.Fprintf(&b, "stdout_len: %d\n", len(diag.stdout))
	b.WriteString("stdout:\n")
	b.WriteString(diag.stdout)
	if !strings.HasSuffix(diag.stdout, "\n") {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "stderr_len: %d\n", len(diag.stderr))
	b.WriteString("stderr:\n")
	b.WriteString(diag.stderr)
	if !strings.HasSuffix(diag.stderr, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}
