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

func TestVMEntrypointGolden(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	entryDir := filepath.Join(root, "testdata", "golden", "vm_entrypoint")
	entries, err := os.ReadDir(entryDir)
	if err != nil {
		t.Fatalf("read vm_entrypoint dir: %v", err)
	}

	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".sg") {
			continue
		}
		name := strings.TrimSuffix(ent.Name(), ".sg")
		t.Run(name, func(t *testing.T) {
			sgRel := filepath.ToSlash(filepath.Join("testdata", "golden", "vm_entrypoint", name+".sg"))
			outAbs := filepath.Join(entryDir, name+".out")
			codeAbs := filepath.Join(entryDir, name+".code")
			argsAbs := filepath.Join(entryDir, name+".args")
			stdinAbs := filepath.Join(entryDir, name+".stdin")

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

			var args []string
			if b, err := os.ReadFile(argsAbs); err == nil {
				for _, line := range strings.Split(string(b), "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					args = append(args, line)
				}
			}

			stdin := ""
			if b, err := os.ReadFile(stdinAbs); err == nil {
				stdin = string(b)
			}

			cmdArgs := []string{"run", "--backend=vm", sgRel}
			if len(args) > 0 {
				cmdArgs = append(cmdArgs, "--")
				cmdArgs = append(cmdArgs, args...)
			}

			stdout, stderr, code := runSurgeWithInput(t, root, surge, stdin, cmdArgs...)

			if code != wantCode {
				t.Fatalf("exit code: want %d, got %d\nstdout:\n%s", wantCode, code, stdout)
			}
			if wantCode == 0 {
				if stderr != "" {
					t.Fatalf("unexpected stderr:\n%s", stderr)
				}
				if stdout != wantOut {
					t.Fatalf("output mismatch:\nwant:\n%s\n\ngot:\n%s", wantOut, stdout)
				}
				return
			}

			if stdout != "" {
				t.Fatalf("unexpected stdout:\n%s", stdout)
			}
			if stderr != wantOut {
				t.Fatalf("output mismatch:\nwant:\n%s\n\ngot:\n%s", wantOut, stderr)
			}
		})
	}
}

func runSurgeWithInput(t *testing.T, root, surgeBin, stdin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(surgeBin, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SURGE_STDLIB="+root)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Stdin = strings.NewReader(stdin)
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
