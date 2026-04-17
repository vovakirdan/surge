#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"

#include <errno.h>
#include <fcntl.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

#if defined(__linux__)
#include <sys/random.h>
#endif

enum {
    ENTROPY_ERR_UNAVAILABLE = 1,
    ENTROPY_ERR_BACKEND = 2,
};

typedef struct EntropyError {
    void* message;
    void* code;
} EntropyError;

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

static const char* entropy_error_message(uint64_t code) {
    switch (code) {
        case ENTROPY_ERR_UNAVAILABLE:
            return "Unavailable";
        default:
            return "Backend";
    }
}

static void* entropy_make_error(uint64_t code) {
    EntropyError* err =
        (EntropyError*)rt_alloc((uint64_t)sizeof(EntropyError), (uint64_t)alignof(EntropyError));
    if (err == NULL) {
        return NULL;
    }
    const char* msg = entropy_error_message(code);
    err->message = rt_string_from_bytes((const uint8_t*)msg, (uint64_t)strlen(msg));
    err->code = rt_biguint_from_u64(code);
    return (void*)err;
}

static void* entropy_make_success_bytes(const uint8_t* bytes, uint64_t len) {
    void* data = NULL;
    if (len > 0) {
        data = rt_alloc(len, (uint64_t)alignof(uint8_t));
        if (data == NULL) {
            return entropy_make_error(ENTROPY_ERR_BACKEND);
        }
        memcpy(data, bytes, (size_t)len);
    }

    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        if (data != NULL) {
            rt_free((uint8_t*)data, len, (uint64_t)alignof(uint8_t));
        }
        return entropy_make_error(ENTROPY_ERR_BACKEND);
    }
    header->len = len;
    header->cap = len;
    header->data = data;

    size_t payload_align = alignof(EntropyError);
    size_t payload_size = sizeof(EntropyError);
    if (payload_size < sizeof(void*)) {
        payload_size = sizeof(void*);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = (uint8_t*)rt_tag_alloc(0, payload_align, payload_size);
    if (mem == NULL) {
        if (data != NULL) {
            rt_free((uint8_t*)data, len, (uint64_t)alignof(uint8_t));
        }
        rt_free((uint8_t*)header,
                (uint64_t)sizeof(SurgeArrayHeader),
                (uint64_t)alignof(SurgeArrayHeader));
        return entropy_make_error(ENTROPY_ERR_BACKEND);
    }
    void* payload = (void*)header;
    memcpy(mem + payload_offset, (const void*)&payload, sizeof(payload));
    return mem;
}

static uint64_t entropy_err_from_errno(int err) {
    switch (err) {
        case ENOENT:
        case ENODEV:
        case ENXIO:
        case ENOSYS:
            return ENTROPY_ERR_UNAVAILABLE;
        default:
            return ENTROPY_ERR_BACKEND;
    }
}

static bool entropy_fill_urandom(uint8_t* out, uint64_t len, uint64_t* err_code) {
    int fd = open("/dev/urandom", O_RDONLY);
    if (fd < 0) {
        if (err_code != NULL) {
            *err_code = entropy_err_from_errno(errno);
        }
        return false;
    }
    uint64_t off = 0;
    while (off < len) {
        ssize_t n = read(fd, out + off, (size_t)(len - off));
        if (n < 0) {
            if (errno == EINTR) {
                continue;
            }
            if (err_code != NULL) {
                *err_code = entropy_err_from_errno(errno);
            }
            close(fd);
            return false;
        }
        if (n == 0) {
            if (err_code != NULL) {
                *err_code = ENTROPY_ERR_UNAVAILABLE;
            }
            close(fd);
            return false;
        }
        off += (uint64_t)n;
    }
    close(fd);
    return true;
}

#if defined(__linux__)
static bool entropy_fill_linux(uint8_t* out, uint64_t len, uint64_t* err_code) {
    uint64_t off = 0;
    while (off < len) {
        ssize_t n = getrandom(out + off, (size_t)(len - off), 0);
        if (n < 0) {
            if (errno == EINTR) {
                continue;
            }
            if (errno == ENOSYS) {
                return entropy_fill_urandom(out + off, len - off, err_code);
            }
            if (err_code != NULL) {
                *err_code = entropy_err_from_errno(errno);
            }
            return false;
        }
        if (n == 0) {
            if (err_code != NULL) {
                *err_code = ENTROPY_ERR_UNAVAILABLE;
            }
            return false;
        }
        off += (uint64_t)n;
    }
    return true;
}
#endif

void* rt_entropy_bytes(uint64_t len) {
    if (len == 0) {
        return entropy_make_success_bytes(NULL, 0);
    }

    uint8_t* tmp = (uint8_t*)malloc((size_t)len);
    if (tmp == NULL) {
        return entropy_make_error(ENTROPY_ERR_BACKEND);
    }

    uint64_t err_code = ENTROPY_ERR_BACKEND;
    bool ok = false;

#if defined(__APPLE__) || defined(__FreeBSD__) || defined(__OpenBSD__) || defined(__NetBSD__)
    arc4random_buf(tmp, (size_t)len);
    ok = true;
#elif defined(__linux__)
    ok = entropy_fill_linux(tmp, len, &err_code);
#else
    ok = entropy_fill_urandom(tmp, len, &err_code);
#endif

    if (!ok) {
        free(tmp);
        return entropy_make_error(err_code);
    }

    void* out = entropy_make_success_bytes(tmp, len);
    free(tmp);
    return out;
}
