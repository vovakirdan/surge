//go:build runtime_v2_pending

package vm_test

// The stand's prologue: includes, fixtures and the helpers every mode uses.
//
// One program with a mode switch rather than one per case, because every case
// needs the same runtime brought up the same way and copies would drift.
const remotePublicationHarnessCommon = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"
#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_remote_spawn.h"
#include "rt_remote_spawn_internal.h"
#include "rt_remote_task.h"
#include "rt_remote_task_internal.h"
#include "rt_sync_point.h"
#include "rt_value_ops.h"
#include "rt_transport.h"

#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

enum {
    POLL_REMOTE_PUBLISHER = 7001,
    POLL_REMOTE_CHILD = 7002,
    DROP_REMOTE_STATE = 7003,
    POLL_IMMEDIATE_CALLER = 7004,
    POLL_SELECT_CALLER = 7005,
    POLL_SELECT_BODY = 7006,
    DROP_SELECT_PAYLOAD = 7007,
    WIDE_SELECT_PAYLOAD = 7008
};

typedef struct remote_child_state {
    _Atomic uint32_t ran;
    _Atomic uint32_t owner;
    _Atomic uint32_t worker;
} remote_child_state;

typedef struct remote_publish_state {
    rt_remote_spawn_pending* pending;
    // Release/acquire twin of the pending pointer for driver threads: the publisher
    // stores it after publish returns PENDING, giving a cross-thread
    // happens-before edge the sync-point counters (relaxed) do not.
    _Atomic(rt_remote_spawn_pending*) pending_shared;
    remote_child_state* child;
    rt_far_task_handle handle;
    uint32_t dst;
    uint32_t fill_queue;
    uint32_t shutdown_first;
    uint32_t droppable;
    uint32_t abandon_mode;
    uint32_t filled;
    uint32_t shutdown_done;
    uint32_t saw_pending;
    uint64_t request_id;
    rt_remote_spawn_status status;
    rt_remote_spawn_status validate_status;
    size_t children_after;
} remote_publish_state;

typedef struct immediate_exec_state {
    rt_remote_task_pending* pending;
    // Release/acquire twin of the pending pointer for driver threads
    // (the sync-point counters alone give no happens-before edge).
    _Atomic(rt_remote_task_pending*) pending_shared;
    remote_child_state* child;
    uint64_t placement;
    uint32_t fill_queue;
    uint32_t shutdown_first;
    uint32_t droppable;
    uint32_t filled;
    uint32_t shutdown_done;
    uint32_t saw_pending;
    // Anchored rows: route through rt_immediate_on_execute_anchored against
    // this token instead of rt_immediate_on_execute against a placement.
    uint32_t anchored;
    rt_far_task_handle anchor;
    uint8_t out_kind;
    uint64_t out_bits;
    rt_remote_task_status status;
} immediate_exec_state;

// Remote select rows (Epic 20 Task 7 rows 2-5). The caller side wraps
// rt_far_channel_select the same way immediate_exec_state wraps
// rt_immediate_on_execute_anchored; the body side is minimal, since the
// real work (rt_anchored_channel_select) is production runtime code, not
// harness code -- the body poll function only has to hand the winner
// index to rt_async_return, mirroring the compiled select lowering.
typedef struct select_exec_state {
    rt_remote_task_pending* pending;
    _Atomic(rt_remote_task_pending*) pending_shared;
    void* body_state;
    uint32_t droppable;
    uint32_t saw_pending;
    uint32_t fill_queue;
    uint32_t filled;
	uint32_t null_anchor_array;
    rt_far_task_handle anchors[2];
    const rt_far_task_handle* anchor_ptrs[2];
    uint8_t kinds[2];
    // A SEND arm's payload lives HERE, in the caller's own storage, and the
    // select is handed its ADDRESS: the arming call moves the value out of it
    // and a losing arm's value is moved back into it. send_bits used to be the
    // value itself, which is why anything wider than a word had to be boxed.
    uint64_t send_bits[2];
    void* send_addrs[2];
    uint64_t send_type_ids[2];
    // Storage for a payload WIDER than a machine word. The arm table could not
    // carry one at all before the arm owned a typed cell: its payload field was
    // a uint64_t, so anything wider had to be boxed to travel as a pointer.
    uint64_t wide_send[2];
    // 1 + the index of the arm whose payload lives in wide_send, or 0.
    uint32_t wide_arm;
    uint64_t count;
    uint8_t out_kind;
    uint64_t out_bits;
    rt_remote_task_status status;
} select_exec_state;

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    return 0;
}

