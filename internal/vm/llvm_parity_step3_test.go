package vm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type stderrMode int

const (
	stderrExact stderrMode = iota
	stderrFirstLine
)

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

func TestLLVMParityStep3(t *testing.T) {
	root := repoRoot(t)

	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed; skipping LLVM parity step3 test")
	}
	if _, err := exec.LookPath("ar"); err != nil {
		t.Skip("ar not installed; skipping LLVM parity step3 test")
	}

	surge := buildSurgeBinary(t, root)

	cases := []struct {
		name   string
		file   string
		stderr stderrMode
	}{
		{name: "array_bounds", file: "array_bounds.sg", stderr: stderrFirstLine},
		{name: "bytesview_bounds", file: "bytesview_bounds.sg", stderr: stderrFirstLine},
		{name: "string_slice_bounds", file: "string_slice_bounds.sg", stderr: stderrExact},
		{name: "result_match", file: "result_match.sg", stderr: stderrExact},
		{name: "method_call", file: "method_call.sg", stderr: stderrExact},
		{name: "panic_format", file: "panic_format.sg", stderr: stderrExact},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sgRel := filepath.ToSlash(filepath.Join("testdata", "llvm_parity_step3", tc.file))

			vmOut, vmErr, vmCode := runSurge(t, root, surge, "run", "--backend=vm", sgRel)

			buildOut, buildErr, buildCode := runSurge(t, root, surge, "build", sgRel, "--backend=llvm")
			if buildCode != 0 {
				t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
			}

			binPath := filepath.Join(root, "build", tc.name)
			llOut, llErr, llCode := runBinary(t, binPath)

			if llCode != vmCode {
				t.Fatalf("exit code mismatch: vm=%d llvm=%d", vmCode, llCode)
			}
			if llOut != vmOut {
				t.Fatalf("stdout mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmOut, llOut)
			}
			switch tc.stderr {
			case stderrExact:
				if llErr != vmErr {
					t.Fatalf("stderr mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmErr, llErr)
				}
			case stderrFirstLine:
				vmLine := firstLine(vmErr)
				llLine := firstLine(llErr)
				if llLine != vmLine {
					t.Fatalf("stderr first line mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmLine, llLine)
				}
			}
		})
	}
}
