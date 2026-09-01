#include "rt_async_internal.h"
#include "rt_sync_point.h"

// Cancelled scope owners wait for their children on the scope's pinned owner
// lane. The active key is write-once at scope creation and survives slot
// removal, so every observation below selects the serializer without first
// dereferencing the object it protects.
static int scope_teardown_finish_if_drained(rt_executor* ex, rt_task* task, waker_key key) {
    rt_shard* owner = rt_waiter_key_shard(ex, key);
    rt_shard_lock(owner);
    rt_scope* scope = rt_scope_resolve_key_locked(ex, key);
    int done = scope == NULL || scope->active_children == 0;
    if (scope != NULL && done) {
        scope_exit_locked(ex, scope);
    } else if (scope == NULL && task->active_scope_key.kind == key.kind &&
               task->active_scope_key.id == key.id) {
        task->active_scope_key = waker_none();
    }
    rt_shard_unlock(owner);
    return done;
}

rt_scope_teardown_status
rt_scope_cancel_teardown(rt_executor* ex, rt_task* task, int cancel_children, waker_key* park_key) {
    if (park_key != NULL) {
        *park_key = waker_none();
    }
    if (ex == NULL || task == NULL || !waker_valid(task->active_scope_key)) {
        if (task != NULL) {
            task->cancel_pending = 0;
        }
        return RT_SCOPE_TEARDOWN_DONE;
    }

    waker_key key = task->active_scope_key;
    if (scope_teardown_finish_if_drained(ex, task, key)) {
        task->cancel_pending = 0;
        return RT_SCOPE_TEARDOWN_DONE;
    }

    task->cancel_pending = 1;
    if (cancel_children) {
        int need_control = !rt_lane_holds_control();
        if (need_control) {
            rt_control_lock(ex);
            rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
        }
        scope_cancel_children_controlled(ex, key);
        if (need_control) {
            rt_control_unlock(ex);
        }
    }

    // A last child may drain after the first observation and before this
    // registration. Publishing the waiter first and verifying under the same
    // serializer closes that window.
    RT_SYNC_POINT(SP_SCOPE_TEARDOWN_BEFORE_REGISTER);
    prepare_park(ex, task, key, 0);
    if (park_key != NULL) {
        *park_key = key;
    }

#ifndef RV2_DEBT_281_NEGATIVE_CONTROL
    if (scope_teardown_finish_if_drained(ex, task, key)) {
        remove_waiter(ex, key, task->id);
        task->park_prepared = 0;
        task->park_key = waker_none();
        pending_key = waker_none();
        task->cancel_pending = 0;
        if (park_key != NULL) {
            *park_key = waker_none();
        }
        return RT_SCOPE_TEARDOWN_DONE;
    }
#endif
    return RT_SCOPE_TEARDOWN_PARKED;
}
