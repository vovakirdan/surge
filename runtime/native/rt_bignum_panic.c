#include "rt_bignum_internal.h"

#include <stdio.h>
#include <string.h>
#include <unistd.h>

// Panic messages must match VM formatting: "panic VM<code>: <msg>".
static void panic_with_code(int code, const char* msg) {
    if (msg == NULL) {
        msg = "invalid numeric conversion";
    }
    char buf[256];
    int n = snprintf(buf, sizeof(buf), "panic VM%d: %s\n", code, msg);
    if (n < 0) {
        const uint8_t fallback[] = "panic VM3202: invalid numeric conversion\n";
        rt_write_stderr(fallback, (uint64_t)(sizeof(fallback) - 1));
        _exit(1);
    }
    if (n >= (int)sizeof(buf)) {
        n = (int)sizeof(buf) - 1;
    }
    rt_write_stderr((const uint8_t*)buf, (uint64_t)n);
    _exit(1);
}

void bignum_panic(const char* msg) {
    if (msg == NULL) {
        msg = "invalid numeric conversion";
    }
    int code = 3202;
    if (strcmp(msg, "numeric size limit exceeded") == 0) {
        code = 3201;
    } else if (strcmp(msg, "division by zero") == 0) {
        code = 3203;
    } else if (strcmp(msg, "integer overflow") == 0) {
        code = 1101;
    }
    panic_with_code(code, msg);
}

void bignum_panic_err(bn_err err) {
    switch (err) {
        case BN_OK:
            return;
        case BN_ERR_MAX_LIMBS:
            bignum_panic("numeric size limit exceeded");
            return;
        case BN_ERR_DIV_ZERO:
            bignum_panic("division by zero");
            return;
        case BN_ERR_UNDERFLOW:
            bignum_panic("unsigned underflow");
            return;
        case BN_ERR_NEG_SHIFT:
            bignum_panic("negative shift");
            return;
        default:
            bignum_panic("invalid numeric conversion");
            return;
    }
}
