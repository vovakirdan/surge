package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAndRunRejectUnionExitWithDiagnostic(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "main.sg")
	source := `tag Help(string);
tag ErrorDiag(Error);
type ParseDiag = Help(string) | ErrorDiag(Error);

fn bad() -> Erring<int, ParseDiag> {
    let e: Error = { message = "bad", code = 1:uint };
    return ErrorDiag(e);
}

@entrypoint
fn main() {
    let result = bad();
    compare result {
        Success(v) => { print(v to string); }
        err => { exit(err); }
    }
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
	if strings.Contains(buildCombined, "LLVM emit failed") {
		t.Fatalf("build should fail in diagnostics before LLVM emit\noutput:\n%s", buildCombined)
	}
	if !strings.Contains(buildCombined, "exit requires ErrorLike-compatible argument") {
		t.Fatalf("missing exit diagnostic in build output:\n%s", buildCombined)
	}

	runOut, runErr, runCode := runSurgeWithInput(t, root, surge, "", "run", "--ui", "off", srcPath)
	if runCode == 0 {
		t.Fatalf("run unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", runOut, runErr)
	}
	runCombined := runOut + runErr
	if strings.Contains(runCombined, "panic VM1003") {
		t.Fatalf("run should fail in diagnostics before VM execution\noutput:\n%s", runCombined)
	}
	if !strings.Contains(runCombined, "exit requires ErrorLike-compatible argument") {
		t.Fatalf("missing exit diagnostic in run output:\n%s", runCombined)
	}
}
