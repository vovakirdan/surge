//go:build runtime_v2_pending

package vm_test

import "testing"

// The transport is budgeted in SLOTS: one credit per envelope, spent from the
// class's own budget, because the payload is a pointer into a refcount graph
// the transport neither copies nor owns and so has no per-message byte cost.
//
// The property under test is the one a backlog must never be able to break:
// data traffic at its bound cannot make a release message inadmissible. The
// release messages -- the publication ack, a cancel and its ack, a handle
// release, a shutdown wake -- are exactly what drains the backlog, so a bound
// that lets data spend their slots deadlocks itself, and a bigger shared
// number only moves the deadlock.
//
// Before the classes were split, a completion and a reply spent the reserve:
// sixteen of them filled it, and the next ack was refused while sixty-three
// data slots stood empty. This stand fills the data budget with exactly that
// traffic and then requires every release message to be admitted.
func TestRuntimeV2TransportSlotCreditReserve(t *testing.T) {
	source := `
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "rt_async_internal.h"
#include "rt_transport.h"

void panic_msg(const char* msg) {
    fprintf(stderr, "panic: %s\n", msg);
}

void rt_trace_control_lock_acquired(void) {
}

static int require_int(int condition, const char* message) {
    if (!condition) {
        fprintf(stderr, "%s\n", message);
        return 1;
    }
    return 0;
}

static int init_shard(rt_shard* shard, rt_runtime* runtime, rt_executor* ex, uint32_t id) {
    memset(shard, 0, sizeof(*shard));
    shard->runtime = runtime;
    shard->executor = ex;
    shard->shard_id = id;
    if (rt_shard_sync_init(shard) != RT_RUNTIME_STATUS_OK) return 1;
    if (rt_transport_state_init(&shard->transport) != RT_RUNTIME_STATUS_OK) return 2;
    shard->scheduler.worker_count = 1;
    return 0;
}

static rt_transport_msg envelope(rt_transport_msg_kind kind, uint64_t route) {
    rt_transport_msg msg = {0};
    msg.kind = kind;
    msg.source_shard_id = 1;
    msg.target_shard_id = 0;
    msg.route_id = route;
    msg.generation = 1;
    return msg;
}

// The data classes, in the order a real handshake produces them: a request
// out, a completion or reply back. Every one of these carries a value the
// target must consume, so every one spends a data credit.
static const rt_transport_msg_kind data_kinds[] = {
    RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
    RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST,
    RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION,
    RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST,
    RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY,
    RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST,
    RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY,
    RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST,
    RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY,
    RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST,
    RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY,
};

// The release-and-teardown classes: the whole reserve, and nothing else.
static const rt_transport_msg_kind control_kinds[] = {
    RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK,
    RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST,
    RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK,
    RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST,
    RT_TRANSPORT_MSG_SHUTDOWN_WAKE,
};

int main(void) {
    rt_executor ex = {0};
    rt_runtime runtime = {0};
    runtime.shard_count = 1;
    ex.runtime = &runtime;
    if (init_shard(&runtime.shards[0], &runtime, &ex, 0) != 0) return 2;
    rt_shard* shard = &runtime.shards[0];

    const size_t data_kind_count = sizeof(data_kinds) / sizeof(data_kinds[0]);
    const size_t control_kind_count = sizeof(control_kinds) / sizeof(control_kinds[0]);

    // The minimal shape of the failure this budget exists to prevent: exactly
    // as many completions as the reserve has slots, and then one release
    // message. A completion is an answer carrying a value, so it is data; if
    // it were charged to the reserve, these would fill it and the ack behind
    // them would be refused with the whole data budget standing empty.
    for (size_t i = 0; i < RT_TRANSPORT_CONTROL_SLOT_RESERVE; i++) {
        rt_transport_msg msg = envelope(RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION, i);
        if (require_int(rt_transport_enqueue(shard, &msg) == RT_TRANSPORT_STATUS_OK,
                        "data budget refused a completion below its bound")) return 3;
    }
    rt_transport_msg ack = envelope(RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK, 700);
    if (require_int(rt_transport_enqueue(shard, &ack) == RT_TRANSPORT_STATUS_OK,
                    "control reserve refused a release message behind a data backlog")) return 4;
    struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
    if (require_int(snap.data_len == RT_TRANSPORT_CONTROL_SLOT_RESERVE,
                    "completions did not spend the data budget")) return 5;
    if (require_int(snap.control_len == 1,
                    "the reserve holds something other than the release message")) return 6;

    // Now to the data bound proper, across every data class. Requests and the
    // completions and replies that answer them share one budget, because each
    // is one envelope over a pointer the transport does not own.
    for (size_t i = snap.data_len; i < RT_TRANSPORT_DATA_SLOT_CREDITS; i++) {
        rt_transport_msg msg = envelope(data_kinds[i % data_kind_count], i);
        if (require_int(rt_transport_enqueue(shard, &msg) == RT_TRANSPORT_STATUS_OK,
                        "data budget refused an envelope below its bound")) return 7;
    }
    snap = rt_transport_debug_snapshot(shard);
    if (require_int(snap.data_len == RT_TRANSPORT_DATA_SLOT_CREDITS,
                    "data traffic did not fill the data budget")) return 8;

    // At the bound the next data envelope is refused, and the refusal is
    // COUNTED: a stall the transport does not record is a stall nobody can
    // measure.
    rt_transport_msg overflow = envelope(RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION, 900);
    if (require_int(rt_transport_enqueue(shard, &overflow) == RT_TRANSPORT_STATUS_QUEUE_FULL,
                    "data budget at its bound did not refuse")) return 9;
    snap = rt_transport_debug_snapshot(shard);
    if (require_int(snap.data_slot_stalls == 1,
                    "refused data envelope was not counted as a slot-credit stall")) return 10;
    if (require_int(snap.control_len == 1,
                    "a refused data envelope fell back onto the control reserve")) return 11;

    // The reserve is intact under a saturated data lane: every release message
    // is still admitted, and none of them stalls.
    for (size_t i = 0; i < control_kind_count; i++) {
        rt_transport_msg msg = envelope(control_kinds[i], 100 + i);
        if (require_int(rt_transport_enqueue(shard, &msg) == RT_TRANSPORT_STATUS_OK,
                        "control reserve refused a release message behind a full data lane")) {
            return 12;
        }
    }
    snap = rt_transport_debug_snapshot(shard);
    if (require_int(snap.control_len == control_kind_count + 1,
                    "release messages did not land in the reserve")) return 13;
    if (require_int(snap.control_reserve_stalls == 0,
                    "a data backlog stalled the control reserve")) return 14;
    if (require_int(snap.data_slot_stalls == 1,
                    "a control admission was charged to the data budget")) return 15;

    // A reply reaching the target is only half of getting through: it must
    // also be DRAINED ahead of the backlog it is meant to release.
    rt_transport_msg out = {0};
    if (require_int(rt_transport_try_drain_one(shard, &out) == RT_TRANSPORT_STATUS_OK,
                    "drain of a saturated shard failed")) return 16;
    if (require_int(out.kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK,
                    "the reserve did not drain ahead of the data backlog")) return 17;

    // Filling the reserve stalls the reserve and only the reserve: the two
    // budgets are counted apart, so neither refusal can be read as the other.
    for (;;) {
        rt_transport_msg msg = envelope(RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK, 500);
        if (rt_transport_enqueue(shard, &msg) != RT_TRANSPORT_STATUS_OK) {
            break;
        }
    }
    snap = rt_transport_debug_snapshot(shard);
    if (require_int(snap.control_len == RT_TRANSPORT_CONTROL_SLOT_RESERVE,
                    "the reserve did not fill to its own bound")) return 18;
    if (require_int(snap.control_reserve_stalls == 1,
                    "a refused control envelope was not counted")) return 19;
    if (require_int(snap.data_slot_stalls == 1,
                    "a control refusal was charged to the data budget")) return 20;

    rt_shard_lock(shard);
    (void)rt_transport_drain_inbound_locked(shard, 0);
    rt_shard_unlock(shard);
    rt_transport_state_destroy(&shard->transport);
    rt_shard_sync_destroy(shard);
    return 0;
}
`

	runTransportCProgram(t, "Runtime V2 transport slot-credit reserve", source, nil)
}
