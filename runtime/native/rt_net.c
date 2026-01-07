#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <netinet/in.h>
#include <poll.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
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

typedef struct NetPollFd {
    int fd;
    uint8_t want_read;
    uint8_t want_write;
} NetPollFd;

static size_t net_align_up(size_t n, size_t align) {
    if (align <= 1) {
        return n;
    }
    size_t r = n % align;
    if (r == 0) {
        return n;
    }
    return n + (align - r);
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
    size_t payload_offset = net_align_up(4, payload_align);
    size_t size = net_align_up(payload_offset + payload_size, payload_align);
    uint8_t* mem = (uint8_t*)rt_alloc((uint64_t)size, (uint64_t)payload_align);
    if (mem == NULL) {
        return NULL;
    }
    *(uint32_t*)mem = 0;
    *(void**)(mem + payload_offset) = payload;
    return mem;
}

static void* net_make_success_nothing(void) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(NetError);
    size_t payload_offset = net_align_up(4, payload_align);
    size_t size = net_align_up(payload_offset + payload_size, payload_align);
    uint8_t* mem = (uint8_t*)rt_alloc((uint64_t)size, (uint64_t)payload_align);
    if (mem == NULL) {
        return NULL;
    }
    *(uint32_t*)mem = 0;
    mem[payload_offset] = 0;
    return mem;
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

    NetListener* listener = (NetListener*)rt_alloc((uint64_t)sizeof(NetListener), (uint64_t)alignof(NetListener));
    if (listener == NULL) {
        close(fd);
        return net_make_error(NET_ERR_IO);
    }
    listener->fd = fd;
    listener->closed = false;
    return net_make_success_ptr((void*)listener);
}

void* rt_net_close_listener(void* listener) {
    NetListener* l = (NetListener*)listener;
    if (l == NULL || l->closed) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    l->closed = true;
    int fd = l->fd;
    l->fd = -1;
    if (close(fd) != 0) {
        return net_make_error(net_error_code_from_errno(errno));
    }
    return net_make_success_nothing();
}

void* rt_net_close_conn(void* conn) {
    NetConn* c = (NetConn*)conn;
    if (c == NULL || c->closed) {
        return net_make_error(NET_ERR_NOT_CONNECTED);
    }
    c->closed = true;
    int fd = c->fd;
    c->fd = -1;
    if (close(fd) != 0) {
        return net_make_error(net_error_code_from_errno(errno));
    }
    return net_make_success_nothing();
}

