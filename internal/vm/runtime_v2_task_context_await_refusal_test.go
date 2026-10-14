//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestRuntimeV2TaskContextAwaitRequiresAsyncLowering(t *testing.T) {
	root := repoRoot(t)
	sourceBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_async_task.c"))
	if err != nil {
		t.Fatalf("read rt_async_task.c: %v", err)
	}
	body, ok := cFunctionBody(string(sourceBytes), "rt_task_await")
	if !ok {
		t.Fatal("rt_task_await definition not found")
	}
	if !strings.Contains(body, `panic_msg("task await requires async lowering")`) {
		t.Fatal("task-context rt_task_await must fail closed with a stable diagnostic")
	}
	for _, forbidden := range []string{"run_until_done", "run_ready_one", "poll_task"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("task-context rt_task_await must not execute a scheduler loop; found %q", forbidden)
		}
	}

	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping task-context await refusal stand")
	}
	bin := filepath.Join(t.TempDir(), "task_context_await_refusal")
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o", bin,
		filepath.Join(root, "internal", "vm", "testdata", "task_context_await_refusal.c"),
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
		t.Fatalf("build task-context await refusal stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	for _, row := range []struct{ mode, message string }{
		{"await", "task await requires async lowering"},
		{"send", "channel send requires async lowering"},
		{"recv", "channel recv requires async lowering"},
	} {
		t.Run(row.mode, func(t *testing.T) {
			command := exec.Command(bin, row.mode)
			command.Env = lifecycleEnv("SURGE_SHARDS=1", "SURGE_BLOCKING_THREADS=1")
			stdout, stderr, code := runCommand(t, command, "")
			if code == 0 || !strings.Contains(stderr, row.message) {
				t.Fatalf("task-context %s did not fail closed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}
