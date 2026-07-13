#include "rt_far_channel.h"
#include "rt_remote_task_internal.h"

void rt_remote_task_release_msg_payload(const rt_transport_msg* msg) {
    if (msg == NULL || msg->payload == NULL) {
        return;
    }
    switch (msg->kind) {
        case RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST:
        case RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION:
        case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST:
        case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK:
        case RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST:
        case RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST:
        case RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY:
            rt_remote_task_pending_release(msg->payload);
            break;
        default:
            break;
    }
}

// Answers a registered owner-side pending once the owner task is DONE and
// drops the owner-registration task reference taken at registration time.
void rt_remote_task_reply_owner_done(rt_executor* ex,
                                     rt_task* task,
                                     rt_remote_task_pending* pending) {
    if (pending->op == RT_REMOTE_TASK_OP_CANCEL) {
        rt_remote_task_reply_or_finish(ex,
                                       pending,
                                       RT_REMOTE_TASK_STATUS_OK,
                                       rt_remote_task_result_kind(task),
                                       0,
                                       RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK);
    } else if (pending->op == RT_REMOTE_TASK_OP_AWAIT) {
        rt_remote_task_reply_or_finish(ex,
                                       pending,
                                       RT_REMOTE_TASK_STATUS_OK,
                                       rt_remote_task_result_kind(task),
                                       task->result_bits,
                                       RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION);
    } else if (pending->op == RT_REMOTE_TASK_OP_EXECUTE ||
               pending->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED) {
        if (pending->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED) {
            rt_far_channel_unpin(ex, &pending->anchor);
        }
        rt_remote_task_reply_or_finish(ex,
                                       pending,
                                       RT_REMOTE_TASK_STATUS_OK,
                                       rt_remote_task_result_kind(task),
                                       task->result_bits,
                                       RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY);
    }
    task_release_lane_aware(ex, task);
}

void rt_remote_task_on_owner_done(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    rt_remote_task_pending* pending = rt_remote_task_pending_take_owner(task);
    if (pending == NULL) {
        return;
    }
    rt_remote_task_reply_owner_done(ex, task, pending);
}
