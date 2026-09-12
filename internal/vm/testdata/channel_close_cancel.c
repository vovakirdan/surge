#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#include "rt_channel_lane.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>

#define CLOSE_CANCEL_POLL UINT64_C(4098)
#define PAYLOAD_MARKER UINT64_C(0x456789abcdef)

int rt_argc;
char** rt_argv_raw;

typedef struct close_cancel_state close_cancel_state;
typedef struct {
    close_cancel_state* witness;
    _Atomic unsigned refs;
    uint64_t marker;
} close_cancel_payload;

struct close_cancel_state {
    rt_executor* ex;
    rt_channel* channel;
    close_cancel_payload* original;
    rt_park_token issued;
    const char* route;
    unsigned workers;
    unsigned round;
    uint32_t channel_owner;
    _Atomic uint64_t task_id;
    _Atomic unsigned polls;
    _Atomic unsigned issued_ready;
    _Atomic unsigned second_entered;
    _Atomic unsigned allow_second;
    _Atomic unsigned drops;
    _Atomic unsigned unused;
    _Atomic unsigned freed;
    unsigned closed_token_present;
};

typedef struct {
    unsigned present;
    unsigned status;
    unsigned polling;
    unsigned kind;
    unsigned same_token;
    unsigned token_live;
    unsigned reserved;
    unsigned waiters;
    unsigned waiter_owner;
    unsigned nonzero_seq;
    unsigned closed;
    uint64_t live;
} close_cancel_census;

static _Noreturn void close_cancel_fail(const close_cancel_state* state, const char* reason) {
    fprintf(stderr,
            "FAIL_CLOSE_CANCEL: route=%s shards=%u workers=%u sender_owner=0 "
            "channel_owner=%u round=%u %s\n",
            state->route,
            state->workers,
            state->workers,
            state->channel_owner,
            state->round,
            reason);
    _Exit(86);
}

static time_t close_cancel_now(const close_cancel_state* state) {
    struct timespec now;
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0)
        close_cancel_fail(state, "clock failed");
    return now.tv_sec;
}

static void close_cancel_pause(const close_cancel_state* state, time_t began) {
    if (close_cancel_now(state) - began >= 10)
        close_cancel_fail(state, "observation deadline");
    struct timespec pause = {.tv_sec = 0, .tv_nsec = 1000000};
    if (nanosleep(&pause, NULL) != 0 && errno != EINTR)
        close_cancel_fail(state, "pause failed");
}

static void raw_lock(const close_cancel_state* state, pthread_mutex_t* lock) {
    time_t began = close_cancel_now(state);
    for (;;) {
        int status = pthread_mutex_trylock(lock);
        if (status == 0)
            return;
        if (status != EBUSY)
            close_cancel_fail(state, "raw lock failed");
        close_cancel_pause(state, began);
    }
}

static void raw_unlock(const close_cancel_state* state, pthread_mutex_t* lock) {
    if (pthread_mutex_unlock(lock) != 0)
        close_cancel_fail(state, "raw unlock failed");
}

static int same_park(rt_park_token left, rt_park_token right) {
    return left.owner == right.owner && left.index == right.index &&
           left.generation == right.generation;
}

