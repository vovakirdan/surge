#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_transport_internal.h"

#include "rt_carrier_bench.h"
#include "rt_resident_bytes.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <string.h>
#include <unistd.h>

// The transport's envelope half: the two class queues and the wake pipe --
// what a message IS and where it sits. Admission, the park/wake protocol,
// shutdown and the drains -- what a message DOES to a shard -- live in
// rt_transport_park.c; rt_transport_internal.h is the seam between the two.

// The reserve is for RELEASING, never for carrying. A message belongs to the
// control class only when its delivery is what lets somebody finish, drop a
// pin, or shut down: the ack that ends a publication handshake, a cancel and
// its ack, a handle release, a shutdown wake. Those are answers to work
// already admitted, so admitting them costs the target nothing it has not
// already agreed to hold.
//
// Everything a caller ASKS for is data -- including a completion or a reply
// that hands back a value the target must then consume. Such a message is
// caller-driven and unbounded in exactly the way a request is, so budgeting
// it against the reserve would let ordinary traffic spend the lane that
// clears a backlog: sixteen queued completions would refuse the seventeenth
// cancel. A backlog must never be able to destroy its own release path.
rt_transport_msg_class rt_transport_msg_class_of(rt_transport_msg_kind kind) {
    switch (kind) {
        case RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK:
        case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST:
        case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK:
        case RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST:
        case RT_TRANSPORT_MSG_SHUTDOWN_WAKE:
            return RT_TRANSPORT_MSG_CLASS_CONTROL;
        case RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST:
        case RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST:
        case RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION:
        case RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST:
        case RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY:
            return RT_TRANSPORT_MSG_CLASS_DATA;
        case RT_TRANSPORT_MSG_NONE:
            return RT_TRANSPORT_MSG_CLASS_INVALID;
    }
    // No `default` arm above: a kind added without a budget fails the -Wswitch
    // build rather than silently inheriting one. This return answers only for
    // a value that is not an enumerator at all.
    return RT_TRANSPORT_MSG_CLASS_INVALID;
}

static void rt_transport_push_unchecked_locked(rt_transport_state* state,
                                               const rt_transport_msg* msg,
                                               int control);

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
    wake->drain_calls = 0;
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