static void sleep_us(unsigned long micros) {
    struct timespec ts;
    ts.tv_sec = (time_t)(micros / 1000000UL);
    ts.tv_nsec = (long)((micros % 1000000UL) * 1000UL);
    while (nanosleep(&ts, &ts) != 0) {
    }
}

static int fail(const char* msg) {
    if (msg != NULL) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

static int wait_child(remote_child_state* child, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&child->ran, memory_order_acquire) != 0) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static uint32_t pin_shard(rt_executor* ex, uint32_t wanted) {
    size_t count = rt_runtime_shard_count(rt_executor_runtime(ex));
    return count > 0 ? (uint32_t)(wanted % (uint32_t)count) : 0;
}

static int fill_data_lane(rt_executor* ex, uint32_t dst) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), dst);
    if (shard == NULL) {
        return 0;
    }
    rt_transport_msg data = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
        .source_shard_id = 0,
        .target_shard_id = dst,
        .route_id = 9000,
        .generation = 1,
        .payload = NULL,
        .payload_len = 0,
    };
    for (size_t i = 0; i < RT_TRANSPORT_DATA_QUEUE_CAP; i++) {
        if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
            return 0;
        }
    }
    data.kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK;
    return rt_transport_enqueue(shard, &data) == RT_TRANSPORT_STATUS_OK;
}

// Drop-dispatch stub: the abandon/refusal rows publish a droppable state
// (DROP_REMOTE_STATE) and count releases here — the exactly-once census
// for the shipped-state ownership contract. Any other id is a test bug.
static _Atomic uint32_t drop_calls;
static _Atomic uint32_t payload_drop_calls;
static void* drop_expected_state;
void __surge_drop_call(uint64_t id, void* state) {
    if (id == DROP_REMOTE_STATE && state == drop_expected_state) {
        atomic_fetch_add_explicit(&drop_calls, 1, memory_order_acq_rel);
        return;
    }
    fputs("unexpected __surge_drop_call\n", stderr);
    exit(97);
}

void __surge_drop_result_call(uint64_t id, void* value) {
    (void)value;
    (void)id;
    fputs("unexpected __surge_drop_result_call\n", stderr);
    exit(97);
}

// The stand's own payload descriptor: a word whose destruction is COUNTED.
//
// A select arm's payload is destroyed through the descriptor its type names,
// so a stand that wants to observe that destruction has to supply one. The
// drop below is what payload_drop_calls counts, in place of the numeric drop
// id the arm table used to carry beside the value.
static void select_payload_move(void* destination, void* source) {
    *(uint64_t*)destination = *(uint64_t*)source;
    *(uint64_t*)source = 0;
}

static void select_payload_drop(void* value) {
    (void)value;
    atomic_fetch_add_explicit(&payload_drop_calls, 1, memory_order_acq_rel);
}

// The same descriptor one word WIDER, for the rows that prove an arm carries a
// value at its own width rather than at a pointer's.
static void wide_payload_move(void* destination, void* source) {
    uint64_t* dst = (uint64_t*)destination;
    uint64_t* src = (uint64_t*)source;
    dst[0] = src[0];
    dst[1] = src[1];
    src[0] = 0;
    src[1] = 0;
}

static void wide_payload_drop(void* value) {
    (void)value;
    atomic_fetch_add_explicit(&payload_drop_calls, 1, memory_order_acq_rel);
}

static rt_carrier_status
select_payload_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

