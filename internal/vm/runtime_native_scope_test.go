package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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

	// The same binary, asked to register a task the scope did not create:
	// creation is the only writer of membership, so the intrinsic must refuse
	// with a fatal panic rather than count, list, or silently drop the task.
	// A run that returns is the mutant this stand exists to catch.
	foreignCmd := exec.Command(binPath)
	foreignCmd.Env = append(os.Environ(),
		"SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1", "SCOPE_STAND_FOREIGN=1")
	foreignOut, foreignErr, foreignCode := runCommand(t, foreignCmd, "")
	const refusal = "a task registered with a scope that did not create it"
	if foreignCode == 0 || !strings.Contains(foreignErr, refusal) {
		t.Fatalf("registering a task the scope did not create must die with %q (code=%d)\nstdout:\n%s\nstderr:\n%s",
			refusal, foreignCode, foreignOut, foreignErr)
	}
}

const scopeChildrenHarness = `
#include "rt_async_internal.h"
#include "rt_scope_membership.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

// Drop-dispatch stub: no harness state struct carries a drop obligation
// (drop-fn id 0 never dispatches), so reaching this is a test bug.
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_poll_call(uint64_t id) {
    (void)id;
}

void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
            *(uint64_t*)out_dst = 0;
        }
        return;
}

static int fail(const char* msg) {
    if (msg != NULL) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

static rt_task* alloc_task(rt_executor* ex, uint64_t id, waker_key creation_scope) {
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        return NULL;
    }
    memset(task, 0, sizeof(*task));
    // A stand's task answers with a machine word, which is exactly what the
    // opaque-word descriptor describes: the result slot carries it the same way
    // it carries a compiled type's value.
    (void)rt_value_cell_bind(&task->result, rt_channel_opaque_word_ops());
    task->id = id;
    task->kind = TASK_KIND_USER;
    task->creation_scope_key = creation_scope;
    task_status_store(task, TASK_READY);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_entitlements_init(&task->entitlements);
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    rt_shard_lock(owner_shard);
    (void)rt_scope_publish_creation_locked(ex, task);
    rt_task_slot_store(ex, id, task);
    rt_shard_unlock(owner_shard);
    if (ex->next_id <= id) {
        ex->next_id = id + 1;
    }
    return task;
}

static void free_task_slot(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    rt_task_slot_store(ex, task->id, NULL);
    rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
}

int main(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }

    rt_control_lock(ex);
    rt_task* owner = alloc_task(ex, ex->next_id, waker_none());
    if (owner == NULL) {
        rt_control_unlock(ex);
        return fail("owner allocation failed");
    }
    rt_set_current_task(owner);
    rt_control_unlock(ex);

    void* scope_handle = rt_scope_enter(false);
    if (scope_handle == NULL) {
        return fail("scope enter failed");
    }

    rt_control_lock(ex);
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        rt_control_unlock(ex);
        return fail("scope missing");
    }
    rt_task* active = alloc_task(ex, ex->next_id, owner->active_scope_key);
    if (active == NULL) {
        rt_control_unlock(ex);
        return fail("active task allocation failed");
    }
    rt_control_unlock(ex);

    rt_control_lock(ex);
    if (scope->children_len != 1 || scope->active_children != 1) {
        rt_control_unlock(ex);
        return fail("active child not tracked");
    }
    if (active->scope_registered == 0 || active->creation_scope_key.id != scope_id) {
        rt_control_unlock(ex);
        return fail("active child registration metadata missing");
    }
    mark_done(ex, active, TASK_RESULT_SUCCESS);
    if (scope->children_len != 0) {
        rt_control_unlock(ex);
        return fail("completed child remained in scope");
    }
    if (scope->active_children != 0) {
        rt_control_unlock(ex);
        return fail("active child count not decremented");
    }
    if (active->scope_registered != 0) {
        rt_control_unlock(ex);
        return fail("completed child still marked as registered");
    }

    // A task the scope did not create. Creation is the only writer of
    // membership, so the register intrinsic has nothing to write here: it
    // compares the two identities and refuses the mismatch as a fatal panic.
    // The refusal is a process exit, so it lives in a second run of this same
    // binary (SCOPE_STAND_FOREIGN=1) that the Go side expects to die with the
    // message; the default run keeps to the members the scope created.
    rt_task* foreign = alloc_task(ex, ex->next_id, waker_none());
    if (foreign == NULL) {
        rt_control_unlock(ex);
        return fail("foreign task allocation failed");
    }
    task_status_store(foreign, TASK_DONE);
    foreign->result_kind = TASK_RESULT_SUCCESS;
    rt_control_unlock(ex);

    if (getenv("SCOPE_STAND_FOREIGN") != NULL) {
        rt_scope_register_child(scope_handle, foreign);
        return fail("a task the scope did not create was accepted as its child");
    }

    rt_control_lock(ex);
    if (scope->children_len != 0 || scope->active_children != 0) {
        rt_control_unlock(ex);
        return fail("a task the scope did not create leaked into scope history");
    }
    if (foreign->scope_registered != 0) {
        rt_control_unlock(ex);
        return fail("a task the scope did not create is marked registered");
    }
    rt_control_unlock(ex);

    rt_scope_exit(scope_handle);

    rt_control_lock(ex);
    if (waker_valid(owner->active_scope_key)) {
        rt_control_unlock(ex);
        return fail("scope exit did not clear owner scope key");
    }
    rt_set_current_task(NULL);
    free_task_slot(ex, foreign);
    free_task_slot(ex, active);
    free_task_slot(ex, owner);
    rt_control_unlock(ex);
    return 0;
}
`
