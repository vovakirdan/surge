#include "rt_net_handles.h"
#include "rt_async_internal.h"

#include <pthread.h>

typedef struct {
    int fd;
    NetListener* listener;
} NetListenerRegistryEntry;

static pthread_mutex_t net_listener_registry_lock = PTHREAD_MUTEX_INITIALIZER;
static NetListenerRegistryEntry* net_listener_registry_entries;
static size_t net_listener_registry_len;
static size_t net_listener_registry_cap;

static int net_listener_registry_ensure_cap(size_t want) {
    if (want <= net_listener_registry_cap) {
        return 1;
    }
    size_t next_cap = net_listener_registry_cap == 0 ? 8 : net_listener_registry_cap;
    while (next_cap < want) {
        if (next_cap > SIZE_MAX / 2U) {
            return 0;
        }
        next_cap *= 2U;
    }
    if (next_cap > SIZE_MAX / sizeof(NetListenerRegistryEntry)) {
        return 0;
    }
    size_t old_size = net_listener_registry_cap * sizeof(NetListenerRegistryEntry);
    size_t new_size = next_cap * sizeof(NetListenerRegistryEntry);
    NetListenerRegistryEntry* next =
        (NetListenerRegistryEntry*)rt_realloc((uint8_t*)net_listener_registry_entries,
                                              (uint64_t)old_size,
                                              (uint64_t)new_size,
                                              _Alignof(NetListenerRegistryEntry));
    if (next == NULL) {
        return 0;
    }
    net_listener_registry_entries = next;
    net_listener_registry_cap = next_cap;
    return 1;
}

static void net_listener_registry_add_fd_locked(NetListener* listener, int fd) {
    if (listener == NULL || fd < 0) {
        return;
    }
    for (size_t i = 0; i < net_listener_registry_len; i++) {
        NetListenerRegistryEntry* entry = &net_listener_registry_entries[i];
        if (entry->fd == fd) {
            entry->listener = listener;
            return;
        }
    }
    net_listener_registry_entries[net_listener_registry_len++] =
        (NetListenerRegistryEntry){fd, listener};
}

int rt_net_listener_registry_add(NetListener* listener) {
    if (listener == NULL) {
        return 0;
    }
    pthread_mutex_lock(&net_listener_registry_lock);
    size_t want = net_listener_registry_len + 1U;
    if (listener->members != NULL) {
        want += listener->member_count;
    }
    if (!net_listener_registry_ensure_cap(want)) {
        pthread_mutex_unlock(&net_listener_registry_lock);
        return 0;
    }
    net_listener_registry_add_fd_locked(listener, listener->fd);
    if (listener->members != NULL) {
        for (size_t i = 0; i < listener->member_count; i++) {
            if (!listener->members[i].closed) {
                net_listener_registry_add_fd_locked(listener, listener->members[i].fd);
            }
        }
    }
    pthread_mutex_unlock(&net_listener_registry_lock);
    return 1;
}

void rt_net_listener_registry_remove(const NetListener* listener) {
    if (listener == NULL) {
        return;
    }
    pthread_mutex_lock(&net_listener_registry_lock);
    size_t out = 0;
    for (size_t i = 0; i < net_listener_registry_len; i++) {
        if (net_listener_registry_entries[i].listener == listener) {
            continue;
        }
        net_listener_registry_entries[out++] = net_listener_registry_entries[i];
    }
    net_listener_registry_len = out;
    pthread_mutex_unlock(&net_listener_registry_lock);
}

NetListener* rt_net_listener_canonical(NetListener* listener) {
    if (listener == NULL || listener->fd < 0) {
        return listener;
    }
    pthread_mutex_lock(&net_listener_registry_lock);
    for (size_t i = 0; i < net_listener_registry_len; i++) {
        if (net_listener_registry_entries[i].fd == listener->fd) {
            NetListener* canonical = net_listener_registry_entries[i].listener;
            pthread_mutex_unlock(&net_listener_registry_lock);
            return canonical;
        }
    }
    pthread_mutex_unlock(&net_listener_registry_lock);
    return listener;
}

const NetListener* rt_net_listener_canonical_const(const NetListener* listener) {
    if (listener == NULL || listener->fd < 0) {
        return listener;
    }
    pthread_mutex_lock(&net_listener_registry_lock);
    for (size_t i = 0; i < net_listener_registry_len; i++) {
        if (net_listener_registry_entries[i].fd == listener->fd) {
            const NetListener* canonical = net_listener_registry_entries[i].listener;
            pthread_mutex_unlock(&net_listener_registry_lock);
            return canonical;
        }
    }
    pthread_mutex_unlock(&net_listener_registry_lock);
    return listener;
}

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
    rt_net_listener_registry_remove(listener);
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
