//go:build golden
// +build golden

package vm_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVMDebugTraceDeterminism(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	dir := tempDirUnderRoot(t, root, "vm-trace-det-*")
	srcPath := filepath.Join(dir, "trace.sg")
	srcRel := relPathFromRoot(t, root, srcPath)

	source := strings.Join([]string{
		"@entrypoint fn main() -> int {",
		"    let a: int = 1;",
		"    let b: int = 2;",
		"    return a + b;",
		"}",
		"",
	}, "\n")
	writeFile(t, srcPath, source)

	args := []string{"run", "--backend=vm", "--vm-trace", srcRel}
	stdout1, stderr1, code1 := runSurge(t, root, surge, args...)
	stdout2, stderr2, code2 := runSurge(t, root, surge, args...)

	if code1 != 0 || code2 != 0 {
		t.Fatalf("unexpected exit codes: first=%d second=%d\nstderr1:\n%s\nstderr2:\n%s", code1, code2, stderr1, stderr2)
	}
	if stdout1 != stdout2 {
		t.Fatalf("stdout mismatch across runs:\nfirst:\n%s\nsecond:\n%s", stdout1, stdout2)
	}
	if stderr1 != stderr2 {
		t.Fatalf("trace mismatch across runs:\nfirst:\n%s\nsecond:\n%s", stderr1, stderr2)
	}
}

func TestVMDebugInspectDeterminism(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	dir := tempDirUnderRoot(t, root, "vm-inspect-det-*")
	srcPath := filepath.Join(dir, "inspect.sg")
	srcRel := relPathFromRoot(t, root, srcPath)

	lines := []string{
		"@entrypoint fn main() -> int {",
		"    let x: int = 1;",
		"    let y: string = \"hi\" + \"!\";",
		"    let z: int[] = [1, 2, 3];",
		"    let w: int[] = z[1..3];",
		"    return x;",
		"}",
		"",
	}
	writeFile(t, srcPath, strings.Join(lines, "\n"))

	breakLine := 6
	script := strings.Join([]string{
		"break " + srcRel + ":" + strconv.Itoa(breakLine),
		"continue",
		"locals",
		"stack",
		"heap",
		"print x",
		"print y",
		"print z",
		"print w",
		"continue",
		"",
	}, "\n")
	scriptPath := filepath.Join(dir, "inspect.script")
	writeFile(t, scriptPath, script)

	args := []string{"run", "--backend=vm", "--vm-debug", "--vm-debug-script", scriptPath, srcRel}
	stdout1, stderr1, code1 := runSurge(t, root, surge, args...)
	stdout2, stderr2, code2 := runSurge(t, root, surge, args...)

	if code1 != 0 || code2 != 0 {
		t.Fatalf("unexpected exit codes: first=%d second=%d\nstderr1:\n%s\nstderr2:\n%s", code1, code2, stderr1, stderr2)
	}
	if stderr1 != "" || stderr2 != "" {
		t.Fatalf("unexpected stderr:\nfirst:\n%s\nsecond:\n%s", stderr1, stderr2)
	}
	if stdout1 != stdout2 {
		t.Fatalf("debug output mismatch across runs:\nfirst:\n%s\nsecond:\n%s", stdout1, stdout2)
	}
}

func tempDirUnderRoot(t *testing.T, root, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp(root, prefix)
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func relPathFromRoot(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("rel path: %v", err)
	}
	return rel
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
