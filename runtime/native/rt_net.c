#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"

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

typedef struct NetListener {
    int fd;
    bool closed;
} NetListener;

typedef struct NetConn {
    int fd;
    bool closed;
} NetConn;

typedef enum {
    NET_WAIT_ACCEPT = 0,
    NET_WAIT_READ = 1,
    NET_WAIT_WRITE = 2,
} NetWaitKind;

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

static int net_poll_wake_read_fd = -1;
static int net_poll_wake_write_fd = -1;
static _Atomic uint64_t net_poll_calls_total;
static _Atomic uint64_t net_poll_timeouts_total;
static _Atomic uint64_t net_poll_wake_fd_total;
static _Atomic uint64_t net_poll_ready_total;
static _Atomic uint64_t net_poll_errors_total;
static _Atomic uint64_t net_poll_timeout_last_ms;
static _Atomic uint64_t net_poll_timeout_max_ms;
static _Atomic uint64_t net_poll_waiters_last;
static _Atomic uint64_t net_poll_waiters_max;
static _Atomic uint64_t net_poll_waiters_total;
static _Atomic uint64_t net_direct_wait_total;
static _Atomic uint64_t net_waiter_scan_entries_total;
static _Atomic uint64_t net_waiter_net_entries_total;
static _Atomic uint64_t net_poll_rebuilds_total;
static _Atomic uint64_t net_poll_allocs_total;
static _Atomic uint64_t net_poll_dedup_checks_total;
static _Atomic uint64_t net_waiter_complete_calls_total;
static _Atomic uint64_t net_waiter_completed_total;

#define NET_TRACE_DUMP_FORMAT                                                                      \
    "TRACE_NET reason=%s io_poll_calls=%llu io_poll_timeouts=%llu "                                \
    "io_poll_wake_fd=%llu io_poll_net_ready=%llu io_poll_errors=%llu "                             \
    "io_poll_timeout_last_ms=%llu io_poll_timeout_max_ms=%llu "                                    \
    "io_poll_waiters_last=%llu io_poll_waiters_max=%llu "                                          \
    "io_poll_waiters_total=%llu io_direct_waits=%llu "                                             \
    "io_waiter_scan_entries=%llu io_waiter_net_entries=%llu "                                      \
    "io_poll_rebuilds=%llu io_poll_allocs=%llu io_poll_dedup_checks=%llu "                         \
    "io_waiter_complete_calls=%llu io_waiter_completed=%llu\n"
#define NET_TRACE_DUMP_ARGS(reason)                                                                \
    (reason), net_trace_load(&net_poll_calls_total), net_trace_load(&net_poll_timeouts_total),     \
        net_trace_load(&net_poll_wake_fd_total), net_trace_load(&net_poll_ready_total),            \
        net_trace_load(&net_poll_errors_total), net_trace_load(&net_poll_timeout_last_ms),         \
        net_trace_load(&net_poll_timeout_max_ms), net_trace_load(&net_poll_waiters_last),          \
        net_trace_load(&net_poll_waiters_max), net_trace_load(&net_poll_waiters_total),            \
        net_trace_load(&net_direct_wait_total), net_trace_load(&net_waiter_scan_entries_total),    \
        net_trace_load(&net_waiter_net_entries_total), net_trace_load(&net_poll_rebuilds_total),   \
        net_trace_load(&net_poll_allocs_total), net_trace_load(&net_poll_dedup_checks_total),      \
        net_trace_load(&net_waiter_complete_calls_total),                                          \
        net_trace_load(&net_waiter_completed_total)

static unsigned long long net_trace_load(const _Atomic uint64_t* counter) {
    return (unsigned long long)atomic_load_explicit(counter, memory_order_relaxed);
}

static void net_trace_inc(_Atomic uint64_t* counter) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    (void)atomic_fetch_add_explicit(counter, 1, memory_order_relaxed);
}

static void net_trace_add(_Atomic uint64_t* counter, uint64_t value) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    (void)atomic_fetch_add_explicit(counter, value, memory_order_relaxed);
}

