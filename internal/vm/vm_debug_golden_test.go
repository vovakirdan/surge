//go:build golden
// +build golden

package vm_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var (
	surgeBinOnce sync.Once
	surgeBinPath string
	errSurgeBin  error
)

func TestVMDebuggerGolden(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	debugDir := filepath.Join(root, "testdata", "golden", "vm_debug")
	entries, err := os.ReadDir(debugDir)
	if err != nil {
		t.Fatalf("read vm_debug dir: %v", err)
	}

	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".sg") {
			continue
		}
		name := strings.TrimSuffix(ent.Name(), ".sg")
		t.Run(name, func(t *testing.T) {
			sgRel := filepath.ToSlash(filepath.Join("testdata", "golden", "vm_debug", name+".sg"))
			scriptAbs := filepath.Join(debugDir, name+".script")
			outAbs := filepath.Join(debugDir, name+".out")
			codeAbs := filepath.Join(debugDir, name+".code")

			wantOutBytes, err := os.ReadFile(outAbs)
			if err != nil {
				t.Fatalf("read %s.out: %v", name, err)
			}
			wantOut := string(wantOutBytes)
			wantCode := 0
			if b, err := os.ReadFile(codeAbs); err == nil {
				n, err := strconv.Atoi(strings.TrimSpace(string(b)))
				if err != nil {
					t.Fatalf("parse %s.code: %v", name, err)
				}
				wantCode = n
			}

			stdout, stderr, code := runSurge(t, root, surge,
				"run",
				"--backend=vm",
				"--vm-debug",
				"--vm-debug-script", scriptAbs,
				sgRel,
			)

			if stderr != "" {
				t.Fatalf("unexpected stderr:\n%s", stderr)
			}
			if code != wantCode {
				t.Fatalf("exit code: want %d, got %d\nstdout:\n%s", wantCode, code, stdout)
			}
			if stdout != wantOut {
				t.Fatalf("output mismatch:\nwant:\n%s\n\ngot:\n%s", wantOut, stdout)
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/vm/vm_debug_golden_test.go -> repo root
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func envWithStdlib(root string) []string {
	env := os.Environ()
	key := "SURGE_STDLIB="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, key) {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, key+root)
	return out
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
		cmd.Env = envWithStdlib(root)
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
	cmd.Env = envWithStdlib(root)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()

	stdout = stripTimingLines(outBuf.String())
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
