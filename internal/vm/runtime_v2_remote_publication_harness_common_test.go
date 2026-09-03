//go:build runtime_v2_pending

package vm_test

// The stand's prologue: includes, fixtures and the helpers every mode uses.
//
// One program with a mode switch rather than one per case, because every case
// needs the same runtime brought up the same way and copies would drift.
const remotePublicationHarnessCommon = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"
#include "rt_channel_refcount.h"
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

// A shipped state's allocation: what a crossing hands the runtime, and what
// the runtime gives back when the crossing is abandoned. It POINTS AT the
// row's observation state rather than being it, for two reasons: a row still
// wants to read what the body saw after the runtime has freed the box, and a
// compiled state box is a heap allocation in every real program, which is the
// shape the abandon paths are written against.
typedef struct remote_state_box {
    remote_child_state* child;
} remote_state_box;

typedef struct remote_publish_state {
    rt_remote_spawn_pending* pending;
    // Release/acquire twin of the pending pointer for driver threads: the publisher
    // stores it after publish returns PENDING, giving a cross-thread
    // happens-before edge the sync-point counters (relaxed) do not.
    _Atomic(rt_remote_spawn_pending*) pending_shared;
    remote_state_box* child;
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
    remote_state_box* child;
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

void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
            *(uint64_t*)out_dst = 0;
        }
        return;
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
    };
    for (size_t i = 0; i < RT_TRANSPORT_DATA_SLOT_CREDITS; i++) {
        if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
            return 0;
        }
    }
    data.kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK;
    return rt_transport_enqueue(shard, &data) == RT_TRANSPORT_STATUS_OK;
}

// The exactly-once census for the shipped-state ownership contract. The
// abandon/refusal rows publish a droppable state and the runtime destroys it
// through that state type's DESCRIPTOR -- the drop below, then the block --
// so this is what counts, in place of the numeric dispatch it replaced.
static remote_state_box* remote_child_box(remote_child_state* child) {
    remote_state_box* box =
        (remote_state_box*)rt_alloc(sizeof(remote_state_box), _Alignof(remote_state_box));
    if (box != NULL) {
        box->child = child;
    }
    return box;
}

static _Atomic uint32_t drop_calls;
static _Atomic uint32_t payload_drop_calls;
static void* drop_expected_state;

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

// A producer parks from its own carrier, so a driver waits for the shard's
// park count rather than for a status.
static int wait_admission_parks(rt_executor* ex, uint32_t shard_id, uint64_t want, uint32_t attempts) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_id);
    for (uint32_t i = 0; i < attempts; i++) {
        if (rt_transport_debug_snapshot(shard).data_admission_parks >= want) {
            return 1;
        }
        // Under one carrier the driver is the pump: the caller parks from a
        // poll only this loop gives it.
        (void)run_ready_one(ex);
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
    for (size_t i = 0; i < RT_TRANSPORT_CONTROL_SLOT_RESERVE * 2; i++) {
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
//
// The probe holds a PIN across the pair. rt_channel_try_recv takes one of its
// own and gives it back on the way out, which leaves the release below holding
// nothing while it reads the channel's descriptor -- so the probe takes the
// hold that spans both, exactly as the select slow lane does around its own
// claim/move/release. rt_channel_release_payload refuses an unpinned caller
// rather than reading storage another lane may already have reclaimed.
static uint8_t channel_probe_take(rt_executor* ex, void* channel, uint64_t* out_bits) {
    (void)ex;
    *out_bits = 0;
    rt_channel_pin(channel);
    if (!rt_channel_try_recv(channel, out_bits)) {
        rt_channel_unpin(channel);
        return 0;
    }
    rt_channel_release_payload(channel, out_bits);
    rt_channel_unpin(channel);
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
`
