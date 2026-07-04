#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"
#include "rt_net_accept_group.h"
#include "rt_net_handles.h"
#include "rt_net_lifecycle.h"
#include "rt_net_listener_socket.h"
#include "rt_net_trace.h"

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <poll.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

enum {
    NET_ERR_WOULD_BLOCK = 1,
    NET_ERR_TIMED_OUT = 2,
    NET_ERR_CONNECTION_RESET = 3,
    NET_ERR_CONNECTION_REFUSED = 4,
    NET_ERR_NOT_CONNECTED = 5,
    NET_ERR_ADDR_IN_USE = 6,
    NET_ERR_INVALID_ADDR = 7,
    NET_ERR_IO = 8,
    NET_ERR_UNSUPPORTED = 9,
};

typedef struct NetError {
    void* message;
    void* code;
} NetError;

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

static const char* net_error_message(uint64_t code) {
    switch (code) {
        case NET_ERR_WOULD_BLOCK:
            return "WouldBlock";
        case NET_ERR_TIMED_OUT:
            return "TimedOut";
        case NET_ERR_CONNECTION_RESET:
            return "ConnectionReset";
        case NET_ERR_CONNECTION_REFUSED:
            return "ConnectionRefused";
        case NET_ERR_NOT_CONNECTED:
            return "NotConnected";
        case NET_ERR_ADDR_IN_USE:
            return "AddrInUse";
        case NET_ERR_INVALID_ADDR:
            return "InvalidAddr";
        case NET_ERR_UNSUPPORTED:
            return "Unsupported";
        default:
            return "Io";
    }
}

static uint64_t net_error_code_from_errno(int err) {
    switch (err) {
        case EAGAIN:
#ifdef EWOULDBLOCK
#if EWOULDBLOCK != EAGAIN
        case EWOULDBLOCK:
#endif
#endif
            return NET_ERR_WOULD_BLOCK;
        case ETIMEDOUT:
            return NET_ERR_TIMED_OUT;
        case ECONNRESET:
        case ECONNABORTED:
        case EPIPE:
            return NET_ERR_CONNECTION_RESET;
        case ECONNREFUSED:
            return NET_ERR_CONNECTION_REFUSED;
        case ENOTCONN:
            return NET_ERR_NOT_CONNECTED;
        case EADDRINUSE:
            return NET_ERR_ADDR_IN_USE;
        case EADDRNOTAVAIL:
        case EINVAL:
            return NET_ERR_INVALID_ADDR;
        case EAFNOSUPPORT:
        case EPROTONOSUPPORT:
        case ENOSYS:
        case EOPNOTSUPP:
            return NET_ERR_UNSUPPORTED;
        default:
            return NET_ERR_IO;
    }
}

static void* net_make_error(uint64_t code) {
    NetError* err = (NetError*)rt_alloc((uint64_t)sizeof(NetError), (uint64_t)alignof(NetError));
    if (err == NULL) {
        return NULL;
    }
    const char* msg = net_error_message(code);
    err->message = rt_string_from_bytes((const uint8_t*)msg, (uint64_t)strlen(msg));
    err->code = rt_biguint_from_u64(code);
    return (void*)err;
}

static void* net_make_success_ptr(void* payload) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(NetError);
    if (payload_size < sizeof(void*)) {
        payload_size = sizeof(void*);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = (uint8_t*)rt_tag_alloc(0, payload_align, payload_size);
    if (mem == NULL) {
        return NULL;
    }
    memcpy(mem + payload_offset, (const void*)&payload, sizeof(payload));
    return mem;
}

static void* net_make_success_nothing(void) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(NetError);
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = (uint8_t*)rt_tag_alloc(0, payload_align, payload_size);
    if (mem == NULL) {
        return NULL;
    }
    mem[payload_offset] = 0;
    return mem;
}

static void* net_make_success_bytes(uint8_t* data, uint64_t len, uint64_t cap) {
    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        if (data != NULL) {
            rt_free(data, cap, (uint64_t)alignof(uint8_t));
        }
        return net_make_error(NET_ERR_IO);
    }
    header->len = len;
    header->cap = cap;
    header->data = data;
    void* out = net_make_success_ptr((void*)header);
    if (out == NULL) {
        if (data != NULL) {
            rt_free(data, cap, (uint64_t)alignof(uint8_t));
        }
        rt_free((uint8_t*)header,
                (uint64_t)sizeof(SurgeArrayHeader),
                (uint64_t)alignof(SurgeArrayHeader));
        return net_make_error(NET_ERR_IO);
    }
    return out;
}

