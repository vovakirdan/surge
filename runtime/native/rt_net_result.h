#ifndef SURGE_RUNTIME_NATIVE_RT_NET_RESULT_H
#define SURGE_RUNTIME_NATIVE_RT_NET_RESULT_H

#include <stdint.h>

// NetResult/NetError ABI constructors (split): this module
// owns the Surge-visible net error codes and the tag/payload construction
// for NetResult success and error values. Extracted verbatim from rt_net.c.

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

uint64_t net_error_code_from_errno(int err);
void* net_make_error(uint64_t code);
void* net_make_success_ptr(void* payload);
void* net_make_success_handle(uint64_t handle_id);
void* net_make_success_nothing(void);
void* net_make_success_bytes(uint8_t* data, uint64_t len, uint64_t cap);
char* net_copy_addr(void* addr, uint64_t* out_len, uint64_t* err_code);

#endif
