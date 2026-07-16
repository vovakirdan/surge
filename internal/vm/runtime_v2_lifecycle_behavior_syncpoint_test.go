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

func TestRuntimeV2LifecycleDebt022DoneCVStoreLoadProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt022(t, false)
	for _, shards := range []string{"1", "2", "8"} {
		shards := shards
		t.Run("positive-shards-"+shards, func(t *testing.T) {
			threads := map[string]string{"1": "4", "2": "2", "8": "8"}[shards]
			env := lifecycleEnv(
				"SURGE_SHARDS="+shards,
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_AWAIT_BEFORE_DONECV_WAIT:block")
			stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "debt022-donecv-storeload-proof", env)
			if exitCode != 0 {
				t.Fatalf("DEBT-022 positive proof failed at SURGE_SHARDS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					shards, exitCode, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt022DoneCVStoreLoadNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt022(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_AWAIT_BEFORE_DONECV_WAIT:block")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "debt022-donecv-storeload-proof", env)
	if exitCode == 0 {
		t.Fatalf("DEBT-022 negative-control proof unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr, "debt022 external awaiter stranded before done_cv wait") {
		t.Fatalf("DEBT-022 negative-control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func TestRuntimeV2LifecycleDebt022ExternalAwaitMatrix(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt022(t, false)
	for _, mode := range []string{
		"debt022-multi-awaiters",
		"debt022-already-done",
		"debt022-parked-target",
		"debt022-cancelled-parked-target",
	} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=2",
				"SURGE_THREADS=2",
				"SURGE_BLOCKING_THREADS=1")
			stdout, stderr, exitCode := runLifecycleHarness(t, binPath, mode, env)
			if exitCode != 0 {
				t.Fatalf("DEBT-022 matrix mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					mode, exitCode, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt046JoinStaleRemovalProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt046(t, false)
	for _, shards := range []string{"1", "2", "8"} {
		shards := shards
		t.Run("positive-shards-"+shards, func(t *testing.T) {
			threads := map[string]string{"1": "4", "2": "2", "8": "8"}[shards]
			env := lifecycleEnv(
				"SURGE_SHARDS="+shards,
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_WAKE_BEFORE_STALE_REMOVAL:block")
			stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "debt046-join-stale-removal-proof", env)
			if exitCode != 0 {
				t.Fatalf("DEBT-046 positive proof failed at SURGE_SHARDS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					shards, exitCode, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt046JoinStaleRemovalNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt046(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS=4",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_WAKE_BEFORE_STALE_REMOVAL:block")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "debt046-join-stale-removal-proof", env)
	if exitCode == 0 {
		t.Fatalf("DEBT-046 negative-control proof unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr, "debt046 joiner stranded after stale removal") {
		t.Fatalf("DEBT-046 negative-control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

const lifecycleHarnessSyncPointModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_CANCEL_PARK_PROOF 4031

static _Atomic uint32_t g_debt020_gap_joiner_enabled;
static _Atomic uint32_t g_debt020_gap_joiner_registered;
static _Atomic uint32_t g_debt022_target_gate;
static _Atomic uint32_t g_debt022_awaiter_done;
static _Atomic uint32_t g_debt022_awaiter_kind;
static _Atomic uint32_t g_debt022_awaiter_bad;
static _Atomic uint64_t g_debt022_awaiter_bits;
static _Atomic uint32_t g_debt022_expected_kind;
static _Atomic uint64_t g_debt022_expected_bits;

static void poll_debt022_gated_target(void) {
    if (atomic_load_explicit(&g_debt022_target_gate, memory_order_acquire) == 0) {
        rt_async_yield(NULL);
        return;
    }
    rt_async_return(NULL, 77);
}

static void* debt022_awaiter_thread(void* arg) {
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(arg, &kind, &bits);
    uint32_t expected_kind = atomic_load_explicit(&g_debt022_expected_kind, memory_order_acquire);
    uint64_t expected = atomic_load_explicit(&g_debt022_expected_bits, memory_order_acquire);
    atomic_store_explicit(&g_debt022_awaiter_bits, bits, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_kind, kind, memory_order_release);
    if (kind != expected_kind || bits != expected) {
        atomic_store_explicit(&g_debt022_awaiter_bad, 1, memory_order_release);
    }
    atomic_fetch_add_explicit(&g_debt022_awaiter_done, 1, memory_order_acq_rel);
    return NULL;
}

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

static int mode_debt022_donecv_storeload_proof(rt_executor* ex) {
    unsigned await_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_AWAIT_BEFORE_DONECV_WAIT);
    unsigned mark_done_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_MARKDONE_BEFORE_DONEWAITERS_LOAD);
    atomic_store_explicit(&g_debt022_target_gate, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_done, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_kind, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_bad, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_bits, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_kind, 1, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_bits, 77, memory_order_release);

    rt_task* target = spawn_pinned(ex, POLL_DEBT022_GATED_TARGET, 1);
    void* await_handle = target != NULL ? rt_task_clone(target) : NULL;
    if (target == NULL || await_handle == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 target allocation failed");
    }

    pthread_t awaiter;
    if (pthread_create(&awaiter, NULL, debt022_awaiter_thread, await_handle) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 awaiter thread failed to start");
    }

    if (!wait_sync_point_count(RT_SYNC_POINT_SP_AWAIT_BEFORE_DONECV_WAIT, await_before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        pthread_join(awaiter, NULL);
        return fail("debt022 awaiter did not reach before-wait window");
    }
    if (task_status_load(target) == TASK_DONE) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        pthread_join(awaiter, NULL);
        return fail("debt022 target completed before awaiter parked");
    }

    atomic_store_explicit(&g_debt022_target_gate, 1, memory_order_release);
#ifdef RV2_DEBT_022_NEGATIVE_CONTROL
    if (!wait_task_status(target, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        pthread_join(awaiter, NULL);
        return fail("debt022 target did not complete");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_MARKDONE_BEFORE_DONEWAITERS_LOAD,
                               mark_done_before,
                               4000)) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        pthread_join(awaiter, NULL);
        return fail("debt022 completer did not reach mark_done window");
    }

    sleep_us(50000);
    rt_sync_point_open();
#else
    sleep_us(50000);
    rt_sync_point_open();
    if (!wait_task_status(target, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        pthread_join(awaiter, NULL);
        return fail("debt022 target did not complete after await release");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_MARKDONE_BEFORE_DONEWAITERS_LOAD,
                               mark_done_before,
                               4000)) {
        (void)rt_executor_request_shutdown(ex);
        pthread_join(awaiter, NULL);
        return fail("debt022 completer did not reach mark_done window");
    }
#endif
    if (!wait_u32_at_least(&g_debt022_awaiter_done, 1, 1500)) {
        (void)rt_executor_request_shutdown(ex);
        pthread_join(awaiter, NULL);
        return fail("debt022 external awaiter stranded before done_cv wait");
    }
    pthread_join(awaiter, NULL);
    if (atomic_load_explicit(&g_debt022_awaiter_bad, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 awaiter observed wrong result");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_debt022_multi_awaiters(rt_executor* ex) {
    atomic_store_explicit(&g_debt022_target_gate, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_done, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_bad, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_kind, 1, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_bits, 77, memory_order_release);
    rt_task* target = spawn_pinned(ex, POLL_DEBT022_GATED_TARGET, 1);
    void* handle_a = target != NULL ? rt_task_clone(target) : NULL;
    void* handle_b = target != NULL ? rt_task_clone(target) : NULL;
    if (target == NULL || handle_a == NULL || handle_b == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 multi-await target allocation failed");
    }
    pthread_t awaiter_a;
    pthread_t awaiter_b;
    if (pthread_create(&awaiter_a, NULL, debt022_awaiter_thread, handle_a) != 0 ||
        pthread_create(&awaiter_b, NULL, debt022_awaiter_thread, handle_b) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 multi-await thread failed to start");
    }
    for (uint32_t i = 0; i < 4000; i++) {
        if (atomic_load_explicit(&ex->done_waiters, memory_order_acquire) >= 2) {
            break;
        }
        sleep_us(1000);
    }
    if (atomic_load_explicit(&ex->done_waiters, memory_order_acquire) < 2) {
        (void)rt_executor_request_shutdown(ex);
        pthread_join(awaiter_a, NULL);
        pthread_join(awaiter_b, NULL);
        return fail("debt022 multi-awaiters did not both park");
    }
    atomic_store_explicit(&g_debt022_target_gate, 1, memory_order_release);
    if (!wait_u32_at_least(&g_debt022_awaiter_done, 2, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        pthread_join(awaiter_a, NULL);
        pthread_join(awaiter_b, NULL);
        return fail("debt022 broadcast did not wake both awaiters");
    }
    pthread_join(awaiter_a, NULL);
    pthread_join(awaiter_b, NULL);
    if (atomic_load_explicit(&g_debt022_awaiter_bad, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 multi-await observed wrong result");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_debt022_already_done(rt_executor* ex) {
    atomic_store_explicit(&g_debt022_target_gate, 1, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_kind, 1, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_bits, 77, memory_order_release);
    rt_task* target = spawn_pinned(ex, POLL_DEBT022_GATED_TARGET, 1);
    if (target == NULL || !wait_task_status(target, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 already-done target setup failed");
    }
    if (!await_expect(ex, target, 1, 77, "debt022 already-done target")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_debt022_parked_target(rt_executor* ex) {
    atomic_store_explicit(&g_park_forever_chan, NULL, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_done, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_bad, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_kind, 1, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_bits, 99, memory_order_release);

    rt_task* maker = spawn_pinned(ex, POLL_MAKE_PARK_FOREVER_CHAN, 0);
    if (maker == NULL || !wait_ptr(&g_park_forever_chan, 4000) ||
        !wait_task_status(maker, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 parked-target channel setup failed");
    }

    rt_task* target = spawn_pinned(ex, POLL_PARK_FOREVER, 1);
    void* await_handle = target != NULL ? rt_task_clone(target) : NULL;
    if (target == NULL || await_handle == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 parked-target allocation failed");
    }
    if (!wait_task_status(target, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 target did not park before external await");
    }

    pthread_t awaiter;
    if (pthread_create(&awaiter, NULL, debt022_awaiter_thread, await_handle) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 parked-target awaiter thread failed to start");
    }
    for (uint32_t i = 0; i < 4000; i++) {
        if (atomic_load_explicit(&ex->done_waiters, memory_order_acquire) >= 1) {
            break;
        }
        sleep_us(1000);
    }
    if (atomic_load_explicit(&ex->done_waiters, memory_order_acquire) < 1) {
        (void)rt_executor_request_shutdown(ex);
        pthread_join(awaiter, NULL);
        return fail("debt022 parked-target awaiter did not park");
    }

    void* ch = atomic_load_explicit(&g_park_forever_chan, memory_order_acquire);
    rt_channel_send_blocking(ch, 99);
    if (!wait_u32_at_least(&g_debt022_awaiter_done, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        pthread_join(awaiter, NULL);
        return fail("debt022 parked-target awaiter was not woken");
    }
    pthread_join(awaiter, NULL);
    if (atomic_load_explicit(&g_debt022_awaiter_bad, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 parked-target observed wrong result");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_debt022_cancelled_parked_target(rt_executor* ex) {
    atomic_store_explicit(&g_park_forever_chan, NULL, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_done, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_awaiter_bad, 0, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_kind, 2, memory_order_release);
    atomic_store_explicit(&g_debt022_expected_bits, 0, memory_order_release);

    rt_task* maker = spawn_pinned(ex, POLL_MAKE_PARK_FOREVER_CHAN, 0);
    if (maker == NULL || !wait_ptr(&g_park_forever_chan, 4000) ||
        !wait_task_status(maker, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 cancelled-target channel setup failed");
    }

    rt_task* target = spawn_pinned(ex, POLL_PARK_FOREVER, 1);
    void* await_handle = target != NULL ? rt_task_clone(target) : NULL;
    if (target == NULL || await_handle == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 cancelled-target allocation failed");
    }
    if (!wait_task_status(target, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 target did not park before cancel");
    }

    pthread_t awaiter;
    if (pthread_create(&awaiter, NULL, debt022_awaiter_thread, await_handle) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 cancelled-target awaiter thread failed to start");
    }
    for (uint32_t i = 0; i < 4000; i++) {
        if (atomic_load_explicit(&ex->done_waiters, memory_order_acquire) >= 1) {
            break;
        }
        sleep_us(1000);
    }
    if (atomic_load_explicit(&ex->done_waiters, memory_order_acquire) < 1) {
        (void)rt_executor_request_shutdown(ex);
        pthread_join(awaiter, NULL);
        return fail("debt022 cancelled-target awaiter did not park");
    }

    rt_task_cancel(target);
    if (!wait_u32_at_least(&g_debt022_awaiter_done, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        pthread_join(awaiter, NULL);
        return fail("debt022 cancelled-target awaiter was not woken");
    }
    pthread_join(awaiter, NULL);
    if (atomic_load_explicit(&g_debt022_awaiter_bad, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt022 cancelled-target observed wrong result");
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

// RV2-DEBT-046 proof: a spurious wake_task(remove_waiter_flag=1) consumes a
// join park and defers the stale-key removal; the woken joiner re-polls and
// re-parks on the SAME join key inside that window. The deferred removal is
// unqualified for join keys (their entries carry no park generation), so the
// pre-fix behavior sweeps the fresh registration too and the joiner never
// hears the target's completion (join wakes are store-driven).
static _Atomic uint32_t g_debt046_wake_done;

static void* debt046_wake_thread(void* arg) {
    rt_executor* ex = ensure_exec();
    wake_task(ex, (uint64_t)(uintptr_t)arg, 1);
    atomic_fetch_add_explicit(&g_debt046_wake_done, 1, memory_order_acq_rel);
    return NULL;
}

static void poll_debt046_joiner(void) {
    void* target = __task_state();
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(target, &bits);
    if (st == 0) {
        rt_async_yield(target);
        return;
    }
    rt_async_return(NULL, st == 1 && bits == 77 ? 77 : 0);
}

static int mode_debt046_join_stale_removal_proof(rt_executor* ex) {
    unsigned sp_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL);
    atomic_store_explicit(&g_debt022_target_gate, 0, memory_order_release);
    atomic_store_explicit(&g_debt046_wake_done, 0, memory_order_release);

    rt_task* target = spawn_pinned(ex, POLL_DEBT022_GATED_TARGET, 0);
    void* join_handle = target != NULL ? rt_task_clone(target) : NULL;
    if (target == NULL || join_handle == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt046 target allocation failed");
    }
    rt_control_lock(ex);
    rt_task* joiner = alloc_ready_task(ex, POLL_DEBT046_JOINER);
    if (joiner != NULL) {
        joiner->state = join_handle;
        rt_task_set_placement(joiner, pin_shard(ex, 0), TASK_PLACEMENT_GENERIC);
        ready_push(ex, joiner->id);
    }
    rt_control_unlock(ex);
    if (joiner == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt046 joiner allocation failed");
    }
    if (!wait_task_status(joiner, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt046 joiner did not park on the join key");
    }

    pthread_t waker;
    if (pthread_create(&waker, NULL, debt046_wake_thread, (void*)(uintptr_t)joiner->id) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt046 wake thread create failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL, sp_before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        pthread_join(waker, NULL);
        return fail("debt046 proof did not reach SP_WAKE_BEFORE_STALE_REMOVAL");
    }
    // The spurious wake consumed the park; the joiner must re-poll and
    // re-park on the same join key while the removal is still held.
    if (!wait_task_status(joiner, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        pthread_join(waker, NULL);
        return fail("debt046 joiner did not re-park inside the removal window");
    }
    rt_sync_point_open();
    pthread_join(waker, NULL);
    if (atomic_load_explicit(&g_debt046_wake_done, memory_order_acquire) == 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt046 wake thread did not finish");
    }

    atomic_store_explicit(&g_debt022_target_gate, 1, memory_order_release);
    if (!wait_task_status(target, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt046 target did not complete");
    }
    if (!wait_task_status(joiner, TASK_DONE, 1500)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt046 joiner stranded after stale removal");
    }
    if (!await_expect(ex, joiner, 1, 77, "debt046 joiner")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