static char* net_copy_addr(void* addr, uint64_t* out_len, uint64_t* err_code) {
    if (err_code != NULL) {
        *err_code = 0;
    }
    uint64_t len = rt_string_len_bytes(addr);
    if (len == 0) {
        if (err_code != NULL) {
            *err_code = NET_ERR_INVALID_ADDR;
        }
        return NULL;
    }
    const uint8_t* bytes = rt_string_ptr(addr);
    if (bytes == NULL) {
        if (err_code != NULL) {
            *err_code = NET_ERR_INVALID_ADDR;
        }
        return NULL;
    }
    if (memchr(bytes, 0, (size_t)len) != NULL) {
        if (err_code != NULL) {
            *err_code = NET_ERR_INVALID_ADDR;
        }
        return NULL;
    }
    char* buf = (char*)malloc((size_t)len + 1);
    if (buf == NULL) {
        if (err_code != NULL) {
            *err_code = NET_ERR_IO;
        }
        return NULL;
    }
    memcpy(buf, bytes, (size_t)len);
    buf[len] = '\0';
    if (out_len != NULL) {
        *out_len = len;
    }
    return buf;
}

static int net_set_nonblocking(int fd, uint64_t* out_code) {
    if (out_code != NULL) {
        *out_code = 0;
    }
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) {
        if (out_code != NULL) {
            *out_code = net_error_code_from_errno(errno);
        }
        return 0;
    }
    if (fcntl(fd, F_SETFL, flags | O_NONBLOCK) < 0) {
        if (out_code != NULL) {
            *out_code = net_error_code_from_errno(errno);
        }
        return 0;
    }
    return 1;
}

static int net_set_tcp_nodelay(int fd, uint64_t* out_code) {
    if (out_code != NULL) {
        *out_code = 0;
    }
    int enabled = 1;
    if (setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &enabled, (socklen_t)sizeof(enabled)) != 0) {
        if (out_code != NULL) {
            *out_code = net_error_code_from_errno(errno);
        }
        return 0;
    }
    return 1;
}

static int net_prepare_conn_fd(int fd, uint64_t* out_code) {
    if (!net_set_tcp_nodelay(fd, out_code)) {
        return 0;
    }
    return net_set_nonblocking(fd, out_code);
}

static const NetListener* net_listener_from_borrowed(const void* listener) {
    if (listener == NULL) {
        return NULL;
    }
    return rt_net_listener_canonical_const(*(const NetListener* const*)listener);
}

static NetListener* net_listener_from_value(void* listener) {
    if (listener == NULL) {
        return NULL;
    }
    return rt_net_listener_canonical((NetListener*)listener);
}

static NetListener* net_listener_from_borrowed_mut(const void* listener) {
    if (listener == NULL) {
        return NULL;
    }
    return rt_net_listener_canonical(*(NetListener* const*)listener);
}

static const NetConn* net_conn_from_borrowed(const void* conn) {
    if (conn == NULL) {
        return NULL;
    }
    return *(const NetConn* const*)conn;
}

static NetConn* net_conn_from_value(void* conn) {
    if (conn == NULL) {
        return NULL;
    }
    return (NetConn*)conn;
}

static size_t net_configured_shard_count(void) {
    if (exec_state.initialized && rt_executor_runtime(&exec_state) != NULL) {
        return rt_runtime_shard_count(rt_executor_runtime(&exec_state));
    }
    rt_runtime_start_config config;
    const char* config_error = NULL;
    if (rt_runtime_start_config_from_env(&config, &config_error) != RT_RUNTIME_STATUS_OK) {
        rt_runtime_config_exit(config_error);
    }
    return config.shard_count;
}

