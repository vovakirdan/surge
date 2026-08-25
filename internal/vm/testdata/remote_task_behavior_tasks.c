// nanosleep, which this stand uses to wait on task status from the outside.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#define _POSIX_C_SOURCE 199309L
#include "remote_task_behavior.h"

#include <stdio.h>
#include <string.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    return 0;
}

int rtb_fail(const char* message) {
    if (message != NULL) {
        fputs(message, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

uint64_t* rtb_word(uint64_t value) {
    static _Thread_local uint64_t slot;
    slot = value;
    return &slot;
}

void rtb_sleep_us(unsigned long micros) {
    struct timespec ts = {
        .tv_sec = (time_t)(micros / 1000000UL),
        .tv_nsec = (long)((micros % 1000000UL) * 1000UL),
    };
    while (nanosleep(&ts, &ts) != 0) {
    }
}

int rtb_wait_u32(_Atomic uint32_t* value, uint32_t expected, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(value, memory_order_acquire) == expected) {
            return 1;
        }
        rtb_sleep_us(1000);
    }
    return 0;
}

int rtb_wait_task_done(rt_executor* ex, uint64_t task_id, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        const rt_task* task = get_task(ex, task_id);
        if (task != NULL && task_status_load(task) == TASK_DONE) {
            return 1;
        }
        rtb_sleep_us(1000);
    }
    return 0;
}

void rtb_wake(rt_executor* ex, uint64_t task_id) {
    rt_control_lock(ex);
    wake_task(ex, task_id, 1);
    rt_control_unlock(ex);
}

void rtb_drain(rt_executor* ex, uint32_t shard_id) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_id);
    if (shard == NULL) {
        return;
    }
    rt_shard_lock(shard);
    (void)rt_remote_spawn_drain_inbound_locked(ex, shard, 0);
    rt_shard_unlock(shard);
}

static void poll_child(rtb_child_state* child) {
    rt_task* current = rt_current_task();
    atomic_store_explicit(&child->ran, 1, memory_order_release);
    atomic_store_explicit(&child->owner,
                          current != NULL ? current->owner_shard_id : UINT32_MAX,
                          memory_order_release);
    if (current_task_cancelled(ensure_exec())) {
        atomic_store_explicit(&child->cancelled, 1, memory_order_release);
        atomic_store_explicit(&child->done, 1, memory_order_release);
        rt_async_return_cancelled(child, 0);
    }
    if (atomic_load_explicit(&child->gate, memory_order_acquire) != 0) {
        atomic_store_explicit(&child->done, 1, memory_order_release);
        rt_async_return(child, rtb_word(91));
    }
    rt_async_yield(child, 0);
}

static void poll_publisher(rtb_publish_state* state) {
    if (state->handle == NULL) {
        state->status = rt_far_task_handle_alloc(&state->handle);
        if (state->status != RT_REMOTE_SPAWN_STATUS_OK) {
            rt_async_return(state, rtb_word((uint64_t)state->status));
        }
    }
    state->status = rt_remote_spawn_publish(state->destination,
                                            0,
                                            state->result_drop_fn_id,
                                            (int64_t)state->poll_id,
                                            state->task_state,
                                            &state->pending,
                                            state->handle);
    if (state->status == RT_REMOTE_SPAWN_STATUS_PENDING) {
        atomic_store_explicit(&state->saw_pending, 1, memory_order_release);
        rt_async_yield(state, 0);
    }
    if (state->status == RT_REMOTE_SPAWN_STATUS_OK) {
        state->published_task_id = state->handle->task_id;
    }
    if (state->status == RT_REMOTE_SPAWN_STATUS_OK && state->return_handle != 0) {
        rt_far_task_prepare_return(state->handle);
        rt_async_return(state, rtb_word((uint64_t)(uintptr_t)state->handle));
    }
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

static void poll_lifecycle(rtb_lifecycle_state* state) {
    uint8_t kind = 0;
    uint64_t bits = 0;
    const rt_far_task_handle* handle = state->phase == 0 ? state->handle : NULL;
    if (state->phase == 0 && state->fill_control != 0 && handle != NULL) {
        rt_executor* ex = ensure_exec();
        rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), handle->owner_shard_id);
        rt_shard_lock(owner);
        memset(owner->transport.control, 0, sizeof(owner->transport.control));
        owner->transport.control_head = 0;
        owner->transport.control_len = RT_TRANSPORT_CONTROL_QUEUE_CAP;
        rt_shard_unlock(owner);
    }
    state->status = state->cancel != 0
                        ? rt_far_task_cancel(handle, 0, &state->pending, &kind, &bits)
                        : rt_far_task_await(handle, 0, &state->pending, &kind, &bits);
    if (state->phase == 0) {
        state->phase = 1;
        rt_far_task_handle_free(state->handle);
        state->handle = NULL;
    }
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        atomic_store_explicit(&state->visible_pending, state->pending, memory_order_release);
        rt_async_yield(state, 0);
    }
    state->result_kind = kind;
    state->result_bits = bits;
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

