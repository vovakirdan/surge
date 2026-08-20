#include "rt_net_accept_group.h"
#include "rt_net_trace.h"

#include <errno.h>
#include <poll.h>
#include <string.h>

// A borrowed listener addresses the Surge struct's own storage, whose first
// word is the handle id; see the matching note in rt_net.c.
static const NetListener* net_listener_from_borrowed(const void* listener) {
    if (listener == NULL) {
        return NULL;
    }
    return rt_net_listener_canonical_const((const NetListener*)listener);
}

static NetListener* net_listener_from_borrowed_mut_group(const void* listener) {
    if (listener == NULL) {
        return NULL;
    }
    return rt_net_listener_canonical((const NetListener*)listener);
}

int rt_net_register_open_fd_on_owner(rt_executor* ex, uint32_t owner_shard_id, int fd) {
    return rt_net_register_open_fd_on_owner_generation(ex, owner_shard_id, fd, NULL);
}

int rt_net_register_open_fd_on_owner_generation(rt_executor* ex,
                                                uint32_t owner_shard_id,
                                                int fd,
                                                uint64_t* out_generation) {
    if (out_generation != NULL) {
        *out_generation = 0;
    }
    if (ex == NULL || fd < 0) {
        return 0;
    }
    rt_control_lock(ex);
    rt_shard* owner_shard = rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id);
    rt_fd_registry* registry = rt_executor_fd_registry_for_shard(ex, owner_shard_id);
    if (owner_shard != NULL) {
        rt_shard_lock(owner_shard);
    }
    int had_row = rt_fd_registry_find_const(registry, fd) != NULL;
    rt_runtime_status status =
        rt_fd_registry_register_open_fd_generation(registry, fd, out_generation);
    if (owner_shard != NULL) {
        rt_shard_unlock(owner_shard);
    }
    rt_control_unlock(ex);
    if (status == RT_RUNTIME_STATUS_OK) {
        if (!had_row) {
            rt_net_trace_fd_owner_registry_row();
        }
        return 1;
    }
    return 0;
}

int rt_net_handle_open_on_owner(rt_executor* ex,
                                uint32_t owner_shard_id,
                                int fd,
                                uint64_t generation) {
    // Owner-locked fd-lifetime guard: the registry mutations this races
    // against (register, mark_closed) run under the same owner shard lock, so
    // a handle whose fd closed or was reused cannot validate.
    if (ex == NULL || fd < 0) {
        return 0;
    }
    rt_shard* owner_shard = rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id);
    if (owner_shard == NULL) {
        return 0;
    }
    rt_shard_lock(owner_shard);
    int open = rt_fd_registry_handle_open(&owner_shard->fd_registry, fd, generation);
    rt_shard_unlock(owner_shard);
    return open;
}

void rt_net_forget_registered_fd_on_owner(rt_executor* ex, uint32_t owner_shard_id, int fd) {
    if (ex == NULL || fd < 0) {
        return;
    }
    rt_fd_lifecycle_snapshot snapshot;
    rt_control_lock(ex);
    rt_shard* owner_shard = rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id);
    rt_fd_registry* registry = rt_executor_fd_registry_for_shard(ex, owner_shard_id);
    if (owner_shard != NULL) {
        rt_shard_lock(owner_shard);
    }
    (void)rt_fd_registry_mark_closed(registry, fd, &snapshot);
    if (owner_shard != NULL) {
        rt_shard_unlock(owner_shard);
    }
    rt_control_unlock(ex);
}

int rt_net_interest_present_for_key(rt_executor* ex, waker_key key) {
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < shard_count; i++) {
        const rt_fd_registry* registry = rt_executor_fd_registry_const_for_shard(ex, i);
        if (rt_fd_registry_net_interest_present(registry, key)) {
            return 1;
        }
    }
    return 0;
}

