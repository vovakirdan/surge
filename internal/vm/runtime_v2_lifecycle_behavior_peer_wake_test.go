//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// This native-only proof holds a producer inside its poll while its sole peer
// enters the real zero-credit worker wait. One successful try_send makes the
// parked receiver the first entry in the producer's local deque. The receiver
// must run on the peer before the producer returns to scheduler selection.
// The VM does not have native worker condvars or this publication policy.
func TestRuntimeV2LifecycleLocalPeerWakeProof(t *testing.T) {
	skipTimeoutTests(t)
	stdout, stderr, code := runRuntimeV2LocalPeerWake(t, false)
	if code != 0 || !strings.Contains(stdout, "receiver_resumes=1 sent=1 distinct_workers=1") ||
		!strings.Contains(stdout, "local-peer-wake: cleanup_complete=1") {
		t.Fatalf("local peer wake proof failed (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

// Restoring precisely the old local->len > 1 guard must strand the receiver
// until the stand releases the producer. Cleanup is part of the witness: an
// outer timeout, setup failure, or stuck producer is not an accepted mutant.
func TestRuntimeV2LifecycleLocalPeerWakeNegativeControl(t *testing.T) {
	skipTimeoutTests(t)
	stdout, stderr, code := runRuntimeV2LocalPeerWake(t, true)
	const want = "local-peer-wake: receiver_resumes=0 while producer held (sent=1)"
	if code != 1 || !strings.Contains(stderr, want) ||
		!strings.Contains(stdout, "local-peer-wake: cleanup_complete=1") {
		t.Fatalf("local peer wake mutant failed for the wrong reason (code=%d), want %q\nstdout:\n%s\nstderr:\n%s",
			code, want, stdout, stderr)
	}
}

func runRuntimeV2LocalPeerWake(t *testing.T, negative bool) (string, string, int) {
	t.Helper()
	name := "lifecycle_local_peer_wake"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negative {
		name += "_negative"
		flags = append(flags, "-DRV2_LOCAL_PEER_WAKE_NEGATIVE_CONTROL")
	}
	bin := buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
	env := lifecycleEnv("SURGE_SHARDS=1", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
		"SURGE_CHANNEL_WAKE_INJECT=0", "SURGE_SCHED=", "SURGE_SYNC_POINT=",
		"SURGE_SCHED_TRACE=0", "SURGE_TRACE_EXEC=0")
	return runLifecycleHarness(t, bin, "local-peer-wake", env)
}

const lifecycleHarnessPeerWakeModes = `
#ifdef RT_TEST_SYNC_POINTS
#include "rt_channel_refcount.h"

#define POLL_LOCAL_PEER_RECEIVER 4052
#define POLL_LOCAL_PEER_PRODUCER 4053

static _Atomic(void*) g_local_peer_channel;
static _Atomic uint32_t g_local_peer_producer_started;
static _Atomic uint32_t g_local_peer_publish;
static _Atomic uint32_t g_local_peer_release;
static _Atomic uint32_t g_local_peer_sent;
static _Atomic uint32_t g_local_peer_receiver_resumes;
static _Atomic uint32_t g_local_peer_producer_worker;
static _Atomic uint32_t g_local_peer_receiver_worker;

static void poll_local_peer_receiver(void) {
    uint64_t value = 0;
    void* channel = atomic_load_explicit(&g_local_peer_channel, memory_order_acquire);
    if (!rt_channel_recv(channel, &value)) {
        rt_async_yield(NULL, 0);
        return;
    }
    uint32_t worker = tls_worker_ctx != NULL ? tls_worker_ctx->worker_id : UINT32_MAX;
    atomic_store_explicit(&g_local_peer_receiver_worker, worker, memory_order_relaxed);
    atomic_store_explicit(&g_local_peer_receiver_resumes, value == 42 ? 1U : 2U,
                          memory_order_release);
    rt_async_return(NULL, &value);
}

static void poll_local_peer_producer(void) {
    uint32_t worker = tls_worker_ctx != NULL ? tls_worker_ctx->worker_id : UINT32_MAX;
    atomic_store_explicit(&g_local_peer_producer_worker, worker, memory_order_relaxed);
    atomic_store_explicit(&g_local_peer_producer_started, 1, memory_order_release);
    while (atomic_load_explicit(&g_local_peer_publish, memory_order_acquire) == 0 &&
           atomic_load_explicit(&g_local_peer_release, memory_order_acquire) == 0) {
        sleep_us(1000);
    }
    if (atomic_load_explicit(&g_local_peer_release, memory_order_acquire) == 0) {
        uint64_t value = 42;
        void* channel = atomic_load_explicit(&g_local_peer_channel, memory_order_acquire);
        uint32_t sent = rt_channel_try_send(channel, &value) ? 1U : 2U;
        atomic_store_explicit(&g_local_peer_sent, sent, memory_order_release);
    }
    // No runtime yield: this carrier cannot claim its newly queued receiver.
    // Only the driver releases it, after recording success or the named failure.
    while (atomic_load_explicit(&g_local_peer_release, memory_order_acquire) == 0) {
        sleep_us(1000);
    }
    rt_async_return(NULL, &(uint64_t){0});
}

static int local_peer_consume_task(rt_task* task) {
    if (task == NULL) {
        return 1;
    }
    if (!wait_task_status(task, TASK_DONE, 4000)) {
        return 0;
    }
    uint8_t kind = 0;
    uint64_t value = 0;
    // The driver owns this handle until await consumes it. No caller reads
    // the task pointer after this helper returns.
    rt_task_await(task, &kind, &value);
    return kind == TASK_RESULT_SUCCESS || kind == TASK_RESULT_CANCELLED;
}

static int mode_local_peer_wake(rt_executor* ex) {
    const rt_sync_point_id point = RT_SYNC_POINT_SP_WORKER_BEFORE_CREDIT_WAIT;
    rt_shard* owner = rt_runtime_shard0(rt_executor_runtime(ex));
    rt_scheduler* scheduler = rt_shard_scheduler(owner);
    if (scheduler == NULL || scheduler->worker_count != 2 || scheduler->local_queues == NULL ||
        channel_wake_force_inject_enabled()) {
        (void)rt_executor_request_shutdown(ex);
        return fail("local-peer-wake: requires two peer workers and local channel wakes");
    }
    void* channel = rt_channel_new(0, rt_channel_opaque_word_ops(), 0);
    if (channel == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("local-peer-wake: channel allocation failed");
    }
    // This one driver-owned handle outlives both tasks, including every failure
    // cleanup. The tasks borrow it only while their driver-owned handles live.
    atomic_store_explicit(&g_local_peer_channel, channel, memory_order_release);
    rt_task* receiver = spawn_pinned(ex, POLL_LOCAL_PEER_RECEIVER, 0);
    rt_task* producer = NULL;
    const char* failure = NULL;
    int armed = 0;
    if (receiver == NULL || !wait_task_status(receiver, TASK_WAITING, 4000)) {
        failure = "local-peer-wake: receiver did not park during setup";
        goto cleanup;
    }
    producer = spawn_pinned(ex, POLL_LOCAL_PEER_PRODUCER, 0);
    if (producer == NULL || !wait_u32_at_least(&g_local_peer_producer_started, 1, 4000)) {
        failure = "local-peer-wake: producer did not enter its held poll";
        goto cleanup;
    }
    uint32_t publisher = atomic_load_explicit(&g_local_peer_producer_worker, memory_order_acquire);
    rt_shard_lock(owner);
    int empty = scheduler->inject.len == 0 && scheduler->local_queues[0].len == 0 &&
                scheduler->local_queues[1].len == 0;
    if (publisher >= 2 || receiver->carrier_valid != 0 || !empty ||
        task_status_load(receiver) != TASK_WAITING || task_enqueued_load(receiver) != 0 ||
        task_status_load(producer) != TASK_RUNNING || scheduler->wake_pending == UINT32_MAX) {
        rt_shard_unlock(owner);
        failure = "local-peer-wake: setup did not leave one held producer and an empty queue";
        goto cleanup;
    }
    unsigned before = rt_sync_point_reached_count(point);
    // Arm and the single setup wake share the owner lock: arming first and
    // taking that lock later could deadlock behind a peer held at the hook.
    // The peer must consume this credit before it can enter the zero-credit
    // hook. The producer cannot enter it because it is held inside its poll.
    rt_sync_point_arm_block(point);
    armed = 1;
    scheduler->wake_pending++;
    int notified = pthread_cond_signal(&owner->worker_cv);
    rt_shard_unlock(owner);
    if (notified != 0 || !wait_sync_point_count(point, before, 4000)) {
        failure = "local-peer-wake: idle peer did not reach the zero-credit wait";
        goto cleanup;
    }
    // The peer holds owner->lock at the hook. The driver takes no runtime
    // lock here: opening lets cond_wait atomically release it, and only then
    // can the producer publish and notify under that same lock.
    atomic_store_explicit(&g_local_peer_publish, 1, memory_order_release);
    rt_sync_point_disarm(point);
    rt_sync_point_open();
    armed = 0;
    if (!wait_u32_at_least(&g_local_peer_sent, 1, 4000) ||
        atomic_load_explicit(&g_local_peer_sent, memory_order_acquire) != 1) {
        failure = "local-peer-wake: the single try_send did not succeed";
        goto cleanup;
    }
    if (!wait_u32_at_least(&g_local_peer_receiver_resumes, 1, 4000)) {
        failure = "local-peer-wake: receiver_resumes=0 while producer held (sent=1)";
        goto cleanup;
    }
    uint32_t resumed = atomic_load_explicit(&g_local_peer_receiver_worker, memory_order_acquire);
    if (atomic_load_explicit(&g_local_peer_receiver_resumes, memory_order_acquire) != 1 ||
        resumed >= 2 || resumed == publisher) {
        failure = "local-peer-wake: receiver resumed with wrong value or on the held producer";
        goto cleanup;
    }
    puts("local-peer-wake: receiver_resumes=1 sent=1 distinct_workers=1");

cleanup:
    // Every exit opens the peer's held lock before cancellation/shutdown can
    // acquire it, and releases the producer before waiting for either task.
    if (armed) {
        rt_sync_point_disarm(point);
        rt_sync_point_open();
    }
    atomic_store_explicit(&g_local_peer_release, 1, memory_order_release);
    if (failure != NULL && receiver != NULL) {
        rt_task_cancel(receiver);
    }
    int producer_done = local_peer_consume_task(producer);
    int receiver_done = local_peer_consume_task(receiver);
    if (producer_done && receiver_done) {
        rt_channel_handle_drop(channel);
    }
    rt_runtime_status shutdown = rt_executor_request_shutdown(ex);
    if (!producer_done || !receiver_done || shutdown != RT_RUNTIME_STATUS_OK) {
        return fail("local-peer-wake: task cleanup did not complete");
    }
    puts("local-peer-wake: cleanup_complete=1");
    return failure != NULL ? fail(failure) : 0;
}
#endif
`
