package vm_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLLVMParityNothingCallArg(t *testing.T) {
	skipTimeoutTests(t)
	root := repoRoot(t)

	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed; skipping LLVM parity test")
	}
	if _, err := exec.LookPath("ar"); err != nil {
		t.Skip("ar not installed; skipping LLVM parity test")
	}

	surge := buildSurgeBinary(t, root)
	parityEnv := envForParity(root)
	sgRel := filepath.ToSlash(filepath.Join("testdata", "llvm_parity", "nothing_arg_option.sg"))

	vmOut, vmErr, vmCode := runSurgeWithEnv(t, root, surge, parityEnv, "run", "--backend=vm", sgRel)

	buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, parityEnv, "build", sgRel)
	if buildCode != 0 {
		t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
	}

	binPath := filepath.Join(root, "target", "debug", "nothing_arg_option")
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
}
