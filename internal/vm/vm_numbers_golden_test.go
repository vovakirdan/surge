package vm_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVMNumbersGolden(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	numbersDir := filepath.Join(root, "testdata", "golden", "vm_numbers")
	entries, err := os.ReadDir(numbersDir)
	if err != nil {
		t.Fatalf("read vm_numbers dir: %v", err)
	}

	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".sg") {
			continue
		}
		if strings.HasSuffix(ent.Name(), "_panics.sg") {
			continue
		}
		name := strings.TrimSuffix(ent.Name(), ".sg")
		t.Run(name, func(t *testing.T) {
			sgRel := filepath.ToSlash(filepath.Join("testdata", "golden", "vm_numbers", name+".sg"))
			outAbs := filepath.Join(numbersDir, name+".out")
			codeAbs := filepath.Join(numbersDir, name+".code")

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
				sgRel,
			)

			if code != wantCode {
				t.Fatalf("exit code: want %d, got %d\nstdout:\n%s", wantCode, code, stdout)
			}
			if wantCode != 0 {
				if stdout != "" {
					t.Fatalf("unexpected stdout:\n%s", stdout)
				}
				if stderr != wantOut {
					t.Fatalf("stderr mismatch:\nwant:\n%s\n\ngot:\n%s", wantOut, stderr)
				}
				return
			}
			if stderr != "" {
				t.Fatalf("unexpected stderr:\n%s", stderr)
			}
			if stdout != wantOut {
				t.Fatalf("output mismatch:\nwant:\n%s\n\ngot:\n%s", wantOut, stdout)
			}
		})
	}
}
