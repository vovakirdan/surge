#ifndef _GNU_SOURCE
#define _GNU_SOURCE // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_net_listener_socket.h"

#include <errno.h>
#include <fcntl.h>
#include <stdbool.h>
#include <stddef.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

static void rt_net_listener_socket_error(int* out_errno) {
    if (out_errno != NULL) {
        *out_errno = errno;
    }
}

static int rt_net_set_nonblocking_cloexec_local(int fd) {
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

static int rt_net_set_listener_reuse(int fd, bool reuse_port, int* out_errno) {
    int reuse = 1;
    if (setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &reuse, (socklen_t)sizeof(reuse)) != 0) {
        rt_net_listener_socket_error(out_errno);
        return 0;
    }
    if (!reuse_port) {
        return 1;
    }
#ifdef SO_REUSEPORT
    if (setsockopt(fd, SOL_SOCKET, SO_REUSEPORT, &reuse, (socklen_t)sizeof(reuse)) != 0) {
        rt_net_listener_socket_error(out_errno);
        return 0;
    }
    return 1;
#else
    if (out_errno != NULL) {
        *out_errno = EOPNOTSUPP;
    }
    return 0;
#endif
}

int rt_net_create_listener_socket(const struct in_addr* ip,
                                  uint16_t requested_port,
                                  uint16_t* bound_port,
                                  int reuse_port,
                                  int* out_errno) {
    if (out_errno != NULL) {
        *out_errno = 0;
    }
    if (ip == NULL) {
        if (out_errno != NULL) {
            *out_errno = EINVAL;
        }
        return -1;
    }
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) {
        rt_net_listener_socket_error(out_errno);
        return -1;
    }
    if (!rt_net_set_listener_reuse(fd, reuse_port != 0, out_errno) ||
        !rt_net_set_nonblocking_cloexec_local(fd)) {
        if (out_errno != NULL && *out_errno == 0) {
            *out_errno = errno;
        }
        close(fd);
        return -1;
    }

    struct sockaddr_in sa;
    memset(&sa, 0, sizeof(sa));
    sa.sin_family = AF_INET;
    sa.sin_addr = *ip;
    sa.sin_port = htons(bound_port != NULL && *bound_port != 0 ? *bound_port : requested_port);
    if (bind(fd, (struct sockaddr*)&sa, sizeof(sa)) != 0) {
        rt_net_listener_socket_error(out_errno);
        close(fd);
        return -1;
    }
    if (bound_port != NULL && *bound_port == 0) {
        // Zeroed rather than left to getsockname alone: the analyser cannot see
        // that the kernel fills this on success, and a listener bind happens
        // once, so the sixteen bytes cost nothing and the read is defined even
        // if a platform ever fills the struct only partially.
        struct sockaddr_in actual = {0};
        socklen_t actual_len = (socklen_t)sizeof(actual);
        if (getsockname(fd, (struct sockaddr*)&actual, &actual_len) != 0) {
            rt_net_listener_socket_error(out_errno);
            close(fd);
            return -1;
        }
        *bound_port = ntohs(actual.sin_port);
    }
    if (listen(fd, SOMAXCONN) != 0) {
        rt_net_listener_socket_error(out_errno);
        close(fd);
        return -1;
    }
    return fd;
}
