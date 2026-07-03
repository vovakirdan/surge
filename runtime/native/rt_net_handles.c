#include "rt_net_handles.h"
#include "rt_async_internal.h"

NetListener*
rt_net_listener_alloc(NetListenerKind kind, size_t member_count, uint32_t owner_shard_id) {
    if (member_count == 0) {
        return NULL;
    }
    NetListener* listener =
        (NetListener*)rt_alloc((uint64_t)sizeof(NetListener), (uint64_t) _Alignof(NetListener));
    if (listener == NULL) {
        return NULL;
    }
    memset(listener, 0, sizeof(*listener));
    listener->fd = -1;
    listener->closed = true;
    listener->kind = (uint8_t)kind;
    listener->owner_shard_id = owner_shard_id;
    listener->member_count = member_count;
    listener->members =
        (NetListenerMember*)rt_alloc((uint64_t)(member_count * sizeof(NetListenerMember)),
                                     (uint64_t) _Alignof(NetListenerMember));
    if (listener->members == NULL) {
        rt_free(
            (uint8_t*)listener, (uint64_t)sizeof(NetListener), (uint64_t) _Alignof(NetListener));
        return NULL;
    }
    memset(listener->members, 0, member_count * sizeof(NetListenerMember));
    for (size_t i = 0; i < member_count; i++) {
        listener->members[i].fd = -1;
        listener->members[i].closed = true;
    }
    return listener;
}

void rt_net_listener_release_members(NetListener* listener) {
    if (listener == NULL || listener->members == NULL) {
        return;
    }
    rt_free((uint8_t*)listener->members,
            (uint64_t)(listener->member_count * sizeof(NetListenerMember)),
            (uint64_t) _Alignof(NetListenerMember));
    listener->members = NULL;
    listener->member_count = 0;
    listener->fd = -1;
    listener->closed = true;
}

void rt_net_listener_free(NetListener* listener) {
    if (listener == NULL) {
        return;
    }
    rt_net_listener_release_members(listener);
    rt_free((uint8_t*)listener, (uint64_t)sizeof(NetListener), (uint64_t) _Alignof(NetListener));
}

int rt_net_listener_set_member(NetListener* listener,
                               size_t index,
                               int fd,
                               uint32_t owner_shard_id) {
    if (listener == NULL || listener->members == NULL || index >= listener->member_count ||
        fd < 0) {
        return 0;
    }
    listener->members[index].fd = fd;
    listener->members[index].owner_shard_id = owner_shard_id;
    listener->members[index].closed = false;
    if (index == 0) {
        listener->fd = fd;
        listener->closed = false;
        listener->owner_shard_id = owner_shard_id;
    }
    return 1;
}

NetListenerMember* rt_net_listener_first_live_member(NetListener* listener) {
    if (listener == NULL || listener->closed || listener->members == NULL) {
        return NULL;
    }
    for (size_t i = 0; i < listener->member_count; i++) {
        NetListenerMember* member = &listener->members[i];
        if (!member->closed && member->fd >= 0) {
            return member;
        }
    }
    return NULL;
}

const NetListenerMember* rt_net_listener_first_live_member_const(const NetListener* listener) {
    if (listener == NULL || listener->closed || listener->members == NULL) {
        return NULL;
    }
    for (size_t i = 0; i < listener->member_count; i++) {
        const NetListenerMember* member = &listener->members[i];
        if (!member->closed && member->fd >= 0) {
            return member;
        }
    }
    return NULL;
}

int rt_net_listener_selected_member_const(const NetListener* listener, NetListenerMember* out) {
    if (listener == NULL || listener->closed || out == NULL) {
        return 0;
    }
    const NetListenerMember* member = rt_net_listener_first_live_member_const(listener);
    if (member != NULL) {
        *out = *member;
        return 1;
    }
    if (listener->fd < 0) {
        return 0;
    }
    out->fd = listener->fd;
    out->owner_shard_id = listener->owner_shard_id;
    out->closed = false;
    return 1;
}

NetConn* rt_net_conn_alloc(int fd, uint32_t owner_shard_id, uint8_t owner_shard_valid) {
    NetConn* conn = (NetConn*)rt_alloc((uint64_t)sizeof(NetConn), (uint64_t) _Alignof(NetConn));
    if (conn == NULL) {
        return NULL;
    }
    conn->fd = fd;
    conn->closed = false;
    conn->owner_shard_valid = owner_shard_valid;
    conn->owner_shard_id = owner_shard_id;
    return conn;
}