// Each snapshot holds control, then only one shard at a time. No runtime
// unlock is called: observing must not drain deferred cleanup on this lane.
static close_cancel_census close_cancel_snapshot(const close_cancel_state* state) {
    close_cancel_census out = {0};
    if (rt_lane_holds_control() || rt_lane_holds_any_shard())
        close_cancel_fail(state, "snapshot entered under scheduler lock");
    raw_lock(state, &state->ex->lock);
    rt_shard* task_owner = &state->ex->runtime->shards[0];
    raw_lock(state, &task_owner->lock);
    uint64_t id = atomic_load_explicit(&state->task_id, memory_order_acquire);
    const rt_task* task = get_task(state->ex, id);
    out.present = task != NULL;
    if (task != NULL) {
        if (rt_task_owner_shard_id(state->ex, task) != 0)
            close_cancel_fail(state, "task owner changed after public create");
        out.status = task_status_load(task);
        out.polling = atomic_load_explicit(&task->polling, memory_order_acquire);
        // Mailbox reads need either a fully parked poll or the explicit
        // second-poll barrier, before that poll is allowed to call send.
        int parked = out.status == TASK_WAITING && out.polling == 0;
        int held = atomic_load_explicit(&state->second_entered, memory_order_acquire) != 0 &&
                   atomic_load_explicit(&state->allow_second, memory_order_acquire) == 0;
        if (parked || held) {
            out.kind = task->resume_kind;
            if (atomic_load_explicit(&state->issued_ready, memory_order_acquire) != 0)
                out.same_token = (unsigned)same_park(task->resume_slot, state->issued);
        }
    }
    raw_unlock(state, &task_owner->lock);
    rt_shard* channel_owner = &state->ex->runtime->shards[state->channel_owner];
    raw_lock(state, &channel_owner->lock);
    const rt_channel* channel = state->channel;
    if (channel->owner_shard_id != state->channel_owner)
        close_cancel_fail(state, "channel owner changed after pre-create binding");
    out.closed = channel->closed;
    out.live = rt_park_pool_live(&channel->parks);
    if (atomic_load_explicit(&state->issued_ready, memory_order_acquire) != 0)
        out.token_live = (unsigned)rt_park_pool_token_is_live(&channel->parks, &state->issued);
    for (uint64_t index = 0; index < channel->parks.capacity; index++)
        out.reserved += channel->parks.slots[index].reserved;
    for (size_t index = 0; index < channel_owner->waiter_store.len; index++) {
        const waiter* entry = &channel_owner->waiter_store.entries[index];
        if (entry->task_id == id && entry->key.kind == WAKER_CHAN_SEND &&
            entry->key.id == (uint64_t)(uintptr_t)channel) {
            out.waiters++;
            out.waiter_owner = entry->owner_hint;
            out.nonzero_seq = entry->seq != 0;
        }
    }
    raw_unlock(state, &channel_owner->lock);
    raw_unlock(state, &state->ex->lock);
    return out;
}

static void close_payload_move(void* destination, void* source) {
    *(close_cancel_payload**)destination = *(close_cancel_payload**)source;
    *(close_cancel_payload**)source = NULL;
}

static void close_payload_drop(void* slot) {
    close_cancel_payload* payload = *(close_cancel_payload**)slot;
    if (payload == NULL)
        abort();
    *(close_cancel_payload**)slot = NULL;
    close_cancel_state* state = payload->witness;
    atomic_fetch_add_explicit(&state->drops, 1, memory_order_relaxed);
    unsigned refs = atomic_fetch_sub_explicit(&payload->refs, 1, memory_order_acq_rel);
    if (refs == 0)
        close_cancel_fail(state, "payload reference underflow");
    if (refs == 1) {
        rt_free((uint8_t*)payload, sizeof(*payload), _Alignof(close_cancel_payload));
        atomic_fetch_add_explicit(&state->freed, 1, memory_order_release);
    }
}