// The compiler emits this lookup for real programs; a stand defines it to give
// its own types descriptors. Only the select payload has one -- every other id
// answers NULL, which the runtime reads as "carried as an opaque word".
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
const rt_value_ops* __surge_value_ops_for(uint64_t type_id) {
    static const rt_value_ops payload_ops = {
        .layout = {.size = sizeof(uint64_t),
                   .align = _Alignof(uint64_t),
                   .stride = sizeof(uint64_t),
                   .flags = RT_VALUE_FLAG_DROPPABLE},
        .move_init = select_payload_move,
        .copy_init = NULL,
        .clone_init = NULL,
        .drop_in_place = select_payload_drop,
        .trace = NULL,
        .plan_cross = select_payload_plan,
        .cross_move_init = NULL,
        .cross_clone_init = NULL,
    };
    static const rt_value_ops wide_ops = {
        .layout = {.size = 2 * sizeof(uint64_t),
                   .align = _Alignof(uint64_t),
                   .stride = 2 * sizeof(uint64_t),
                   .flags = RT_VALUE_FLAG_DROPPABLE},
        .move_init = wide_payload_move,
        .copy_init = NULL,
        .clone_init = NULL,
        .drop_in_place = wide_payload_drop,
        .trace = NULL,
        .plan_cross = select_payload_plan,
        .cross_move_init = NULL,
        .cross_clone_init = NULL,
    };
    if (type_id == DROP_SELECT_PAYLOAD) {
        return &payload_ops;
    }
    return type_id == WIDE_SELECT_PAYLOAD ? &wide_ops : NULL;
}

// No row here threads a nonzero abandoned-state drop id through
// rt_async_yield/rt_async_return_cancelled, so reaching this is a test bug.
void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    fputs("unexpected __surge_drop_abandoned_state_call\n", stderr);
    exit(97);
}

static int wait_reached(rt_sync_point_id id, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (rt_sync_point_reached_count(id) > 0) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static int wait_drops(uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&drop_calls, memory_order_acquire) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static int wait_payload_drops(uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

// Saturate the shard's control lane so the NEXT control-kind enqueue
// (an ack) fails deterministically. NULL-payload acks are harmless to
// drain later (the ack dispatcher ignores them).
static int fill_control_lane(rt_shard* shard, uint32_t shard_id) {
    if (shard == NULL) {
        return 0;
    }
    rt_transport_msg ack = {0};
    ack.kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK;
    ack.target_shard_id = shard_id;
    for (size_t i = 0; i < RT_TRANSPORT_CONTROL_QUEUE_CAP * 2; i++) {
        if (rt_transport_enqueue(shard, &ack) == RT_TRANSPORT_STATUS_QUEUE_FULL) {
            return 1;
        }
    }
    return 0;
}

// Dispatch-completion signal for the redelivery rows: the redelivered
// message's pending_release is the LAST step of its dispatch path, so an
// acquire-load seeing the reference count fall to the wanted value happens-after
// everything that dispatch did (or erroneously did) with the pending.
static int wait_refs(rt_remote_spawn_pending* req, uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&req->refs, memory_order_acquire) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static rt_remote_task_pending* wait_task_pending_shared(immediate_exec_state* st,
                                                        uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        rt_remote_task_pending* req =
            atomic_load_explicit(&st->pending_shared, memory_order_acquire);
        if (req != NULL) {
            return req;
        }
        sleep_us(1000);
    }
    return NULL;
}

static int wait_task_refs(rt_remote_task_pending* req, uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&req->refs, memory_order_acquire) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static rt_remote_task_pending* wait_select_pending_shared(select_exec_state* st,
                                                          uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        rt_remote_task_pending* req =
            atomic_load_explicit(&st->pending_shared, memory_order_acquire);
        if (req != NULL) {
            return req;
        }
        sleep_us(1000);
    }
    return NULL;
}

