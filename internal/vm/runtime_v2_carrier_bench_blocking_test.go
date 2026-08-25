package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const carrierBenchBlockingSyncPoint = "SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER"

func TestRuntimeV2CarrierBenchBlockingRegisterThenVerify(t *testing.T) {
	checkCarrierBenchBlockingBoundary(t)
	for _, test := range []struct {
		name            string
		negativeControl bool
	}{
		{name: "fixed"},
		{name: "old-path-negative-control", negativeControl: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			binary := buildCarrierBenchBlockingHarness(t, test.negativeControl)
			command := exec.Command(binary)
			command.Env = os.Environ()
			for key, value := range map[string]string{
				"SURGE_SHARDS":           "2",
				"SURGE_THREADS":          "2",
				"SURGE_BLOCKING_THREADS": "1",
				"SURGE_SYNC_POINT":       carrierBenchBlockingSyncPoint + ":block",
			} {
				command.Env = overrideEnvVar(command.Env, key, value)
			}
			stdout, stderr, exitCode := runCommand(t, command, "")
			if test.negativeControl {
				if exitCode == 0 || !strings.Contains(
					stderr, "negative control reproduced stranded blocking waiter",
				) {
					t.Fatalf("old blocking path did not fail for the lost-wake reason (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
				}
				return
			}
			if exitCode != 0 || !strings.Contains(stdout, "blocking register-then-verify passed") {
				t.Fatalf("blocking register-then-verify proof failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
			}
		})
	}
}

func checkCarrierBenchBlockingBoundary(t *testing.T) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(repoRoot(t), "runtime", "native", "rt_async_blocking.c"))
	if err != nil {
		t.Fatalf("read blocking runtime: %v", err)
	}
	boundary := regexp.MustCompile(
		`(?s)RT_SYNC_POINT\(SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER\);\s*` +
			`prepare_park\(ex, task, key, 0\);.*?` +
			`status = atomic_load_explicit\(&job->status, memory_order_acquire\);.*?` +
			`RT_DEBT143_POST_REGISTER_TERMINAL\(status\).*?` +
			`remove_waiter\(ex, key, task->id\).*?` +
			`goto observe_terminal`,
	)
	if !boundary.Match(source) {
		t.Fatal("blocking poll must register, re-check terminal state, then unlink the waiter")
	}
	if !strings.Contains(string(source), "memory_order_acquire") {
		t.Fatal("blocking terminal re-check must acquire the worker's published result")
	}
}

func buildCarrierBenchBlockingHarness(t *testing.T, negativeControl bool) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping blocking lost-wake proof")
	}
	root := repoRoot(t)
	temporary := t.TempDir()
	sourcePath := filepath.Join(temporary, "blocking_register_verify.c")
	binaryPath := filepath.Join(temporary, "blocking_register_verify")
	if writeErr := os.WriteFile(
		sourcePath, []byte(carrierBenchBlockingHarness), 0o600,
	); writeErr != nil {
		t.Fatalf("write blocking proof harness: %v", writeErr)
	}
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	arguments := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-DRT_TEST_SYNC_POINTS", "-I" + filepath.Join(root, "runtime", "native"),
		"-o", binaryPath, sourcePath,
	}
	if negativeControl {
		arguments = append(arguments, "-DRV2_DEBT_143_NEGATIVE_CONTROL")
	}
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			arguments = append(arguments, source)
		}
	}
	command := exec.Command(clang, arguments...)
	command.Dir = root
	stdout, stderr, exitCode := runCommand(t, command, "")
	if exitCode != 0 {
		t.Fatalf("build blocking proof harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	return binaryPath
}

const carrierBenchBlockingHarness = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"
#include "rt_sync_point.h"

#include <sched.h>
#include <stdatomic.h>
#include <stdio.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

enum { BLOCKING_FN_QUICK = 9701 };

static _Atomic unsigned g_sync_before;

static int fail(const char* message) {
    fputs(message, stderr);
    fputc('\n', stderr);
    return 1;
}

static uint64_t monotonic_millis(void) {
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    return (uint64_t)now.tv_sec * 1000U + (uint64_t)now.tv_nsec / 1000000U;
}

static int wait_task_status(const rt_task* task, uint8_t want) {
    uint64_t deadline = monotonic_millis() + 4000U;
    while (monotonic_millis() < deadline) {
        if (task_status_load(task) == want) {
            return 1;
        }
        sched_yield();
    }
    return 0;
}

void __surge_poll_call(uint64_t id) {
    (void)id;
    rt_async_return(NULL, &(uint64_t){0});
}

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

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)state;
    if (id != BLOCKING_FN_QUICK) {
        return 0;
    }
    unsigned before = atomic_load_explicit(&g_sync_before, memory_order_acquire);
    if (!rt_sync_point_wait_until_after(
            RT_SYNC_POINT_SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER, before)) {
        return 0;
    }
    return 42;
}

int main(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    unsigned before = rt_sync_point_reached_count(
        RT_SYNC_POINT_SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER);
    atomic_store_explicit(&g_sync_before, before, memory_order_release);
    uint64_t completed_before = atomic_load_explicit(
        &ex->blocking_completed, memory_order_acquire);
    rt_task* task = (rt_task*)rt_blocking_submit(BLOCKING_FN_QUICK, NULL, 0, 0);
    if (task == NULL) {
        return fail("blocking submit failed");
    }
    if (!rt_sync_point_wait_until_after(
            RT_SYNC_POINT_SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER, before)) {
        return fail("blocking task did not reach the pre-registration window");
    }
    uint64_t deadline = monotonic_millis() + 4000U;
    while (monotonic_millis() < deadline) {
        uint64_t completed = atomic_load_explicit(
            &ex->blocking_completed, memory_order_acquire);
        uint64_t running = atomic_load_explicit(
            &ex->blocking_running, memory_order_acquire);
        if (completed > completed_before && running == 0) {
            break;
        }
        sched_yield();
    }
    rt_blocking_job* job = (rt_blocking_job*)task->state;
    if (job == NULL ||
        atomic_load_explicit(&job->status, memory_order_acquire) != BLOCKING_JOB_DONE ||
        atomic_load_explicit(&ex->blocking_running, memory_order_acquire) != 0) {
        rt_sync_point_open();
        return fail("blocking worker did not publish and drain completion");
    }
    rt_sync_point_open();

#ifdef RV2_DEBT_143_NEGATIVE_CONTROL
    if (!wait_task_status(task, TASK_WAITING)) {
        return fail("negative control did not park the blocking task");
    }
    fputs("negative control reproduced stranded blocking waiter\n", stderr);
    return 86;
#else
    if (!wait_task_status(task, TASK_DONE)) {
        return fail("fixed blocking task missed completion");
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    (void)rt_executor_request_shutdown(ex);
    if (kind != 1 || bits != 42) {
        return fail("fixed blocking task returned the wrong result");
    }
    puts("blocking register-then-verify passed");
    return 0;
#endif
}
`
