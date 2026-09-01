#ifndef SURGE_RUNTIME_NATIVE_RT_SCOPE_TEARDOWN_H
#define SURGE_RUNTIME_NATIVE_RT_SCOPE_TEARDOWN_H

#include "rt_waiter.h"

typedef struct rt_scope rt_scope;

rt_shard* rt_scope_owner_shard(rt_executor* ex, const rt_scope* scope);
rt_scope* rt_scope_resolve_key_locked(rt_executor* ex, waker_key key);
void scope_on_child_done(rt_executor* ex, rt_task* task, uint8_t result_kind);
void scope_cancel_children_controlled(rt_executor* ex, waker_key key);
void scope_exit_locked(rt_executor* ex, rt_scope* scope);

typedef enum {
    RT_SCOPE_TEARDOWN_DONE = 0,
    RT_SCOPE_TEARDOWN_PARKED = 1,
} rt_scope_teardown_status;

rt_scope_teardown_status
rt_scope_cancel_teardown(rt_executor* ex, rt_task* task, int cancel_children, waker_key* park_key);

#endif
