//go:build !golden
// +build !golden

package vm_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	surgeBinOnce sync.Once
	surgeBinPath string
	errSurgeBin  error
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func buildSurgeBinary(t *testing.T, root string) string {
	t.Helper()

	surgeBinOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "surge-bin-*")
		if err != nil {
			errSurgeBin = err
			return
		}
		surgeBinPath = filepath.Join(tmp, "surge")

		cmd := exec.Command("go", "build", "-o", surgeBinPath, "./cmd/surge")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "SURGE_STDLIB="+root)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errSurgeBin = errors.New(strings.TrimSpace(stderr.String()))
			if errSurgeBin.Error() == "" {
				errSurgeBin = err
			}
		}
	})

	if errSurgeBin != nil {
		t.Fatalf("build surge binary: %v", errSurgeBin)
	}
	return surgeBinPath
}

func runSurge(t *testing.T, root, surgeBin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(surgeBin, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SURGE_STDLIB="+root)
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
		t.Fatalf("run surge: %v\nstderr:\n%s", err, stderr)
	}
	return stdout, stderr, exitErr.ProcessState.ExitCode()
}
