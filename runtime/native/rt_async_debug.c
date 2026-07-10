#include "rt_async_internal.h"

#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>

static int async_debug_enabled_cached = -1;

int rt_async_debug_enabled(void) {
    if (async_debug_enabled_cached >= 0) {
        return async_debug_enabled_cached;
    }
    const char* value = getenv("SURGE_ASYNC_DEBUG");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        async_debug_enabled_cached = 0;
        return 0;
    }
    async_debug_enabled_cached = 1;
    return 1;
}

void rt_async_debug_printf(const char* fmt, ...) {
    if (!rt_async_debug_enabled() || fmt == NULL) {
        return;
    }
    char buf[512];
    va_list args;
    va_start(args, fmt);
#if defined(__clang__)
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wformat-nonliteral"
#elif defined(__GNUC__)
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wformat-nonliteral"
#endif
    int n = vsnprintf(buf, sizeof(buf), fmt, args);
#if defined(__clang__)
#pragma clang diagnostic pop
#elif defined(__GNUC__)
#pragma GCC diagnostic pop
#endif
    va_end(args);
    if (n <= 0) {
        return;
    }
    uint64_t len = (uint64_t)n;
    if ((size_t)n >= sizeof(buf)) {
        len = (uint64_t)(sizeof(buf) - 1);
    }
    rt_write_stderr((const uint8_t*)buf, len);
}
