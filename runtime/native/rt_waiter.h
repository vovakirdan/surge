#ifndef SURGE_RUNTIME_NATIVE_RT_WAITER_H
#define SURGE_RUNTIME_NATIVE_RT_WAITER_H

#include "rt_runtime_config.h"
#include <stddef.h>
#include <stdint.h>

// Waiter keys, entries, and stores. Stores are owned per the Epic 7/9
// dependency map: net keys by the fd owner shard, join keys by the task's
// atomic join-owner route, timer/blocking keys by the parked-on task's owner
// shard, scope keys by the control lane, and channel keys by the channel owner
// (shard 0 until the Task 10 migration).

typedef struct rt_executor rt_executor;
typedef struct rt_task rt_task;
typedef struct rt_channel rt_channel;
typedef struct rt_shard rt_shard;

typedef enum {
    WAKER_NONE = 0,
    WAKER_JOIN = 1,
    WAKER_TIMER = 2,
    WAKER_CHAN_SEND = 3,
    WAKER_CHAN_RECV = 4,
    WAKER_NET_ACCEPT = 5,
    WAKER_NET_READ = 6,
    WAKER_NET_WRITE = 7,
    WAKER_SCOPE = 8,
    WAKER_BLOCKING = 9,
    WAKER_REMOTE_SPAWN_REPLY = 10,
} waker_kind;

typedef struct {
    uint8_t kind;
    uint64_t id;
} waker_key;

typedef struct {
    waker_key key;
    uint64_t task_id;
    // Owner shard of task_id at registration time (D3/D5): stable for
    // non-accept keys, refreshed by the control-lane accept transition.
    uint32_t owner_hint;
    // Channel-lane registration generation (candidate/validate): matches the
    // task's park_seq while the park is current. Zero marks wake-only
    // entries (select arms and other add_waiter registrations), which carry
    // no value mailbox and must never receive a resume write.
    uint32_t seq;
} waiter;

typedef struct {
    size_t removed;
    size_t woken;
} rt_waiter_completion;

typedef struct {
    waiter* entries;
    size_t len;
    size_t cap;
    size_t net_len;
} rt_waiter_store;

typedef struct {
    uint64_t total;
    uint64_t join;
    uint64_t timer;
    uint64_t chan_send;
    uint64_t chan_recv;
    uint64_t net;
    uint64_t other;
} rt_waiter_trace_counts;

waker_key waker_none(void);
int waker_valid(waker_key key);
waker_key join_key(uint64_t id);
waker_key timer_key(uint64_t id);
waker_key scope_key(uint64_t id);
waker_key channel_send_key(const rt_channel* ch);
waker_key channel_recv_key(const rt_channel* ch);
waker_key net_accept_key(int fd);
waker_key net_read_key(int fd);
waker_key net_write_key(int fd);
int waker_is_net(waker_key key);
waker_key blocking_key(uint64_t id);
waker_key remote_spawn_reply_key(uint64_t id);
uint32_t rt_channel_owner_shard_id(const rt_channel* ch);

rt_runtime_status rt_waiter_store_ensure_cap(rt_waiter_store* store);
rt_waiter_store* rt_waiter_store_for_key(rt_executor* ex, waker_key key);
rt_shard* rt_waiter_key_shard(rt_executor* ex, waker_key key);
void rt_waiter_migrate_join_waiters(rt_executor* ex,
                                    uint64_t task_id,
                                    uint32_t from_shard_id,
                                    uint32_t to_shard_id);
void rt_waiter_publish_join_owner_and_migrate(rt_executor* ex,
                                              rt_task* task,
                                              uint32_t from_shard_id,
                                              uint32_t to_shard_id);
rt_runtime_status
rt_waiter_add_join_waiter(rt_executor* ex, waker_key key, uint64_t task_id, uint32_t owner_hint);
void rt_waiter_remove_join_waiter_generation(rt_executor* ex,
                                             waker_key key,
                                             uint64_t task_id,
                                             uint32_t seq);
int rt_waiter_pop_join_waiter(rt_executor* ex, waker_key key, uint64_t* out_id);
size_t rt_waiter_collect_join_waiters(rt_executor* ex,
                                      waker_key key,
                                      uint64_t** batch,
                                      size_t* batch_cap,
                                      const uint64_t* inline_batch);
rt_waiter_completion rt_executor_wake_net_waiters_for_key(rt_executor* ex, waker_key key);
rt_waiter_completion rt_executor_wake_net_waiters_for_key_on_owner(rt_executor* ex,
                                                                   waker_key key,
                                                                   uint32_t owner_shard_id);
uint32_t rt_net_owner_shard_for_key(rt_executor* ex, waker_key key, uint32_t fallback_shard_id);
uint32_t rt_net_owner_shard_probe_locked(rt_executor* ex, int fd, uint32_t hint_shard_id);
void ensure_waiter_cap(rt_executor* ex);
void remove_waiter(rt_executor* ex, waker_key key, uint64_t task_id);
void remove_waiter_generation(rt_executor* ex, waker_key key, uint64_t task_id, uint32_t seq);
void add_waiter(rt_executor* ex, waker_key key, uint64_t task_id);
void clear_wait_keys(rt_executor* ex, rt_task* task);
void add_wait_key(rt_executor* ex, rt_task* task, waker_key key);
void prepare_park(rt_executor* ex, rt_task* task, waker_key key, int already_added);
int pop_waiter(rt_executor* ex, waker_key key, uint64_t* out_id);
void rt_trace_collect_waiter_counts(const rt_executor* ex, rt_waiter_trace_counts* out);

#endif
