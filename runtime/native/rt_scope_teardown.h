#ifndef SURGE_RUNTIME_NATIVE_RT_SCOPE_TEARDOWN_H
#define SURGE_RUNTIME_NATIVE_RT_SCOPE_TEARDOWN_H

#include "rt_waiter.h"

typedef struct rt_scope rt_scope;

rt_shard* rt_scope_owner_shard(rt_executor* ex, const rt_scope* scope);
rt_scope* rt_scope_resolve_key_locked(rt_executor* ex, waker_key key);
void scope_on_child_done(rt_executor* ex, rt_task* task, uint8_t result_kind);
void scope_cancel_children_controlled(rt_executor* ex, waker_key key);
void scope_exit_locked(rt_executor* ex, rt_scope* scope);

// What one child's completion decided under the scope's serializer, carried
// out of the lock: a raised fail-fast names the owner to cancel for; a
// drained set wakes the scope key.
typedef struct rt_scope_child_done_effects {
    uint64_t owner_to_wake;
    int drained;
} rt_scope_child_done_effects;

rt_scope_child_done_effects rt_scope_take_child_done_locked(
    rt_executor* ex, waker_key key, uint64_t child_id, uint8_t result_kind, int child_registered);
void rt_scope_child_done_effects_apply(rt_executor* ex,
                                       waker_key key,
                                       rt_scope_child_done_effects fx);

// Cross-owner completion (rt_scope_event.c): the child's lane publishes,
// the scope's owner lane applies on drain; shutdown applies under the lock
// the drain already holds so no scope reads a child it still counts.
struct rt_transport_msg;
void rt_scope_publish_child_done(rt_executor* ex,
                                 waker_key key,
                                 const rt_task* child,
                                 uint8_t result_kind,
                                 int child_registered);
void rt_scope_dispatch_child_done(rt_executor* ex, const struct rt_transport_msg* msg);
void rt_scope_apply_child_done_at_shutdown_locked(rt_executor* ex,
                                                  const struct rt_transport_msg* msg);

typedef enum {
    RT_SCOPE_TEARDOWN_DONE = 0,
    RT_SCOPE_TEARDOWN_PARKED = 1,
} rt_scope_teardown_status;

rt_scope_teardown_status
rt_scope_cancel_teardown(rt_executor* ex, rt_task* task, int cancel_children, waker_key* park_key);

#endif
