#include "rt.h"

#include <stdlib.h>
#include <string.h>

static uint64_t min_u64(uint64_t a, uint64_t b) {
    return a < b ? a : b;
}

void* rt_alloc(uint64_t size, uint64_t align) {
    if (size == 0) {
        size = 1;
    }
    if (align <= sizeof(void*)) {
        return malloc((size_t)size);
    }
    void* ptr = NULL;
    if (posix_memalign(&ptr, (size_t)align, (size_t)size) != 0) {
        return NULL;
    }
    return ptr;
}

void rt_free(uint8_t* ptr, uint64_t size, uint64_t align) {
    (void)size;
    (void)align;
    free(ptr);
}

void* rt_realloc(uint8_t* ptr, uint64_t old_size, uint64_t new_size, uint64_t align) {
    if (new_size == 0) {
        rt_free(ptr, old_size, align);
        return NULL;
    }
    if (align <= sizeof(void*)) {
        return realloc(ptr, (size_t)new_size);
    }
    void* next = rt_alloc(new_size, align);
    if (next == NULL) {
        return NULL;
    }
    if (ptr != NULL && old_size > 0) {
        rt_memcpy((uint8_t*)next, ptr, min_u64(old_size, new_size));
        rt_free(ptr, old_size, align);
    }
    return next;
}

void rt_memcpy(uint8_t* dst, const uint8_t* src, uint64_t n) {
    if (n == 0) {
        return;
    }
    (void)memcpy(dst, src, (size_t)n);
}

void rt_memmove(uint8_t* dst, const uint8_t* src, uint64_t n) {
    if (n == 0) {
        return;
    }
    (void)memmove(dst, src, (size_t)n);
}
