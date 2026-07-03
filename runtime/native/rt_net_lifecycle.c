#include "rt_net_lifecycle.h"
#include "rt_async_internal.h"
#include "rt_net_trace.h"

#include <errno.h>
#include <unistd.h>

uint32_t rt_net_owner_shard_or_compat(rt_executor* ex, uint32_t requested) {
    if (ex == NULL || rt_executor_runtime(ex) == NULL) {
        return 0;
    }
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    if (requested < shard_count) {
        return requested;
    }
    return 0;
}

uint32_t rt_net_current_owner_shard(rt_executor* ex) {
    return rt_net_owner_shard_or_compat(ex, rt_debug_current_worker_shard_id());
}

rt_net_lifecycle_status rt_net_close_fd_on_owner(
    rt_executor* ex, uint32_t owner_shard_id, int* fd_slot, bool* closed_slot, int* out_errno) {
    if (out_errno != NULL) {
        *out_errno = 0;
    }
    if (ex == NULL || fd_slot == NULL || closed_slot == NULL || *closed_slot || *fd_slot < 0) {
        return RT_NET_LIFECYCLE_INVALID;
    }
    int fd = *fd_slot;
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    if (owner_shard_id >= shard_count) {
        return RT_NET_LIFECYCLE_REGISTRY_ERROR;
    }
    rt_fd_lifecycle_snapshot snapshot;
    rt_lock(ex);
    rt_fd_registry* registry = rt_executor_fd_registry_for_shard(ex, owner_shard_id);
    rt_runtime_status status = rt_fd_registry_mark_closed(registry, fd, &snapshot);
    snapshot.owner_shard_id = owner_shard_id;
    rt_unlock(ex);
    if (status != RT_RUNTIME_STATUS_OK) {
        return RT_NET_LIFECYCLE_REGISTRY_ERROR;
    }
    *closed_slot = true;
    *fd_slot = -1;
    int close_errno = 0;
    if (close(fd) != 0) {
        close_errno = errno;
        if (out_errno != NULL) {
            *out_errno = close_errno;
        }
    }
    rt_fd_completion_summary summary = rt_fd_registry_wake_closed_net_waiters(ex, &snapshot);
    if (summary.calls != 0 || summary.woken != 0) {
        rt_net_trace_close_owner_wakeup();
    }
    rt_net_trace_waiter_completion(summary.calls, summary.woken);
    return close_errno == 0 ? RT_NET_LIFECYCLE_OK : RT_NET_LIFECYCLE_CLOSE_ERROR;
}

rt_net_lifecycle_status
rt_net_close_listener_members(rt_executor* ex, NetListener* listener, int* out_errno) {
    if (out_errno != NULL) {
        *out_errno = 0;
    }
    if (ex == NULL || listener == NULL || listener->closed) {
        return RT_NET_LIFECYCLE_INVALID;
    }
    rt_net_lifecycle_status final_status = RT_NET_LIFECYCLE_OK;
    listener->closed = true;
    for (size_t i = 0; i < listener->member_count; i++) {
        NetListenerMember* member = &listener->members[i];
        int close_errno = 0;
        rt_net_lifecycle_status status = rt_net_close_fd_on_owner(
            ex, member->owner_shard_id, &member->fd, &member->closed, &close_errno);
        if (status == RT_NET_LIFECYCLE_OK || status == RT_NET_LIFECYCLE_INVALID) {
            if (status == RT_NET_LIFECYCLE_OK) {
                rt_net_trace_listener_group_member_closed();
            }
            continue;
        }
        final_status =
            final_status == RT_NET_LIFECYCLE_OK ? status : RT_NET_LIFECYCLE_PARTIAL_CLOSE;
        if (out_errno != NULL && *out_errno == 0) {
            *out_errno = close_errno;
        }
    }
    rt_net_listener_release_members(listener);
    return final_status;
}
