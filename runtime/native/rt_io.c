#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"

#include <ctype.h>
#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

extern int rt_argc;
extern char** rt_argv_raw;

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

void* rt_readline(void) {
    char* buf = NULL;
    size_t cap = 0;
    ssize_t n = getline(&buf, &cap, stdin);
    if (n <= 0) {
        free(buf);
        void* out = rt_string_from_bytes(NULL, 0);
        if (out == NULL) {
            const char* msg = "readline allocation failed";
            rt_panic((const uint8_t*)msg, (uint64_t)strlen(msg));
        }
        return out;
    }
    size_t len = (size_t)n;
    if (buf[len - 1] == '\n') {
        len--;
    }
    if (len > 0 && buf[len - 1] == '\r') {
        len--;
    }
    void* out = rt_string_from_bytes((const uint8_t*)buf, (uint64_t)len);
    free(buf);
    if (out == NULL) {
        const char* msg = "readline allocation failed";
        rt_panic((const uint8_t*)msg, (uint64_t)strlen(msg));
    }
    return out;
}

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

void* rt_argv(void) {
    int argc = rt_argc;
    char** argv = rt_argv_raw;
    int count = 0;
    if (argc > 1) {
        count = argc - 1;
    }
    void* data = NULL;
    if (count > 0) {
        data = rt_alloc((uint64_t)count * (uint64_t)sizeof(void*), (uint64_t)alignof(void*));
        if (data == NULL) {
            return NULL;
        }
    }
    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        return NULL;
    }
    header->len = (uint64_t)count;
    header->cap = (uint64_t)count;
    header->data = data;

    if (data != NULL && argv != NULL) {
        void** slots = (void**)data;
        for (int i = 0; i < count; i++) {
            const char* arg = argv[i + 1];
            if (arg == NULL) {
                slots[i] = rt_string_from_bytes(NULL, 0);
                continue;
            }
            size_t n = strlen(arg);
            slots[i] = rt_string_from_bytes((const uint8_t*)arg, (uint64_t)n);
        }
    }
    return (void*)header;
}

void* rt_stdin_read_all(void) {
    uint8_t* buf = NULL;
    size_t len = 0;
    size_t cap = 0;

    for (;;) {
        if (cap - len < 1024) {
            size_t next = cap == 0 ? 4096 : cap * 2;
            uint8_t* tmp = (uint8_t*)realloc(buf, next);
            if (tmp == NULL) {
                free(buf);
                return rt_string_from_bytes(NULL, 0);
            }
            buf = tmp;
            cap = next;
        }
        ssize_t n = read(STDIN_FILENO, buf + len, cap - len);
        if (n <= 0) {
            break;
        }
        len += (size_t)n;
    }

    size_t start = 0;
    size_t end = len;
    while (start < end && isspace((unsigned char)buf[start])) {
        start++;
    }
    while (end > start && isspace((unsigned char)buf[end - 1])) {
        end--;
    }

    void* out = rt_string_from_bytes(buf + start, (uint64_t)(end - start));
    free(buf);
    return out;
}

void rt_exit(int64_t code) {
    rt_sched_trace_dump();
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

void rt_panic_numeric(const uint8_t* ptr, uint64_t length) {
    static const uint8_t prefix[] = "panic VM3202: ";
    static const uint8_t fallback[] = "invalid numeric conversion";
    rt_write_stderr(prefix, (uint64_t)(sizeof(prefix) - 1));
    if (ptr != NULL && length > 0) {
        rt_write_stderr(ptr, length);
        if (ptr[length - 1] != '\n') {
            rt_write_stderr((const uint8_t*)"\n", 1);
        }
    } else {
        rt_write_stderr(fallback, (uint64_t)(sizeof(fallback) - 1));
        rt_write_stderr((const uint8_t*)"\n", 1);
    }
    _exit(1);
}

void rt_panic_bounds(uint64_t kind, int64_t index, int64_t length) {
    const char* code = "VM1004";
    if (kind == 1) {
        code = "VM2105";
    }
    char buf[128];
    int n = 0;
    if (kind == 1) {
        n = snprintf(buf,
                     sizeof(buf),
                     "panic %s: array index %" PRId64 " out of range for length %" PRId64 "\n",
                     code,
                     index,
                     length);
    } else {
        n = snprintf(buf,
                     sizeof(buf),
                     "panic %s: index %" PRId64 " out of bounds for length %" PRId64 "\n",
                     code,
                     index,
                     length);
    }
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
