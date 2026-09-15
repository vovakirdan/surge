#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#include "rt_channel_lane.h"
#include "rt_frame.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>

#define CANCEL_FRAME_TYPE UINT64_C(4096)
#define CANCEL_FRAME_POLL UINT64_C(4097)

extern int rt_argc;
extern char** rt_argv_raw;

typedef struct {
    rt_executor* ex;
    rt_shard* owner;
    _Atomic uint64_t task_id;
    _Atomic unsigned polls;
    _Atomic unsigned frame_drops;
    _Atomic unsigned payload_drops;
    _Atomic unsigned staging_drops;
    _Atomic unsigned driver_drops;
    _Atomic unsigned issued;
    _Atomic uintptr_t addresses[3];
    _Atomic unsigned frees[3];
} frame_witness;

typedef struct {
    frame_witness* witness;
    _Atomic unsigned refs;
} frame_payload;

typedef struct {
    void* lifecycle;
    rt_channel* channel;
    frame_payload* payload;
    frame_witness* witness;
    rt_park_token issued;
} cancel_frame;

typedef struct {
    unsigned present;
    unsigned status;
    unsigned polling;
    unsigned handles;
    unsigned pins;
    unsigned waiters;
    unsigned reserved;
    unsigned token_live;
    uint64_t live;
} frame_census;

static _Atomic(frame_witness*) active_witness;

static _Noreturn void frame_fail(const char* reason) {
    fprintf(stderr, "FAIL_CHANNEL_CANCEL_FRAME: %s\n", reason);
    _Exit(86);
}

// Only addresses cross the physical free boundary. No observer reads an
// object after its descriptor has surrendered the last owner.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __real_rt_free(uint8_t* pointer, uint64_t size, uint64_t align);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __wrap_rt_free(uint8_t* pointer, uint64_t size, uint64_t align);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __wrap_rt_free(uint8_t* pointer, uint64_t size, uint64_t align) {
    frame_witness* witness = atomic_load_explicit(&active_witness, memory_order_acquire);
    if (witness != NULL && pointer != NULL) {
        for (unsigned index = 0; index < 3; index++) {
            uintptr_t expected = (uintptr_t)pointer;
            if (atomic_compare_exchange_strong_explicit(&witness->addresses[index],
                                                        &expected,
                                                        0,
                                                        memory_order_acq_rel,
                                                        memory_order_acquire)) {
                atomic_fetch_add_explicit(&witness->frees[index], 1, memory_order_release);
            }
        }
    }
    __real_rt_free(pointer, size, align);
}

static time_t frame_now(void) {
    struct timespec now;
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0)
        frame_fail("clock failed");
    return now.tv_sec;
}

static void frame_pause(time_t began) {
    if (frame_now() - began >= 10)
        frame_fail("observation deadline exceeded");
    struct timespec pause = {.tv_sec = 0, .tv_nsec = 1000000};
    if (nanosleep(&pause, NULL) != 0 && errno != EINTR)
        frame_fail("pause failed");
}

static void frame_unlock(const frame_witness* witness) {
    if (pthread_mutex_unlock(&witness->owner->lock) != 0 ||
        pthread_mutex_unlock(&witness->ex->lock) != 0)
        frame_fail("raw unlock failed");
}

static void frame_lock(const frame_witness* witness) {
    if (rt_lane_holds_control() || rt_lane_holds_any_shard() || rt_lane_holds_token_lock())
        frame_fail("observer entered under runtime lock");
    time_t began = frame_now();
    for (;;) {
        int control = pthread_mutex_trylock(&witness->ex->lock);
        if (control == 0) {
            int shard = pthread_mutex_trylock(&witness->owner->lock);
            if (shard == 0)
                return;
            if (pthread_mutex_unlock(&witness->ex->lock) != 0)
                frame_fail("raw control unlock failed");
            if (shard != EBUSY)
                frame_fail("raw shard lock failed");
        } else if (control != EBUSY) {
            frame_fail("raw control lock failed");
        }
        frame_pause(began);
    }
}