// Non-blocking, driver-thread-safe channel probes for the select rows: the
// driver runs outside any task, so the task-context channel API
// (rt_channel_recv/rt_channel_try_send) is unavailable; the control-locked
// status wrappers used by the select slow lane work from any thread.
//
// A probe never wants the value, and must still TAKE it. The try-recv core
// refuses a null sink on purpose (rt_channel_sync.c): popping a buffered entry
// and acking a parked sender are both irreversible, so a caller with no sink
// is not asking a question, it is destroying the answer where nobody can see.
// Every probe therefore receives into a real sink and hands whatever came out
// to rt_channel_release_payload -- the runtime's own way to destroy a value
// that will never reach a receiver -- after the control lock is released,
// because compiled drop glue must not run under a runtime lock.
//
// Releasing does not move the census these rows assert: every channel they
// mint goes through mint_anchor/mint_channel_anchor, which call
// rt_channel_new(capacity, rt_channel_opaque_word_ops(), 0); that descriptor
// claims no drop, so the release below runs nothing. payload_drop_calls counts
// only DROP_SELECT_PAYLOAD, which is the SELECT arm's own drop id and never the
// channel's, so the probe contributes nothing to it either way.
//
// The probe goes through the PUBLIC entry point now. It used to hold the
// control lock across a claim of its own, which a typed take cannot do: the
// element's move runs between the claim and the commit, and no generated
// operation may run under a runtime lock. rt_channel_try_recv performs that
// whole sequence, taking and releasing the channel's own lock as it goes.
static uint8_t channel_probe_take(rt_executor* ex, void* channel, uint64_t* out_bits) {
    (void)ex;
    *out_bits = 0;
    if (!rt_channel_try_recv(channel, out_bits)) {
        return 0;
    }
    rt_channel_release_payload(channel, out_bits);
    return 1;
}

static int channel_recv_once(rt_executor* ex, void* channel, uint64_t want_bits) {
    uint64_t bits = 0;
    if (channel_probe_take(ex, channel, &bits) != 1 || bits != want_bits) {
        return 0;
    }
    return channel_probe_take(ex, channel, &bits) == 0;
}

static int channel_is_empty(rt_executor* ex, void* channel) {
    uint64_t bits = 0;
    return channel_probe_take(ex, channel, &bits) == 0;
}

static rt_remote_spawn_pending* wait_pending_shared(remote_publish_state* st, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        rt_remote_spawn_pending* req =
            atomic_load_explicit(&st->pending_shared, memory_order_acquire);
        if (req != NULL) {
            return req;
        }
        sleep_us(1000);
    }
    return NULL;
}