static rt_carrier_status
close_payload_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops close_payload_ops = {
    .layout = {.size = sizeof(close_cancel_payload*),
               .align = _Alignof(close_cancel_payload*),
               .stride = sizeof(close_cancel_payload*),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = close_payload_move,
    .drop_in_place = close_payload_drop,
    .plan_cross = close_payload_plan,
};

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    close_cancel_state* state = __task_state();
    const rt_task* current = rt_current_task();
    if (id != CLOSE_CANCEL_POLL || current == NULL || tls_worker_ctx == NULL ||
        tls_worker_ctx->ex != state->ex || tls_worker_ctx->shard_id != 0 ||
        rt_task_owner_shard_id(state->ex, current) != 0)
        close_cancel_fail(state, "poll did not run on the real owner-zero worker");
    atomic_store_explicit(&state->task_id, current->id, memory_order_release);
    unsigned poll = atomic_fetch_add_explicit(&state->polls, 1, memory_order_acq_rel);
    if (poll == 1) {
        // close already woke this real poll, but no send can consume its
        // mailbox before main observes SEND_CLOSED and lands public cancel.
        atomic_store_explicit(&state->second_entered, 1, memory_order_release);
        time_t began = close_cancel_now(state);
        while (atomic_load_explicit(&state->allow_second, memory_order_acquire) == 0)
            close_cancel_pause(state, began);
        if (task_cancelled_load(current) == 0)
            close_cancel_fail(state, "second send preceded public cancellation");
    } else if (poll != 0) {
        close_cancel_fail(state, "unexpected third poll");
    }
    close_cancel_payload* disposable = state->original;
    atomic_fetch_add_explicit(&disposable->refs, 1, memory_order_relaxed);
    if (rt_channel_send_offer(state->channel, (void*)&disposable) || disposable != NULL)
        close_cancel_fail(state, "send did not return Pending and consume its disposable");
    if (poll == 0) {
        rt_shard* owner = &state->ex->runtime->shards[0];
        rt_shard_lock(owner);
        state->issued = current->resume_slot;
        rt_shard_unlock(owner);
        atomic_store_explicit(&state->issued_ready, 1, memory_order_release);
    } else {
        if (atomic_load_explicit(&state->drops, memory_order_acquire) != 1)
            close_cancel_fail(state, "cancelled send did not drop only its fresh offer");
        atomic_fetch_add_explicit(&state->unused, 1, memory_order_release);
    }
    // The driver's independent owning activation holds both this state and
    // Channel through await; this stand does not claim allocated-frame proof.
    rt_async_yield(state, 0);
}

static void wait_initial_park(const close_cancel_state* state) {
    time_t began = close_cancel_now(state);
    for (;;) {
        close_cancel_census before = close_cancel_snapshot(state);
        if (before.present && before.status == TASK_WAITING && before.polling == 0) {
            if (before.live != 1 || before.token_live != 1 || before.same_token != 1 ||
                before.reserved != 0 || before.waiters != 1 || before.waiter_owner != 0 ||
                before.nonzero_seq != 1 || before.closed != 0)
                close_cancel_fail(state,
                                  "parked candidate did not prove the selected delivery route");
            return;
        }
        close_cancel_pause(state, began);
    }
}

static void wait_retired(const close_cancel_state* state) {
    time_t began = close_cancel_now(state);
    for (;;) {
        raw_lock(state, &state->ex->lock);
        rt_shard* owner = &state->ex->runtime->shards[0];
        raw_lock(state, &owner->lock);
        uint64_t id = atomic_load_explicit(&state->task_id, memory_order_acquire);
        int gone = get_task(state->ex, id) == NULL && rt_shard_scheduler(owner)->running_count == 0;
        raw_unlock(state, &owner->lock);
        raw_unlock(state, &state->ex->lock);
        if (gone)
            return;
        close_cancel_pause(state, began);
    }
}

