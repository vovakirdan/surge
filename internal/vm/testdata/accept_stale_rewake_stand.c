// SUR-126 / RV2-DEBT-313 stand: a stale sibling accept-wake must not re-own a
// task that no longer holds that accept key.
//
// Shape, with no sockets and no scheduler timing: a multi-member listener
// registers one accept waiter per member and add_waiter files each under the
// shard that owns that member's fd, so ONE accept task sits in N shards'
// waiter stores. The winner delivers readiness; the task accepts, self-places
// onto the member's shard, and clears its wait keys -- which removes its
// sibling entries. A loser that already popped its batch still holds the task
// id. This stand reconstructs exactly that state and fires the loser's wake.
//
// Expected: the task keeps the owner the accept placed (shard 0).
// Negative control (-DRV2_DEBT_313_NEGATIVE_CONTROL): the stale wake re-owns
// it to shard 1, which is the state that strands an accepted conn on a shard
// no handler task is placed on.

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"

#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

enum { POLL_PARK_NOOP = 7101 };
enum { STALE_ACCEPT_FD = 4242 };

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    (void)id;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    (void)out_dst;
}

// A well-formed WAITING task that no queue holds, so nothing polls it while
// the stand inspects its placement.
static rt_task* make_parked_task(rt_executor* ex, uint32_t owner_shard_id) {
    uint64_t id = atomic_fetch_add_explicit(&ex->next_id, 1, memory_order_relaxed);
    rt_control_lock(ex);
    ensure_task_cap(ex, id);
    rt_control_unlock(ex);

    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->generation = id;
    task->poll_fn_id = (int64_t)POLL_PARK_NOOP;
    task->kind = TASK_KIND_USER;
    task_status_store(task, TASK_WAITING);
    task_cancel_gate_init(task);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->remote_handle_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_lease, NULL, memory_order_relaxed);
    // Held high so a wake that does enqueue it cannot retire it under the
    // assertion below.
    atomic_store_explicit(&task->handle_refs, 8, memory_order_relaxed);
    rt_task_entitlements_init(&task->entitlements);

    // What rt_net_accept's rt_net_place_current_task_on_owner leaves behind.
    rt_task_set_placement(task, owner_shard_id, TASK_PLACEMENT_CONNECTION);
    rt_task_slot_store(ex, id, task);
    return task;
}

// The loser's entry, still in its shard's store because that shard's poller
// popped its batch before the winner's clear_wait_keys ran.
static int plant_stale_accept_waiter(rt_executor* ex, uint32_t shard_id, uint64_t task_id) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_id);
    if (shard == NULL) {
        return 0;
    }
    waker_key key = net_accept_key(STALE_ACCEPT_FD);
    rt_shard_lock(shard);
    rt_waiter_store* store = &shard->waiter_store;
    int ok = rt_waiter_store_ensure_cap(store) == RT_RUNTIME_STATUS_OK;
    if (ok) {
        store->entries[store->len++] = (waiter){key, task_id, shard_id, 0};
        store->net_len++;
    }
    rt_shard_unlock(shard);
    return ok;
}

int main(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        printf("FAIL: no executor\n");
        return 2;
    }
    size_t shards = rt_runtime_shard_count(rt_executor_runtime(ex));
    if (shards < 2) {
        printf("FAIL: need >=2 shards, got %zu (set SURGE_SHARDS=2)\n", shards);
        return 2;
    }

    // The accept landed on a member owned by ACCEPT_SHARD, so that is where
    // rt_net_accept placed the task and what conn->owner_shard_id records.
    // STALE_SHARD is the loser whose already-popped batch fires afterwards.
    const uint32_t accept_shard = 0;
    const uint32_t stale_shard = 1;

    rt_task* task = make_parked_task(ex, accept_shard);
    if (task == NULL) {
        printf("FAIL: task alloc\n");
        return 2;
    }
    // Readiness already consumed: rt_net_consume_ready_accept_member cleared
    // the wait keys under control. A freshly zeroed task is already in that
    // state; state it explicitly so the precondition is not an accident.
    task->wait_keys_len = 0;
    task->net_ready_accept_valid = 0;

    if (!plant_stale_accept_waiter(ex, stale_shard, task->id)) {
        printf("FAIL: could not plant stale waiter\n");
        return 2;
    }

    const uint32_t owner_before = task->owner_shard_id;
    if (owner_before != accept_shard) {
        printf("FAIL: precondition, owner=%u before the stale wake\n", owner_before);
        return 2;
    }

    // The losing shard's poller fires its already-popped batch.
    (void)rt_executor_wake_net_waiters_for_key_on_owner(
        ex, net_accept_key(STALE_ACCEPT_FD), stale_shard);

    const uint32_t owner_after = task->owner_shard_id;
    // The wake above reaches this task through the executor's task table, which
    // cppcheck does not treat as aliasing `task`, so it folds both loads to one
    // value and calls the comparison constant. Whether that load CHANGED across
    // the wake is precisely what this stand asserts.
    // cppcheck-suppress knownConditionTrueFalse
    if (owner_after != owner_before) {
        printf("FAIL: stale accept wake re-owned the task: owner %u -> %u; "
               "an accepted conn owned by shard %u is now handled from shard %u\n",
               owner_before,
               owner_after,
               owner_before,
               owner_after);
        fflush(stdout);
        // Leave immediately: the stub task this stand planted is not a real
        // poll body, so letting the runtime shut down would raise an
        // unrelated panic on top of the verdict.
        _exit(1);
    }
    printf("OK: stale accept wake left owner=%u\n", owner_after);
    fflush(stdout);
    _exit(0);
}
