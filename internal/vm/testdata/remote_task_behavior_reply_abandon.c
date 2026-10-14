#include "remote_task_behavior.h"

// Dispatch the real reply before cancelling its caller. Constructing a
// terminal pending directly misses the reply's registry removal.
int rtb_mode_reply_caller_abandon(int reply_first) {
    rt_executor* ex = ensure_exec();
    const rt_remote_task_op ops[] = {
        RT_REMOTE_TASK_OP_AWAIT,
        RT_REMOTE_TASK_OP_CANCEL,
        RT_REMOTE_TASK_OP_EXECUTE,
        RT_REMOTE_TASK_OP_EXECUTE_ANCHORED,
        RT_REMOTE_TASK_OP_CHANNEL_SELECT,
        RT_REMOTE_TASK_OP_CHANNEL_CREATE,
        RT_REMOTE_TASK_OP_CHANNEL_SHARE,
    };
    rt_task caller = {0};
    caller.id = 999999;
    for (size_t i = 0; i < sizeof(ops) / sizeof(ops[0]); i++) {
        rt_far_task_handle handle = {
            .task_id = reply_first ? 888888 : 0,
            .generation = 1,
            .owner_shard_id = 0,
            .kind = RT_FAR_HANDLE_KIND_TASK,
        };
        rt_remote_task_pending* pending = rt_remote_task_pending_new(ex, &handle, 0, ops[i], 1);
        if (pending == NULL)
            return rtb_fail("reply-abandon: pending allocation failed");
        pending->caller_task_id = caller.id;
        pending->cancel_routed = 1;
        pending->reply_status = RT_REMOTE_TASK_STATUS_OK;
        rt_remote_task_pending_add_ref(pending); // in-flight reply
        rt_remote_task_pending_add_ref(pending); // test observer
        rt_transport_msg reply = {
            .kind = RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY,
            .source_shard_id = 0,
            .target_shard_id = 0,
            .route_id = pending->request_id,
            .generation = handle.generation,
            .payload = pending,
        };
        if (reply_first != 1) {
            rt_immediate_on_release_owned(ex, &caller);
            rt_remote_task_release_owned(ex, &caller);
        }
        if (reply_first == 2) {
            rt_remote_task_pending_finish(
                ex, pending, RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN, 2, NULL);
            rt_remote_task_release_msg_payload(&reply);
        } else if (!rt_remote_task_dispatch_message(ex, &reply)) {
            return rtb_fail("reply-abandon: reply was not dispatched");
        }
        if (reply_first == 1 && (!pending->listed || atomic_load(&pending->refs) != 2)) {
            return rtb_fail("reply-abandon: landed reply hid the caller-owned pending");
        }
        rt_immediate_on_release_owned(ex, &caller);
        rt_remote_task_release_owned(ex, &caller);
        if (pending->listed || atomic_load(&pending->refs) != 1) {
            return rtb_fail("reply-abandon: caller teardown did not unlink and release");
        }
        rt_remote_task_pending_release(pending);
    }
    rt_runtime_shutdown_for_exit();
    return 0;
}