bool rt_net_fd_ready_now(int fd, RtNetWaitKind kind) {
    if (fd < 0) {
        return true;
    }
    short events = POLLIN;
    short ready_mask = POLLIN | POLLERR | POLLHUP | POLLNVAL;
    if (kind == RT_NET_WAIT_WRITE) {
        events = POLLOUT;
        ready_mask = POLLOUT | POLLERR | POLLHUP | POLLNVAL;
    }
    struct pollfd pfd;
    memset(&pfd, 0, sizeof(pfd));
    pfd.fd = fd;
    pfd.events = events;
    int n = -1;
    do {
        n = poll(&pfd, 1, 0);
    } while (n < 0 && errno == EINTR);
    if (n <= 0) {
        return n < 0;
    }
    return (pfd.revents & ready_mask) != 0;
}

static void
net_task_set_ready_accept(rt_executor* ex, rt_task* task, int fd, uint32_t owner_shard_id) {
    if (task == NULL || fd < 0) {
        return;
    }
    task->net_ready_accept_valid = 1;
    task->net_ready_accept_fd = fd;
    task->net_ready_accept_owner_shard = owner_shard_id;
    rt_task_replace_owner(ex, task, owner_shard_id, TASK_PLACEMENT_CONNECTION);
}

void rt_net_place_current_task_on_owner(rt_executor* ex, uint32_t owner_shard_id) {
    if (ex == NULL) {
        return;
    }
    rt_control_lock(ex);
    rt_task* task = rt_current_task();
    if (task != NULL) {
        rt_task_replace_owner(ex, task, owner_shard_id, TASK_PLACEMENT_CONNECTION);
    }
    rt_control_unlock(ex);
}

static int
net_listener_live_member_for_fd(const NetListener* listener, int fd, NetListenerMember* out) {
    if (listener == NULL || listener->closed || listener->members == NULL || fd < 0) {
        return 0;
    }
    for (size_t i = 0; i < listener->member_count; i++) {
        const NetListenerMember* member = &listener->members[i];
        if (!member->closed && member->fd == fd) {
            if (out != NULL) {
                *out = *member;
            }
            return 1;
        }
    }
    return 0;
}

void rt_net_listener_note_accept(NetListener* listener, int member_fd) {
    if (listener == NULL || listener->members == NULL || listener->member_count == 0) {
        return;
    }
    for (size_t i = 0; i < listener->member_count; i++) {
        if (listener->members[i].fd == member_fd) {
            listener->next_accept_index = (i + 1U) % listener->member_count;
            return;
        }
    }
}

size_t rt_net_listener_index_for_fd(const NetListener* listener, int member_fd) {
    if (listener == NULL || listener->members == NULL || listener->member_count == 0) {
        return 0;
    }
    for (size_t i = 0; i < listener->member_count; i++) {
        if (listener->members[i].fd == member_fd) {
            return i;
        }
    }
    return 0;
}

int rt_net_consume_ready_accept_member(const NetListener* listener, NetListenerMember* out) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL || out == NULL) {
        return 0;
    }
    rt_control_lock(ex);
    rt_task* task = rt_current_task();
    if (task == NULL || task->net_ready_accept_valid == 0) {
        rt_control_unlock(ex);
        return 0;
    }
    int fd = task->net_ready_accept_fd;
    uint32_t owner_shard_id = task->net_ready_accept_owner_shard;
    task->net_ready_accept_valid = 0;
    task->net_ready_accept_fd = -1;
    clear_wait_keys(ex, task);
    rt_control_unlock(ex);
    if (!net_listener_live_member_for_fd(listener, fd, out)) {
        return 0;
    }
    return out->owner_shard_id == owner_shard_id;
}

