#include "rt_async_internal.h"

static struct rt_transport_debug_snapshot snapshot_locked(const rt_shard* shard) {
    struct rt_transport_debug_snapshot snapshot = {0};
    if (shard == NULL) {
        return snapshot;
    }
    const rt_transport_state* state = &shard->transport;
    snapshot.control_len = state->control_len;
    snapshot.data_len = state->data_len;
    snapshot.inbound_len = snapshot.control_len + snapshot.data_len;
    snapshot.park_state =
        (rt_transport_park_state)atomic_load_explicit(&state->park_state, memory_order_seq_cst);
    snapshot.enqueue_count = state->enqueue_count;
    snapshot.control_enqueue_count = state->control_enqueue_count;
    snapshot.data_enqueue_count = state->data_enqueue_count;
    snapshot.drain_count = state->drain_count;
    snapshot.control_drain_count = state->control_drain_count;
    snapshot.data_drain_count = state->data_drain_count;
    snapshot.transport_spawn_requests = state->transport_spawn_requests;
    snapshot.transport_spawn_acks = state->transport_spawn_acks;
    snapshot.remote_task_completion_replies = state->remote_task_completion_replies;
    snapshot.remote_task_cancel_replies = state->remote_task_cancel_replies;
    snapshot.remote_task_stale_drops = state->remote_task_stale_drops;
    snapshot.remote_task_release_requests = state->remote_task_release_requests;
    snapshot.immediate_on_execute_requests = state->immediate_on_execute_requests;
    snapshot.immediate_on_replies = state->immediate_on_replies;
    snapshot.far_channel_create_requests = state->far_channel_create_requests;
    snapshot.far_channel_create_replies = state->far_channel_create_replies;
    snapshot.credit_stalls = state->credit_stalls;
    snapshot.unsupported_fallback_attempts = state->unsupported_fallback_attempts;
    snapshot.transport_wake_writes = state->transport_wake_writes;
    snapshot.transport_wake_elisions = state->transport_wake_elisions;
    snapshot.shutdown_wakes = state->shutdown_wakes;
    snapshot.parked_with_work_violations = state->parked_with_work_violations;
    snapshot.wake_drain_count = state->wake.drain_count;
    snapshot.wake_drain_bytes = state->wake.drain_bytes;
    snapshot.wake_write_failures = state->wake.write_failures;
    return snapshot;
}

struct rt_transport_debug_snapshot rt_transport_debug_snapshot(rt_shard* shard) {
    if (shard == NULL) {
        struct rt_transport_debug_snapshot snapshot = {0};
        return snapshot;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        return snapshot_locked(shard);
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: debug snapshot while holding another shard lock");
        struct rt_transport_debug_snapshot snapshot = {0};
        return snapshot;
    }
    rt_shard_lock(shard);
    struct rt_transport_debug_snapshot snapshot = snapshot_locked(shard);
    rt_shard_unlock(shard);
    return snapshot;
}

void rt_transport_record_remote_task_stale(rt_shard* shard) {
    if (shard == NULL) {
        return;
    }
    int need_lock = !rt_lane_holds_shard(shard->shard_id);
    if (need_lock) {
        rt_shard_lock(shard);
    }
    shard->transport.remote_task_stale_drops++;
    if (need_lock) {
        rt_shard_unlock(shard);
    }
}
