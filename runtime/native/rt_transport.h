#ifndef SURGE_RUNTIME_NATIVE_RT_TRANSPORT_H
#define SURGE_RUNTIME_NATIVE_RT_TRANSPORT_H

#include <stdatomic.h>
#include <stddef.h>
#include <stdint.h>

#include "rt_runtime_config.h"

typedef struct rt_executor rt_executor;
typedef struct rt_shard rt_shard;

typedef enum rt_transport_status {
    RT_TRANSPORT_STATUS_OK = 0,
    RT_TRANSPORT_STATUS_UNAVAILABLE = 1,
    RT_TRANSPORT_STATUS_QUEUE_FULL = 2,
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
    RT_TRANSPORT_MSG_SHUTDOWN_WAKE = 8,
    RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST = 9,
    RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST = 10,
    RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST = 11,
    RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY = 12,
    RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST = 13,
    RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY = 14,
    RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST = 15,
    RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY = 16,
} rt_transport_msg_kind;

// Which budget an envelope spends. DATA is everything a caller asks for --
// every request, and every completion or reply that hands back a value the
// target must consume. CONTROL is the release-and-teardown traffic that lets
// a party finish, free a pin, or shut down; it spends a reserve of its own so
// that a data backlog can never starve the messages that clear the backlog.
typedef enum rt_transport_msg_class {
    RT_TRANSPORT_MSG_CLASS_INVALID = 0,
    RT_TRANSPORT_MSG_CLASS_DATA = 1,
    RT_TRANSPORT_MSG_CLASS_CONTROL = 2,
} rt_transport_msg_class;

typedef enum rt_transport_park_state {
    RT_TRANSPORT_SHARD_RUNNING = 0,
    RT_TRANSPORT_SHARD_PARKED = 1,
    RT_TRANSPORT_SHARD_SHUTDOWN = 2,
} rt_transport_park_state;

// `payload` points into a refcount graph the transport neither copies nor
// owns, so the envelope carries no length: the bytes behind the pointer are
// shared, have no per-message cost, and are not the transport's to measure.
// A length belongs here only once a message carries a buffer the transport
// itself owns -- a serialized or explicitly handed-over one -- and that
// buffer will bring its own length with it.
typedef struct rt_transport_msg {
    rt_transport_msg_kind kind;
    uint32_t source_shard_id;
    uint32_t target_shard_id;
    uint64_t route_id;
    uint64_t generation;
    void* payload;
} rt_transport_msg;

// The bound is SLOTS, not bytes. An envelope is a fixed record over a pointer
// the transport does not own, so its whole cost to the target is one slot:
// one credit per envelope, and the free slots ARE the credits -- there is no
// second counter beside the occupancy to drift away from it.
//
// The two budgets are independent. A data envelope is never admitted to the
// control reserve, so no volume of data traffic can spend it; that is what
// keeps a reply, a cancel, a release, or a shutdown wake admissible while the
// data lane sits at its bound.
#define RT_TRANSPORT_DATA_SLOT_CREDITS 64U
#define RT_TRANSPORT_CONTROL_SLOT_RESERVE 16U
#define RT_TRANSPORT_DRAIN_TURN_LIMIT 16U

typedef struct rt_transport_wake {
    int read_fd;
    int write_fd;
    uint8_t initialized;
    uint64_t drain_count;
    uint64_t drain_bytes;
    uint64_t write_failures;
    // Counts every drain that reached the pipe, including the ones that read
    // nothing. drain_count only counts the ones that found bytes, so it cannot
    // see a read(2) issued under the shard lock for an already-empty queue --
    // which is the cost this counter exists to bound.
    uint64_t drain_calls;
} rt_transport_wake;

typedef struct rt_transport_state {
    rt_transport_msg data[RT_TRANSPORT_DATA_SLOT_CREDITS];
    rt_transport_msg control[RT_TRANSPORT_CONTROL_SLOT_RESERVE];
    size_t data_head;
    size_t data_len;
    size_t control_head;
    size_t control_len;
    _Atomic uint8_t park_state;
    rt_transport_wake wake;
    uint64_t enqueue_count;
    uint64_t control_enqueue_count;
    uint64_t data_enqueue_count;
    uint64_t drain_count;
    uint64_t control_drain_count;
    uint64_t data_drain_count;
    uint64_t transport_spawn_requests;
    uint64_t transport_spawn_acks;
    uint64_t remote_task_completion_replies;
    uint64_t remote_task_cancel_replies;
    uint64_t remote_task_stale_drops;
    uint64_t remote_task_release_requests;
    uint64_t immediate_on_execute_requests;
    uint64_t immediate_on_replies;
    uint64_t far_channel_create_requests;
    uint64_t far_channel_create_replies;
    uint64_t far_channel_share_requests;
    uint64_t far_channel_share_replies;
    uint64_t far_channel_select_requests;
    uint64_t far_channel_select_replies;
    uint64_t data_credit_stalls;
    uint64_t control_reserve_stalls;
    uint64_t unsupported_fallback_attempts;
    uint64_t transport_wake_writes;
    uint64_t transport_wake_elisions;
    uint64_t shutdown_wakes;
    uint64_t parked_with_work_violations;
} rt_transport_state;

struct rt_transport_debug_snapshot {
    size_t inbound_len;
    size_t control_len;
    size_t data_len;
    rt_transport_park_state park_state;
    uint64_t enqueue_count;
    uint64_t control_enqueue_count;
    uint64_t data_enqueue_count;
    uint64_t drain_count;
    uint64_t control_drain_count;
    uint64_t data_drain_count;
    uint64_t transport_spawn_requests;
    uint64_t transport_spawn_acks;
    uint64_t remote_task_completion_replies;
    uint64_t remote_task_cancel_replies;
    uint64_t remote_task_stale_drops;
    uint64_t remote_task_release_requests;
    uint64_t immediate_on_execute_requests;
    uint64_t immediate_on_replies;
    uint64_t far_channel_create_requests;
    uint64_t far_channel_create_replies;
    uint64_t far_channel_share_requests;
    uint64_t far_channel_share_replies;
    uint64_t far_channel_select_requests;
    uint64_t far_channel_select_replies;
    uint64_t data_credit_stalls;
    uint64_t control_reserve_stalls;
    uint64_t unsupported_fallback_attempts;
    uint64_t transport_wake_writes;
    uint64_t transport_wake_elisions;
    uint64_t shutdown_wakes;
    uint64_t parked_with_work_violations;
    uint64_t wake_drain_count;
    uint64_t wake_drain_bytes;
    uint64_t wake_write_failures;
    uint64_t wake_drain_calls;
};

rt_runtime_status rt_transport_state_init(rt_transport_state* state);
void rt_transport_state_destroy(rt_transport_state* state);
rt_transport_status rt_transport_enqueue(rt_shard* shard, const rt_transport_msg* msg);
rt_transport_status rt_transport_try_drain_one(rt_shard* shard, rt_transport_msg* out);
rt_transport_status rt_transport_prepare_shard_park(rt_shard* shard);
void rt_transport_mark_shard_running(rt_shard* shard);
uint64_t rt_transport_shutdown_wake_all(rt_executor* ex);
struct rt_transport_debug_snapshot rt_transport_debug_snapshot(rt_shard* shard);
void rt_transport_record_remote_task_stale(rt_shard* shard);
size_t rt_transport_drain_inbound_locked(rt_shard* shard, size_t limit);
size_t rt_transport_inbound_len_locked(const rt_shard* shard);
int rt_transport_reply_wait_before_task_suspend(void);

#endif
