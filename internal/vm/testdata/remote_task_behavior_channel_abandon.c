#include "remote_task_behavior.h"

// The test holds automatic transport draining, then drives the real owner
// dispatcher and reply dispatcher on either side of caller cancellation.
int rtb_mode_channel_reply_abandon(void) {
    rt_executor* ex = ensure_exec();
    rt_task caller = {0};
    caller.id = 999999;
    for (int share = 0; share < 2; share++) {
        for (int phase = 0; phase < 3; phase++) {
            rt_far_task_handle original = {0};
            if (share) {
                void* channel = rt_channel_new(
                    0, rt_channel_element_ops_for(RTB_DROP_MARK_ID), RTB_DROP_MARK_ID);
                if (rt_far_channel_mint(ex, channel, 0, &original) != RT_REMOTE_TASK_STATUS_OK) {
                    return rtb_fail("channel-abandon: source mint failed");
                }
            }
            rt_far_task_handle route = {.owner_shard_id = 0, .kind = RT_FAR_HANDLE_KIND_CHANNEL};
            rt_remote_task_pending* pending = rt_remote_task_pending_new(
                ex,
                &route,
                0,
                share ? RT_REMOTE_TASK_OP_CHANNEL_SHARE : RT_REMOTE_TASK_OP_CHANNEL_CREATE,
                1);
            if (pending == NULL)
                return rtb_fail("channel-abandon: pending allocation failed");
            pending->handle.generation = pending->request_id;
            pending->caller_task_id = caller.id;
            pending->payload_type_id = RTB_DROP_MARK_ID;
            pending->anchor = original;
            rt_remote_task_pending_add_ref(pending); // request -> reply
            rt_remote_task_pending_add_ref(pending); // observer
            rt_transport_msg request = {
                .kind = share ? RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST
                              : RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST,
                .source_shard_id = 0,
                .target_shard_id = 0,
                .route_id = pending->request_id,
                .generation = pending->handle.generation,
                .payload = pending,
            };
            if (phase == 0)
                rt_remote_task_release_owned(ex, &caller);
            (void)rt_remote_task_dispatch_message(ex, &request);
            if (phase == 1)
                rt_remote_task_release_owned(ex, &caller);
            {
                rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
                rt_transport_msg reply = {0};
                rt_shard_lock(shard);
                rt_transport_status drained = rt_transport_try_drain_one(shard, &reply);
                rt_shard_unlock(shard);
                if (drained != RT_TRANSPORT_STATUS_OK || reply.payload != pending) {
                    return rtb_fail("channel-abandon: minted reply missing");
                }
                (void)rt_remote_task_dispatch_message(ex, &reply);
            }
            if (phase == 2)
                rt_remote_task_release_owned(ex, &caller);
            if (pending->listed || atomic_load(&pending->refs) != 1) {
                return rtb_fail("channel-abandon: caller or transport retained pending");
            }
            rt_far_task_handle abandoned = pending->handle;
            rt_remote_task_pending_release(pending);
            if (rt_far_channel_debug_live_count(ex) != (size_t)share ||
                rt_far_channel_resolve(ex, &abandoned) != NULL ||
                (share && rt_far_channel_active_lease_count(ex, &original) != 1)) {
                return rtb_fail("channel-abandon: minted reply lease survived before shutdown");
            }
            if (share)
                (void)rt_far_channel_release(ex, &original);
            if (rt_far_channel_debug_live_count(ex) != 0 ||
                rt_far_channel_debug_lease_count(ex) != 0) {
                return rtb_fail("channel-abandon: released entry retained lease storage");
            }
        }
    }
    rt_runtime_shutdown_for_exit();
    return 0;
}