static void net_trace_store(_Atomic uint64_t* counter, uint64_t value) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    atomic_store_explicit(counter, value, memory_order_relaxed);
}

void rt_net_trace_dump(const char* reason) {
    if (reason == NULL || reason[0] == '\0') {
        reason = "unknown";
    }
    (void)dprintf(STDERR_FILENO, NET_TRACE_DUMP_FORMAT, NET_TRACE_DUMP_ARGS(reason));
}

static uint64_t net_trace_timeout_ms(int timeout_ms) {
    if (timeout_ms <= 0) {
        return 0;
    }
    return (uint64_t)timeout_ms;
}

static void net_trace_max(_Atomic uint64_t* counter, uint64_t value) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    uint64_t current = atomic_load_explicit(counter, memory_order_relaxed);
    while (current < value &&
           !atomic_compare_exchange_weak_explicit(
               counter, &current, value, memory_order_relaxed, memory_order_relaxed)) {
    }
}

static size_t net_next_cap(size_t current, size_t want) {
    size_t next = current == 0 ? 8 : current;
    while (next < want) {
        if (next > SIZE_MAX / 2U) {
            return want;
        }
        next *= 2U;
    }
    return next;
}

static int net_set_nonblocking_cloexec(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0 || fcntl(fd, F_SETFL, flags | O_NONBLOCK) < 0) {
        return 0;
    }
    flags = fcntl(fd, F_GETFD, 0);
    if (flags < 0 || fcntl(fd, F_SETFD, flags | FD_CLOEXEC) < 0) {
        return 0;
    }
    return 1;
}

static int net_poll_wake_init(void) {
    if (net_poll_wake_read_fd >= 0 && net_poll_wake_write_fd >= 0) {
        return 1;
    }
    int fds[2] = {-1, -1};
    if (pipe(fds) != 0) {
        return 0;
    }
    if (!net_set_nonblocking_cloexec(fds[0]) || !net_set_nonblocking_cloexec(fds[1])) {
        close(fds[0]);
        close(fds[1]);
        return 0;
    }
    net_poll_wake_read_fd = fds[0];
    net_poll_wake_write_fd = fds[1];
    return 1;
}

void rt_net_wake_poll(void) {
    if (net_poll_wake_write_fd < 0) {
        return;
    }
    uint8_t byte = 1;
    ssize_t n = -1;
    do {
        n = write(net_poll_wake_write_fd, &byte, 1);
    } while (n < 0 && errno == EINTR);
    (void)n;
}

static void net_poll_wake_drain(void) {
    if (net_poll_wake_read_fd < 0) {
        return;
    }
    uint8_t buf[64];
    for (;;) {
        ssize_t n = read(net_poll_wake_read_fd, buf, sizeof(buf));
        if (n > 0) {
            continue;
        }
        if (n < 0 && errno == EINTR) {
            continue;
        }
        break;
    }
}

static void net_trace_waiter_completion(rt_fd_completion_summary summary) {
    net_trace_add(&net_waiter_complete_calls_total, summary.calls);
    net_trace_add(&net_waiter_completed_total, summary.woken);
}

static int
ensure_net_poll_fds(rt_net_poll_scratch* scratch, size_t want, rt_fd_poll_interest** out) {
    if (scratch == NULL || out == NULL) {
        return 0;
    }
    if (scratch->fds_cap < want) {
        size_t next_cap = net_next_cap(scratch->fds_cap, want);
        rt_fd_poll_interest* next = (rt_fd_poll_interest*)rt_realloc(
            (uint8_t*)scratch->fds,
            (uint64_t)scratch->fds_cap * (uint64_t)sizeof(rt_fd_poll_interest),
            (uint64_t)next_cap * (uint64_t)sizeof(rt_fd_poll_interest),
            _Alignof(rt_fd_poll_interest));
        if (next == NULL) {
            return 0;
        }
        scratch->fds = next;
        scratch->fds_cap = next_cap;
        net_trace_inc(&net_poll_allocs_total);
    }
    *out = (rt_fd_poll_interest*)scratch->fds;
    return 1;
}

