package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAndRunRejectCompareArmBlockFallingThroughAsNothing(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "main.sg")
	source := `fn source(flag: bool) -> Erring<string, Error> {
    if flag {
        return Success("hello");
    }
    return Error { message = "missing", code = 1:uint };
}

fn recover(flag: bool) -> Erring<string, Error> {
    let res = source(flag);
    return compare res {
        Success(text) => Success(text);
        err => {
            if err.code == 1:uint {
                let empty: string = "";
                Success(empty);
            } else {
                err;
            }
        }
    };
}

@entrypoint
fn main() {
    let res = recover(false);
    compare res {
        Success(text) => print("text=" + text);
        err => exit(err);
    }
    return nothing;
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
	if !strings.Contains(buildCombined, "nothing") || (!strings.Contains(buildCombined, "compare arm type mismatch") && !strings.Contains(buildCombined, "cannot assign nothing")) {
		t.Fatalf("missing compare-arm diagnostic in build output:\n%s", buildCombined)
	}

	runOut, runErr, runCode := runSurgeWithInput(t, root, surge, "", "run", "--ui", "off", srcPath)
	if runCode == 0 {
		t.Fatalf("run unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", runOut, runErr)
	}
	runCombined := runOut + runErr
	if strings.Contains(runCombined, "panic VM1003") || strings.Contains(runCombined, "panic:") {
		t.Fatalf("run should fail in diagnostics before VM execution\noutput:\n%s", runCombined)
	}
	if !strings.Contains(runCombined, "nothing") || (!strings.Contains(runCombined, "compare arm type mismatch") && !strings.Contains(runCombined, "cannot assign nothing")) {
		t.Fatalf("missing compare-arm diagnostic in run output:\n%s", runCombined)
	}
}

func TestBuildAndRunRejectRetBlockFallingThroughAsNothing(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "main.sg")
	source := `fn f(flag: bool) -> int {
    let x = {
        if flag {
            ret 1;
        }
    };
    return x;
}

@entrypoint
fn main() -> int {
    return f(false);
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
	if !strings.Contains(buildCombined, "block result type mismatch") || !strings.Contains(buildCombined, "got nothing") {
		t.Fatalf("missing block-result diagnostic in build output:\n%s", buildCombined)
	}

	runOut, runErr, runCode := runSurgeWithInput(t, root, surge, "", "run", "--ui", "off", srcPath)
	if runCode == 0 {
		t.Fatalf("run unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", runOut, runErr)
	}
	runCombined := runOut + runErr
	if strings.Contains(runCombined, "panic VM1999") || strings.Contains(runCombined, "panic:") {
		t.Fatalf("run should fail in diagnostics before VM execution\noutput:\n%s", runCombined)
	}
	if !strings.Contains(runCombined, "block result type mismatch") || !strings.Contains(runCombined, "got nothing") {
		t.Fatalf("missing block-result diagnostic in run output:\n%s", runCombined)
	}
}

func TestCompareArmBlockMutationKeepsOuterStringLive(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "main.sg")
	source := `fn case_bool() -> string {
    let mut out: string = "";
    compare true {
        true => {
            out = out + "x";
        };
        false => {};
    };
    return out;
}

@entrypoint
fn main() -> nothing {
    print(case_bool());
    return nothing;
}
`
	if err := os.WriteFile(srcPath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stdout, stderr, exitCode := runSurgeWithInput(t, root, surge, "", "run", "--backend=vm", "--ui", "off", srcPath)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "x" {
		t.Fatalf("expected stdout x, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got:\n%s", stderr)
	}
}

func TestBuildAndRunRejectUseAfterMoveFromCompareOwnedScrutinee(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "main.sg")
	source := `fn bad() -> int? {
    let next: int? = Some(1);
    compare next {
        nothing => {};
        _ => {};
    };
    return next;
}

@entrypoint
fn main() -> int {
    compare bad() {
        nothing => return 0;
        Some(v) => return v;
    };
    return 2;
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
	if !strings.Contains(buildCombined, "use of moved value 'next'") {
		t.Fatalf("missing use-after-move diagnostic in build output:\n%s", buildCombined)
	}

	runOut, runErr, runCode := runSurgeWithInput(t, root, surge, "", "run", "--ui", "off", srcPath)
	if runCode == 0 {
		t.Fatalf("run unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", runOut, runErr)
	}
	runCombined := runOut + runErr
	if strings.Contains(runCombined, "panic VM1002") || strings.Contains(runCombined, "panic:") {
		t.Fatalf("run should fail in diagnostics before VM execution\noutput:\n%s", runCombined)
	}
	if !strings.Contains(runCombined, "use of moved value 'next'") {
		t.Fatalf("missing use-after-move diagnostic in run output:\n%s", runCombined)
	}
}
