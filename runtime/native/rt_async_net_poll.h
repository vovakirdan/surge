#ifndef SURGE_RT_ASYNC_NET_POLL_H
#define SURGE_RT_ASYNC_NET_POLL_H

#include <stdint.h>

// Waking a shard that is parked in the network poller.
//
// One question asked at several granularities -- is anyone waiting on this
// shard, begin a poll, poll the ones this shard owns, wake every shard -- and
// reading them together is how a caller picks the right one instead of the
// first one that compiles.
//
// Two siblings stay in rt_async_internal.h rather than move here: they take
// rt_net_poll_wake, rt_task and waker_key, which are typedefs of anonymous
// structs and therefore cannot be forward declared. Splitting them off would
// mean naming those structs, which is a change to the runtime's types made for
// a file-size rule -- the wrong reason.

typedef struct rt_executor rt_executor;
typedef struct rt_shard rt_shard;

int rt_net_poll_wake_init(rt_shard* shard);
void rt_net_poll_wake_drain(rt_shard* shard);
int rt_net_has_waiters_on_shard(const rt_executor* ex, uint32_t owner_shard_id);
int rt_net_begin_poll_on_shard(rt_executor* ex, uint32_t owner_shard_id);
int rt_net_poll_waiters_owned_on_shard(rt_executor* ex, uint32_t owner_shard_id, int timeout_ms);
int poll_net_waiters_on_shard(rt_executor* ex, uint32_t owner_shard_id, int timeout_ms);
uint64_t rt_net_wake_poll_on_shard(rt_executor* ex, uint32_t owner_shard_id);
uint64_t rt_net_wake_poll_all_shards(rt_executor* ex);

#endif // SURGE_RT_ASYNC_NET_POLL_H
