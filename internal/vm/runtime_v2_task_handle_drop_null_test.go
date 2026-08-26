package vm_test

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The moved-out slot of a task-handle container is NULL, and the container's
// drop glue still calls rt_task_handle_drop on it. The stand makes exactly
// that call with no executor and no task; without the guard the runtime
// panics "invalid task handle" and the process exits 1 before the marker.
func TestRuntimeV2TaskHandleDropTreatsAnEmptySlotAsNothing(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the empty-slot task handle stand")
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "task_handle_drop_null")
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o", bin,
		filepath.Join(root, "internal", "vm", "testdata", "task_handle_drop_null.c"),
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
		t.Fatalf("build empty-slot stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	stdout, stderr, code = runCommand(t, exec.Command(bin), "")
	if code != 0 || !strings.Contains(stdout, "null-handle-drop-ok") {
		t.Fatalf("dropping an empty task-handle slot must be nothing: exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