void* rt_net_listen(void* addr, uint64_t port) {
    uint64_t err_code = 0;
    char* buf = net_copy_addr(addr, NULL, &err_code);
    if (buf == NULL) {
        return net_make_error(err_code == 0 ? NET_ERR_INVALID_ADDR : err_code);
    }
    if (port > 65535) {
        free(buf);
        return net_make_error(NET_ERR_INVALID_ADDR);
    }
    struct in_addr ip;
    int parse_ok = inet_pton(AF_INET, buf, &ip);
    free(buf);
    if (parse_ok != 1) {
        return net_make_error(NET_ERR_INVALID_ADDR);
    }

    size_t shard_count = net_configured_shard_count();
    if (shard_count == 0) {
        return net_make_error(NET_ERR_IO);
    }
    size_t member_count = shard_count;
    NetListenerKind kind = member_count > 1 ? NET_LISTENER_REUSEPORT_GROUP : NET_LISTENER_SINGLE;
    NetListener* listener = rt_net_listener_alloc(kind, member_count, 0);
    if (listener == NULL) {
        return net_make_error(NET_ERR_IO);
    }
    uint16_t bound_port = (uint16_t)port;
    for (size_t i = 0; i < member_count; i++) {
        int listener_errno = 0;
        int fd = rt_net_create_listener_socket(
            &ip, (uint16_t)port, &bound_port, member_count > 1, &listener_errno);
        if (fd < 0) {
            for (size_t j = 0; j < i; j++) {
                if (listener->members[j].fd >= 0) {
                    close(listener->members[j].fd);
                }
            }
            rt_net_listener_free(listener);
            return net_make_error(listener_errno == 0 ? NET_ERR_IO
                                                      : net_error_code_from_errno(listener_errno));
        }
        if (!rt_net_listener_set_member(listener, i, fd, (uint32_t)i)) {
            close(fd);
            for (size_t j = 0; j < i; j++) {
                if (listener->members[j].fd >= 0) {
                    close(listener->members[j].fd);
                }
            }
            rt_net_listener_free(listener);
            return net_make_error(NET_ERR_IO);
        }
    }
    if (!rt_net_listener_registry_add(listener)) {
        for (size_t j = 0; j < member_count; j++) {
            if (listener->members[j].fd >= 0) {
                close(listener->members[j].fd);
            }
        }
        rt_net_listener_free(listener);
        return net_make_error(NET_ERR_IO);
    }
    return net_make_success_ptr(listener);
}

void* rt_net_connect(void* addr, uint64_t port) {
    uint64_t err_code = 0;
    char* buf = net_copy_addr(addr, NULL, &err_code);
    if (buf == NULL) {
        return net_make_error(err_code == 0 ? NET_ERR_INVALID_ADDR : err_code);
    }
    if (port > 65535) {
        free(buf);
        return net_make_error(NET_ERR_INVALID_ADDR);
    }
    struct in_addr ip;
    int parse_ok = inet_pton(AF_INET, buf, &ip);
    free(buf);
    if (parse_ok != 1) {
        return net_make_error(NET_ERR_INVALID_ADDR);
    }

    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) {
        return net_make_error(net_error_code_from_errno(errno));
    }
    struct sockaddr_in sa;
    memset(&sa, 0, sizeof(sa));
    sa.sin_family = AF_INET;
    sa.sin_port = htons((uint16_t)port);
    sa.sin_addr = ip;
    int res;
    do {
        res = connect(fd, (struct sockaddr*)&sa, sizeof(sa));
    } while (res < 0 && errno == EINTR);
    if (res != 0) {
        uint64_t code = net_error_code_from_errno(errno);
        close(fd);
        return net_make_error(code);
    }

    if (!net_prepare_conn_fd(fd, &err_code)) {
        close(fd);
        return net_make_error(err_code == 0 ? NET_ERR_IO : err_code);
    }

    uint32_t owner_shard_id =
        rt_net_owner_shard_or_compat(&exec_state, rt_debug_current_worker_shard_id());
    rt_executor* ex = ensure_exec();
    if (ex == NULL || !rt_net_register_open_fd_on_owner(ex, owner_shard_id, fd)) {
        close(fd);
        return net_make_error(NET_ERR_IO);
    }
    NetConn* conn = rt_net_conn_alloc(fd, owner_shard_id, 1);
    if (conn == NULL) {
        rt_net_forget_registered_fd_on_owner(ex, owner_shard_id, fd);
        close(fd);
        return net_make_error(NET_ERR_IO);
    }
    return net_make_success_ptr(conn);
}

void* rt_net_close_listener(void* listener) {
    NetListener* l = net_listener_from_value(listener);
    if (l == NULL || l->closed) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return net_make_error(NET_ERR_IO);
    }
    int close_errno = 0;
    rt_net_lifecycle_status status = rt_net_close_listener_members(ex, l, &close_errno);
    if (status != RT_NET_LIFECYCLE_OK) {
        return net_make_error(close_errno == 0 ? NET_ERR_IO
                                               : net_error_code_from_errno(close_errno));
    }
    return net_make_success_nothing();
}

