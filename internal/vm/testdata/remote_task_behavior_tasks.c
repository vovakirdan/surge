// nanosleep, which this stand uses to wait on task status from the outside.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#define _POSIX_C_SOURCE 199309L
#include "remote_task_behavior.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
    return;
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

void rtb_select_bind_addrs(void* addrs[], uint64_t bits[], const uint8_t kinds[], uint64_t count) {
    // `bits` is not const: what these addresses are FOR is a move that empties
    // the storage they name, and one that moves a losing payload back into it.
    for (uint64_t i = 0; i < count; i++) {
        addrs[i] = kinds[i] == SELECT_CHAN_SEND ? (void*)&bits[i] : NULL;
    }
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
                                            state->result_type_id,
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
    // An await or a cancel asks the owner for work, so it spends a DATA
    // credit; saturating the reserve would no longer refuse it, and must not.
    if (state->phase == 0 && state->fill_inbound != 0 && handle != NULL) {
        rt_executor* ex = ensure_exec();
        rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), handle->owner_shard_id);
        rt_shard_lock(owner);
        memset(owner->transport.data, 0, sizeof(owner->transport.data));
        owner->transport.data_head = 0;
        owner->transport.data_len = RT_TRANSPORT_DATA_SLOT_CREDITS;
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

// A row's shipped state box. Allocation failure aborts rather than being
// threaded through every row: a stand that cannot allocate has nothing to
// measure, and the rows read better without a branch none of them exercises.
rtb_shipped_state* rtb_shipped_state_new(uint64_t mark) {
    rtb_shipped_state* box =
        (rtb_shipped_state*)rt_alloc(sizeof(rtb_shipped_state), _Alignof(rtb_shipped_state));
    if (box == NULL) {
        fputs("shipped state box alloc failed\n", stderr);
        exit(97);
    }
    box->mark = mark;
    return box;
}

// The stand's shipped-state descriptor. A crossing names its state by TYPE
// now, and the runtime destroys an abandoned one through that type -- the drop
// below, then the block at this width. Counting here is what the migration
// rows assert, in place of the numeric dispatch it replaced.
static void rtb_state_move(void* destination, void* source) {
    memcpy(destination, source, sizeof(rtb_shipped_state));
    memset(source, 0, sizeof(rtb_shipped_state));
}

static void rtb_state_drop(void* value) {
    atomic_fetch_add_explicit(&rtb_drop_calls, 1, memory_order_acq_rel);
    atomic_store_explicit(&rtb_drop_last_id, RTB_DROP_MARK_ID, memory_order_release);
    atomic_store_explicit(&rtb_drop_last_state, value, memory_order_release);
}

static rt_carrier_status
rtb_state_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

// The compiler emits this lookup for a real program; this stand defines it so
// its own shipped-state type has a descriptor to be destroyed through.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
const rt_value_ops* __surge_value_ops_for(uint64_t type_id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
const rt_value_ops* __surge_value_ops_for(uint64_t type_id) {
    static const rt_value_ops state_ops = {
        .layout = {.size = sizeof(rtb_shipped_state),
                   .align = _Alignof(rtb_shipped_state),
                   .stride = sizeof(rtb_shipped_state),
                   .flags = RT_VALUE_FLAG_DROPPABLE},
        .move_init = rtb_state_move,
        .copy_init = NULL,
        .clone_init = NULL,
        .drop_in_place = rtb_state_drop,
        .trace = NULL,
        .plan_cross = rtb_state_plan,
        .cross_move_init = NULL,
        .cross_clone_init = NULL,
    };
    return type_id == RTB_DROP_MARK_ID ? &state_ops : NULL;
}

// Owner-side result-drop census (RV2-DEBT-053a): the release-while-DONE row
// installs a nonzero result_type_id and a heap result_bits; free_task
// routes the reclamation here. The stub frees the block (mimicking the
// compiled drop wrapper) so heap accounting balances and counts calls so
// the row can assert exactly-once with the right id and pointer.
_Atomic uint64_t rtb_result_drop_calls;
_Atomic uint64_t rtb_result_drop_last_id;
_Atomic(void*) rtb_result_drop_last_value;

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
