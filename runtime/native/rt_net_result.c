#include "rt_net_result.h"
#include "rt_async_internal.h"

#include <errno.h>
#include <stdlib.h>
#include <string.h>

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

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

uint64_t net_error_code_from_errno(int err) {
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

void* net_make_error(uint64_t code) {
    NetError* err = (NetError*)rt_alloc((uint64_t)sizeof(NetError), (uint64_t)alignof(NetError));
    if (err == NULL) {
        return NULL;
    }
    const char* msg = net_error_message(code);
    err->message = rt_string_from_bytes((const uint8_t*)msg, (uint64_t)strlen(msg));
    err->code = rt_biguint_from_u64(code);
    return (void*)err;
}

void* net_make_success_ptr(void* payload) {
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

void* net_make_success_nothing(void) {
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

void* net_make_success_bytes(uint8_t* data, uint64_t len, uint64_t cap) {
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

char* net_copy_addr(void* addr, uint64_t* out_len, uint64_t* err_code) {
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