// Raw control -> shard locks never drain deferred work. The caller either
// still owns this frame or has not cancelled its parked task yet.
static frame_census frame_snapshot(const cancel_frame* frame) {
    frame_witness* witness = frame->witness;
    frame_census out = {0};
    frame_lock(witness);
    uint64_t id = atomic_load_explicit(&witness->task_id, memory_order_acquire);
    const rt_task* task = get_task(witness->ex, id);
    out.present = task != NULL;
    if (task != NULL) {
        out.status = task_status_load(task);
        out.polling = atomic_load_explicit(&task->polling, memory_order_acquire);
    }
    const rt_channel* channel = frame->channel;
    out.handles = atomic_load_explicit(&channel->handle_refs, memory_order_acquire);
    out.pins =
        atomic_load_explicit(&channel->pin_state, memory_order_acquire) & RT_CHANNEL_PIN_COUNT_MASK;
    out.live = rt_park_pool_live(&channel->parks);
    if (atomic_load_explicit(&witness->issued, memory_order_acquire) != 0)
        out.token_live = (unsigned)rt_park_pool_token_is_live(&channel->parks, &frame->issued);
    for (uint64_t index = 0; index < channel->parks.capacity; index++)
        out.reserved += channel->parks.slots[index].reserved;
    for (size_t index = 0; index < witness->owner->waiter_store.len; index++) {
        const waiter* entry = &witness->owner->waiter_store.entries[index];
        if (entry->task_id == id && waker_is_channel(entry->key) &&
            entry->key.id == (uint64_t)(uintptr_t)channel)
            out.waiters++;
    }
    frame_unlock(witness);
    return out;
}

static void payload_move(void* destination, void* source) {
    *(frame_payload**)destination = *(frame_payload**)source;
    *(frame_payload**)source = NULL;
}

static void payload_drop(void* slot) {
    frame_payload* payload = *(frame_payload**)slot;
    if (payload == NULL)
        frame_fail("empty payload drop");
    *(frame_payload**)slot = NULL;
    frame_witness* witness = payload->witness;
    if (atomic_load_explicit(&witness->frame_drops, memory_order_acquire) == 0)
        atomic_fetch_add_explicit(&witness->staging_drops, 1, memory_order_relaxed);
    atomic_fetch_add_explicit(&witness->payload_drops, 1, memory_order_relaxed);
    unsigned refs = atomic_fetch_sub_explicit(&payload->refs, 1, memory_order_acq_rel);
    if (refs == 0)
        frame_fail("payload reference underflow");
    if (refs == 1)
        rt_free((uint8_t*)payload, sizeof(*payload), _Alignof(frame_payload));
}