void* rt_net_close_conn(void* conn) {
    NetConn* c = net_conn_from_value(conn);
    if (c == NULL || c->closed) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return net_make_error(NET_ERR_IO);
    }
    int close_errno = 0;
    rt_net_lifecycle_status status =
        rt_net_close_fd_on_owner(ex, c->owner_shard_id, &c->fd, &c->closed, &close_errno);
    if (status == RT_NET_LIFECYCLE_OK) {
        return net_make_success_nothing();
    }
    return net_make_error(
        status == RT_NET_LIFECYCLE_INVALID
            ? NET_ERR_NOT_CONNECTED
            : (close_errno == 0 ? NET_ERR_IO : net_error_code_from_errno(close_errno)));
}

void* rt_net_accept(const void* listener) {
    const NetListener* l = net_listener_from_borrowed(listener);
    NetListenerMember member;
    int have_preferred = rt_net_consume_ready_accept_member(l, &member);
    if (!have_preferred && !rt_net_listener_selected_member_const(l, &member)) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    NetListener* mutable_listener = net_listener_from_borrowed_mut(listener);
    int fd = -1;
    uint64_t last_error = NET_ERR_WOULD_BLOCK;
    size_t member_count = l != NULL ? l->member_count : 0;
    size_t start = have_preferred ? rt_net_listener_index_for_fd(l, member.fd)
                                  : (mutable_listener != NULL && member_count > 0
                                         ? mutable_listener->next_accept_index % member_count
                                         : 0);
    for (size_t offset = 0; fd < 0 && l != NULL && offset < member_count; offset++) {
        size_t index = (start + offset) % member_count;
        const NetListenerMember* next = &l->members[index];
        if (next->closed || next->fd < 0) {
            continue;
        }
        member = *next;
        do {
            fd = accept(member.fd, NULL, NULL);
        } while (fd < 0 && errno == EINTR);
        if (fd >= 0) {
            break;
        }
        last_error = net_error_code_from_errno(errno);
        if (last_error != NET_ERR_WOULD_BLOCK) {
            return net_make_error(last_error);
        }
    }
    if (fd < 0) {
        return net_make_error(last_error);
    }
    rt_net_listener_note_accept(mutable_listener, member.fd);
    uint64_t err_code = 0;
    if (!net_prepare_conn_fd(fd, &err_code)) {
        close(fd);
        return net_make_error(err_code == 0 ? NET_ERR_IO : err_code);
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL || !rt_net_register_open_fd_on_owner(ex, member.owner_shard_id, fd)) {
        close(fd);
        return net_make_error(NET_ERR_IO);
    }
    NetConn* conn = rt_net_conn_alloc(fd, member.owner_shard_id, 1);
    if (conn == NULL) {
        rt_net_forget_registered_fd_on_owner(ex, member.owner_shard_id, fd);
        close(fd);
        return net_make_error(NET_ERR_IO);
    }
    rt_net_place_current_task_on_owner(ex, member.owner_shard_id);
    if (rt_async_debug_enabled()) {
        rt_async_debug_printf("net-accept-success fd=%d owner=%u\n", fd, member.owner_shard_id);
    }
    rt_net_trace_accept_owner(member.owner_shard_id);
    return net_make_success_ptr(conn);
}

void* rt_net_read(const void* conn, uint8_t* buf, uint64_t cap) {
    const NetConn* c = net_conn_from_borrowed(conn);
    if (c == NULL || c->closed) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    if (cap == 0) {
        void* count = rt_biguint_from_u64(0);
        return net_make_success_ptr(count);
    }
    if (buf == NULL || cap > (uint64_t)SSIZE_MAX) {
        return net_make_error(NET_ERR_IO);
    }
    ssize_t n = -1;
    do {
        n = read(c->fd, buf, (size_t)cap);
    } while (n < 0 && errno == EINTR);
    if (n < 0) {
        return net_make_error(net_error_code_from_errno(errno));
    }
    void* count = rt_biguint_from_u64((uint64_t)n);
    return net_make_success_ptr(count);
}

void* rt_net_write(const void* conn, const uint8_t* buf, uint64_t len) {
    const NetConn* c = net_conn_from_borrowed(conn);
    if (c == NULL || c->closed) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    if (len == 0) {
        void* count = rt_biguint_from_u64(0);
        return net_make_success_ptr(count);
    }
    if (buf == NULL || len > (uint64_t)SSIZE_MAX) {
        return net_make_error(NET_ERR_IO);
    }
    ssize_t n = -1;
    do {
        n = write(c->fd, buf, (size_t)len);
    } while (n < 0 && errno == EINTR);
    if (n < 0) {
        return net_make_error(net_error_code_from_errno(errno));
    }
    void* count = rt_biguint_from_u64((uint64_t)n);
    return net_make_success_ptr(count);
}

