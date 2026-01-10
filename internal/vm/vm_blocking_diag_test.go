//go:build !golden
// +build !golden

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVMBlockingNotSupported(t *testing.T) {
	requireVMBackend(t)

	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	artifacts := newTestArtifacts(t, root)
	src := `fn make() -> Task<int> {
    return blocking { 42 };
}

@entrypoint fn main() -> int {
    let _ = make();
    return 0;
}
`
	srcPath := filepath.Join(artifacts.Dir, "blocking_vm.sg")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stdout, stderr, exitCode := runSurgeWithEnv(t, root, surge, envWithStdlib(root), "run", "--backend=vm", srcPath)
	if exitCode == 0 {
		t.Fatalf("expected failure (stdout=%q, stderr=%q)", stdout, stderr)
	}
	if !strings.Contains(stderr, "blocking { } is not supported in the VM backend") {
		t.Fatalf("missing blocking diagnostic:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}