static int ensure_net_poll_pfds(rt_net_poll_scratch* scratch, size_t want, struct pollfd** out) {
    if (scratch == NULL || out == NULL) {
        return 0;
    }
    if (scratch->pfds_cap < want) {
        size_t next_cap = net_next_cap(scratch->pfds_cap, want);
        struct pollfd* next = (struct pollfd*)rt_realloc(
            (uint8_t*)scratch->pfds,
            (uint64_t)scratch->pfds_cap * (uint64_t)sizeof(struct pollfd),
            (uint64_t)next_cap * (uint64_t)sizeof(struct pollfd),
            _Alignof(struct pollfd));
        if (next == NULL) {
            return 0;
        }
        scratch->pfds = next;
        scratch->pfds_cap = next_cap;
        net_trace_inc(&net_poll_allocs_total);
    }
    *out = (struct pollfd*)scratch->pfds;
    return 1;
}

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
    return *(const NetListener* const*)listener;
}

static NetListener* net_listener_from_value(void* listener) {
    if (listener == NULL) {
        return NULL;
    }
    return (NetListener*)listener;
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

    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) {
        return net_make_error(net_error_code_from_errno(errno));
    }
    int reuse = 1;
    if (setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &reuse, (socklen_t)sizeof(reuse)) != 0) {
        uint64_t code = net_error_code_from_errno(errno);
        close(fd);
        return net_make_error(code);
    }
    if (!net_set_nonblocking(fd, &err_code)) {
        close(fd);
        return net_make_error(err_code == 0 ? NET_ERR_IO : err_code);
    }

    struct sockaddr_in sa;
    memset(&sa, 0, sizeof(sa));
    sa.sin_family = AF_INET;
    sa.sin_port = htons((uint16_t)port);
    sa.sin_addr = ip;
    if (bind(fd, (struct sockaddr*)&sa, sizeof(sa)) != 0) {
        uint64_t code = net_error_code_from_errno(errno);
        close(fd);
        return net_make_error(code);
    }
    if (listen(fd, SOMAXCONN) != 0) {
        uint64_t code = net_error_code_from_errno(errno);
        close(fd);
        return net_make_error(code);
    }

    NetListener* listener =
        (NetListener*)rt_alloc((uint64_t)sizeof(NetListener), (uint64_t)alignof(NetListener));
    if (listener == NULL) {
        close(fd);
        return net_make_error(NET_ERR_IO);
    }
    listener->fd = fd;
    listener->closed = false;
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

    NetConn* conn = (NetConn*)rt_alloc((uint64_t)sizeof(NetConn), (uint64_t)alignof(NetConn));
    if (conn == NULL) {
        close(fd);
        return net_make_error(NET_ERR_IO);
    }
    conn->fd = fd;
    conn->closed = false;
    return net_make_success_ptr(conn);
}

static void* close_net_fd_slot(int* fd_slot, bool* closed_slot) {
    if (fd_slot == NULL || closed_slot == NULL || *closed_slot) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    int fd = *fd_slot;
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return net_make_error(NET_ERR_IO);
    }
    rt_fd_lifecycle_snapshot snapshot;
    rt_lock(ex);
    rt_runtime_status status =
        rt_fd_registry_mark_closed(rt_executor_fd_registry(ex), fd, &snapshot);
    rt_unlock(ex);
    if (status != RT_RUNTIME_STATUS_OK) {
        return net_make_error(NET_ERR_IO);
    }
    *closed_slot = true;
    *fd_slot = -1;
    int close_errno = 0;
    if (close(fd) != 0) {
        close_errno = errno;
    }
    net_trace_waiter_completion(rt_fd_registry_wake_closed_net_waiters(ex, &snapshot));
    if (close_errno != 0) {
        return net_make_error(net_error_code_from_errno(close_errno));
    }
    return net_make_success_nothing();
}

void* rt_net_close_listener(void* listener) {
    NetListener* l = net_listener_from_value(listener);
    return l == NULL ? net_make_error(NET_ERR_NOT_CONNECTED)
                     : close_net_fd_slot(&l->fd, &l->closed);
}