void* rt_net_read_bytes(const void* conn, uint64_t cap) {
    const NetConn* c = net_conn_from_borrowed(conn);
    if (c == NULL || c->closed) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    if (cap == 0) {
        return net_make_success_bytes(NULL, 0, 0);
    }
    if (cap > (uint64_t)SSIZE_MAX) {
        return net_make_error(NET_ERR_IO);
    }
    uint8_t* data = (uint8_t*)rt_alloc(cap, (uint64_t)alignof(uint8_t));
    if (data == NULL) {
        return net_make_error(NET_ERR_IO);
    }
    ssize_t n = -1;
    do {
        n = read(c->fd, data, (size_t)cap);
    } while (n < 0 && errno == EINTR);
    if (n < 0) {
        uint64_t code = net_error_code_from_errno(errno);
        rt_free(data, cap, (uint64_t)alignof(uint8_t));
        return net_make_error(code);
    }
    if (n == 0) {
        rt_free(data, cap, (uint64_t)alignof(uint8_t));
        return net_make_success_bytes(NULL, 0, 0);
    }
    return net_make_success_bytes(data, (uint64_t)n, cap);
}

void* rt_net_write_bytes(const void* conn, const void* bytes, uint64_t offset, uint64_t len) {
    const NetConn* c = net_conn_from_borrowed(conn);
    if (c == NULL || c->closed) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    const SurgeArrayHeader* header = (const SurgeArrayHeader*)bytes;
    if (header == NULL || offset > header->len || len > header->len - offset ||
        len > (uint64_t)SSIZE_MAX) {
        return net_make_error(NET_ERR_IO);
    }
    if (len == 0) {
        void* count = rt_biguint_from_u64(0);
        return net_make_success_ptr(count);
    }
    const uint8_t* data = (const uint8_t*)header->data;
    if (data == NULL) {
        return net_make_error(NET_ERR_IO);
    }
    ssize_t n = -1;
    do {
        n = write(c->fd, data + offset, (size_t)len);
    } while (n < 0 && errno == EINTR);
    if (n < 0) {
        return net_make_error(net_error_code_from_errno(errno));
    }
    void* count = rt_biguint_from_u64((uint64_t)n);
    return net_make_success_ptr(count);
}

static bool net_wait_current_task(int fd, RtNetWaitKind kind) {
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
    if (fd < 0 || rt_net_fd_ready_now(fd, kind)) {
        rt_control_unlock(ex);
        return true;
    }
    waker_key key;
    switch (kind) {
        case RT_NET_WAIT_ACCEPT:
            key = net_accept_key(fd);
            break;
        case RT_NET_WAIT_READ:
            key = net_read_key(fd);
            break;
        case RT_NET_WAIT_WRITE:
            key = net_write_key(fd);
            break;
        default:
            rt_control_unlock(ex);
            return true;
    }
    if (!waker_valid(key)) {
        rt_control_unlock(ex);
        return true;
    }
    rt_net_trace_direct_wait();
    prepare_park(ex, task, key, 0);
    if (!rt_net_interest_present_for_key(ex, key)) {
        // Attach failed or closed the row: undo the park so the rowless waiter
        // cannot be lost now that poll input is registry-only.
        remove_waiter(ex, key, task->id);
        task->park_prepared = 0;
        task->park_key = waker_none();
        pending_key = waker_none();
        rt_control_unlock(ex);
        return true;
    }
    pending_key = key;
    rt_control_unlock(ex);
    return false;
}

bool rt_net_wait_readable(const void* conn) {
    const NetConn* c = net_conn_from_borrowed(conn);
    int fd = -1;
    if (c != NULL && !c->closed) {
        fd = c->fd;
    }
    return net_wait_current_task(fd, RT_NET_WAIT_READ);
}

bool rt_net_wait_writable(const void* conn) {
    const NetConn* c = net_conn_from_borrowed(conn);
    int fd = -1;
    if (c != NULL && !c->closed) {
        fd = c->fd;
    }
    return net_wait_current_task(fd, RT_NET_WAIT_WRITE);
}
