#include "rt_remote_task_internal.h"

// Caller teardown: a caller that unwinds (cancel/failfast) while an immediate
// execute or a remote select is in flight leaves the pending's caller-owned
// reference behind. Bound requests get a routed cancel so the destination
// body is not orphaned; unbound requests (still queued, or still parked on a
// slot key and never sent) are resolved so a late dispatch refuses to create
// a body (and, for select, never pins the arms). The reply edge still
// resolves exactly once.
void rt_immediate_on_release_owned(rt_executor* ex, const rt_task* caller) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL || caller == NULL) {
        return;
    }
    for (;;) {
        rt_remote_task_pending* pending = NULL;
        int bound = 0;
        pthread_mutex_lock(&state->lock);
        for (rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
            if ((it->op == RT_REMOTE_TASK_OP_EXECUTE ||
                 it->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED ||
                 it->op == RT_REMOTE_TASK_OP_CHANNEL_SELECT) &&
                it->caller_task_id == caller->id) {
                pending = it;
                bound = it->handle.task_id != 0;
                it->caller_task_id = 0;
                break;
            }
        }
        pthread_mutex_unlock(&state->lock);
        if (pending == NULL) {
            return;
        }
        if (bound) {
            // Keep the pending listed: the destination owner registration
            // still routes the orphaned reply through it exactly once.
            rt_immediate_on_cancel_inflight(ex, pending);
            rt_remote_task_pending_release(pending);
        } else {
            // A request still parked on a slot key never left: its
            // registration and reservation go with the caller, and so does
            // the message reference nothing will ever enqueue.
            if (rt_remote_admission_abandon(ex, caller->id, &pending->admission)) {
                rt_remote_task_pending_release(pending);
            }
            rt_remote_task_pending_finish(ex, pending, RT_REMOTE_TASK_STATUS_REFUSED, 2, NULL);
            rt_remote_task_pending_consume(pending);
        }
    }
}
