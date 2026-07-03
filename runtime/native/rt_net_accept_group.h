#ifndef SURGE_RT_NET_ACCEPT_GROUP_H
#define SURGE_RT_NET_ACCEPT_GROUP_H

#include "rt_async_internal.h"
#include "rt_net_handles.h"

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef enum {
    RT_NET_WAIT_ACCEPT = 0,
    RT_NET_WAIT_READ = 1,
    RT_NET_WAIT_WRITE = 2,
} RtNetWaitKind;

int rt_net_register_open_fd_on_owner(rt_executor* ex, uint32_t owner_shard_id, int fd);
void rt_net_forget_registered_fd_on_owner(rt_executor* ex, uint32_t owner_shard_id, int fd);
int rt_net_interest_present_for_key(rt_executor* ex, waker_key key);
bool rt_net_fd_ready_now(int fd, RtNetWaitKind kind);
void rt_net_place_current_task_on_owner(rt_executor* ex, uint32_t owner_shard_id);
void rt_net_listener_note_accept(NetListener* listener, int member_fd);
size_t rt_net_listener_index_for_fd(const NetListener* listener, int member_fd);
int rt_net_consume_ready_accept_member(const NetListener* listener, NetListenerMember* out);

#endif
