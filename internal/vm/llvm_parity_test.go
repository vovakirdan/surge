package vm_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLLVMParity(t *testing.T) {
	root := repoRoot(t)

	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed; skipping LLVM parity test")
	}
	if _, err := exec.LookPath("ar"); err != nil {
		t.Skip("ar not installed; skipping LLVM parity test")
	}

	surge := buildSurgeBinary(t, root)

	cases := []struct {
		name string
		file string
	}{
		{name: "exit_code", file: "exit_code.sg"},
		{name: "panic", file: "panic.sg"},
		{name: "string_concat", file: "string_concat.sg"},
		{name: "tagged_switch", file: "tagged_switch.sg"},
		{name: "unicode_len", file: "unicode_len.sg"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sgRel := filepath.ToSlash(filepath.Join("testdata", "llvm_parity", tc.file))

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
			if llErr != vmErr {
				t.Fatalf("stderr mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmErr, llErr)
			}
		})
	}
}

// runBinary is defined in llvm_smoke_test.go
