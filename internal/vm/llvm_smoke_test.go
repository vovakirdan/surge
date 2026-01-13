package vm_test

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLLVMSmoke(t *testing.T) {
	skipTimeoutTests(t)
	root := repoRoot(t)

	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed; skipping LLVM smoke test")
	}
	if _, err := exec.LookPath("ar"); err != nil {
		t.Skip("ar not installed; skipping LLVM smoke test")
	}

	surge := buildSurgeBinary(t, root)
	parityEnv := envForParity(root)

	cases := []struct {
		name string
		file string
	}{
		{name: "hello_print", file: "hello_print.sg"},
		{name: "unicode_print", file: "unicode_print.sg"},
		{name: "exit_code", file: "exit_code.sg"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sgRel := filepath.ToSlash(filepath.Join("testdata", "llvm_smoke", tc.file))

			vmOut, vmErr, vmCode := runSurgeWithEnv(t, root, surge, parityEnv, "run", "--backend=vm", sgRel)

			buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, parityEnv, "build", sgRel)
			if buildCode != 0 {
				t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
			}

			binPath := filepath.Join(root, "target", "debug", tc.name)
			llOut, llErr, llCode := runBinary(t, binPath)

			if llCode != vmCode {
				t.Fatalf("exit code mismatch: vm=%d llvm=%d", vmCode, llCode)
			}
			if llOut != vmOut {
				t.Fatalf("stdout mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmOut, llOut)
			}
			if llErr != vmErr {
				t.Fatalf("stderr mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmErr, llErr)
			}
		})
	}
}

func runBinary(t *testing.T, path string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(path, args...)
	cmd.Env = envForParity(repoRoot(t))
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()

	stdout = outBuf.String()
	stderr = errBuf.String()

	if err == nil {
		return stdout, stderr, 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run binary: %v\nstderr:\n%s", err, stderr)
	}
	return stdout, stderr, exitErr.ExitCode()
}