static void close_cancel_round(const char* route, unsigned workers, unsigned round) {
    close_cancel_state state = {.route = route, .workers = workers, .round = round};
    state.ex = ensure_exec();
    state.channel_owner = strcmp(route, "same") == 0 ? 0u : 1u;
    if (state.ex->runtime->shard_count != workers || rt_worker_count() != workers ||
        rt_current_task() != NULL || tls_worker_ctx != NULL)
        close_cancel_fail(&state, "configuration or main-lane precondition failed");
    state.original = rt_alloc(sizeof(*state.original), _Alignof(close_cancel_payload));
    state.channel = rt_channel_new(0, &close_payload_ops, 0);
    if (state.original == NULL || state.channel == NULL)
        close_cancel_fail(&state, "allocation failed");
    state.original->witness = &state;
    state.original->marker = PAYLOAD_MARKER;
    atomic_init(&state.original->refs, 1);
    // Constructor publication happens inside __task_create. Bind the still
    // unused Channel first; main's non-worker spawn gets owner zero by D3.
    rt_channel_bind_owner_shard(state.channel, state.channel_owner);
    void* handle = __task_create(CLOSE_CANCEL_POLL, &state, NULL);
    if (handle == NULL)
        close_cancel_fail(&state, "public create failed");
    wait_initial_park(&state);
    rt_channel_close(state.channel);
    time_t began = close_cancel_now(&state);
    while (atomic_load_explicit(&state.second_entered, memory_order_acquire) == 0)
        close_cancel_pause(&state, began);
    close_cancel_census closed = close_cancel_snapshot(&state);
    if (closed.closed != 1 || closed.kind != RESUME_CHAN_SEND_CLOSED || closed.waiters != 0 ||
        closed.live != 1 || closed.token_live != 1 || closed.reserved != 0)
        close_cancel_fail(&state, "actual close did not deliver SEND_CLOSED to the staged sender");
    state.closed_token_present = closed.same_token;
    rt_task_cancel(handle);
    atomic_store_explicit(&state.allow_second, 1, memory_order_release);
    uint8_t kind = 0;
    rt_task_await(handle, &kind, NULL);
    if (kind != TASK_RESULT_CANCELLED)
        close_cancel_fail(&state, "await did not return Cancelled");
    wait_retired(&state);
    // No handle dereference after await. The driver still owns Channel and
    // the original payload, so an orphan is observed before either teardown.
    close_cancel_census after = close_cancel_snapshot(&state);
    unsigned drops = atomic_load_explicit(&state.drops, memory_order_acquire);
    unsigned refs = atomic_load_explicit(&state.original->refs, memory_order_acquire);
    if (state.closed_token_present != 1 || after.live != 0 || after.token_live != 0 || drops != 2 ||
        refs != 1) {
        fprintf(stderr,
                "FAIL_CLOSE_CANCEL: route=%s shards=%u workers=%u sender_owner=0 channel_owner=%u "
                "round=%u closed_delivery=1 closed_token_present=%u cancelled=1 polls=%u "
                "pool=%llu token_live=%u payload_drops=%u original_refs=%u "
                "token-lost-or-pool-orphan-before-teardown\n",
                route,
                workers,
                workers,
                state.channel_owner,
                round,
                state.closed_token_present,
                atomic_load_explicit(&state.polls, memory_order_acquire),
                (unsigned long long)after.live,
                after.token_live,
                drops,
                refs);
        _Exit(86);
    }
    if (after.present || after.waiters != 0 || after.reserved != 0 ||
        state.original->marker != PAYLOAD_MARKER ||
        atomic_load_explicit(&state.polls, memory_order_acquire) != 2 ||
        atomic_load_explicit(&state.unused, memory_order_acquire) != 1)
        close_cancel_fail(&state, "completed ownership census differs");
    close_payload_drop((void*)&state.original);
    rt_channel_handle_drop(state.channel);
    if (atomic_load_explicit(&state.drops, memory_order_acquire) != 3 ||
        atomic_load_explicit(&state.freed, memory_order_acquire) != 1)
        close_cancel_fail(&state, "original cleanup did not free exactly one payload");
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    abort();
}
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
    abort();
}
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out) {
    (void)id;
    (void)state;
    (void)out;
    abort();
}

int main(int argc, char** argv) {
    if (argc != 4 || (strcmp(argv[1], "same") != 0 && strcmp(argv[1], "foreign") != 0) ||
        (strcmp(argv[2], "2") != 0 && strcmp(argv[2], "8") != 0) || strcmp(argv[3], "32") != 0) {
        fprintf(stderr, "invalid close/cancel stand arguments\n");
        return 2;
    }
    unsigned workers = strcmp(argv[2], "2") == 0 ? 2u : 8u;
    for (unsigned round = 0; round < 32; round++)
        close_cancel_round(argv[1], workers, round);
    printf("CLOSE_CANCEL: route=%s shards=%u workers=%u sender_owner=0 channel_owner=%u rounds=32 "
           "closed_delivery=32 polls=64 unused_drops=32 staged_drops=32 final_payload_drops=96 "
           "payload_frees=32\n",
           argv[1],
           workers,
           workers,
           strcmp(argv[1], "same") == 0 ? 0u : 1u);
    return 0;
}