void __surge_poll_call(uint64_t id) {
    if (id == POLL_REMOTE_CHILD) {
        remote_child_state* child = (remote_child_state*)__task_state();
        const rt_task* task = rt_current_task();
        atomic_store_explicit(&child->owner,
                              task != NULL ? task->owner_shard_id : UINT32_MAX,
                              memory_order_release);
        atomic_store_explicit(&child->worker, rt_debug_current_worker_shard_id(), memory_order_release);
        // A counter, not a flag: the duplicate/stale rows assert that a
        // redelivered request never creates a SECOND body.
        atomic_fetch_add_explicit(&child->ran, 1, memory_order_acq_rel);
        rt_async_return(child, &(uint64_t){77});
        return;
    }
    if (id == POLL_IMMEDIATE_CALLER) {
        immediate_exec_state* st = (immediate_exec_state*)__task_state();
        rt_executor* ex = ensure_exec();
        if (st->shutdown_first && !st->shutdown_done) {
            st->shutdown_done = 1;
            (void)rt_executor_request_shutdown(ex);
        }
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ex, 0)) {
                st->status = RT_REMOTE_TASK_STATUS_REFUSED;
                rt_async_return(st, &(uint64_t){(uint64_t)st->status});
                return;
            }
        }
        rt_remote_task_status status = st->anchored
            ? rt_immediate_on_execute_anchored(&st->anchor, st->droppable ? DROP_REMOTE_STATE : 0, 0, POLL_REMOTE_CHILD, st->child, &st->pending, &st->out_kind, &st->out_bits)
            : rt_immediate_on_execute(st->placement, st->droppable ? DROP_REMOTE_STATE : 0, 0, POLL_REMOTE_CHILD, st->child, &st->pending, &st->out_kind, &st->out_bits);
        if (status == RT_REMOTE_TASK_STATUS_PENDING) {
            st->saw_pending = 1;
            atomic_store_explicit(&st->pending_shared, st->pending, memory_order_release);
            rt_async_yield(st, 0);
            return;
        }
        st->status = status;
        rt_async_return(st, &(uint64_t){(uint64_t)status});
        return;
    }
    if (id == POLL_SELECT_CALLER) {
        select_exec_state* st = (select_exec_state*)__task_state();
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ensure_exec(), st->anchors[0].owner_shard_id)) {
                st->status = RT_REMOTE_TASK_STATUS_REFUSED;
                rt_async_return(st, &(uint64_t){(uint64_t)st->status});
                return;
            }
        }
		const rt_far_task_handle* const* anchors =
			st->null_anchor_array ? NULL : st->anchor_ptrs;
		for (uint64_t i = 0; i < st->count; i++) {
			st->send_addrs[i] = st->kinds[i] == 2 ? (void*)&st->send_bits[i] : NULL;
		}
		if (st->wide_arm != 0) {
			st->send_addrs[st->wide_arm - 1] = (void*)st->wide_send;
		}
		rt_remote_task_status status = rt_far_channel_select(
			anchors, st->kinds, st->send_addrs, st->send_type_ids, st->count,
            st->droppable ? DROP_REMOTE_STATE : 0, POLL_SELECT_BODY, st->body_state,
            &st->pending, &st->out_kind, &st->out_bits);
        if (status == RT_REMOTE_TASK_STATUS_PENDING) {
            st->saw_pending = 1;
            atomic_store_explicit(&st->pending_shared, st->pending, memory_order_release);
            rt_async_yield(st, 0);
            return;
        }
        st->status = status;
        rt_async_return(st, &(uint64_t){(uint64_t)status});
        return;
    }
    if (id == POLL_SELECT_BODY) {
        // The compiled lowering calls rt_anchored_channel_select and stores
        // the winner into the block's select_index destination; the
        // harness body mirrors that exactly, since the select machinery
        // itself is production runtime code under test, not a stand-in.
        uint64_t winner = rt_anchored_channel_select();
        rt_async_return(__task_state(), &(uint64_t){winner});
        return;
    }
    if (id == POLL_REMOTE_PUBLISHER) {
        remote_publish_state* st = (remote_publish_state*)__task_state();
        rt_executor* ex = ensure_exec();
        if (st->abandon_mode && st->saw_pending) {
            // The abandon rows model a departed caller: after the driver
            // abandons the handle the pending may be freed at any moment,
            // so this task must never touch it again.
            rt_async_yield(st, 0);
            return;
        }
        if (st->shutdown_first && !st->shutdown_done) {
            st->shutdown_done = 1;
            (void)rt_executor_request_shutdown(ex);
        }
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ex, st->dst)) {
                rt_async_return(st, &(uint64_t){RT_REMOTE_SPAWN_STATUS_REFUSED});
                return;
            }
        }
        rt_remote_spawn_status status = rt_remote_spawn_publish(
            st->dst, st->droppable ? DROP_REMOTE_STATE : 0, 0, POLL_REMOTE_CHILD,
            st->child, &st->pending, &st->handle);
        if (status == RT_REMOTE_SPAWN_STATUS_PENDING) {
            st->saw_pending = 1;
            st->request_id = rt_remote_spawn_pending_request_id(st->pending);
            atomic_store_explicit(&st->pending_shared, st->pending, memory_order_release);
            rt_async_yield(st, 0);
            return;
        }
        st->status = status;
        st->children_after = rt_current_task() != NULL ? rt_current_task()->children_len : 999;
        st->validate_status = status == RT_REMOTE_SPAWN_STATUS_OK
                                  ? rt_remote_spawn_handle_validate(ex, &st->handle)
                                  : status;
        rt_async_return(st, &(uint64_t){(uint64_t)status});
        return;
    }
    rt_async_return(NULL, &(uint64_t){0});
}

static int await_parent(remote_publish_state* st) {
    void* task = __task_create(POLL_REMOTE_PUBLISHER, st, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    if (kind != 1) {
        return 0;
    }
    return bits == (uint64_t)st->status;
}
`
