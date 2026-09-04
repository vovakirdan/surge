#include "rt_async_internal.h"
#include "rt_remote_spawn.h"
#include "rt_scope_teardown.h"
#include "rt_transport.h"

// A scope's accounting has one serializer, the pinned owner shard lock, and
// one lane that writes under it (owner ruling 2026-09-02, Р6). A child that
// completes on another shard therefore does not reach the scope from its own
// lane: it publishes a scope event -- which scope, which child, the committed
// kind, whether the child was counted -- into the owner shard's inbound
// control lane, and the owner lane applies it on drain in the same single
// critical section a same-shard completion uses. The event is release-class
// traffic: it is what lets a join finish, so a data backlog cannot hold a
// scope open, and it never takes the process-wide control lane.
//
// Envelope: route_id carries the scope id in its low 56 bits and the outcome
// in the top byte, generation carries the child id, payload nothing. Scope
// ids are a monotonic counter and never reused, so id + owner shard is the
// generation the ruling asks for: a stale event resolves no scope and is a
// no-op.

#define SCOPE_EVENT_ID_BITS 56u
#define SCOPE_EVENT_ID_MASK ((UINT64_C(1) << SCOPE_EVENT_ID_BITS) - 1u)
#define SCOPE_EVENT_KIND_MASK UINT64_C(0x7F)
#define SCOPE_EVENT_REGISTERED_BIT UINT64_C(0x80)

// The control reserve holds sixteen envelopes; every retry rescue-drains the
// owner shard first, so an attempt fails only while sixteen other events or
// releases refill the lane faster than it drains. Well past that the process
// answers with the invariant rather than spin.
#define SCOPE_EVENT_PUBLISH_ATTEMPTS 64u

#ifndef RV2_DEBT_280_NEGATIVE_CONTROL
static uint64_t scope_event_route(uint64_t scope_id, uint8_t result_kind, int child_registered) {
    uint64_t outcome = ((uint64_t)result_kind & SCOPE_EVENT_KIND_MASK) |
                       (child_registered ? SCOPE_EVENT_REGISTERED_BIT : 0u);
    return (scope_id & SCOPE_EVENT_ID_MASK) | (outcome << SCOPE_EVENT_ID_BITS);
}
#endif

static waker_key scope_event_key(const rt_transport_msg* msg) {
    return scope_key(msg->route_id & SCOPE_EVENT_ID_MASK, msg->target_shard_id);
}

static uint8_t scope_event_result_kind(const rt_transport_msg* msg) {
    return (uint8_t)((msg->route_id >> SCOPE_EVENT_ID_BITS) & SCOPE_EVENT_KIND_MASK);
}

static int scope_event_child_registered(const rt_transport_msg* msg) {
    return ((msg->route_id >> SCOPE_EVENT_ID_BITS) & SCOPE_EVENT_REGISTERED_BIT) != 0;
}

// Child's lane. No shard lock is held here (the caller released the child's
// own), so the enqueue may take the owner shard's lock for the push and the
// rescue drain may run other shards' traffic on this carrier -- both are the
// transport's ordinary admission path.
void rt_scope_publish_child_done(rt_executor* ex,
                                 waker_key key,
                                 const rt_task* child,
                                 uint8_t result_kind,
                                 int child_registered) {
    if (ex == NULL || child == NULL || !waker_valid(key)) {
        return;
    }
    if (key.id > SCOPE_EVENT_ID_MASK) {
        panic_msg("async: scope id does not fit the scope event envelope");
        return;
    }
#ifdef RV2_DEBT_280_NEGATIVE_CONTROL
    // Negative control: the pre-ruling shape. The child's lane reaches the
    // scope itself through the process-wide control lane and the owner's
    // lock, and no event is ever published: a driver holding the owner's join
    // open sees the accounting change under it with the event counter at
    // zero, which is exactly what the proof row refuses.
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
    }
    rt_shard* pinned = rt_waiter_key_shard(ex, key);
    rt_shard_lock(pinned);
    rt_scope_child_done_effects fx =
        rt_scope_take_child_done_locked(ex, key, child->id, result_kind, child_registered);
    rt_shard_unlock(pinned);
    rt_scope_child_done_effects_apply(ex, key, fx);
    if (need_control) {
        rt_control_unlock(ex);
    }
    return;
#else
    rt_transport_msg msg = {0};
    msg.kind = RT_TRANSPORT_MSG_SCOPE_CHILD_DONE;
    msg.source_shard_id = child->owner_shard_id;
    msg.target_shard_id = key.owner_shard_id;
    msg.route_id = scope_event_route(key.id, result_kind, child_registered);
    msg.generation = child->id;
    msg.payload = NULL;
    rt_shard* owner = rt_waiter_key_shard(ex, key);
    for (unsigned attempt = 0; attempt < SCOPE_EVENT_PUBLISH_ATTEMPTS; attempt++) {
        rt_transport_status status = rt_remote_spawn_enqueue_with_drain(ex, owner, &msg);
        if (status == RT_TRANSPORT_STATUS_OK) {
            return;
        }
        if (status != RT_TRANSPORT_STATUS_QUEUE_FULL) {
            break;
        }
    }
    panic_msg("async: a scope child-done event could not reach the scope's owner lane");
#endif
}

// Owner lane, on drain, with no shard lock held (the drain released it around
// dispatch). The one critical section, then the effects outside it.
void rt_scope_dispatch_child_done(rt_executor* ex, const rt_transport_msg* msg) {
    if (ex == NULL || msg == NULL) {
        return;
    }
    waker_key key = scope_event_key(msg);
    rt_shard* pinned = rt_waiter_key_shard(ex, key);
    rt_shard_lock(pinned);
    rt_scope_child_done_effects fx = rt_scope_take_child_done_locked(
        ex, key, msg->generation, scope_event_result_kind(msg), scope_event_child_registered(msg));
    rt_shard_unlock(pinned);
    rt_scope_child_done_effects_apply(ex, key, fx);
}

// Shutdown drain, holding the owner shard's lock already (the event's target
// IS that shard). The accounting step of every completed child is finished
// so no scope is left counting a child that is gone; nothing is woken or
// cancelled, there is no one left to wake.
void rt_scope_apply_child_done_at_shutdown_locked(rt_executor* ex, const rt_transport_msg* msg) {
    if (ex == NULL || msg == NULL) {
        return;
    }
    waker_key key = scope_event_key(msg);
    if (!rt_lane_holds_shard(key.owner_shard_id)) {
        panic_msg("async: scope event applied at shutdown outside its owner lane");
        return;
    }
    (void)rt_scope_take_child_done_locked(
        ex, key, msg->generation, scope_event_result_kind(msg), scope_event_child_registered(msg));
}
