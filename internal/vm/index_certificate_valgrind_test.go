package vm_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func runIndexCertificateValgrind(t *testing.T, binary string, env []string, budget time.Duration) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	cmd := exec.CommandContext(ctx, "valgrind", "--error-exitcode=99", "--leak-check=full", "--errors-for-leak-kinds=definite,indirect", binary)
	cmd.Dir, cmd.Env = repoRoot(t), env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("Valgrind timed out: %v", ctx.Err())
	}
	code := 0
	if err != nil {
		var exited *exec.ExitError
		if !errors.As(err, &exited) {
			t.Fatalf("Valgrind failed to start: %v", err)
		}
		code = exited.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}