static rt_carrier_status
frame_local_cross_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops payload_ops = {
    .layout = {.size = sizeof(frame_payload*),
               .align = _Alignof(frame_payload*),
               .stride = sizeof(frame_payload*),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = payload_move,
    .drop_in_place = payload_drop,
    .plan_cross = frame_local_cross_plan,
};

static void* packed_word(void) {
    return (void*)(uintptr_t)(((uint64_t)SURGE_FRAME_STATE_PACKED << 1) | UINT64_C(1));
}

static void frame_drop(void* value) {
    cancel_frame* frame = value;
    frame_witness* witness = frame->witness;
    frame_census before = frame_snapshot(frame);
    unsigned staged = atomic_load_explicit(&witness->staging_drops, memory_order_acquire);
    if (before.live != 0 || before.token_live != 0 || before.reserved != 0 || staged != 1) {
        fprintf(stderr,
                "FAIL_CHANNEL_CANCEL_FRAME: pool-not-retired-before-frame-drop "
                "live=%llu token_live=%u reserved=%u staging_drops=%u\n",
                (unsigned long long)before.live,
                before.token_live,
                before.reserved,
                staged);
        // Stop before last-handle destruction could hide the missing cleanup
        // by draining the pool, or any later code could touch stale storage.
        _Exit(86);
    }
    if (frame->lifecycle != packed_word() || before.handles != 1 || before.pins != 0 ||
        before.waiters != 0 ||
        atomic_load_explicit(&frame->payload->refs, memory_order_acquire) != 1 ||
        atomic_fetch_add_explicit(&witness->frame_drops, 1, memory_order_acq_rel) != 0)
        frame_fail("frame did not hold the sole channel and original payload");
    payload_drop((void*)&frame->payload);
    rt_channel* channel = frame->channel;
    frame->channel = NULL;
    rt_channel_handle_drop(channel);
}

static void frame_move(void* destination, void* source) {
    memcpy(destination, source, sizeof(cancel_frame));
    memset(source, 0, sizeof(cancel_frame));
}

static const rt_value_ops frame_ops = {
    .layout = {.size = sizeof(cancel_frame),
               .align = _Alignof(cancel_frame),
               .stride = sizeof(cancel_frame),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = frame_move,
    .drop_in_place = frame_drop,
    .plan_cross = frame_local_cross_plan,
};

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
const rt_value_ops* __surge_value_ops_for(uint64_t type_id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
const rt_value_ops* __surge_value_ops_for(uint64_t type_id) {
    return type_id == CANCEL_FRAME_TYPE ? &frame_ops : NULL;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    if (id != CANCEL_FRAME_POLL)
        frame_fail("unexpected poll descriptor");
    cancel_frame* frame = __task_state();
    frame_witness* witness = frame->witness;
    rt_task* current = rt_current_task();
    if (current == NULL || frame->lifecycle != packed_word())
        frame_fail("poll lacks packed frame");
    atomic_store_explicit(&witness->task_id, current->id, memory_order_release);
    unsigned poll = atomic_fetch_add_explicit(&witness->polls, 1, memory_order_acq_rel);
    if (poll == 0) {
        frame_payload* disposable = frame->payload;
        atomic_fetch_add_explicit(&disposable->refs, 1, memory_order_relaxed);
        if (rt_channel_send_offer(frame->channel, (void*)&disposable) || disposable != NULL)
            frame_fail("initial offer did not park and consume the disposable");
        rt_shard_lock(witness->owner);
        frame->issued = current->resume_slot;
        rt_shard_unlock(witness->owner);
        atomic_store_explicit(&witness->issued, 1, memory_order_release);
    } else {
        frame_census before = frame_snapshot(frame);
        if (poll != 1 || task_cancelled_load(current) == 0 || before.handles != 1 ||
            before.pins != 0 || before.waiters != 0 || before.live != 1 || before.token_live != 1 ||
            before.reserved != 0 ||
            atomic_load_explicit(&witness->driver_drops, memory_order_acquire) != 1)
            frame_fail("cancelled poll did not outlive its waiter pin on the last frame handle");
    }
    rt_async_yield(frame, CANCEL_FRAME_TYPE);
}

static void wait_parked(cancel_frame* frame) {
    time_t began = frame_now();
    for (;;) {
        frame_census observed = frame_snapshot(frame);
        if (observed.present && observed.status == TASK_WAITING && observed.polling == 0) {
            if (observed.handles != 2 || observed.pins != 1 || observed.waiters != 1 ||
                observed.live != 1 || observed.token_live != 1 || observed.reserved != 0)
                frame_fail("initial parked ownership census differs");
            return;
        }
        if (rt_worker_count() == 1)
            (void)run_ready_one(frame->witness->ex);
        frame_pause(began);
    }
}

static void wait_completion_tail(frame_witness* witness) {
    time_t began = frame_now();
    for (;;) {
        frame_lock(witness);
        uint64_t id = atomic_load_explicit(&witness->task_id, memory_order_acquire);
        int absent = get_task(witness->ex, id) == NULL;
        uint32_t running = rt_shard_scheduler(witness->owner)->running_count;
        frame_unlock(witness);
        int freed = 1;
        for (unsigned index = 0; index < 3; index++)
            freed =
                freed && atomic_load_explicit(&witness->frees[index], memory_order_acquire) == 1;
        if (absent && running == 0 && freed)
            return;
        frame_pause(began);
    }
}

static void run_frame_round(void) {
    frame_witness witness = {0};
    witness.ex = ensure_exec();
    witness.owner = &witness.ex->runtime->shards[0];
    cancel_frame* frame = rt_frame_alloc(&frame_ops);
    frame_payload* payload = rt_alloc(sizeof(*payload), _Alignof(frame_payload));
    rt_channel* channel = rt_channel_new(0, &payload_ops, 0);
    if (frame == NULL || payload == NULL || channel == NULL)
        frame_fail("allocation failed");
    payload->witness = &witness;
    atomic_init(&payload->refs, 1);
    frame->lifecycle = packed_word();
    frame->channel = channel;
    frame->payload = payload;
    frame->witness = &witness;
    rt_channel_handle_retain(channel);
    atomic_store_explicit(&witness.addresses[0], (uintptr_t)frame, memory_order_relaxed);
    atomic_store_explicit(&witness.addresses[1], (uintptr_t)channel, memory_order_relaxed);
    atomic_store_explicit(&witness.addresses[2], (uintptr_t)payload, memory_order_relaxed);
    atomic_store_explicit(&active_witness, &witness, memory_order_release);
    void* task = __task_create(CANCEL_FRAME_POLL, frame, NULL);
    if (task == NULL)
        frame_fail("public task creation failed");
    wait_parked(frame);
    rt_channel_handle_drop(channel);
    channel = NULL;
    frame = NULL;
    atomic_store_explicit(&witness.driver_drops, 1, memory_order_release);
    rt_task_cancel(task);
    uint8_t kind = 0;
    rt_task_await(task, &kind, NULL);
    // Await consumed the only Task handle. From here, inspect only the
    // independent witness and fresh task-table lookup, never task/frame/channel.
    if (kind != TASK_RESULT_CANCELLED)
        frame_fail("public await did not return Cancelled");
    wait_completion_tail(&witness);
    if (atomic_load_explicit(&witness.polls, memory_order_acquire) != 2 ||
        atomic_load_explicit(&witness.frame_drops, memory_order_acquire) != 1 ||
        atomic_load_explicit(&witness.payload_drops, memory_order_acquire) != 2 ||
        atomic_load_explicit(&witness.staging_drops, memory_order_acquire) != 1)
        frame_fail("final drop census differs");
    atomic_store_explicit(&active_witness, NULL, memory_order_release);
}

// Required native dispatch ABI, unused by this no-result, nonblocking stand.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    frame_fail("unexpected legacy drop dispatch");
}
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
    frame_fail("unexpected result drop dispatch");
}
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out) {
    (void)id;
    (void)state;
    (void)out;
    frame_fail("unexpected blocking dispatch");
}
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_start(void);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_start(void) {
    if (rt_argc != 2 || rt_argv_raw == NULL)
        frame_fail("driver argv differs");
    char* end = NULL;
    errno = 0;
    unsigned long rounds = strtoul(rt_argv_raw[1], &end, 10);
    if (errno != 0 || end == rt_argv_raw[1] || *end != '\0' || rounds == 0 || rounds > 32)
        frame_fail("invalid workload count");
    for (unsigned long round = 0; round < rounds; round++)
        run_frame_round();
    char marker[180];
    int length = snprintf(marker,
                          sizeof(marker),
                          "CHANNEL_CANCEL_FRAME: polls=2 staging_drops=1 frame_drops=1 "
                          "payload_drops=2 frees=1/1/1 rounds=%lu\n",
                          rounds);
    if (length < 0 || (size_t)length >= sizeof(marker) ||
        write(STDOUT_FILENO, marker, (size_t)length) != (ssize_t)length)
        frame_fail("marker write failed");
}
