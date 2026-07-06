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

const lifecycleHarnessSyncPointModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_CANCEL_PARK_PROOF 4029

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

static int wait_sync_point_count(rt_sync_point_id id, unsigned before, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (rt_sync_point_reached_count(id) > before) {
            return 1;
        }
        sleep_us(1000);
    }
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
