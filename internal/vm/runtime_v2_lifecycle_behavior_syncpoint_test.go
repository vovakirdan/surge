//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

func TestRuntimeV2LifecycleDebt023CancelParkWakeTokenProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessSyncPoints(t, false)
	for _, shards := range []string{"1", "2", "8"} {
		shards := shards
		t.Run("positive-shards-"+shards, func(t *testing.T) {
			threads := map[string]string{"1": "4", "2": "2", "8": "8"}[shards]
			env := lifecycleEnv(
				"SURGE_SHARDS="+shards,
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_PARK_BEFORE_WAITING:block")
			stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "debt023-cancel-park-proof", env)
			if exitCode != 0 {
				t.Fatalf("DEBT-023 positive proof failed at SURGE_SHARDS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					shards, exitCode, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt023CancelParkWakeTokenNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessSyncPoints(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_PARK_BEFORE_WAITING:block")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "debt023-cancel-park-proof", env)
	if exitCode == 0 {
		t.Fatalf("DEBT-023 negative-control proof unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr, "debt023 proof target stranded after release") {
		t.Fatalf("DEBT-023 negative-control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func TestRuntimeV2LifecycleDebt020MigrateGapProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt020(t, false)
	for _, shards := range []string{"2", "8"} {
		shards := shards
		t.Run("positive-shards-"+shards, func(t *testing.T) {
			threads := map[string]string{"2": "2", "8": "8"}[shards]
			env := lifecycleEnv(
				"SURGE_SHARDS="+shards,
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_MIGRATE_GAP:block")
			stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "debt020-migrate-gap-proof", env)
			if exitCode != 0 {
				t.Fatalf("DEBT-020 positive proof failed at SURGE_SHARDS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					shards, exitCode, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt020MigrateGapNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt020(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_MIGRATE_GAP:block")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "debt020-migrate-gap-proof", env)
	if exitCode == 0 {
		t.Fatalf("DEBT-020 negative-control proof unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr, "debt020 migrate-gap joiner stranded") {
		t.Fatalf("DEBT-020 negative-control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

const lifecycleHarnessSyncPointModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_CANCEL_PARK_PROOF 4031

static _Atomic uint32_t g_debt020_gap_joiner_enabled;
static _Atomic uint32_t g_debt020_gap_joiner_registered;

static void poll_cancel_park_proof(void) {
    rt_task* self = rt_current_task();
    if (self == NULL) {
        rt_async_return(NULL, 0);
        return;
    }
    if (task_cancelled_load(self) != 0) {
        rt_async_return_cancelled(NULL);
        return;
    }
    void* ch = atomic_load_explicit(&g_park_forever_chan, memory_order_acquire);
    uint64_t bits = 0;
    uint8_t st = rt_channel_recv(ch, &bits);
    if (st != 0 || !waker_valid(pending_key)) {
        rt_async_return(NULL, 0);
        return;
    }
    rt_async_yield(NULL);
}

static void poll_debt020_adopt_joiner(void) {
    void* target = __task_state();
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(target, &bits);
    if (st == 0) {
        rt_async_yield(target);
        return;
    }
    rt_async_return(NULL, st == 1 && bits == 55 ? 55 : 0);
}

static void poll_debt020_gap_joiner(void) {
    void* target = __task_state();
    if (atomic_load_explicit(&g_debt020_gap_joiner_enabled, memory_order_acquire) == 0) {
        rt_async_yield(target);
        return;
    }
    if (atomic_load_explicit(&g_debt020_gap_joiner_registered, memory_order_acquire) == 0) {
        rt_executor* ex = ensure_exec();
        rt_task* self = rt_current_task();
        rt_task* target_task = task_from_handle(target);
        if (ex == NULL || self == NULL || target_task == NULL) {
            rt_async_return(NULL, 2);
            return;
        }
        waker_key key = join_key(target_task->id);
        prepare_park(ex, self, key, 0);
        pending_key = key;
        atomic_store_explicit(&g_debt020_gap_joiner_registered, 1, memory_order_release);
        rt_async_yield(target);
        return;
    }
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(target, &bits);
    if (st == 0) {
        rt_async_yield(target);
        return;
    }
    rt_async_return(NULL, st == 1 ? 1 : 2);
}

static int wait_sync_point_count(rt_sync_point_id id, unsigned before, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (rt_sync_point_reached_count(id) > before) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static int mode_debt020_migrate_gap_proof(rt_executor* ex) {
    unsigned gap_before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_MIGRATE_GAP);
    atomic_store_explicit(&g_debt020_gap_joiner_enabled, 0, memory_order_release);
    atomic_store_explicit(&g_debt020_gap_joiner_registered, 0, memory_order_release);
    rt_task* target = spawn_placed(ex, POLL_ADOPT_TARGET, 1, TASK_PLACEMENT_CONNECTION, NULL);
    if (target == NULL || !wait_task_status(target, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt020 target setup failed");
    }
    rt_control_lock(ex);
    rt_task* adopter = alloc_ready_task(ex, POLL_DEBT020_ADOPT_JOINER);
    if (adopter != NULL) {
        adopter->state = target;
        rt_task_set_placement(adopter, pin_shard(ex, 0), TASK_PLACEMENT_GENERIC);
    }
    void* join_handle = adopter != NULL ? rt_task_clone(adopter) : NULL;
    rt_task* gap_joiner = join_handle != NULL ? alloc_ready_task(ex, POLL_DEBT020_GAP_JOINER) : NULL;
    if (gap_joiner != NULL) {
        gap_joiner->state = join_handle;
        rt_task_set_placement(gap_joiner, pin_shard(ex, 1), TASK_PLACEMENT_GENERIC);
        ready_push(ex, gap_joiner->id);
    }
    if (adopter != NULL) {
        ready_push(ex, adopter->id);
    }
    rt_control_unlock(ex);
    if (adopter == NULL || join_handle == NULL || gap_joiner == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt020 adopter allocation failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_MIGRATE_GAP, gap_before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt020 proof did not reach SP_MIGRATE_GAP");
    }
    if (task_status_load(adopter) != TASK_RUNNING) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        return fail("debt020 adopter was not RUNNING at migrate gap");
    }
    atomic_store_explicit(&g_debt020_gap_joiner_enabled, 1, memory_order_release);
    if (!wait_task_status(gap_joiner, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        return fail("debt020 gap joiner did not register");
    }
    rt_sync_point_open();
    if (!wait_task_status(adopter, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt020 adopter did not complete after release");
    }
    if (!wait_task_status(gap_joiner, TASK_DONE, 1500)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt020 migrate-gap joiner stranded");
    }
    if (!await_expect(ex, gap_joiner, 1, 1, "debt020 gap joiner")) {
        return 1;
    }
    if (!await_expect(ex, adopter, 1, 55, "debt020 adopter")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_debt023_cancel_park_proof(rt_executor* ex) {
    rt_task* maker = spawn_pinned(ex, POLL_MAKE_PARK_FOREVER_CHAN, 0);
    if (maker == NULL || !wait_ptr(&g_park_forever_chan, 4000) ||
        !wait_task_status(maker, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt023 proof channel setup failed");
    }

    unsigned park_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_PARK_BEFORE_WAITING);
    unsigned cancel_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE);

    rt_task* proof = spawn_pinned(ex, POLL_CANCEL_PARK_PROOF, 0);
    if (proof == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt023 proof task allocation failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_PARK_BEFORE_WAITING, park_before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        fprintf(stderr,
                "debt023 proof status/counts before missing park syncpoint: status=%u park_before=%u park_now=%u\n",
                (unsigned)task_status_load(proof),
                park_before,
                rt_sync_point_reached_count(RT_SYNC_POINT_SP_PARK_BEFORE_WAITING));
        return fail("debt023 proof did not reach SP_PARK_BEFORE_WAITING");
    }
    if (task_status_load(proof) != TASK_RUNNING) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        return fail("debt023 proof target was not RUNNING at park syncpoint");
    }
    rt_task_cancel(proof);
    if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE) <= cancel_before) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        return fail("debt023 proof did not reach SP_CANCEL_BEFORE_WAKE");
    }
    rt_sync_point_open();
    if (!wait_task_status(proof, TASK_DONE, 1500)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt023 proof target stranded after release");
    }
    if (!await_expect(ex, proof, 2, 0, "debt023 cancelled park proof")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
