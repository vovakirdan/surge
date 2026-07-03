#ifndef SURGE_RUNTIME_NATIVE_RT_NET_LISTENER_SOCKET_H
#define SURGE_RUNTIME_NATIVE_RT_NET_LISTENER_SOCKET_H

#include <netinet/in.h>
#include <stdint.h>

int rt_net_create_listener_socket(const struct in_addr* ip,
                                  uint16_t requested_port,
                                  uint16_t* bound_port,
                                  int reuse_port,
                                  int* out_errno);

#endif