void* rt_net_close_conn(void* conn) {
    NetConn* c = net_conn_from_value(conn);
    return c == NULL ? net_make_error(NET_ERR_NOT_CONNECTED)
                     : close_net_fd_slot(&c->fd, &c->closed);
}

void* rt_net_accept(const void* listener) {
    const NetListener* l = net_listener_from_borrowed(listener);
    if (l == NULL || l->closed) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    int fd = -1;
    do {
        fd = accept(l->fd, NULL, NULL);
    } while (fd < 0 && errno == EINTR);
    if (fd < 0) {
        return net_make_error(net_error_code_from_errno(errno));
    }
    uint64_t err_code = 0;
    if (!net_prepare_conn_fd(fd, &err_code)) {
        close(fd);
        return net_make_error(err_code == 0 ? NET_ERR_IO : err_code);
    }
    NetConn* conn = (NetConn*)rt_alloc((uint64_t)sizeof(NetConn), (uint64_t)alignof(NetConn));
    if (conn == NULL) {
        close(fd);
        return net_make_error(NET_ERR_IO);
    }
    conn->fd = fd;
    conn->closed = false;
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

static bool net_fd_ready_now(int fd, NetWaitKind kind) {
    if (fd < 0) {
        return true;
    }
    short events = POLLIN;
    short ready_mask = POLLIN | POLLERR | POLLHUP | POLLNVAL;
    if (kind == NET_WAIT_WRITE) {
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

static bool net_wait_current_task(int fd, NetWaitKind kind) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return true;
    }
    rt_lock(ex);
    rt_task* task = rt_current_task();
    if (task == NULL || rt_current_task_id() == 0) {
        rt_unlock(ex);
        panic_msg("async net wait outside task");
        return true;
    }
    if (current_task_cancelled(ex)) {
        pending_key = waker_none();
        rt_unlock(ex);
        return false;
    }
    if (fd < 0 || net_fd_ready_now(fd, kind)) {
        rt_unlock(ex);
        return true;
    }
    waker_key key;
    switch (kind) {
        case NET_WAIT_ACCEPT:
            key = net_accept_key(fd);
            break;
        case NET_WAIT_READ:
            key = net_read_key(fd);
            break;
        case NET_WAIT_WRITE:
            key = net_write_key(fd);
            break;
        default:
            rt_unlock(ex);
            return true;
    }
    if (!waker_valid(key)) {
        rt_unlock(ex);
        return true;
    }
    net_trace_inc(&net_direct_wait_total);
    prepare_park(ex, task, key, 0);
    if (!rt_fd_registry_net_interest_present(rt_executor_fd_registry_const(ex), key)) {
        // Attach failed or closed the row: undo the park so the rowless waiter
        // cannot be lost now that poll input is registry-only.
        remove_waiter(ex, key, task->id);
        task->park_prepared = 0;
        task->park_key = waker_none();
        pending_key = waker_none();
        rt_unlock(ex);
        return true;
    }
    pending_key = key;
    rt_unlock(ex);
    return false;
}

bool rt_net_wait_accept(const void* listener) {
    const NetListener* l = net_listener_from_borrowed(listener);
    int fd = -1;
    if (l != NULL && !l->closed) {
        fd = l->fd;
    }
    return net_wait_current_task(fd, NET_WAIT_ACCEPT);
}

bool rt_net_wait_readable(const void* conn) {
    const NetConn* c = net_conn_from_borrowed(conn);
    int fd = -1;
    if (c != NULL && !c->closed) {
        fd = c->fd;
    }
    return net_wait_current_task(fd, NET_WAIT_READ);
}

bool rt_net_wait_writable(const void* conn) {
    const NetConn* c = net_conn_from_borrowed(conn);
    int fd = -1;
    if (c != NULL && !c->closed) {
        fd = c->fd;
    }
    return net_wait_current_task(fd, NET_WAIT_WRITE);
}

int poll_net_waiters(rt_executor* ex, int timeout_ms) {
    // Caller must hold ex->lock; this function releases it while polling.
    // The registry snapshot copied under ex->lock is the only guarded poll input.
    const rt_fd_registry* registry = rt_executor_fd_registry_const(ex);
    size_t cap = rt_fd_registry_len(registry);
    if (cap == 0) {
        return 0;
    }
    rt_net_poll_scratch* scratch = rt_executor_net_poll_scratch(ex);
    rt_fd_poll_interest* fds = NULL;
    if (!ensure_net_poll_fds(scratch, cap, &fds)) {
        return 0;
    }
    size_t count = rt_fd_registry_snapshot_poll_interest(registry, fds, cap);
    if (count == 0) {
        return 0;
    }
    int wake_fd = net_poll_wake_init() ? net_poll_wake_read_fd : -1;
    size_t wake_count = wake_fd >= 0 ? 1U : 0U;
    size_t poll_count = count + wake_count;
    if (poll_count < count || poll_count > (size_t)((nfds_t)-1)) {
        return 0;
    }
    struct pollfd* pfds = NULL;
    if (!ensure_net_poll_pfds(scratch, poll_count, &pfds)) {
        return 0;
    }
    net_trace_inc(&net_poll_rebuilds_total);
    net_trace_inc(&net_poll_calls_total);
    uint64_t requested_timeout_ms = net_trace_timeout_ms(timeout_ms);
    net_trace_store(&net_poll_timeout_last_ms, requested_timeout_ms);
    net_trace_max(&net_poll_timeout_max_ms, requested_timeout_ms);
    net_trace_store(&net_poll_waiters_last, (uint64_t)count);
    net_trace_max(&net_poll_waiters_max, (uint64_t)count);
    net_trace_add(&net_poll_waiters_total, (uint64_t)count);

    size_t offset = 0;
    if (wake_fd >= 0) {
        pfds[0].fd = wake_fd;
        pfds[0].events = POLLIN;
        pfds[0].revents = 0;
        offset = 1;
    }
    for (size_t i = 0; i < count; i++) {
        size_t poll_idx = offset + i;
        pfds[poll_idx].fd = fds[i].fd;
        pfds[poll_idx].events = 0;
        pfds[poll_idx].revents = 0;
        if (fds[i].want_accept || fds[i].want_read) {
            pfds[poll_idx].events |= POLLIN;
        }
        if (fds[i].want_write) {
            pfds[poll_idx].events |= POLLOUT;
        }
    }

    rt_unlock(ex);
    int n = -1;
    nfds_t nfds = (nfds_t)poll_count;
    do {
        n = poll(pfds, nfds, timeout_ms);
    } while (n < 0 && errno == EINTR);
    rt_lock(ex);
    if (n < 0) {
        net_trace_inc(&net_poll_errors_total);
        for (size_t i = 0; i < count; i++) {
            net_trace_waiter_completion(
                rt_fd_registry_complete_ready_net_waiters(ex, &fds[i], 1, 1));
        }
        return 1;
    }
    if (n == 0) {
        net_trace_inc(&net_poll_timeouts_total);
        return 0;
    }

    int woke = 0;
    if (wake_fd >= 0 && pfds[0].revents != 0) {
        net_poll_wake_drain();
        net_trace_inc(&net_poll_wake_fd_total);
        woke = 1;
    }
    for (size_t i = 0; i < count; i++) {
        size_t poll_idx = offset + i;
        if (pfds[poll_idx].revents == 0) {
            continue;
        }
        bool read_ready = (pfds[poll_idx].revents & (POLLIN | POLLERR | POLLHUP | POLLNVAL)) != 0;
        bool write_ready = (pfds[poll_idx].revents & (POLLOUT | POLLERR | POLLHUP | POLLNVAL)) != 0;
        rt_fd_completion_summary completion =
            rt_fd_registry_complete_ready_net_waiters(ex, &fds[i], read_ready, write_ready);
        if (read_ready) {
            net_trace_inc(&net_poll_ready_total);
            woke = 1;
        }
        if (write_ready) {
            net_trace_inc(&net_poll_ready_total);
            woke = 1;
        }
        net_trace_waiter_completion(completion);
    }

    return woke;
}
