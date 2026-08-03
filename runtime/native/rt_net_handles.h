#ifndef SURGE_RUNTIME_NATIVE_RT_NET_HANDLES_H
#define SURGE_RUNTIME_NATIVE_RT_NET_HANDLES_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef enum {
    NET_LISTENER_SINGLE = 0,
    NET_LISTENER_REUSEPORT_GROUP = 1,
    NET_LISTENER_FALLBACK_HANDOFF = 2,
} NetListenerKind;

typedef enum {
    NET_HANDLE_LISTENER = 1,
    NET_HANDLE_CONN = 2,
} NetHandleKind;

typedef struct {
    int fd;
    uint32_t owner_shard_id;
    bool closed;
    // fd-registry row generation stamped when the member fd is registered on
    // its owner shard (RV2-DEBT-010); 0 = not yet registered.
    uint64_t generation;
} NetListenerMember;

typedef struct NetListener {
    uint64_t handle_id;
    int fd;
    bool closed;
    uint8_t kind;
    uint32_t owner_shard_id;
    size_t member_count;
    size_t next_accept_index;
    NetListenerMember* members;
} NetListener;

// Public handle-word contract (recovery): Surge intrinsic net
// structs expose exactly one word, `__opaque`. That word is a runtime-generated
// handle id, never an OS fd and never a pointer. Language-owned 8-byte boxes
// and full runtime objects both start with this word, but they are distinct
// allocations: dropping a public box must not free the registry's canonical
// object. Every native entrypoint resolves the word through the handle table
// before reading fields at offset >= 8. The handle table is platform-neutral
// and owns stale copy rejection independently of fd reuse behavior.
typedef struct NetConn {
    uint64_t handle_id;
    int fd;
    bool closed;
    uint32_t owner_shard_id;
    uint64_t generation;
} NetConn;

NetListener*
rt_net_listener_alloc(NetListenerKind kind, size_t member_count, uint32_t owner_shard_id);
// Roll back a listener that has never escaped to Surge code. This closes any
// live member fds and frees the canonical object. Published canonicals are not
// reclaimed here because handle-table lookup readers do not retain them yet.
void rt_net_listener_rollback_unpublished(NetListener* listener);
void rt_net_listener_release_members(NetListener* listener);
int rt_net_listener_registry_add(NetListener* listener);
void rt_net_listener_registry_remove(const NetListener* listener);
NetListener* rt_net_listener_canonical(const NetListener* listener);
const NetListener* rt_net_listener_canonical_const(const NetListener* listener);
int rt_net_listener_set_member(NetListener* listener,
                               size_t index,
                               int fd,
                               uint32_t owner_shard_id);
NetListenerMember* rt_net_listener_first_live_member(NetListener* listener);
const NetListenerMember* rt_net_listener_first_live_member_const(const NetListener* listener);
int rt_net_listener_selected_member_const(const NetListener* listener, NetListenerMember* out);
NetConn* rt_net_conn_alloc(int fd, uint32_t owner_shard_id, uint64_t generation);
NetConn* rt_net_conn_canonical(const NetConn* conn);
const NetConn* rt_net_conn_canonical_const(const NetConn* conn);
void rt_net_conn_registry_remove(const NetConn* conn);
// Free only a connection that has never escaped to Surge code. The caller must
// first forget its fd-registry row and close its fd. Published canonicals are
// invalidated but intentionally not reclaimed until lookup readers retain them.
void rt_net_conn_free_unpublished(NetConn* conn);

#endif
