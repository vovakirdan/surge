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

typedef struct {
    int fd;
    uint32_t owner_shard_id;
    bool closed;
    // fd-registry row generation stamped when the member fd is registered on
    // its owner shard (Epic 10 Task 3, RV2-DEBT-010); 0 = not yet registered.
    uint64_t generation;
} NetListenerMember;

typedef struct NetListener {
    int fd;
    bool closed;
    uint8_t kind;
    uint32_t owner_shard_id;
    size_t member_count;
    size_t next_accept_index;
    NetListenerMember* members;
} NetListener;

// Handle-word contract (Epic 10 Task 3, RV2-DEBT-010). Surge lowers struct
// values as heap-boxed pointers, and TcpConn's box pointer IS the NetConn*,
// so `conn.__opaque` reads the FIRST 8 BYTES of this struct and a
// reconstructed handle (`{ __opaque = handle }`) is a fresh 8-byte box
// holding only that prefix. Therefore:
// - the first 8 bytes (fd, closed, owner_shard_valid, generation_check) are
//   the public handle word and the ONLY bytes a conn data-path op may read;
// - fields at offset >= 8 exist only on runtime-allocated handles
//   (connect/accept) and are OUT OF BOUNDS on reconstructed boxes;
// - generation_check carries the low 16 bits of the owning fd-registry row's
//   generation, stamped write-once at registration, so every copy or
//   reconstruction of a live handle can be validated against the registry.
typedef struct NetConn {
    int fd;
    bool closed;
    uint8_t owner_shard_valid;
    uint16_t generation_check;
    uint32_t owner_shard_id;
} NetConn;

NetListener*
rt_net_listener_alloc(NetListenerKind kind, size_t member_count, uint32_t owner_shard_id);
void rt_net_listener_free(NetListener* listener);
void rt_net_listener_release_members(NetListener* listener);
int rt_net_listener_registry_add(NetListener* listener);
void rt_net_listener_registry_remove(const NetListener* listener);
NetListener* rt_net_listener_canonical(NetListener* listener);
const NetListener* rt_net_listener_canonical_const(const NetListener* listener);
int rt_net_listener_set_member(NetListener* listener,
                               size_t index,
                               int fd,
                               uint32_t owner_shard_id);
NetListenerMember* rt_net_listener_first_live_member(NetListener* listener);
const NetListenerMember* rt_net_listener_first_live_member_const(const NetListener* listener);
int rt_net_listener_selected_member_const(const NetListener* listener, NetListenerMember* out);
NetConn*
rt_net_conn_alloc(int fd, uint32_t owner_shard_id, uint8_t owner_shard_valid, uint64_t generation);

#endif
