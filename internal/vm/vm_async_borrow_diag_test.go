package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAndRunRejectBorrowCaptureAcrossAsyncSuspend(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "main.sg")
	source := `@entrypoint
fn main() -> int {
    let value: int = 2;
    let refv = &value;
    let task: Task<int> = spawn async {
        checkpoint().await();
        return *refv;
    };
    compare task.await() {
        Success(out) => return out;
        Cancelled() => return 9;
    };
}
`
	if err := os.WriteFile(srcPath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	buildOut, buildErr, buildCode := runSurgeWithInput(t, root, surge, "", "build", "--ui", "off", srcPath)
	if buildCode == 0 {
		t.Fatalf("build unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", buildOut, buildErr)
	}
	buildCombined := buildOut + buildErr
	if strings.Contains(buildCombined, "panic VM") || strings.Contains(buildCombined, "invalid local id") {
		t.Fatalf("build should fail in diagnostics before VM execution\noutput:\n%s", buildCombined)
	}
	if !strings.Contains(buildCombined, "cannot send 'refv' to a task") {
		t.Fatalf("missing async borrow capture diagnostic in build output:\n%s", buildCombined)
	}

	runOut, runErr, runCode := runSurgeWithInput(t, root, surge, "", "run", "--ui", "off", "--backend=vm", srcPath)
	if runCode == 0 {
		t.Fatalf("run unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", runOut, runErr)
	}
	runCombined := runOut + runErr
	if strings.Contains(runCombined, "panic VM") || strings.Contains(runCombined, "invalid local id") {
		t.Fatalf("run should fail in diagnostics before VM execution\noutput:\n%s", runCombined)
	}
	if !strings.Contains(runCombined, "cannot send 'refv' to a task") {
		t.Fatalf("missing async borrow capture diagnostic in run output:\n%s", runCombined)
	}
}
