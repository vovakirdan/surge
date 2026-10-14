//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// The carrier can complete a running task after its external last-handle drop
// reaches zero. A delayed release must decide from its own RMW, without reading
// the task after the carrier has reclaimed it (RV2-DEBT-291).
func TestRuntimeV2LifecycleTaskFreeIsOneObservation(t *testing.T) {
	// All asserted frames are in the locally built runtime; do not wait for
	// network debug-info downloads while symbolizing the negative control.
	t.Setenv("DEBUGINFOD_URLS", "")
	for _, negative := range []bool{false, true} {
		name := "packed"
		flags := []string{"-DRT_TEST_SYNC_POINTS", "-fsanitize=address", "-g", "-O1", "-fno-omit-frame-pointer"}
		if negative {
			name = "split_negative"
			flags = append(flags, "-DRT_TASK_RELEASE_SPLIT_NEGATIVE_CONTROL")
		}
		t.Run(name, func(t *testing.T) {
			bin := buildRuntimeV2LifecycleHarnessWithFlags(t, "task_ref_"+name, flags)
			stdout, stderr, code := runLifecycleHarness(t, bin, "task-ref-one-observation", lifecycleEnv(
				"SURGE_SHARDS=1", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_TASK_RELEASE_AFTER_ZERO_BEFORE_STATUS:block",
				"ASAN_OPTIONS=detect_leaks=0:halt_on_error=1:abort_on_error=0"))
			for _, want := range []string{
				"task-release window: refs=0 completed=0",
				"task-release carrier completion: freed=1",
			} {
				if !strings.Contains(stderr, want) {
					t.Fatalf("release window missing %q (code=%d)\nstdout:\n%s\nstderr:\n%s", want, code, stdout, stderr)
				}
			}
			if !negative {
				if code != 0 || strings.Contains(stderr, "AddressSanitizer") {
					t.Fatalf("packed release failed (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
				}
				t.Log("last external handle dropped; carrier reclaimed the task before release resumed; no ASan error")
				return
			}
			fault, _, _ := strings.Cut(stderr, " is located ")
			if code == 0 || !strings.Contains(fault, "heap-use-after-free") ||
				!strings.Contains(fault, "task_drop_ref_owes_free") {
				t.Fatalf("split release did not fail at its stale status read (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			t.Log("same window: split decision reported heap-use-after-free in task_drop_ref_owes_free")
		})
	}
}

const lifecycleHarnessTaskRefObservationModes = `
#ifdef RT_TEST_SYNC_POINTS
#include "rt_task_refs.h"
#define POLL_TASK_REF_OBSERVATION 4055

static _Atomic uint32_t g_task_ref_running;
static _Atomic uint32_t g_task_ref_complete;

static void poll_task_ref_observation(void) {
    atomic_store_explicit(&g_task_ref_running, 1, memory_order_release);
    if (!wait_u32_at_least(&g_task_ref_complete, 1, 4000)) {
        panic_msg("task-ref carrier was never allowed to complete");
    }
    rt_async_return(NULL, NULL);
}

typedef struct {
    rt_task* task;
} task_ref_release_args;

static void* task_ref_release_thread(void* raw) {
    task_ref_release_args* args = (task_ref_release_args*)raw;
    rt_task_handle_drop(args->task);
    return NULL;
}

static int mode_task_ref_one_observation(rt_executor* ex) {
    rt_task* task = spawn_pinned(ex, POLL_TASK_REF_OBSERVATION, 0);
    if (task == NULL || !wait_u32_at_least(&g_task_ref_running, 1, 4000)) {
        return fail("task-ref owner carrier did not start its poll");
    }
    uint64_t id = task->id;
    task_ref_release_args args = {task};
    pthread_t release_thread;
    unsigned before = rt_sync_point_reached_count(
        RT_SYNC_POINT_SP_TASK_RELEASE_AFTER_ZERO_BEFORE_STATUS);
    if (pthread_create(&release_thread, NULL, task_ref_release_thread, &args) != 0) {
        return fail("task-ref release thread failed to start");
    }
    if (!wait_sync_point_count(
            RT_SYNC_POINT_SP_TASK_RELEASE_AFTER_ZERO_BEFORE_STATUS, before, 4000)) {
        rt_sync_point_open();
        atomic_store_explicit(&g_task_ref_complete, 1, memory_order_release);
        pthread_join(release_thread, NULL);
        return fail("release did not reach its zero-ref window");
    }
    uint32_t refs = atomic_load_explicit(&task->handle_refs, memory_order_acquire);
    fprintf(stderr, "task-release window: refs=%u completed=%u\n",
            refs & RT_TASK_REFS_COUNT_MASK, (refs & RT_TASK_REFS_COMPLETED) != 0);
    atomic_store_explicit(&g_task_ref_complete, 1, memory_order_release);
    int freed = 0;
    for (unsigned i = 0; i < 4000 && !freed; i++) {
        rt_control_lock(ex);
        freed = get_task(ex, id) == NULL;
        rt_control_unlock(ex);
        if (!freed) {
            sleep_us(1000);
        }
    }
    fprintf(stderr, "task-release carrier completion: freed=%d\n", freed);
    rt_sync_point_open();
    pthread_join(release_thread, NULL);
    (void)rt_executor_request_shutdown(ex);
    return freed ? 0 : fail("carrier did not reclaim its completed zero-ref task");
}
#else
static int mode_task_ref_one_observation(rt_executor* ex) {
    (void)ex;
    return fail("task-ref proof requires sync points");
}
#endif
`
