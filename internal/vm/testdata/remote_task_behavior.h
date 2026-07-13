#ifndef SURGE_TEST_REMOTE_TASK_BEHAVIOR_H
#define SURGE_TEST_REMOTE_TASK_BEHAVIOR_H

#include "rt_async_internal.h"
#include "rt_far_channel.h"
#include "rt_remote_spawn_internal.h"
#include "rt_remote_task_internal.h"
#include "rt_sync_point.h"
#include "rt_transport.h"

#include <stdatomic.h>
#include <stdint.h>

enum {
    POLL_RTB_CHILD = 9101,
    POLL_RTB_PUBLISHER = 9102,
    POLL_RTB_LIFECYCLE = 9103,
    POLL_RTB_EXECUTE = 9104,
    POLL_RTB_CHANNEL_CREATE = 9105,
    POLL_RTB_ANCHORED_BODY = 9106,
    POLL_RTB_ANCHORED_CALLER = 9107,
    POLL_RTB_ANCHORED_RECV_BODY = 9108,
    POLL_RTB_ANCHORED_PINNED_BODY = 9109,
    POLL_RTB_ANCHORED_HELPER_SEND = 9110,
    POLL_RTB_ANCHORED_HELPER_RECV = 9111,
    POLL_RTB_ANCHORED_HELPER_CLOSE = 9112,
    POLL_RTB_ANCHORED_FLOODED_CALLER = 9113,
    POLL_RTB_CHANNEL_SHARE = 9114,
    POLL_RTB_SPINNER = 9115,
    POLL_RTB_SELECT_CALLER = 9116,
    POLL_RTB_SELECT_BODY = 9117,
    POLL_RTB_DROP_EXECUTE = 9118,
    POLL_RTB_DROP_ANCHORED = 9119,
    POLL_RTB_DROP_SELECT = 9120,
    POLL_RTB_DROP_BODY = 9121,
    POLL_RTB_DROP_ANCHORED_RECV = 9122,
    POLL_RTB_DROP_RECV_BODY = 9123,
};

typedef struct rtb_child_state {
    _Atomic uint32_t gate;
    _Atomic uint32_t ran;
    _Atomic uint32_t done;
    _Atomic uint32_t cancelled;
    _Atomic uint32_t owner;
} rtb_child_state;

typedef struct rtb_publish_state {
    rt_remote_spawn_pending* pending;
    rt_far_task_handle* handle;
    void* task_state;
    uint64_t poll_id;
    uint32_t destination;
    uint32_t return_handle;
    _Atomic uint32_t saw_pending;
    rt_remote_spawn_status status;
    uint64_t published_task_id;
} rtb_publish_state;

typedef struct rtb_lifecycle_state {
    rt_remote_task_pending* pending;
    rt_far_task_handle* handle;
    uint32_t cancel;
    uint32_t phase;
    uint32_t fill_control;
    _Atomic(rt_remote_task_pending*) visible_pending;
    rt_remote_task_status status;
    uint8_t result_kind;
    uint64_t result_bits;
} rtb_lifecycle_state;

extern _Atomic uint64_t rtb_drop_calls;
extern _Atomic uint64_t rtb_drop_last_id;
extern _Atomic(void*) rtb_drop_last_state;

int rtb_fail(const char* message);
void rtb_sleep_us(unsigned long micros);
int rtb_wait_u32(_Atomic uint32_t* value, uint32_t expected, uint32_t attempts);
int rtb_wait_task_done(rt_executor* ex, uint64_t task_id, uint32_t attempts);
void rtb_wake(rt_executor* ex, uint64_t task_id);
void rtb_drain(rt_executor* ex, uint32_t shard_id);
rt_far_task_handle* rtb_publish_handle(rtb_child_state* child, uint32_t destination);
rt_far_task_handle* rtb_publish_poll(uint64_t poll_id, void* state, uint32_t destination);
void* rtb_start_lifecycle(rtb_lifecycle_state* state, rt_far_task_handle* handle, int cancel);
int rtb_await(void* task, uint8_t* kind, uint64_t* bits);
int rtb_mode_already_done(void);
int rtb_mode_stale(void);
int rtb_mode_registration_race(rt_sync_point_id point);
int rtb_mode_teardown(void);
int rtb_mode_pre_ack_cancel(void);
int rtb_mode_queue_failure(void);
int rtb_mode_shutdown_waiters(void);

typedef struct rtb_execute_state {
    rt_remote_task_pending* pending;
    uint64_t placement;
    uint64_t body_poll_id;
    void* body_state;
    _Atomic(rt_remote_task_pending*) visible_pending;
    rt_remote_task_status status;
    uint8_t result_kind;
    uint64_t result_bits;
} rtb_execute_state;