static void net_register_listener_members_locked(rt_executor* ex, NetListener* listener) {
    if (ex == NULL || listener == NULL || listener->members == NULL) {
        return;
    }
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < listener->member_count; i++) {
        NetListenerMember* member = &listener->members[i];
        if (member->closed || member->fd < 0 || member->owner_shard_id >= shard_count) {
            continue;
        }
        rt_shard* member_shard = rt_runtime_shard(rt_executor_runtime(ex), member->owner_shard_id);
        rt_fd_registry* registry = rt_executor_fd_registry_for_shard(ex, member->owner_shard_id);
        if (member_shard != NULL) {
            rt_shard_lock(member_shard);
        }
        int had_row = rt_fd_registry_find_const(registry, member->fd) != NULL;
        uint64_t generation = 0;
        rt_runtime_status member_status =
            rt_fd_registry_register_open_fd_generation(registry, member->fd, &generation);
        if (member_shard != NULL) {
            rt_shard_unlock(member_shard);
        }
        if (member_status == RT_RUNTIME_STATUS_OK) {
            // Stamp the row generation into the shared member so accept and
            // close validate against the row this registration confirmed
            // (RV2-DEBT-010). Members are shared via the canonical listener,
            // so every copy observes the stamp.
            member->generation = generation;
            if (!had_row) {
                rt_net_trace_fd_owner_registry_row();
            }
        }
    }
}

void rt_net_stamp_listener_members(rt_executor* ex, NetListener* listener) {
    if (ex == NULL || listener == NULL || listener->closed) {
        return;
    }
    rt_control_lock(ex);
    net_register_listener_members_locked(ex, listener);
    rt_control_unlock(ex);
}

bool rt_net_wait_accept(const void* listener) {
    const NetListener* l = net_listener_from_borrowed(listener);
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return true;
    }
    rt_control_lock(ex);
    rt_task* task = rt_current_task();
    if (task == NULL || rt_current_task_id() == 0) {
        rt_control_unlock(ex);
        panic_msg("async net wait outside task");
        return true;
    }
    if (current_task_cancelled(ex)) {
        pending_key = waker_none();
        rt_control_unlock(ex);
        return false;
    }
    if (l == NULL || l->closed || l->members == NULL) {
        rt_control_unlock(ex);
        return true;
    }
    net_register_listener_members_locked(ex, net_listener_from_borrowed_mut_group(listener));
    for (size_t i = 0; i < l->member_count; i++) {
        const NetListenerMember* member = &l->members[i];
        if (member->closed || member->fd < 0) {
            continue;
        }
        if (rt_net_fd_ready_now(member->fd, RT_NET_WAIT_ACCEPT)) {
            if (rt_async_debug_enabled()) {
                rt_async_debug_printf("net-accept-wait-ready-now fd=%d owner=%u\n",
                                      member->fd,
                                      member->owner_shard_id);
            }
            net_task_set_ready_accept(ex, task, member->fd, member->owner_shard_id);
            rt_control_unlock(ex);
            return true;
        }
    }
    rt_net_trace_direct_wait();
    waker_key first_key = waker_none();
    int first_added = 0;
    for (size_t i = 0; i < l->member_count; i++) {
        const NetListenerMember* member = &l->members[i];
        if (member->closed || member->fd < 0) {
            continue;
        }
        waker_key key = net_accept_key(member->fd);
        size_t prev_len = task->wait_keys_len;
        add_wait_key(ex, task, key);
        int interest_present = rt_net_interest_present_for_key(ex, key);
        if (task->wait_keys_len == prev_len || !interest_present) {
            remove_waiter(ex, key, task->id);
            task->wait_keys_len = prev_len;
            continue;
        }
        if (!waker_valid(first_key)) {
            first_key = key;
            first_added = 1;
        }
        if (rt_async_debug_enabled()) {
            rt_async_debug_printf("net-accept-wait-add task=%llu fd=%d owner=%u\n",
                                  (unsigned long long)task->id,
                                  member->fd,
                                  member->owner_shard_id);
        }
    }
    if (!waker_valid(first_key)) {
        pending_key = waker_none();
        rt_control_unlock(ex);
        return true;
    }
    prepare_park(ex, task, first_key, first_added);
    pending_key = first_key;
    rt_control_unlock(ex);
    return false;
}
