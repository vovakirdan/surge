//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The canonical result slot a task owns, proven before any task uses one.
//
// What it has to answer, and what no later test can ask as cheaply: a result is
// published once and read once; a narrow result costs no allocation, because
// the machine word it replaces cost none either; a wide one costs exactly one,
// which the box it replaces already cost; and a result nobody took is destroyed
// exactly once, by the slot rather than by whoever noticed last.
func TestRuntimeV2TaskResultSlotHoldsOneValueExactlyOnce(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the task-result slot proof")
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "task_result_slot")
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-g",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o", bin,
		filepath.Join(root, "internal", "vm", "testdata", "task_result_slot.c"),
	}
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	build := exec.Command(clang, args...)
	build.Dir = root
	stdout, stderr, code := runCommand(t, build, "")
	if code != 0 {
		t.Fatalf("build task-result slot stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	run := exec.Command(bin)
	// Leak detection stays OFF for the same reason the channel stand gives: this
	// links the real runtime, whose one-per-process state outlives main by
	// design. What the slot leaks is answered by its own drop census below.
	run.Env = append(overrideEnvVar(nil, "ASAN_OPTIONS", "abort_on_error=1:detect_leaks=0"),
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1")
	stdout, stderr, code = runCommand(t, run, "")
	if code != 0 {
		t.Fatalf("task-result slot stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	if !strings.Contains(stdout, "task result slot: drops=2") {
		t.Fatalf("the stand reported an unexpected drop census\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
}