int rtb_mode_basic(void);
int rtb_mode_distributed(void);
int rtb_mode_invalid_shard(void);
int rtb_mode_immediate_stale(void);
int rtb_mode_cancel_race(void);
int rtb_mode_shutdown(void);
int rtb_mode_immediate_self_crossing(void);

typedef struct rtb_create_state {
    rt_remote_task_pending* pending;
    uint64_t placement;
    uint64_t capacity;
    rt_far_task_handle handle;
    rt_remote_task_status status;
} rtb_create_state;

void* rtb_start_channel_create(rtb_create_state* state, uint64_t placement, uint64_t capacity);

typedef struct rtb_share_state {
    rt_remote_task_pending* pending;
    rt_far_task_handle source;
    rt_far_task_handle sibling;
    rt_remote_task_status status;
    _Atomic uint32_t spin_gate;
} rtb_share_state;

typedef struct rtb_select_state {
    rt_remote_task_pending* pending;
    rt_far_task_handle anchor_slots[RT_FAR_CHANNEL_SELECT_MAX_ARMS];
    const rt_far_task_handle* anchors[RT_FAR_CHANNEL_SELECT_MAX_ARMS];
    uint8_t kinds[RT_FAR_CHANNEL_SELECT_MAX_ARMS];
    uint64_t bits[RT_FAR_CHANNEL_SELECT_MAX_ARMS];
    uint64_t count;
    _Atomic(rt_remote_task_pending*) visible_pending;
    rt_remote_task_status status;
    uint8_t result_kind;
    uint64_t result_bits;
} rtb_select_state;

void rtb_select_poll_dispatch(uint64_t id);
int rtb_mode_select_ready_first(void);
int rtb_mode_select_park_then_send(void);
int rtb_mode_select_tie_break(void);
int rtb_mode_select_close_before(void);
int rtb_mode_select_park_then_close(void);
int rtb_mode_select_cancel_unbound(void);
int rtb_mode_select_cancel_parked(void);
int rtb_mode_select_cancel_vs_send(void);
int rtb_mode_select_retry_single_body(void);
int rtb_mode_select_stale_wake(void);
int rtb_mode_select_release_while_parked(void);
int rtb_mode_select_sibling_isolation(void);
int rtb_mode_select_caller_teardown(void);
int rtb_mode_select_owner_teardown(void);
int rtb_mode_select_no_deadlock_when_runnable(void);
int rtb_mode_select_self_deadlock(void);

void rtb_drop_poll_dispatch(uint64_t id);
int rtb_mode_drop_invalid_placement(void);
int rtb_mode_drop_stale_anchor(void);
int rtb_mode_drop_queue_full(void);
int rtb_mode_drop_select_mixed_owners(void);
int rtb_mode_drop_handoff_not_dropped(void);
int rtb_mode_drop_zero_id_never_dispatches(void);
int rtb_mode_drop_bound_cancel_no_pending_drop(void);

void rtb_share_poll_dispatch(uint64_t id);
int rtb_mode_share_round_trip(void);
int rtb_mode_share_release_independence(void);
int rtb_mode_share_from_released_lease(void);
int rtb_mode_share_pin_outlives_leases(void);
int rtb_mode_share_teardown(void);
int rtb_mode_share_deadlock_two_holders(void);
int rtb_mode_share_no_deadlock_when_runnable(void);
int rtb_mode_share_deadlock_after_peer_release(void);
typedef struct rtb_anchored_state {
    rt_remote_task_pending* pending;
    rt_far_task_handle anchor;
    void* channel;
    uint64_t value;
    uint64_t body_poll_id;
    _Atomic uint32_t body_ran;
    _Atomic uint32_t proceed;
    rt_remote_task_status status;
    uint8_t result_kind;
    uint64_t result_bits;
} rtb_anchored_state;

void rtb_anchored_poll_dispatch(uint64_t id);
int rtb_mode_anchored_send_round_trip(void);
int rtb_mode_anchored_stale_anchor(void);
int rtb_mode_anchored_full_channel(void);
int rtb_mode_anchored_close_vs_recv(void);
int rtb_mode_anchored_cancel_parked_body(void);
int rtb_mode_anchored_owner_teardown(void);
int rtb_mode_anchored_self_deadlock(void);
int rtb_mode_anchored_pin_vs_release(void);
int rtb_mode_anchored_helper_protocol(void);
int rtb_mint_channel(rtb_create_state* create, uint64_t placement, uint64_t capacity);
void rtb_anchored_audit_poll_dispatch(uint64_t id);
int rtb_mode_anchored_queue_full(void);
int rtb_mode_anchored_leak_audit(void);
int rtb_mode_anchored_cross_producer_order(void);

int rtb_mode_channel_create(void);
int rtb_mode_channel_kind_aliasing(void);
int rtb_mode_channel_shutdown_release(void);
int rtb_mode_channel_create_self(void);

#endif
