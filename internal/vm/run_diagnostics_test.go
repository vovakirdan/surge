package vm_test

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

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
