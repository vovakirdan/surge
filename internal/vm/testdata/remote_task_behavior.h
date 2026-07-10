#ifndef SURGE_TEST_REMOTE_TASK_BEHAVIOR_H
#define SURGE_TEST_REMOTE_TASK_BEHAVIOR_H

#include "rt_async_internal.h"
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

#endif