static void poll_rtb_execute(rtb_execute_state* state) {
    uint8_t kind = 0;
    uint64_t bits = 0;
    state->status = rt_immediate_on_execute(state->placement,
                                            0,
                                            0,
                                            (int64_t)state->body_poll_id,
                                            state->body_state,
                                            &state->pending,
                                            &kind,
                                            &bits);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        atomic_store_explicit(&state->visible_pending, state->pending, memory_order_release);
        rt_async_yield(state, 0);
    }
    state->result_kind = kind;
    state->result_bits = bits;
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

static void poll_rtb_channel_create(rtb_create_state* state) {
    uint8_t kind = 0;
    uint64_t bits = 0;
    state->status = rt_far_channel_create(
        state->placement, state->capacity, 0, &state->pending, &state->handle, &kind, &bits);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state, 0);
    }
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

void* rtb_start_channel_create(rtb_create_state* state, uint64_t placement, uint64_t capacity) {
    memset(state, 0, sizeof(*state));
    state->placement = placement;
    state->capacity = capacity;
    return __task_create(POLL_RTB_CHANNEL_CREATE, state, rt_channel_opaque_word_ops());
}

// Drop-dispatch stub with a census: the migration rows install nonzero
// drop-fn ids and assert exactly-once destruction; everything else keeps
// id 0 and never reaches this.
_Atomic uint64_t rtb_drop_calls;
_Atomic uint64_t rtb_drop_last_id;
_Atomic(void*) rtb_drop_last_state;

void __surge_drop_call(uint64_t id, void* state) {
    atomic_fetch_add_explicit(&rtb_drop_calls, 1, memory_order_acq_rel);
    atomic_store_explicit(&rtb_drop_last_id, id, memory_order_release);
    atomic_store_explicit(&rtb_drop_last_state, state, memory_order_release);
}

// Owner-side result-drop census (RV2-DEBT-053a): the release-while-DONE row
// installs a nonzero result_drop_fn_id and a heap result_bits; free_task
// routes the reclamation here. The stub frees the block (mimicking the
// compiled drop wrapper) so heap accounting balances and counts calls so
// the row can assert exactly-once with the right id and pointer.
_Atomic uint64_t rtb_result_drop_calls;
_Atomic uint64_t rtb_result_drop_last_id;
_Atomic(void*) rtb_result_drop_last_value;

void __surge_drop_result_call(uint64_t id, void* value) {
    atomic_fetch_add_explicit(&rtb_result_drop_calls, 1, memory_order_acq_rel);
    atomic_store_explicit(&rtb_result_drop_last_id, id, memory_order_release);
    atomic_store_explicit(&rtb_result_drop_last_value, value, memory_order_release);
    if (value != NULL) {
        rt_free((uint8_t*)value, RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    }
}

// This harness family never threads a nonzero abandoned-state drop id
// through rt_async_yield/rt_async_return_cancelled, so mark_done's call
// here is always a no-op in practice; the stub exists only to satisfy the
// link (the real definition lives in compiled Surge output).
void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_poll_call(uint64_t id) {
    if (id == POLL_RTB_CHILD) {
        poll_child((rtb_child_state*)__task_state());
    }
    if (id == POLL_RTB_PUBLISHER) {
        poll_publisher((rtb_publish_state*)__task_state());
    }
    if (id == POLL_RTB_LIFECYCLE) {
        poll_lifecycle((rtb_lifecycle_state*)__task_state());
    }
    if (id == POLL_RTB_CHANNEL_CREATE) {
        poll_rtb_channel_create((rtb_create_state*)__task_state());
    }
    rtb_anchored_poll_dispatch(id);
    rtb_anchored_audit_poll_dispatch(id);
    rtb_share_poll_dispatch(id);
    rtb_select_poll_dispatch(id);
    rtb_drop_poll_dispatch(id);
    if (id == POLL_RTB_EXECUTE) {
        poll_rtb_execute((rtb_execute_state*)__task_state());
    }
    rt_async_return(NULL, rtb_word(0));
}

int rtb_await(void* task, uint8_t* kind, uint64_t* bits) {
    rt_task_await(task, kind, bits);
    return *kind == 1;
}

rt_far_task_handle* rtb_publish_handle(rtb_child_state* child, uint32_t destination) {
    return rtb_publish_poll(POLL_RTB_CHILD, child, destination);
}

rt_far_task_handle* rtb_publish_poll(uint64_t poll_id, void* task_state, uint32_t destination) {
    rtb_publish_state state;
    memset(&state, 0, sizeof(state));
    state.task_state = task_state;
    state.poll_id = poll_id;
    state.destination = destination;
    state.return_handle = 1;
    void* task = __task_create(POLL_RTB_PUBLISHER, &state, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    if (!rtb_await(task, &kind, &bits) || state.status != RT_REMOTE_SPAWN_STATUS_OK) {
        return NULL;
    }
    return (rt_far_task_handle*)(uintptr_t)bits;
}

void* rtb_start_lifecycle(rtb_lifecycle_state* state, rt_far_task_handle* handle, int cancel) {
    memset(state, 0, sizeof(*state));
    state->handle = handle;
    state->cancel = cancel != 0;
    rt_far_task_begin_transfer(handle);
    void* task = __task_create(POLL_RTB_LIFECYCLE, state, rt_channel_opaque_word_ops());
    rt_far_task_finish_transfer(handle, task);
    return task;
}
