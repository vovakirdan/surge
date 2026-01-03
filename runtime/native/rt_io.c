#include "rt.h"

#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

uint64_t rt_write_stdout(const uint8_t* ptr, uint64_t length) {
    if (ptr == NULL || length == 0) {
        return 0;
    }
    uint64_t written = 0;
    while (written < length) {
        ssize_t chunk = write(STDOUT_FILENO, ptr + written, (size_t)(length - written));
        if (chunk <= 0) {
            break;
        }
        written += (uint64_t)chunk;
    }
    return written;
}

uint64_t rt_write_stderr(const uint8_t* ptr, uint64_t length) {
    if (ptr == NULL || length == 0) {
        return 0;
    }
    uint64_t written = 0;
    while (written < length) {
        ssize_t chunk = write(STDERR_FILENO, ptr + written, (size_t)(length - written));
        if (chunk <= 0) {
            break;
        }
        written += (uint64_t)chunk;
    }
    return written;
}

void rt_exit(int64_t code) {
    exit((int)code);
}

void rt_panic(const uint8_t* ptr, uint64_t length) {
    static const uint8_t prefix[] = "panic: ";
    rt_write_stderr(prefix, (uint64_t)(sizeof(prefix) - 1));
    if (ptr != NULL && length > 0) {
        rt_write_stderr(ptr, length);
        if (ptr[length - 1] != '\n') {
            rt_write_stderr((const uint8_t*)"\n", 1);
        }
    } else {
        rt_write_stderr((const uint8_t*)"\n", 1);
    }
    _exit(1);
}

void rt_panic_bounds(uint64_t kind, int64_t index, int64_t length) {
    const char* code = "VM1004";
    const char* fmt = "panic %s: index %" PRId64 " out of bounds for length %" PRId64 "\n";
    if (kind == 1) {
        code = "VM2105";
        fmt = "panic %s: array index %" PRId64 " out of range for length %" PRId64 "\n";
    }
    char buf[128];
    int n = snprintf(buf, sizeof(buf), fmt, code, index, length);
    if (n < 0) {
        const uint8_t fallback[] = "panic VM1004: bounds check failed\n";
        rt_write_stderr(fallback, (uint64_t)(sizeof(fallback) - 1));
        _exit(1);
    }
    if (n >= (int)sizeof(buf)) {
        n = (int)sizeof(buf) - 1;
    }
    rt_write_stderr((const uint8_t*)buf, (uint64_t)n);
    _exit(1);
}
