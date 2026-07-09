#ifndef SURGE_RUNTIME_NATIVE_RT_TRANSPORT_H
#define SURGE_RUNTIME_NATIVE_RT_TRANSPORT_H

#include <stddef.h>
#include <stdint.h>

typedef struct rt_executor rt_executor;
typedef struct rt_shard rt_shard;

typedef enum rt_transport_status {
    RT_TRANSPORT_STATUS_OK = 0,
    RT_TRANSPORT_STATUS_UNAVAILABLE = 1,
    RT_TRANSPORT_STATUS_PENDING_SPINE = 2,
    RT_TRANSPORT_STATUS_INVALID_ARGUMENT = 3,
} rt_transport_status;

typedef enum rt_transport_msg_kind {
    RT_TRANSPORT_MSG_NONE = 0,
    RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST = 1,
    RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK = 2,
    RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION = 3,
    RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST = 4,
    RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK = 5,
    RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST = 6,
    RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY = 7,
    RT_TRANSPORT_MSG_CREDIT_CONTROL = 8,
    RT_TRANSPORT_MSG_SHUTDOWN_WAKE = 9,
} rt_transport_msg_kind;

typedef enum rt_transport_park_state {
    RT_TRANSPORT_SHARD_RUNNING = 0,
    RT_TRANSPORT_SHARD_PARKED = 1,
    RT_TRANSPORT_SHARD_SHUTDOWN = 2,
} rt_transport_park_state;

typedef struct rt_transport_msg {
    rt_transport_msg_kind kind;
    uint32_t source_shard_id;
    uint32_t target_shard_id;
    uint64_t route_id;
    uint64_t generation;
    const void* payload;
    size_t payload_len;
} rt_transport_msg;

struct rt_transport_debug_snapshot {
    size_t inbound_len;
    rt_transport_park_state park_state;
    uint8_t pending_spine;
    uint64_t enqueue_count;
    uint64_t drain_count;
    uint64_t transport_wake_writes;
    uint64_t transport_wake_elisions;
    uint64_t shutdown_wakes;
    uint64_t parked_with_work_violations;
};

rt_transport_status rt_transport_enqueue(rt_shard* shard, const rt_transport_msg* msg);
rt_transport_status rt_transport_try_drain_one(rt_shard* shard, rt_transport_msg* out);
rt_transport_status rt_transport_prepare_shard_park(rt_shard* shard);
void rt_transport_mark_shard_running(rt_shard* shard);
uint64_t rt_transport_shutdown_wake_all(rt_executor* ex);
struct rt_transport_debug_snapshot rt_transport_debug_snapshot(const rt_shard* shard);

#endif