void rt_transport_wake_write(rt_transport_state* state) {
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

void rt_transport_wake_drain(rt_transport_state* state) {
    if (state == NULL || state->wake.initialized == 0 || state->wake.read_fd < 0) {
        return;
    }
    state->wake.drain_calls++;
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

_Static_assert(sizeof(rt_transport_msg) >= RT_TRANSPORT_MSG_FIELD_BYTES,
               "an envelope cannot be narrower than its fields");

// One envelope's residency, entered at push and left at pop: its fields and,
// beside them, the padding the struct layout inserts.
static void envelope_resident_acquire(void) {
    rt_resident_bytes_acquire(RT_RESIDENT_ENVELOPE, RT_TRANSPORT_MSG_FIELD_BYTES);
    rt_resident_bytes_acquire(RT_RESIDENT_PADDING, RT_TRANSPORT_MSG_PADDING_BYTES);
}

static void envelope_resident_release(void) {
    rt_resident_bytes_release(RT_RESIDENT_ENVELOPE, RT_TRANSPORT_MSG_FIELD_BYTES);
    rt_resident_bytes_release(RT_RESIDENT_PADDING, RT_TRANSPORT_MSG_PADDING_BYTES);
}

void rt_transport_state_destroy(rt_transport_state* state) {
    if (state == NULL) {
        return;
    }
    rt_transport_wake_destroy(&state->wake);
    // Envelopes still queued at teardown leave residency here: the lanes are
    // about to be zeroed and nothing will pop them.
    for (size_t left = state->data_len + state->control_len; left > 0; left--) {
        envelope_resident_release();
    }
    memset(state, 0, sizeof(*state));
    rt_transport_wake_init_empty(&state->wake);
}

size_t rt_transport_inbound_len_locked(const rt_shard* shard) {
    if (shard == NULL) {
        return 0;
    }
    return shard->transport.control_len + shard->transport.data_len;
}

rt_transport_status
rt_transport_push_locked(rt_transport_state* state, const rt_transport_msg* msg, int control) {
    if (state == NULL || msg == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    size_t cap = control ? RT_TRANSPORT_CONTROL_SLOT_RESERVE : RT_TRANSPORT_DATA_SLOT_CREDITS;
    const size_t* len = control ? &state->control_len : &state->data_len;
    // One credit per envelope, spent from this class's own budget. The other
    // class's occupancy is not consulted, so neither can exhaust the other.
    // On the data lane a slot promised to a reply counts as occupied: the
    // reply spends it later without asking, so nobody else may take it.
    size_t promised = control ? 0 : state->reply_reserved;
    if (*len + promised >= cap) {
        if (control) {
            state->control_reserve_stalls++;
        } else {
            state->data_credit_stalls++;
            rt_carrier_bench_record_credit_stall();
        }
        return RT_TRANSPORT_STATUS_QUEUE_FULL;
    }
    rt_transport_push_unchecked_locked(state, msg, control);
    return RT_TRANSPORT_STATUS_OK;
}

int rt_transport_msg_kind_is_reply(rt_transport_msg_kind kind) {
    switch (kind) {
        case RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION:
        case RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY:
            return 1;
        default:
            return 0;
    }
}

rt_transport_status rt_transport_reserve_reply_slot_locked(rt_transport_state* state) {
    if (state == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (state->data_len + state->reply_reserved >= RT_TRANSPORT_DATA_SLOT_CREDITS) {
        state->data_credit_stalls++;
        rt_carrier_bench_record_credit_stall();
        return RT_TRANSPORT_STATUS_QUEUE_FULL;
    }
    state->reply_reserved++;
    state->reply_reservations++;
    return RT_TRANSPORT_STATUS_OK;
}

void rt_transport_release_reply_slot_locked(rt_transport_state* state) {
    if (state == NULL) {
        return;
    }
    if (state->reply_reserved == 0) {
        panic_msg("transport: reply slot released more times than it was reserved");
        return;
    }
    state->reply_reserved--;
    state->reply_reservation_releases++;
}

rt_transport_status rt_transport_push_reserved_reply_locked(rt_transport_state* state,
                                                            const rt_transport_msg* msg) {
    if (state == NULL || msg == NULL || !rt_transport_msg_kind_is_reply(msg->kind)) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (state->reply_reserved == 0) {
        panic_msg("transport: a reply spent a reservation nobody held");
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    // The invariant every admission keeps is data_len + reply_reserved <= cap,
    // so a held reservation IS a free physical slot.
    state->reply_reserved--;
    state->reply_reservations_spent++;
    rt_transport_push_unchecked_locked(state, msg, 0);
    return RT_TRANSPORT_STATUS_OK;
}

static void rt_transport_push_unchecked_locked(rt_transport_state* state,
                                               const rt_transport_msg* msg,
                                               int control) {
    rt_transport_msg* queue = control ? state->control : state->data;
    size_t cap = control ? RT_TRANSPORT_CONTROL_SLOT_RESERVE : RT_TRANSPORT_DATA_SLOT_CREDITS;
    const size_t* head = control ? &state->control_head : &state->data_head;
    size_t* len = control ? &state->control_len : &state->data_len;
    size_t index = (*head + *len) % cap;
    queue[index] = *msg;
    (*len)++;
    envelope_resident_acquire();
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
    } else if (msg->kind == RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST) {
        state->far_channel_share_requests++;
    } else if (msg->kind == RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY) {
        state->far_channel_share_replies++;
    } else if (msg->kind == RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST) {
        state->far_channel_select_requests++;
    } else if (msg->kind == RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY) {
        state->far_channel_select_replies++;
    }
}

rt_transport_status
rt_transport_pop_locked(rt_transport_state* state, rt_transport_msg* out, int control) {
    if (state == NULL || out == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_transport_msg* queue = control ? state->control : state->data;
    size_t cap = control ? RT_TRANSPORT_CONTROL_SLOT_RESERVE : RT_TRANSPORT_DATA_SLOT_CREDITS;
    size_t* head = control ? &state->control_head : &state->data_head;
    size_t* len = control ? &state->control_len : &state->data_len;
    if (*len == 0) {
        return RT_TRANSPORT_STATUS_UNAVAILABLE;
    }
    *out = queue[*head];
    memset(&queue[*head], 0, sizeof(queue[*head]));
    *head = (*head + 1U) % cap;
    (*len)--;
    envelope_resident_release();
    state->drain_count++;
    if (control) {
        state->control_drain_count++;
    } else {
        state->data_drain_count++;
    }
    return RT_TRANSPORT_STATUS_OK;
}
