#include "rt.h"

#include <stddef.h>
#include <string.h>

// Tag-union layout: 32-bit tag followed by aligned payload.
static size_t rt_align_up(size_t n, size_t align) {
    if (align <= 1) {
        return n;
    }
    size_t r = n % align;
    if (r == 0) {
        return n;
    }
    return n + (align - r);
}

size_t rt_tag_payload_offset(size_t payload_align) {
    return rt_align_up(sizeof(uint32_t), payload_align);
}

void* rt_tag_alloc(uint32_t tag, size_t payload_align, size_t payload_size) {
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    size_t size = rt_align_up(payload_offset + payload_size, payload_align);
    if (size == 0) {
        size = 1;
    }
    size_t align = payload_align;
    if (align == 0) {
        align = 1;
    }
    uint8_t* mem = (uint8_t*)rt_alloc((uint64_t)size, (uint64_t)align);
    if (mem == NULL) {
        return NULL;
    }
    memset(mem, 0, size);
    memcpy(mem, &tag, sizeof(tag));
    return mem;
}

// The tagged twin of rt_alloc_or_report (rt_alloc.c): the result blocks the
// filesystem, socket, entropy and terminal entry points hand generated code
// are built here, and a refused one is reported here rather than answered
// as NULL. Rule 13 as there: RV2_DEBT_309_NEGATIVE_CONTROL hands the NULL
// back.
void* rt_tag_alloc_or_report(uint32_t tag,
                             size_t payload_align,
                             size_t payload_size,
                             const uint8_t* message,
                             uint64_t message_length) {
    void* mem = rt_tag_alloc(tag, payload_align, payload_size);
    if (mem == NULL) {
#ifdef RV2_DEBT_309_NEGATIVE_CONTROL
        (void)message;
        (void)message_length;
        return NULL;
#else
        rt_fatal_static(RT_OOM, message, message_length);
#endif
    }
    return mem;
}
