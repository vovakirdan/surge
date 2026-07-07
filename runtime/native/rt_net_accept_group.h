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
// Epic 10 Task 3 (RV2-DEBT-010): register and report the fd-registry row
// generation so the caller can stamp it into the public handle.
int rt_net_register_open_fd_on_owner_generation(rt_executor* ex,
                                                uint32_t owner_shard_id,
                                                int fd,
                                                uint64_t* out_generation);
// Epic 10 Task 3 (RV2-DEBT-010): owner-locked stale-handle guard. Returns 1
// only while {fd, generation} matches an OPEN registered row on owner_shard_id.
// For listener members only: members live behind the canonical listener and
// carry the full 64-bit generation.
int rt_net_handle_open_on_owner(rt_executor* ex,
                                uint32_t owner_shard_id,
                                int fd,
                                uint64_t generation);
// Epic 10 Task 3 (RV2-DEBT-010): conn-handle probe guard. A public TcpConn
// may be a reconstructed 8-byte box carrying only the handle word (see
// rt_net_handles.h), so the owner shard cannot be read from the struct. The
// probe scans shard fd registries (hint first, then all) under each shard's
// lock; on the row it validates the 16-bit generation check. Returns 1 and
// stores the owner shard when the handle is current; 0 when the fd is
// closed, reused (check mismatch), or unknown.
int rt_net_conn_probe_open(rt_executor* ex,
                           uint32_t hint_shard_id,
                           int fd,
                           uint16_t generation_check,
                           uint32_t* out_owner_shard_id);
// Epic 10 Task 3 (RV2-DEBT-010): register every live listener member fd on its
// owner shard and stamp the row generation into the member.
void rt_net_stamp_listener_members(rt_executor* ex, NetListener* listener);
void rt_net_forget_registered_fd_on_owner(rt_executor* ex, uint32_t owner_shard_id, int fd);
int rt_net_interest_present_for_key(rt_executor* ex, waker_key key);
bool rt_net_fd_ready_now(int fd, RtNetWaitKind kind);
void rt_net_place_current_task_on_owner(rt_executor* ex, uint32_t owner_shard_id);
void rt_net_listener_note_accept(NetListener* listener, int member_fd);
size_t rt_net_listener_index_for_fd(const NetListener* listener, int member_fd);
int rt_net_consume_ready_accept_member(const NetListener* listener, NetListenerMember* out);

#endif
