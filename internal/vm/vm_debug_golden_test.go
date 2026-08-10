package vm_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
	return stdout, stderr, exitErr.ExitCode()
}
