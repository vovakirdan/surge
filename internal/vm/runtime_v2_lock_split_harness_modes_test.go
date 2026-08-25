//go:build runtime_v2_pending

package vm_test

// The lock-split stand's MODES, split out of the file that builds them so
// neither carries the whole program. The prologue file owns the task/channel
// scaffolding and main(); this owns the scenarios that scaffolding exists for.
const lockSplitHarnessModes = `
static int mode_cross_join(rt_executor* ex) {
    rt_task* target = spawn_pinned(ex, POLL_YIELDER, 1);
    if (target == NULL) {
        return fail("target allocation failed");
    }
    g_join_target = target;
    rt_task* joiner = spawn_pinned(ex, POLL_JOINER, 0);
    if (joiner == NULL) {
        return fail("joiner allocation failed");
    }
    if (!await_expect(ex, joiner, 1, 7, "cross-shard joiner")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_cross_cancel(rt_executor* ex) {
    if (!make_chan_on(ex, 2)) {
        return fail("channel maker failed");
    }
    rt_task* gate = spawn_pinned(ex, POLL_PARK_RECV, 1);
    if (gate == NULL) {
        return fail("gate allocation failed");
    }
    if (!wait_task_status(gate, TASK_WAITING, 4000)) {
        return fail("gate did not park on the channel");
    }
    rt_task_cancel(gate);
    if (!await_expect(ex, gate, 2, 0, "cancelled cross-shard receiver")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_cross_channel(rt_executor* ex) {
    if (!make_chan_on(ex, 2)) {
        return fail("channel maker failed");
    }
    rt_task* receiver = spawn_pinned(ex, POLL_RECEIVER_FIFO, 0);
    rt_task* sender = spawn_pinned(ex, POLL_SENDER, 1);
    if (receiver == NULL || sender == NULL) {
        return fail("actor allocation failed");
    }
    if (!await_expect(ex, sender, 1, 0, "cross-shard sender")) {
        return 1;
    }
    if (!await_expect(ex, receiver, 1, 0, "cross-shard receiver")) {
        return 1;
    }
    if (atomic_load_explicit(&g_recv_count, memory_order_acquire) != LOCK_SPLIT_FIFO_VALUES) {
        return fail("receiver did not observe every value");
    }
    if (atomic_load_explicit(&g_recv_bad_order, memory_order_acquire) != 0) {
        return fail("channel FIFO order violated");
    }
    if (atomic_load_explicit(&g_recv_closed, memory_order_acquire) != 1) {
        return fail("receiver missed channel close");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_close_wakes(rt_executor* ex) {
    if (!make_chan_on(ex, 1)) {
        return fail("channel maker failed");
    }
    rt_task* gate = spawn_pinned(ex, POLL_PARK_RECV, 2);
    if (gate == NULL) {
        return fail("gate allocation failed");
    }
    if (!wait_task_status(gate, TASK_WAITING, 4000)) {
        return fail("receiver did not park on the channel");
    }
    rt_channel_close(atomic_load_explicit(&g_chan_a, memory_order_acquire));
    if (!await_expect(ex, gate, 1, 2, "close-woken receiver")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_blocking_completion(rt_executor* ex) {
    rt_task* awaiter = spawn_pinned(ex, POLL_BLOCKING_AWAITER, 1);
    if (awaiter == NULL) {
        return fail("awaiter allocation failed");
    }
    if (!await_expect(ex, awaiter, 1, 42, "blocking awaiter")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_sleep_idle_advance(rt_executor* ex) {
    rt_task* sleeper = spawn_pinned(ex, POLL_SLEEPER, 1);
    if (sleeper == NULL) {
        return fail("sleeper allocation failed");
    }
    if (!await_expect(ex, sleeper, 1, 9, "idle-advanced sleeper")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_select_across_owners(rt_executor* ex) {
    if (!make_chan_on(ex, 1)) {
        return fail("channel maker failed");
    }
    rt_task* gate = spawn_pinned(ex, POLL_PARK_RECV, 1);
    rt_task* yielder = spawn_pinned(ex, POLL_YIELDER, 2);
    if (gate == NULL || yielder == NULL) {
        return fail("arm allocation failed");
    }
    g_select_arm0 = gate;
    g_select_arm1 = yielder;
    rt_task* selector = spawn_pinned(ex, POLL_SELECTOR_TASKS, 0);
    if (selector == NULL) {
        return fail("selector allocation failed");
    }
    if (!await_expect(ex, selector, 1, 1, "cross-owner selector")) {
        return 1;
    }
    rt_task_cancel(gate);
    if (!await_expect(ex, gate, 2, 0, "losing select arm")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_timeout_across_owners(rt_executor* ex) {
    if (!make_chan_on(ex, 1)) {
        return fail("channel maker failed");
    }
    rt_task* gate = spawn_pinned(ex, POLL_PARK_RECV, 2);
    if (gate == NULL) {
        return fail("gate allocation failed");
    }
    if (!wait_task_status(gate, TASK_WAITING, 4000)) {
        return fail("gate did not park on the channel");
    }
    // rt_timeout_poll releases the target handle on the timeout path; keep a
    // main-owned reference so the later status wait and await stay valid.
    g_join_target = rt_task_clone(gate, NULL);
    if (g_join_target == NULL) {
        return fail("gate clone failed");
    }
    rt_task* awaiter = spawn_pinned(ex, POLL_TIMEOUT_AWAITER, 1);
    if (awaiter == NULL) {
        return fail("timeout awaiter allocation failed");
    }
    if (!await_expect(ex, awaiter, 1, 2, "timeout awaiter")) {
        return 1;
    }
    if (!await_expect(ex, gate, 2, 0, "timed-out target")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_shutdown_liveness(rt_executor* ex) {
    atomic_store_explicit(&g_chan_a, rt_channel_new(0, rt_channel_opaque_word_ops(), 0), memory_order_release);
    rt_task* gates[3];
    for (uint32_t i = 0; i < 3; i++) {
        gates[i] = spawn_pinned(ex, POLL_PARK_RECV, i);
        if (gates[i] == NULL) {
            return fail("gate allocation failed");
        }
    }
    for (uint32_t i = 0; i < 3; i++) {
        if (!wait_task_status(gates[i], TASK_WAITING, 4000)) {
            return fail("a receiver did not park before shutdown");
        }
    }
    if (rt_executor_request_shutdown(ex) != RT_RUNTIME_STATUS_OK) {
        return fail("shutdown request failed");
    }
    return 0;
}
`

// The whole stand, in build order: scaffolding, then the modes, then main().
const lockSplitHarness = lockSplitHarnessPrologue + lockSplitHarnessModes + lockSplitHarnessMain
