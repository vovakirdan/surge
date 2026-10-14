#include "rt_far_channel.h"
#include "rt_remote_task_internal.h"
#include "rt_resident_bytes.h"

static _Thread_local rt_remote_task_pending* reclaim_head;
static _Thread_local int reclaim_draining;

static void destroy_pending(rt_remote_task_pending* pending) {
#ifndef RV2_REPLY_CHANNEL_LEASE_NEGATIVE_CONTROL
    if (pending->channel_handle_owned) {
        (void)rt_far_channel_release(pending->executor, &pending->handle);
    }
#endif
    if (rt_remote_admission_take_reservation(&pending->admission)) {
        rt_remote_admission_release_reservation_belt(pending->executor, &pending->admission);
    }
    if (pending->state_owned != 0 && pending->state_type_id != 0 && pending->body_state != NULL) {
        const rt_value_ops* operations = rt_channel_element_ops_for(pending->state_type_id);
        rt_resident_payload_release(operations);
        rt_value_release_owned_block(operations, pending->body_state);
    }
    // An untaken reply still pins its producer's typed result. The producer
    // destroys the value when this final pin is returned.
    if (pending->result_source.task_id != 0) {
        rt_remote_task_release_result_source(pending->executor, &pending->result_source);
    }
    rt_remote_task_select_arms_free(pending->select_arms, pending->select_count);
    rt_resident_bytes_release(RT_RESIDENT_RECORD, sizeof(*pending));
    rt_free((uint8_t*)pending, sizeof(*pending), _Alignof(rt_remote_task_pending));
}

void rt_remote_task_pending_reclaim(rt_remote_task_pending* pending) {
    if (pending->listed) {
        panic_msg("remote request: final reference released before registry retirement");
        return;
    }
#ifndef RV2_REPLY_RECLAIM_LOCK_NEGATIVE_CONTROL
    if (rt_lane_holds_control() || rt_lane_holds_any_shard()) {
        // The final reference has gone and the registry already unlinked it.
        // Reuse its list link: deferring arbitrary batches allocates nothing.
        pending->next = reclaim_head;
        reclaim_head = pending;
        return;
    }
#endif
    destroy_pending(pending);
}

void rt_remote_task_reclaim_drain(void) {
    // Destruction may unlock another runtime lane. Its nested drain must not
    // turn a batch of pending records into a recursive destructor chain.
    if (reclaim_draining) {
        return;
    }
    reclaim_draining = 1;
    while (reclaim_head != NULL) {
        rt_remote_task_pending* pending = reclaim_head;
        reclaim_head = pending->next;
        pending->next = NULL;
        destroy_pending(pending);
    }
    reclaim_draining = 0;
}
