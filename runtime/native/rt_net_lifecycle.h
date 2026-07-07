#ifndef SURGE_RUNTIME_NATIVE_RT_NET_LIFECYCLE_H
#define SURGE_RUNTIME_NATIVE_RT_NET_LIFECYCLE_H

#include "rt_net_handles.h"

#include <stdbool.h>
#include <stdint.h>

typedef struct rt_executor rt_executor;

typedef enum {
    RT_NET_LIFECYCLE_OK = 0,
    RT_NET_LIFECYCLE_INVALID,
    RT_NET_LIFECYCLE_REGISTRY_ERROR,
    RT_NET_LIFECYCLE_CLOSE_ERROR,
    RT_NET_LIFECYCLE_PARTIAL_CLOSE,
} rt_net_lifecycle_status;

uint32_t rt_net_owner_shard_or_compat(rt_executor* ex, uint32_t requested);
uint32_t rt_net_current_owner_shard(rt_executor* ex);
// generation is the canonical handle's fd-registry row generation: it must
// match the row under the owner shard lock or the close is rejected as stale,
// so exactly one closer wins and a reused fd is never close(2)'d twice.
// generation 0 preserves the legacy behavior for never-registered members.
rt_net_lifecycle_status rt_net_close_fd_on_owner(rt_executor* ex,
                                                 uint32_t owner_shard_id,
                                                 int* fd_slot,
                                                 bool* closed_slot,
                                                 uint64_t generation,
                                                 int* out_errno);
rt_net_lifecycle_status
rt_net_close_listener_members(rt_executor* ex, NetListener* listener, int* out_errno);

#endif
