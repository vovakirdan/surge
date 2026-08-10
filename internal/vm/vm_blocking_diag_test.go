package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// blockingFixture spells the body as statements. The trailing-expression
// spelling parses on neither backend, so a fixture written that way dies at the
// parser and the VM never gets a say — the row would then be green whatever the
// VM does with `blocking`.
const blockingFixture = `fn make() -> Task<int> {
    return blocking {
        return 42;
    };
}

@entrypoint fn main() -> int {
    let _ = make();
    return 0;
}
`

func TestVMBlockingNotSupported(t *testing.T) {
	requireVMBackend(t)

	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	artifacts := newTestArtifacts(t, root)
	srcPath := filepath.Join(artifacts.Dir, "blocking_vm.sg")
	if err := os.WriteFile(srcPath, []byte(blockingFixture), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	t.Run("vm_refuses_before_running", func(t *testing.T) {
		stdout, stderr, exitCode := runSurgeWithEnv(t, root, surge, envWithStdlib(root), "run", "--backend=vm", srcPath)
		if exitCode == 0 {
			t.Fatalf("expected failure (stdout=%q, stderr=%q)", stdout, stderr)
		}
		if n := strings.Count(stderr, "blocking { } is not supported in the VM backend"); n != 1 {
			t.Fatalf("want exactly 1 blocking diagnostic, got %d:\nstdout:\n%s\nstderr:\n%s", n, stdout, stderr)
		}
		// The refusal has to arrive from the compiler. A VM that accepted the
		// program and then hit an opcode it has no case for would also exit
		// non-zero, and that is the failure this row exists to keep out.
		if strings.Contains(stderr, "unimplemented") {
			t.Fatalf("VM ran the program instead of refusing it:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
	})

	// Without this leg the row cannot tell a VM refusal from a fixture that
	// stopped being a valid program.
	t.Run("fixture_still_compiles_and_runs_natively", func(t *testing.T) {
		ensureLLVMToolchain(t)
		stdout, stderr, exitCode := runSurgeWithEnv(t, root, surge, envWithStdlib(root), "run", "--backend=llvm", srcPath)
		if exitCode != 0 {
			t.Fatalf("fixture no longer builds on the native backend (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
		}
	})
}