void* rt_net_accept(void* listener) {
    NetListener* l = (NetListener*)listener;
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
    if (!net_set_nonblocking(fd, &err_code)) {
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
    return net_make_success_ptr((void*)conn);
}

void* rt_net_read(void* conn, uint8_t* buf, uint64_t cap) {
    NetConn* c = (NetConn*)conn;
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

void* rt_net_write(void* conn, const uint8_t* buf, uint64_t len) {
    NetConn* c = (NetConn*)conn;
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

static void* net_spawn_wait_task(int fd, uint8_t kind) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        panic_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->status = TASK_READY;
    task->kind = kind;
    task->net_fd = fd;
    task->handle_refs = 1;
    ex->tasks[id] = task;
    ready_push(ex, id);
    return task;
}

void* rt_net_wait_accept(void* listener) {
    NetListener* l = (NetListener*)listener;
    int fd = -1;
    if (l != NULL && !l->closed) {
        fd = l->fd;
    }
    return net_spawn_wait_task(fd, TASK_KIND_NET_ACCEPT);
}

void* rt_net_wait_readable(void* conn) {
    NetConn* c = (NetConn*)conn;
    int fd = -1;
    if (c != NULL && !c->closed) {
        fd = c->fd;
    }
    return net_spawn_wait_task(fd, TASK_KIND_NET_READ);
}

void* rt_net_wait_writable(void* conn) {
    NetConn* c = (NetConn*)conn;
    int fd = -1;
    if (c != NULL && !c->closed) {
        fd = c->fd;
    }
    return net_spawn_wait_task(fd, TASK_KIND_NET_WRITE);
}

poll_outcome poll_net_task(const rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task->cancelled) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    int fd = task->net_fd;
    if (fd < 0) {
        out.kind = POLL_DONE_SUCCESS;
        return out;
    }
    short events = POLLIN;
    short ready_mask = POLLIN | POLLERR | POLLHUP;
    if (task->kind == TASK_KIND_NET_WRITE) {
        events = POLLOUT;
        ready_mask = POLLOUT | POLLERR | POLLHUP;
    }
    struct pollfd pfd;
    memset(&pfd, 0, sizeof(pfd));
    pfd.fd = fd;
    pfd.events = events;
    int n = poll(&pfd, 1, 0);
    if (n < 0) {
        if (errno == EINTR) {
            n = 0;
        } else {
            out.kind = POLL_DONE_SUCCESS;
            return out;
        }
    }
    if (n == 0) {
        out.kind = POLL_PARKED;
        switch (task->kind) {
            case TASK_KIND_NET_ACCEPT:
                out.park_key = net_accept_key(fd);
                break;
            case TASK_KIND_NET_READ:
                out.park_key = net_read_key(fd);
                break;
            case TASK_KIND_NET_WRITE:
                out.park_key = net_write_key(fd);
                break;
            default:
                out.park_key = waker_none();
                break;
        }
        return out;
    }
    if (pfd.revents & ready_mask) {
        out.kind = POLL_DONE_SUCCESS;
        return out;
    }
    out.kind = POLL_PARKED;
    switch (task->kind) {
        case TASK_KIND_NET_ACCEPT:
            out.park_key = net_accept_key(fd);
            break;
        case TASK_KIND_NET_READ:
            out.park_key = net_read_key(fd);
            break;
        case TASK_KIND_NET_WRITE:
            out.park_key = net_write_key(fd);
            break;
        default:
            out.park_key = waker_none();
            break;
    }
    return out;
}

int poll_net_waiters(rt_executor* ex, int timeout_ms) {
    if (ex == NULL || ex->waiters_len == 0) {
        return 0;
    }
    size_t cap = ex->waiters_len;
    NetPollFd* fds = (NetPollFd*)rt_alloc((uint64_t)(cap * sizeof(NetPollFd)), _Alignof(NetPollFd));
    if (fds == NULL) {
        return 0;
    }
    size_t count = 0;
    for (size_t i = 0; i < ex->waiters_len; i++) {
        waiter w = ex->waiters[i];
        uint8_t kind = w.key.kind;
        if (kind != WAKER_NET_ACCEPT && kind != WAKER_NET_READ && kind != WAKER_NET_WRITE) {
            continue;
        }
        int fd = (int)w.key.id;
        if (fd <= 0) {
            continue;
        }
        size_t idx = count;
        for (size_t j = 0; j < count; j++) {
            if (fds[j].fd == fd) {
                idx = j;
                break;
            }
        }
        if (idx == count) {
            fds[idx] = (NetPollFd){fd, 0, 0};
            count++;
        }
        if (kind == WAKER_NET_WRITE) {
            fds[idx].want_write = 1;
        } else {
            fds[idx].want_read = 1;
        }
    }
    if (count == 0) {
        rt_free((uint8_t*)fds, (uint64_t)(cap * sizeof(NetPollFd)), _Alignof(NetPollFd));
        return 0;
    }

    struct pollfd* pfds = (struct pollfd*)rt_alloc((uint64_t)(count * sizeof(struct pollfd)),
                                                   _Alignof(struct pollfd));
    if (pfds == NULL) {
        rt_free((uint8_t*)fds, (uint64_t)(cap * sizeof(NetPollFd)), _Alignof(NetPollFd));
        return 0;
    }
    for (size_t i = 0; i < count; i++) {
        pfds[i].fd = fds[i].fd;
        pfds[i].events = 0;
        pfds[i].revents = 0;
        if (fds[i].want_read) {
            pfds[i].events |= POLLIN;
        }
        if (fds[i].want_write) {
            pfds[i].events |= POLLOUT;
        }
    }

    int n = -1;
    do {
        n = poll(pfds, count, timeout_ms);
    } while (n < 0 && errno == EINTR);
    if (n < 0) {
        for (size_t i = 0; i < count; i++) {
            if (fds[i].want_read) {
                wake_key_all(ex, net_read_key(fds[i].fd));
                wake_key_all(ex, net_accept_key(fds[i].fd));
            }
            if (fds[i].want_write) {
                wake_key_all(ex, net_write_key(fds[i].fd));
            }
        }
        rt_free((uint8_t*)pfds, (uint64_t)(count * sizeof(struct pollfd)), _Alignof(struct pollfd));
        rt_free((uint8_t*)fds, (uint64_t)(cap * sizeof(NetPollFd)), _Alignof(NetPollFd));
        return 1;
    }
    if (n == 0) {
        rt_free((uint8_t*)pfds, (uint64_t)(count * sizeof(struct pollfd)), _Alignof(struct pollfd));
        rt_free((uint8_t*)fds, (uint64_t)(cap * sizeof(NetPollFd)), _Alignof(NetPollFd));
        return 0;
    }

    int woke = 0;
    for (size_t i = 0; i < count; i++) {
        if (pfds[i].revents == 0) {
            continue;
        }
        bool read_ready = (pfds[i].revents & (POLLIN | POLLERR | POLLHUP)) != 0;
        bool write_ready = (pfds[i].revents & (POLLOUT | POLLERR | POLLHUP)) != 0;
        if (read_ready) {
            wake_key_all(ex, net_read_key(fds[i].fd));
            wake_key_all(ex, net_accept_key(fds[i].fd));
            woke = 1;
        }
        if (write_ready) {
            wake_key_all(ex, net_write_key(fds[i].fd));
            woke = 1;
        }
    }

    rt_free((uint8_t*)pfds, (uint64_t)(count * sizeof(struct pollfd)), _Alignof(struct pollfd));
    rt_free((uint8_t*)fds, (uint64_t)(cap * sizeof(NetPollFd)), _Alignof(NetPollFd));
    return woke;
}
