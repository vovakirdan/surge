package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLLVMBuildPortable(t *testing.T) {
	root := repoRoot(t)
	ensureLLVMToolchain(t)
	surge := buildSurgeBinary(t, root)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "main.sg")
	source := "@entrypoint\nfn main() {\n    print(\"portable\\n\");\n}\n"
	if err := os.WriteFile(srcPath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	// #nosec G204 -- test executes a locally built binary with known args
	buildCmd := exec.Command(surge, "build", srcPath)
	buildCmd.Dir = tmpDir
	buildCmd.Env = envWithStdlib(root)
	buildOut, buildErr, buildCode := runCommand(t, buildCmd, "")
	if buildCode != 0 {
		t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
	}

	binPath := filepath.Join(tmpDir, "target", "debug", "main")
	// #nosec G204 -- test executes the freshly built binary
	runCmd := exec.Command(binPath)
	runCmd.Dir = tmpDir
	stdout, stderr, exitCode := runCommand(t, runCmd, "")
	if exitCode != 0 {
		t.Fatalf("run failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if stdout != "portable\n\n" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}
