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

	buildCmd := exec.Command(surge, "build", srcPath, "--backend=llvm")
	buildCmd.Dir = tmpDir
	buildCmd.Env = append(os.Environ(), "SURGE_STDLIB="+root)
	buildOut, buildErr, buildCode := runCommand(t, buildCmd, "")
	if buildCode != 0 {
		t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
	}

	binPath := filepath.Join(tmpDir, "build", "main")
	runCmd := exec.Command(binPath)
	runCmd.Dir = tmpDir
	stdout, stderr, exitCode := runCommand(t, runCmd, "")
	if exitCode != 0 {
		t.Fatalf("run failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if stdout != "portable\n" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}
