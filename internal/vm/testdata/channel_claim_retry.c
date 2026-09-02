// Shared fixture for the RV2-DEBT-277 bounded claim-retry stand.

#include "channel_claim_retry_stand.h"

#include <stdatomic.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

enum { POLL_RETRY_STAND = 7277 };

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst);

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    (void)id;
}
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    (void)out_dst;
}

static void retry_word_move(void* destination, void* source) {
    *(uint64_t*)destination = *(uint64_t*)source;
    *(uint64_t*)source = 0;
}

static rt_carrier_status
retry_word_plan_cross(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static void retry_word_probe_drop(void* value);

const rt_value_ops retry_word_ops = {
    .layout = {.size = sizeof(uint64_t),
               .align = _Alignof(uint64_t),
               .stride = sizeof(uint64_t),
               .flags = 0},
    .move_init = retry_word_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = NULL,
    .trace = NULL,
    .plan_cross = retry_word_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

// The probe descriptor: its drop runs the direct-refusal drive from INSIDE a
// value-bearing park release, so the stand can watch that release expose the
// slot claim the drive is refused on (park-finish-release mode).
const rt_value_ops retry_word_probe_ops = {
    .layout = {.size = sizeof(uint64_t),
               .align = _Alignof(uint64_t),
               .stride = sizeof(uint64_t),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = retry_word_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = retry_word_probe_drop,
    .trace = NULL,
    .plan_cross = retry_word_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static retry_fixture* park_release_probe_fixture;
static int park_release_probe_armed;
static uint64_t park_release_probe_value;

rt_task* make_retry_task(rt_executor* ex) {
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
    task->poll_fn_id = POLL_RETRY_STAND;
    task->kind = TASK_KIND_USER;
    task_status_store(task, TASK_RUNNING);
    task_cancel_gate_init(task);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 8, memory_order_relaxed);
    atomic_store_explicit(&task->remote_handle_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_lease, NULL, memory_order_relaxed);
    rt_task_entitlements_init(&task->entitlements);
    rt_task_set_placement(task, 0, TASK_PLACEMENT_GENERIC);
    rt_task_slot_store(ex, id, task);
    return task;
}

_Noreturn void stand_fail(const char* message) {
    printf("FAIL: %s\n", message);
    fflush(stdout);
    _exit(1);
}

retry_fixture make_fixture_with_ops(const rt_value_ops* ops) {
    retry_fixture f = {0};
    f.ex = ensure_exec();
    f.task = f.ex != NULL ? make_retry_task(f.ex) : NULL;
    f.handle = rt_channel_new(1, ops, 0);
    if (f.ex == NULL || f.task == NULL || f.handle == NULL) {
        printf("FAIL: setup refused\n");
        fflush(stdout);
        _exit(1);
    }
    f.channel = (rt_channel*)f.handle;
    f.owner = channel_owner_shard(f.ex, f.channel);
    rt_shard_lock(f.owner);
    if (rt_typed_fifo_reserve_push_locked(&f.channel->ring, &f.held) != RT_SLOT_CONTROL_OK) {
        rt_shard_unlock(f.owner);
        stand_fail("could not hold the ring claim");
    }
    rt_shard_unlock(f.owner);
    rt_set_current_task(f.task);
    return f;
}

retry_fixture make_fixture(void) {
    return make_fixture_with_ops(&retry_word_ops);
}

void drive_direct_refusals(retry_fixture* f, uint64_t* value) {
    for (uint8_t attempt = 1; attempt <= RT_CHANNEL_RETRY_BUDGET; attempt++) {
        pending_key = waker_none();
        if (rt_channel_send(f->handle, value)) {
            stand_fail("a send committed through another operation's claim");
        }
        if (f->task->channel_retry.count != attempt) {
            stand_fail("retry count did not follow actual refusals");
        }
        if (attempt < 8 && waker_valid(pending_key)) {
            stand_fail("operation parked before the eighth refusal");
        }
    }
    if (f->task->channel_retry.count != 8 || pending_key.kind != WAKER_CHAN_SEND_RETRY ||
        !f->task->park_prepared) {
        stand_fail("operation did not park on the eighth refusal");
    }
}

static void retry_word_probe_drop(void* value) {
    (void)value;
    if (!park_release_probe_armed || park_release_probe_fixture == NULL) {
        return;
    }
    park_release_probe_armed = 0;
    drive_direct_refusals(park_release_probe_fixture, &park_release_probe_value);
}

void arm_park_release_probe(retry_fixture* f, uint64_t value) {
    park_release_probe_fixture = f;
    park_release_probe_value = value;
    park_release_probe_armed = 1;
}

int park_release_probe_is_armed(void) {
    return park_release_probe_armed;
}

void disarm_park_release_probe(void) {
    park_release_probe_fixture = NULL;
    park_release_probe_armed = 0;
}

void commit_retry_park(retry_fixture* f) {
    park_current(f->ex, pending_key);
    if (task_status_load(f->task) != TASK_WAITING) {
        stand_fail("retry park did not commit");
    }
}

void release_held_claim(retry_fixture* f) {
    rt_shard_lock(f->owner);
    if (rt_typed_fifo_abandon_push_locked(&f->channel->ring, &f->held) != RT_SLOT_CONTROL_OK) {
        rt_shard_unlock(f->owner);
        stand_fail("held claim could not be released");
    }
    rt_channel_claim_released_locked(f->ex, f->owner, f->channel);
    rt_shard_unlock(f->owner);
}

void hold_ring_push(retry_fixture* f) {
    rt_shard_lock(f->owner);
    if (rt_typed_fifo_reserve_push_locked(&f->channel->ring, &f->held) != RT_SLOT_CONTROL_OK) {
        rt_shard_unlock(f->owner);
        stand_fail("could not hold another ring claim");
    }
    rt_shard_unlock(f->owner);
}

void clear_prepared_waiter(retry_fixture* f) {
    remove_waiter(f->ex, pending_key, f->task->id);
    f->task->park_key = waker_none();
    f->task->park_prepared = 0;
    pending_key = waker_none();
}

void add_dead_receiver(retry_fixture* f, int foreign_owner_hint) {
    rt_task* dead = make_retry_task(f->ex);
    if (dead == NULL) {
        stand_fail("could not create stale receiver");
    }
    rt_shard_lock(f->owner);
    channel_park_prepare_locked(f->owner, dead, channel_recv_key(f->channel));
    if (foreign_owner_hint) {
        rt_waiter_store* store = &f->owner->waiter_store;
        if (store->len == 0) {
            rt_shard_unlock(f->owner);
            stand_fail("stale receiver registration disappeared");
        }
        store->entries[store->len - 1].owner_hint = f->channel->owner_shard_id + 1;
    }
    rt_shard_unlock(f->owner);
    task_status_store(dead, TASK_DONE);
}

void turn_held_push_into_pop(retry_fixture* f) {
    uint64_t value = 7277;
    rt_value_move_init_detached(&retry_word_ops, f->held.address, &value);
    rt_shard_lock(f->owner);
    if (rt_typed_fifo_commit_push_locked(&f->channel->ring, &f->held) != RT_SLOT_CONTROL_OK) {
        rt_shard_unlock(f->owner);
        stand_fail("seed value could not be published");
    }
    rt_channel_claim_released_locked(f->ex, f->owner, f->channel);
    if (rt_typed_fifo_reserve_pop_locked(&f->channel->ring, &f->held) != RT_SLOT_CONTROL_OK) {
        rt_shard_unlock(f->owner);
        stand_fail("could not hold the ring take claim");
    }
    rt_shard_unlock(f->owner);
}

void release_held_pop(retry_fixture* f) {
    rt_shard_lock(f->owner);
    if (rt_typed_fifo_commit_pop_locked(&f->channel->ring, &f->held) != RT_SLOT_CONTROL_OK) {
        rt_shard_unlock(f->owner);
        stand_fail("held take claim could not be released");
    }
    rt_channel_claim_released_locked(f->ex, f->owner, f->channel);
    rt_shard_unlock(f->owner);
}

void require_woken(const retry_fixture* f) {
    if (task_status_load(f->task) == TASK_WAITING) {
        stand_fail("claim release did not wake the retry park");
    }
}

void resume_for_poll(retry_fixture* f) {
    task_enqueued_store(f->task, 0);
    task_status_store(f->task, TASK_RUNNING);
    pending_key = waker_none();
}

void finish_fixture(retry_fixture* f) {
    rt_exec_trace_dump();
    rt_set_current_task(NULL);
    rt_channel_handle_drop(f->handle);
}
