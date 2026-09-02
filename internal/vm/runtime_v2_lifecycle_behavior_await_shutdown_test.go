//go:build runtime_v2_pending

package vm_test

import (
	"testing"
)

// TestRuntimeV2LifecycleWorkerAwaitVsExternalAwait proves worker-side join
// (rt_task_poll, the owner-lane path rule 2 describes) and external
// multi-worker rt_task_await (rt_async_task.c:183-217, the done_cv
// compatibility path rule 5 names) both observe correct results when driven
// concurrently: a non-worker thread (this harness's main()) calls
// rt_task_await directly while a separately spawned worker-side joiner task
// uses rt_task_poll on its own target. Guards rule 5 for the await path.
func TestRuntimeV2LifecycleWorkerAwaitVsExternalAwait(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	env := lifecycleEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "external-await", env)
	if exitCode != 0 {
		t.Fatalf("worker-await vs external-await failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// TestRuntimeV2LifecycleShutdownWithParkedTasks parks tasks simultaneously in
// join, scope join-all, timer/sleep, channel recv, and blocking-completion
// waits (the epic's focused-probe list: "shutdown with tasks parked in join,
// scope, timer, channel, blocking, and net waits"), asserts
// rt_debug_assert_no_parked_with_work (rt_scheduler_placement.c:126, the same
// invariant TestRuntimeV2SchedulerPlacementParkedWithWorkInvariant proves) on
// every shard immediately beforehand, then requests a clean shutdown.
//
// Net-parked shutdown is deliberately not duplicated here: LIVENESS_PROBES.md
// already names TestRuntimeV2NetPollerShutdownWakesEveryShard (wired into
// `make runtime-v2-accept-check`, Makefile:124) as the focused probe for
// shutdown waking every shard's net waiters, and building a real listening
// socket into this synthetic harness would add net-fd plumbing this task's
// scope does not need.
func TestRuntimeV2LifecycleShutdownWithParkedTasks(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	env := lifecycleEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "shutdown-parked", env)
	if exitCode != 0 {
		t.Fatalf("shutdown-with-parked-tasks failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// lifecycleHarnessMain holds the per-mode drivers and main. The
// __surge_poll_call dispatcher and the drop stubs it used to carry moved to
// lifecycleHarnessPollDispatch
// (runtime_v2_lifecycle_behavior_poll_dispatch_test.go), which every new stand
// adds a case to. Concatenated last (see
// buildRuntimeV2LifecycleHarnessWithFlags in
// runtime_v2_lifecycle_behavior_harness_test.go) into one translation unit.
const lifecycleHarnessMain = `

static int mode_scope_basic(rt_executor* ex) {
    atomic_store_explicit(&g_scope_owner_phase, 0, memory_order_relaxed);
    atomic_store_explicit(&g_scope_handle, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_scope_child_a, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_scope_child_b, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_scope_spin_steps, 0, memory_order_relaxed);
    rt_task* owner = spawn_pinned(ex, POLL_SCOPE_OWNER, 0);
    if (owner == NULL) {
        return fail("scope owner allocation failed");
    }
    if (!wait_ptr(&g_scope_child_a, 4000) || !wait_ptr(&g_scope_child_b, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope children were not spawned");
    }
    if (!await_expect(ex, owner, 1, 0, "scope owner")) {
        return 1;
    }
    void* handle = atomic_load_explicit(&g_scope_handle, memory_order_acquire);
    uint64_t scope_id = (uint64_t)(uintptr_t)handle;
    rt_control_lock(ex);
    rt_scope* scope_after_exit = get_scope(ex, scope_id);
    rt_control_unlock(ex);
    if (scope_after_exit != NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope was not cleared after exit");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_scope_cancelled_poll_teardown(rt_executor* ex) {
    atomic_store_explicit(&g_cancel_owner_phase, 0, memory_order_relaxed);
    atomic_store_explicit(&g_cancel_scope_handle, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_cancel_child, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_park_forever_chan, NULL, memory_order_relaxed);
    rt_task* chan_maker = spawn_pinned(ex, POLL_MAKE_PARK_FOREVER_CHAN, 0);
    if (chan_maker == NULL || !wait_ptr(&g_park_forever_chan, 4000) ||
        !wait_task_status(chan_maker, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("cancel-owner channel setup failed");
    }
#ifdef RT_TEST_SYNC_POINTS
    unsigned teardown_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_SCOPE_TEARDOWN_BEFORE_REGISTER);
#endif
    rt_task* owner = spawn_pinned(ex, POLL_SCOPE_CANCEL_OWNER, 1);
    if (owner == NULL) {
        return fail("cancel-owner allocation failed");
    }
    if (!wait_ptr(&g_cancel_child, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("cancel-owner did not register its child");
    }
    rt_task* child = (rt_task*)atomic_load_explicit(&g_cancel_child, memory_order_acquire);
    if (!wait_task_status(child, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("cancel-owner child did not park");
    }
    void* handle = atomic_load_explicit(&g_cancel_scope_handle, memory_order_acquire);
    uint64_t scope_id = (uint64_t)(uintptr_t)handle;
    waker_key scope_wait_key = owner->active_scope_key;
#ifdef RT_TEST_SYNC_POINTS
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_SCOPE_TEARDOWN_BEFORE_REGISTER,
                               teardown_before,
                               4000)) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("scope teardown did not reach pre-register window");
    }
    rt_task_cancel(child);
    size_t active = SIZE_MAX;
    rt_shard* scope_owner = rt_waiter_key_shard(ex, scope_wait_key);
    for (uint32_t i = 0; i < 4000 && active != 0; i++) {
        rt_shard_lock(scope_owner);
        rt_scope* live_scope = rt_scope_resolve_key_locked(ex, scope_wait_key);
        active = live_scope != NULL ? live_scope->active_children : 0;
        rt_shard_unlock(scope_owner);
        if (active != 0) {
            sleep_us(1000);
        }
    }
    if (active != 0) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        fprintf(stderr,
                "scope child did not drain inside teardown gap: active=%zu child=%u registered=%u creation_scope=%llu\n",
                active,
                (unsigned)task_status_load(child),
                (unsigned)child->scope_registered,
                (unsigned long long)child->creation_scope_key.id);
        return 1;
    }
    rt_sync_point_open();
#ifdef RV2_DEBT_281_NEGATIVE_CONTROL
    if (!wait_task_status(owner, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("negative-control owner did not strand in WAITING");
    }
    size_t waiters = 0;
    rt_shard_lock(scope_owner);
    rt_waiter_store* store = rt_waiter_store_for_key(ex, scope_wait_key);
    for (size_t i = 0; store != NULL && i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.key.kind == scope_wait_key.kind && w.key.id == scope_wait_key.id &&
            w.task_id == owner->id) {
            waiters++;
        }
    }
    rt_shard_unlock(scope_owner);
    fprintf(stderr,
            "scope teardown stranded: owner=%u active=%zu waiters=%zu\n",
            (unsigned)task_status_load(owner),
            active,
            waiters);
    (void)rt_executor_request_shutdown(ex);
    return 1;
#endif
#else
    rt_task_cancel(owner);
#endif
    if (!await_expect(ex, owner, 2, 0, "cancelled scope owner")) {
        return 1;
    }
    if (!await_expect(ex, child, 2, 0, "cascaded-cancel scope child")) {
        return 1;
    }
    rt_control_lock(ex);
    rt_scope* scope_after = get_scope(ex, scope_id);
    rt_control_unlock(ex);
    if (scope_after != NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope was not torn down after cancelled owner drained");
    }
    if (rt_runtime_shard_count(rt_executor_runtime(ex)) > 1) {
        rt_shard* stamped =
            rt_runtime_shard(rt_executor_runtime(ex), scope_wait_key.owner_shard_id);
        if (rt_waiter_key_shard(ex, scope_wait_key) != stamped ||
            rt_waiter_store_for_key(ex, scope_wait_key) != rt_shard_waiter_store(stamped)) {
            (void)rt_executor_request_shutdown(ex);
            return fail("scope key lost stamped owner after exit");
        }
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_external_await(rt_executor* ex) {
    atomic_store_explicit(&g_external_await_steps, 0, memory_order_relaxed);
    atomic_store_explicit(&g_join_spin_steps, 0, memory_order_relaxed);
    rt_task* external_target = spawn_pinned(ex, POLL_EXTERNAL_AWAIT_TARGET, 0);
    if (external_target == NULL) {
        return fail("external-await target allocation failed");
    }
    rt_task* worker_target = spawn_pinned(ex, POLL_JOIN_TARGET_SPIN, 1);
    if (worker_target == NULL) {
        return fail("worker-side target allocation failed");
    }
    g_join_target = worker_target;
    rt_task* worker_joiner = spawn_pinned(ex, POLL_JOINER, 0);
    if (worker_joiner == NULL) {
        return fail("worker-side joiner allocation failed");
    }

    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(external_target, &kind, &bits);
    if (kind != 1 || bits != 123) {
        (void)rt_executor_request_shutdown(ex);
        return fail("external await observed the wrong result");
    }
    if (!await_expect(ex, worker_joiner, 1, 42, "worker-side joiner during external await")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_shutdown_parked(rt_executor* ex) {
    // The forever-channel must exist before anything parks on it (no
    // create-time race between the join-parked target and the scope's
    // never-completing child, both POLL_PARK_FOREVER).
    rt_task* chan_maker = spawn_pinned(ex, POLL_MAKE_PARK_FOREVER_CHAN, 0);
    if (chan_maker == NULL || !wait_ptr(&g_park_forever_chan, 4000) ||
        !wait_task_status(chan_maker, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("park-forever channel setup failed");
    }

    rt_task* join_target = spawn_pinned(ex, POLL_PARK_FOREVER, 0);
    if (join_target == NULL) {
        return fail("join-parked target allocation failed");
    }
    g_join_target = join_target;
    rt_task* joiner = spawn_pinned(ex, POLL_JOINER, 0);
    if (joiner == NULL) {
        return fail("join-parked joiner allocation failed");
    }
    if (!wait_task_status(joiner, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("joiner did not park before shutdown");
    }

    atomic_store_explicit(&g_scope_forever_phase, 0, memory_order_relaxed);
    atomic_store_explicit(&g_scope_forever_handle, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_scope_forever_child, NULL, memory_order_relaxed);
    rt_task* scope_owner = spawn_pinned(ex, POLL_SCOPE_OWNER_FOREVER, 1);
    if (scope_owner == NULL) {
        return fail("scope owner allocation failed");
    }
    if (!wait_task_status(scope_owner, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope owner did not park before shutdown");
    }

    // A long rt_sleep is not a stable "stays parked" state in this runtime:
    // tick_virtual/advance_time_to_next_timer (rt_async_state.c:1199-1257)
    // fast-forwards the virtual clock to the next timer deadline once
    // workers go idle, so this task may legitimately observe TASK_WAITING
    // only briefly before completing -- confirmed by direct observation
    // while developing this harness (a 3600000ms sleep fired within ~200ms
    // of real time once nothing else was ready). The shutdown-liveness
    // property this probe actually needs is "no hang", so WAITING or DONE
    // are both acceptable; only getting stuck at TASK_READY forever (a lost
    // wakeup or scheduling bug) would be a real failure.
    rt_task* timer_task = spawn_pinned(ex, POLL_TIMER_PARK, 0);
    if (timer_task == NULL) {
        return fail("timer-parked task allocation failed");
    }
    if (!wait_task_status(timer_task, TASK_WAITING, 4000) &&
        !wait_task_status(timer_task, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("timer task never left TASK_READY before shutdown");
    }

    rt_task* maker = spawn_pinned(ex, POLL_MAKE_CHAN, 1);
    if (maker == NULL || !wait_ptr(&g_chan_a, 4000) || !wait_task_status(maker, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("channel maker failed");
    }
    rt_task* channel_task = spawn_pinned(ex, POLL_CHANNEL_PARK, 1);
    if (channel_task == NULL) {
        return fail("channel-parked task allocation failed");
    }
    if (!wait_task_status(channel_task, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("channel task did not park before shutdown");
    }

    rt_task* blocking_task = spawn_pinned(ex, POLL_BLOCKING_PARK, 0);
    if (blocking_task == NULL) {
        return fail("blocking-parked task allocation failed");
    }
    if (!wait_task_status(blocking_task, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("blocking task did not park before shutdown");
    }

    // No shard may be asleep with queued ready work while five different
    // wait kinds sit parked (LIVENESS_PROBES.md "Parked-with-work
    // invariant"; epic probe list: "no parked-with-work after lifecycle
    // ops"). A real violation panics with "parked-with-work invariant
    // violated" (rt_scheduler_placement.c:126), surfacing as a nonzero exit
    // and that exact stderr text.
    rt_control_lock(ex);
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < shard_count; i++) {
        rt_debug_assert_no_parked_with_work(ex, (uint32_t)i);
    }
    rt_control_unlock(ex);

    if (rt_executor_request_shutdown(ex) != RT_RUNTIME_STATUS_OK) {
        return fail("shutdown request failed");
    }
    return 0;
}

int main(int argc, char** argv) {
    if (argc != 2) {
        return fail("usage: lifecycle_harness <mode>");
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    if (strcmp(argv[1], "owner-local-create") == 0) {
        return mode_owner_local_create(ex);
    }
    if (strcmp(argv[1], "join-same-shard") == 0) {
        return mode_join_same_shard(ex);
    }
    if (strcmp(argv[1], "join-cross-shard") == 0) {
        return mode_join_cross_shard(ex);
    }
    if (strcmp(argv[1], "join-cleanup-pre-done") == 0) {
        return mode_join_cleanup_pre_done(ex);
    }
    if (strcmp(argv[1], "join-cleanup-race-stress") == 0) {
        return mode_join_cleanup_race_stress(ex);
    }
    if (strcmp(argv[1], "join-cleanup-post-park") == 0) {
        return mode_join_cleanup_post_park(ex);
    }
    if (strcmp(argv[1], "clone-release-stress") == 0) {
        return mode_clone_release_stress(ex);
    }
    if (strcmp(argv[1], "completion-pin-stress") == 0) {
        return mode_completion_pin_stress(ex);
    }
    if (strcmp(argv[1], "scope-basic") == 0) {
        return mode_scope_basic(ex);
    }
    if (strcmp(argv[1], "scope-failfast") == 0) {
        return mode_scope_failfast(ex);
    }
    if (strcmp(argv[1], "scope-cancelled-poll-teardown") == 0) {
        return mode_scope_cancelled_poll_teardown(ex);
    }
    if (strcmp(argv[1], "external-await") == 0) {
        return mode_external_await(ex);
    }
    if (strcmp(argv[1], "shutdown-parked") == 0) {
        return mode_shutdown_parked(ex);
    }
    if (strcmp(argv[1], "placement-adopt-positive") == 0) {
        return mode_placement_adopt_positive(ex);
    }
    if (strcmp(argv[1], "placement-adopt-negative") == 0) {
        return mode_placement_adopt_negative(ex);
    }
    if (strcmp(argv[1], "scope-cross-owner") == 0) {
        return mode_scope_cross_owner(ex);
    }
    if (strcmp(argv[1], "stand-helper-held-poll-trap") == 0) {
        return mode_stand_helper_held_poll_trap(ex);
    }
	#ifdef RT_TEST_SYNC_POINTS
    if (strcmp(argv[1], "debt020-migrate-gap-proof") == 0) {
        return mode_debt020_migrate_gap_proof(ex);
    }
    if (strcmp(argv[1], "debt022-donecv-storeload-proof") == 0) {
        return mode_debt022_donecv_storeload_proof(ex);
    }
    if (strcmp(argv[1], "debt022-multi-awaiters") == 0) {
        return mode_debt022_multi_awaiters(ex);
    }
    if (strcmp(argv[1], "debt022-already-done") == 0) {
        return mode_debt022_already_done(ex);
    }
    if (strcmp(argv[1], "debt022-parked-target") == 0) {
        return mode_debt022_parked_target(ex);
    }
    if (strcmp(argv[1], "debt022-cancelled-parked-target") == 0) {
        return mode_debt022_cancelled_parked_target(ex);
    }
    if (strcmp(argv[1], "debt023-cancel-park-proof") == 0) {
        return mode_debt023_cancel_park_proof(ex);
    }
    if (strcmp(argv[1], "debt046-join-stale-removal-proof") == 0) {
        return mode_debt046_join_stale_removal_proof(ex);
    }
    if (strcmp(argv[1], "debt201-park-abort-retires-entry") == 0) {
        return mode_debt201_park_abort_retires_entry(ex);
    }
    if (strcmp(argv[1], "debt248-second-token-abort") == 0) {
        return mode_debt248_second_token_abort(ex);
    }
    if (strcmp(argv[1], "ready-requeue-wake-race") == 0) {
        return mode_ready_requeue_wake_race(ex);
    }
    if (strcmp(argv[1], "sleep-fired-idle-sample") == 0) {
        return mode_sleep_fired_idle_sample(ex);
    }
    if (strcmp(argv[1], "debt261-failfast-join-verify") == 0) {
        return mode_debt261_failfast_join_verify(ex);
    }
    if (strcmp(argv[1], "debt263-cancel-commit-boundary") == 0) {
        return mode_debt263_cancel_commit_boundary(ex);
    }
    if (strcmp(argv[1], "debt263-cancel-after-seal") == 0) {
        return mode_debt263_cancel_after_seal(ex);
    }
    if (strcmp(argv[1], "debt080-cancel-before-claim") == 0) {
        return mode_debt080_cancel_before_claim(ex);
    }
    if (strcmp(argv[1], "debt080-cancel-after-claim") == 0) {
        return mode_debt080_cancel_after_claim(ex);
    }
    if (strcmp(argv[1], "debt080-poll-cancelled-queued") == 0) {
        return mode_debt080_poll_cancelled_queued(ex);
    }
    if (strcmp(argv[1], "debt080-shutdown-drains-queued") == 0) {
        return mode_debt080_shutdown_drains_queued(ex);
    }
    if (strcmp(argv[1], "debt080-release-refuses-under-lock") == 0) {
        return mode_debt080_release_refuses_under_lock(ex);
    }
    if (strcmp(argv[1], "inline-claim-off-queue") == 0) {
        return mode_inline_claim_off_queue(ex);
    }
    if (strcmp(argv[1], "scope-membership-claim") == 0) {
        return mode_scope_membership_claim(ex);
    }
    if (strcmp(argv[1], "scope-membership-completed-before-registration") == 0) {
        return mode_scope_membership_completed_before_registration(ex);
    }
    if (strcmp(argv[1], "entitlement-shutdown-vs-claimed-clone") == 0) {
        return mode_entitlement_shutdown_vs_claimed_clone(ex);
    }
    if (strcmp(argv[1], "entitlement-cancel-vs-committed-result") == 0) {
        return mode_entitlement_cancel_vs_committed_result(ex);
    }
    if (strcmp(argv[1], "entitlement-stale-result-capability") == 0) {
        return mode_entitlement_stale_result_capability(ex);
    }
    if (strcmp(argv[1], "entitlement-user-clone-panic") == 0) {
        return mode_entitlement_user_clone_panic(ex);
    }
    if (strcmp(argv[1], "entitlement-alloc-null-fatal") == 0) {
        return mode_entitlement_alloc_null_fatal(ex);
    }
    if (strcmp(argv[1], "debt291-poll-outcome-pin") == 0) {
        return mode_debt291_poll_outcome_pin(ex);
    }
#endif
    return fail("unknown mode");
}
`
