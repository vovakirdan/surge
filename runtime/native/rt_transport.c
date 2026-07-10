#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"
#include "rt_sync_point.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <string.h>
#include <unistd.h>

static int rt_transport_msg_is_control(rt_transport_msg_kind kind) {
    switch (kind) {
        case RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK:
        case RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST:
        case RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION:
        case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST:
        case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK:
        case RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST:
        case RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY:
        case RT_TRANSPORT_MSG_CREDIT_CONTROL:
        case RT_TRANSPORT_MSG_SHUTDOWN_WAKE:
            return 1;
        case RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST:
        case RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST:
            return 0;
        case RT_TRANSPORT_MSG_NONE:
        default:
            return -1;
    }
}

static void rt_transport_wake_init_empty(rt_transport_wake* wake) {
    if (wake == NULL) {
        return;
    }
    wake->read_fd = -1;
    wake->write_fd = -1;
    wake->initialized = 0;
    wake->drain_count = 0;
    wake->drain_bytes = 0;
    wake->write_failures = 0;
}

static int rt_transport_set_nonblocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) {
        return -1;
    }
    return fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

static rt_runtime_status rt_transport_wake_init(rt_transport_wake* wake) {
    if (wake == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_transport_wake_init_empty(wake);
    int fds[2] = {-1, -1};
    if (pipe(fds) != 0) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    if (rt_transport_set_nonblocking(fds[0]) != 0 || rt_transport_set_nonblocking(fds[1]) != 0) {
        (void)close(fds[0]);
        (void)close(fds[1]);
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    wake->read_fd = fds[0];
    wake->write_fd = fds[1];
    wake->initialized = 1;
    return RT_RUNTIME_STATUS_OK;
}

static void rt_transport_wake_destroy(rt_transport_wake* wake) {
    if (wake == NULL) {
        return;
    }
    if (wake->initialized == 0) {
        rt_transport_wake_init_empty(wake);
        return;
    }
    if (wake->read_fd >= 0) {
        (void)close(wake->read_fd);
    }
    if (wake->write_fd >= 0) {
        (void)close(wake->write_fd);
    }
    rt_transport_wake_init_empty(wake);
}

static void rt_transport_wake_write(rt_transport_state* state) {
    if (state == NULL) {
        return;
    }
    const uint8_t byte = 1;
    if (state->wake.initialized == 0 || state->wake.write_fd < 0) {
        state->wake.write_failures++;
        return;
    }
    ssize_t n = write(state->wake.write_fd, &byte, sizeof(byte));
    if (n != (ssize_t)sizeof(byte) && errno != EAGAIN && errno != EWOULDBLOCK) {
        state->wake.write_failures++;
    }
}

#ifdef RT_TRANSPORT_NEG_RELAXED_PARK_ORDER
static uint8_t rt_transport_negative_relaxed_state(void) {
    volatile uint8_t state = RT_TRANSPORT_SHARD_RUNNING;
    return state;
}
#endif

#ifdef RT_TRANSPORT_NEG_SHUTDOWN_NO_WAKE
static void rt_transport_negative_shutdown_no_wake_touch(rt_executor* ex) {
    uint8_t shutdown = atomic_load_explicit(&ex->shutdown, memory_order_relaxed);
    atomic_store_explicit(&ex->shutdown, shutdown, memory_order_relaxed);
}
#endif

static void rt_transport_wake_drain(rt_transport_state* state) {
    if (state == NULL || state->wake.initialized == 0 || state->wake.read_fd < 0) {
        return;
    }
    uint8_t buf[64];
    size_t drained = 0;
    for (;;) {
        ssize_t n = read(state->wake.read_fd, buf, sizeof(buf));
        if (n > 0) {
            drained += (size_t)n;
            continue;
        }
        if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
            break;
        }
        break;
    }
    if (drained > 0) {
        state->wake.drain_count++;
        state->wake.drain_bytes += drained;
    }
}

rt_runtime_status rt_transport_state_init(rt_transport_state* state) {
    if (state == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    memset(state, 0, sizeof(*state));
    rt_runtime_status status = rt_transport_wake_init(&state->wake);
    if (status != RT_RUNTIME_STATUS_OK) {
        return status;
    }
    atomic_store_explicit(&state->park_state, RT_TRANSPORT_SHARD_RUNNING, memory_order_seq_cst);
    return RT_RUNTIME_STATUS_OK;
}

void rt_transport_state_destroy(rt_transport_state* state) {
    if (state == NULL) {
        return;
    }
    rt_transport_wake_destroy(&state->wake);
    memset(state, 0, sizeof(*state));
    rt_transport_wake_init_empty(&state->wake);
}

size_t rt_transport_inbound_len_locked(const rt_shard* shard) {
    if (shard == NULL) {
        return 0;
    }
    return shard->transport.control_len + shard->transport.data_len;
}

static rt_transport_status
rt_transport_push_locked(rt_transport_state* state, const rt_transport_msg* msg, int control) {
    if (state == NULL || msg == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_transport_msg* queue = control ? state->control : state->data;
    size_t cap = control ? RT_TRANSPORT_CONTROL_QUEUE_CAP : RT_TRANSPORT_DATA_QUEUE_CAP;
    const size_t* head = control ? &state->control_head : &state->data_head;
    size_t* len = control ? &state->control_len : &state->data_len;
    if (*len >= cap) {
        return RT_TRANSPORT_STATUS_QUEUE_FULL;
    }
    size_t index = (*head + *len) % cap;
    queue[index] = *msg;
    (*len)++;
    state->enqueue_count++;
    if (control) {
        state->control_enqueue_count++;
    } else {
        state->data_enqueue_count++;
    }
    if (msg->kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST) {
        state->transport_spawn_requests++;
    } else if (msg->kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK) {
        state->transport_spawn_acks++;
    } else if (msg->kind == RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION) {
        state->remote_task_completion_replies++;
    } else if (msg->kind == RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK) {
        state->remote_task_cancel_replies++;
    } else if (msg->kind == RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST) {
        state->remote_task_release_requests++;
    } else if (msg->kind == RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST) {
        state->immediate_on_execute_requests++;
    } else if (msg->kind == RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY) {
        state->immediate_on_replies++;
    } else if (msg->kind == RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST) {
        state->far_channel_create_requests++;
    } else if (msg->kind == RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY) {
        state->far_channel_create_replies++;
    }
    return RT_TRANSPORT_STATUS_OK;
}

static rt_transport_status
rt_transport_pop_locked(rt_transport_state* state, rt_transport_msg* out, int control) {
    if (state == NULL || out == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_transport_msg* queue = control ? state->control : state->data;
    size_t cap = control ? RT_TRANSPORT_CONTROL_QUEUE_CAP : RT_TRANSPORT_DATA_QUEUE_CAP;
    size_t* head = control ? &state->control_head : &state->data_head;
    size_t* len = control ? &state->control_len : &state->data_len;
    if (*len == 0) {
        return RT_TRANSPORT_STATUS_UNAVAILABLE;
    }
    *out = queue[*head];
    memset(&queue[*head], 0, sizeof(queue[*head]));
    *head = (*head + 1U) % cap;
    (*len)--;
    state->drain_count++;
    if (control) {
        state->control_drain_count++;
    } else {
        state->data_drain_count++;
    }
    return RT_TRANSPORT_STATUS_OK;
}

static void rt_transport_worker_wake_locked(rt_shard* shard) {
    rt_scheduler* scheduler = shard != NULL ? &shard->scheduler : NULL;
    if (shard == NULL || scheduler == NULL) {
        return;
    }
    if (scheduler->wake_pending < UINT32_MAX) {
        scheduler->wake_pending++;
    }
    pthread_cond_signal(&shard->worker_cv);
}

static rt_transport_status rt_transport_enqueue_locked(rt_shard* shard,
                                                       const rt_transport_msg* msg) {
    if (shard == NULL || msg == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    int category = rt_transport_msg_is_control(msg->kind);
    if (category < 0) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_transport_status status = rt_transport_push_locked(&shard->transport, msg, category != 0);
    if (status != RT_TRANSPORT_STATUS_OK) {
        return status;
    }
    atomic_thread_fence(memory_order_seq_cst);
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD);
#ifdef RT_TRANSPORT_NEG_RELAXED_PARK_ORDER
    (void)atomic_load_explicit(&shard->transport.park_state, memory_order_relaxed);
    uint8_t park_state = rt_transport_negative_relaxed_state();
#else
    uint8_t park_state = atomic_load_explicit(&shard->transport.park_state, memory_order_seq_cst);
#endif
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE);
    if (park_state == RT_TRANSPORT_SHARD_PARKED) {
#ifndef RT_TRANSPORT_NEG_SKIP_PARKED_WAKE
        shard->transport.transport_wake_writes++;
        rt_transport_wake_write(&shard->transport);
        rt_transport_worker_wake_locked(shard);
#endif
        return RT_TRANSPORT_STATUS_OK;
    }
#ifdef RT_TRANSPORT_NEG_WRITE_RUNNING_WAKE
    shard->transport.transport_wake_writes++;
    rt_transport_wake_write(&shard->transport);
    rt_transport_worker_wake_locked(shard);
#else
    shard->transport.transport_wake_elisions++;
#endif
    return RT_TRANSPORT_STATUS_OK;
}

rt_transport_status rt_transport_enqueue(rt_shard* shard, const rt_transport_msg* msg) {
    if (shard == NULL || msg == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        return rt_transport_enqueue_locked(shard, msg);
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: enqueue while holding another shard lock");
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(shard);
    rt_transport_status status = rt_transport_enqueue_locked(shard, msg);
    rt_shard_unlock(shard);
    return status;
}

rt_transport_status rt_transport_try_drain_one(rt_shard* shard, rt_transport_msg* out) {
    if (shard == NULL || out == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        rt_transport_status status = rt_transport_pop_locked(&shard->transport, out, 1);
        if (status == RT_TRANSPORT_STATUS_UNAVAILABLE) {
            status = rt_transport_pop_locked(&shard->transport, out, 0);
        }
        if (rt_transport_inbound_len_locked(shard) == 0) {
            rt_transport_wake_drain(&shard->transport);
        }
        return status;
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: drain while holding another shard lock");
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(shard);
    rt_transport_status status = rt_transport_pop_locked(&shard->transport, out, 1);
    if (status == RT_TRANSPORT_STATUS_UNAVAILABLE) {
        status = rt_transport_pop_locked(&shard->transport, out, 0);
    }
    if (rt_transport_inbound_len_locked(shard) == 0) {
        rt_transport_wake_drain(&shard->transport);
    }
    rt_shard_unlock(shard);
    return status;
}

static rt_transport_status rt_transport_prepare_shard_park_locked(rt_shard* shard) {
    if (shard == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK);
#ifdef RT_TRANSPORT_NEG_RELAXED_PARK_ORDER
    atomic_store_explicit(
        &shard->transport.park_state, RT_TRANSPORT_SHARD_PARKED, memory_order_relaxed);
#else
    atomic_store_explicit(
        &shard->transport.park_state, RT_TRANSPORT_SHARD_PARKED, memory_order_seq_cst);
#endif
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK);
#ifdef RT_TRANSPORT_NEG_SKIP_RECHECK
    return RT_TRANSPORT_STATUS_OK;
#else
    if (rt_transport_inbound_len_locked(shard) != 0) {
        atomic_store_explicit(
            &shard->transport.park_state, RT_TRANSPORT_SHARD_RUNNING, memory_order_seq_cst);
        return RT_TRANSPORT_STATUS_UNAVAILABLE;
    }
    return RT_TRANSPORT_STATUS_OK;
#endif
}

rt_transport_status rt_transport_prepare_shard_park(rt_shard* shard) {
    if (shard == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        return rt_transport_prepare_shard_park_locked(shard);
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: park while holding another shard lock");
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(shard);
    rt_transport_status status = rt_transport_prepare_shard_park_locked(shard);
    rt_shard_unlock(shard);
    return status;
}

void rt_transport_mark_shard_running(rt_shard* shard) {
    if (shard == NULL) {
        return;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        if (atomic_load_explicit(&shard->transport.park_state, memory_order_seq_cst) !=
            RT_TRANSPORT_SHARD_SHUTDOWN) {
            atomic_store_explicit(
                &shard->transport.park_state, RT_TRANSPORT_SHARD_RUNNING, memory_order_seq_cst);
        }
        return;
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: mark running while holding another shard lock");
        return;
    }
    rt_shard_lock(shard);
    rt_transport_mark_shard_running(shard);
    rt_shard_unlock(shard);
}

uint64_t rt_transport_shutdown_wake_all(rt_executor* ex) {
    if (ex == NULL || ex->runtime == NULL) {
        return 0;
    }
    RT_SYNC_POINT(SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE);
#ifdef RT_TRANSPORT_NEG_SHUTDOWN_NO_WAKE
    rt_transport_negative_shutdown_no_wake_touch(ex);
    return 0;
#else
    rt_runtime* runtime = ex->runtime;
    uint64_t wakes = 0;
    size_t count = runtime->shard_count;
    if (count > RT_RUNTIME_MAX_SHARDS) {
        count = RT_RUNTIME_MAX_SHARDS;
    }
    for (size_t i = 0; i < count; i++) {
        rt_shard* shard = &runtime->shards[i];
        rt_shard_lock(shard);
        uint8_t state = atomic_load_explicit(&shard->transport.park_state, memory_order_seq_cst);
        atomic_store_explicit(
            &shard->transport.park_state, RT_TRANSPORT_SHARD_SHUTDOWN, memory_order_seq_cst);
        shard->transport.shutdown_wakes++;
        shard->transport.transport_wake_writes++;
        rt_transport_wake_write(&shard->transport);
        rt_transport_worker_wake_locked(shard);
        wakes++;
        if (state == RT_TRANSPORT_SHARD_PARKED) {
            rt_transport_wake_drain(&shard->transport);
        }
        rt_shard_unlock(shard);
    }
    return wakes;
#endif
}

size_t rt_transport_drain_inbound_locked(rt_shard* shard, size_t limit) {
    if (shard == NULL) {
        return 0;
    }
    size_t drained = 0;
    rt_transport_msg msg = {0};
    while ((limit == 0 || drained < limit) &&
           rt_transport_pop_locked(&shard->transport, &msg, 1) == RT_TRANSPORT_STATUS_OK) {
        drained++;
    }
    while ((limit == 0 || drained < limit) &&
           rt_transport_pop_locked(&shard->transport, &msg, 0) == RT_TRANSPORT_STATUS_OK) {
        drained++;
    }
    if (rt_transport_inbound_len_locked(shard) == 0) {
        rt_transport_wake_drain(&shard->transport);
    }
    return drained;
}

int rt_transport_reply_wait_before_task_suspend(void) {
    RT_SYNC_POINT(SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND);
#ifdef RT_TRANSPORT_NEG_REPLY_WAIT_PARKS_SHARD
    return 0;
#else
    return 1;
#endif
}
