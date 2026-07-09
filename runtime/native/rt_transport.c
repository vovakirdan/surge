#include "rt_transport.h"

#include "rt_sync_point.h"

static struct rt_transport_debug_snapshot rt_transport_pending_snapshot(void) {
    struct rt_transport_debug_snapshot snapshot = {
        .inbound_len = 0,
        .park_state = RT_TRANSPORT_SHARD_RUNNING,
        .pending_spine = 1,
        .enqueue_count = 0,
        .drain_count = 0,
        .transport_wake_writes = 0,
        .transport_wake_elisions = 0,
        .shutdown_wakes = 0,
        .parked_with_work_violations = 0,
    };
    return snapshot;
}

rt_transport_status rt_transport_enqueue(rt_shard* shard, const rt_transport_msg* msg) {
    (void)shard;
    (void)msg;
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD);
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE);
    return RT_TRANSPORT_STATUS_PENDING_SPINE;
}

rt_transport_status rt_transport_try_drain_one(rt_shard* shard, rt_transport_msg* out) {
    (void)shard;
    (void)out;
    return RT_TRANSPORT_STATUS_UNAVAILABLE;
}

rt_transport_status rt_transport_prepare_shard_park(rt_shard* shard) {
    (void)shard;
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK);
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK);
    return RT_TRANSPORT_STATUS_PENDING_SPINE;
}

void rt_transport_mark_shard_running(rt_shard* shard) {
    (void)shard;
}

uint64_t rt_transport_shutdown_wake_all(rt_executor* ex) {
    (void)ex;
    RT_SYNC_POINT(SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE);
    return 0;
}

struct rt_transport_debug_snapshot rt_transport_debug_snapshot(const rt_shard* shard) {
    (void)shard;
    RT_SYNC_POINT(SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND);
    return rt_transport_pending_snapshot();
}
