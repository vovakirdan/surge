#include "remote_task_behavior.h"
#include "rt_resident_bytes.h"

int rtb_mode_pending_listed_final_refused(void) {
    rt_executor* ex = ensure_exec();
    rt_far_task_handle route = {.kind = RT_FAR_HANDLE_KIND_TASK};
    rt_remote_task_pending* pending =
        rt_remote_task_pending_new(ex, &route, 0, RT_REMOTE_TASK_OP_EXECUTE, 1);
    if (pending == NULL)
        return rtb_fail("pending-reclaim: allocation failed");
    // Deliberately violate registry ownership, as the old shutdown edge did.
    rt_remote_task_pending_release(pending);
    return rtb_fail("pending-reclaim: linked final release was accepted");
}

int rtb_mode_pending_reclaim_detached(void) {
    rt_executor* ex = ensure_exec();
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
    for (int control = 0; control < 2; control++) {
        rt_remote_task_pending* pending[16];
        atomic_store(&rtb_drop_calls, 0);
        for (size_t i = 0; i < 16; i++) {
            rt_far_task_handle route = {.kind = RT_FAR_HANDLE_KIND_TASK};
            pending[i] = rt_remote_task_pending_new(ex, &route, 0, RT_REMOTE_TASK_OP_EXECUTE, 1);
            if (pending[i] == NULL)
                return rtb_fail("pending-reclaim: allocation failed");
            pending[i]->body_state = rtb_shipped_state_new(RTB_DROP_MARK_ID);
            pending[i]->state_type_id = RTB_DROP_MARK_ID;
            pending[i]->state_owned = 1;
            rt_resident_payload_acquire(rt_channel_element_ops_for(RTB_DROP_MARK_ID));
        }
        if (control)
            rt_control_lock(ex);
        else
            rt_shard_lock(shard);
        for (size_t i = 0; i < 16; i++)
            rt_remote_task_pending_consume(pending[i]);
        if (atomic_load(&rtb_drop_calls) != 0) {
            return rtb_fail("pending-reclaim: generated drop ran under a scheduler lock");
        }
        if (control)
            rt_control_unlock(ex);
        else
            rt_shard_unlock(shard);
        if (atomic_load(&rtb_drop_calls) != 16) {
            return rtb_fail("pending-reclaim: unlock did not reclaim every pending exactly once");
        }
    }
    rt_runtime_shutdown_for_exit();
    return 0;
}
