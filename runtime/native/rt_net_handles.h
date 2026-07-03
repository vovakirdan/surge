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

typedef struct NetConn {
    int fd;
    bool closed;
    uint8_t owner_shard_valid;
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
NetConn* rt_net_conn_alloc(int fd, uint32_t owner_shard_id, uint8_t owner_shard_valid);

#endif
