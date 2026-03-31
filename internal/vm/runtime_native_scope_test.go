package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func TestNativeScopeDropsCompletedChildrenImmediately(t *testing.T) {
	skipTimeoutTests(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping native runtime scope test")
	}

	root := repoRoot(t)
	tmpDir := t.TempDir()
	harnessPath := filepath.Join(tmpDir, "scope_children_harness.c")
	binPath := filepath.Join(tmpDir, "scope_children_harness")
	if writeErr := os.WriteFile(harnessPath, []byte(scopeChildrenHarness), 0o600); writeErr != nil {
		t.Fatalf("write harness: %v", writeErr)
	}

	sources, globErr := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if globErr != nil {
		t.Fatalf("glob runtime sources: %v", globErr)
	}
	sort.Strings(sources)

	args := []string{
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o",
		binPath,
		harnessPath,
	}
	for _, src := range sources {
		if filepath.Base(src) == "rt_entry.c" {
			continue
		}
		args = append(args, src)
	}

	buildCmd := exec.Command(clang, args...)
	buildCmd.Dir = root
	buildOut, buildErr, buildCode := runCommand(t, buildCmd, "")
	if buildCode != 0 {
		t.Fatalf("build harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
	}

	runCmd := exec.Command(binPath)
	runCmd.Env = append(os.Environ(), "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runCommand(t, runCmd, "")
	if exitCode != 0 {
		t.Fatalf("harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

const scopeChildrenHarness = `
#include "rt_async_internal.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

void __surge_poll_call(uint64_t id) {
    (void)id;
}

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    return 0;
}

static int fail(const char* msg) {
    if (msg != NULL) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

static rt_task* alloc_task(rt_executor* ex, uint64_t id) {
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        return NULL;
    }
    memset(task, 0, sizeof(*task));
    task->id = id;
    task->kind = TASK_KIND_USER;
    task_status_store(task, TASK_READY);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    ex->tasks[id] = task;
    if (ex->next_id <= id) {
        ex->next_id = id + 1;
    }
    return task;
}

static void free_task_slot(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    if (task->id < ex->tasks_cap) {
        ex->tasks[task->id] = NULL;
    }
    rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
}

int main(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }

    rt_lock(ex);
    rt_task* owner = alloc_task(ex, ex->next_id);
    if (owner == NULL) {
        rt_unlock(ex);
        return fail("owner allocation failed");
    }
    rt_set_current_task(owner);
    rt_unlock(ex);

    void* scope_handle = rt_scope_enter(false);
    if (scope_handle == NULL) {
        return fail("scope enter failed");
    }

    rt_lock(ex);
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        rt_unlock(ex);
        return fail("scope missing");
    }
    rt_task* active = alloc_task(ex, ex->next_id);
    if (active == NULL) {
        rt_unlock(ex);
        return fail("active task allocation failed");
    }
    rt_unlock(ex);

    rt_scope_register_child(scope_handle, active);

    rt_lock(ex);
    if (scope->children_len != 1 || scope->active_children != 1) {
        rt_unlock(ex);
        return fail("active child not tracked");
    }
    if (active->scope_registered == 0 || active->parent_scope_id != scope_id) {
        rt_unlock(ex);
        return fail("active child registration metadata missing");
    }
    mark_done(ex, active, TASK_RESULT_SUCCESS, 0);
    if (scope->children_len != 0) {
        rt_unlock(ex);
        return fail("completed child remained in scope");
    }
    if (scope->active_children != 0) {
        rt_unlock(ex);
        return fail("active child count not decremented");
    }
    if (active->scope_registered != 0) {
        rt_unlock(ex);
        return fail("completed child still marked as registered");
    }

    rt_task* completed = alloc_task(ex, ex->next_id);
    if (completed == NULL) {
        rt_unlock(ex);
        return fail("completed task allocation failed");
    }
    task_status_store(completed, TASK_DONE);
    completed->result_kind = TASK_RESULT_SUCCESS;
    rt_unlock(ex);

    rt_scope_register_child(scope_handle, completed);

    rt_lock(ex);
    if (scope->children_len != 0 || scope->active_children != 0) {
        rt_unlock(ex);
        return fail("already completed child leaked into scope history");
    }
    if (completed->scope_registered != 0) {
        rt_unlock(ex);
        return fail("completed child should not be marked registered");
    }
    rt_set_current_task(NULL);
    rt_unlock(ex);

    rt_scope_exit(scope_handle);

    rt_lock(ex);
    if (owner->scope_id != 0) {
        rt_unlock(ex);
        return fail("scope exit did not clear owner scope id");
    }
    free_task_slot(ex, completed);
    free_task_slot(ex, active);
    free_task_slot(ex, owner);
    rt_unlock(ex);
    return 0;
}
`
