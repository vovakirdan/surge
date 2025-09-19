#include "codebuf.h"
#include <stdlib.h>
#include <string.h>

static bool grow(CodeBuf *cb, uint32_t need) {
    uint32_t cap = cb->cap ? cb->cap : 64;
    while (cap < need) cap <<= 1;
    uint8_t *p = (uint8_t*)realloc(cb->data, cap);
    if (!p) return false;
    cb->data = p; cb->cap = cap; return true;
}

void codebuf_init(CodeBuf *cb) { memset(cb, 0, sizeof(*cb)); }
void codebuf_free(CodeBuf *cb) { free(cb->data); memset(cb, 0, sizeof(*cb)); }

bool codebuf_reserve(CodeBuf *cb, uint32_t add) {
    uint32_t need = cb->len + add;
    if (need <= cb->cap) return true;
    return grow(cb, need);
}

bool codebuf_emit_raw(CodeBuf *cb, const void *src, uint32_t n) {
    if (!codebuf_reserve(cb, n)) return false;
    memcpy(cb->data + cb->len, src, n);
    cb->len += n; return true;
}

bool codebuf_emit_u8(CodeBuf *cb, uint8_t v) {
    return codebuf_emit_raw(cb, &v, 1);
}
bool codebuf_emit_u16_le(CodeBuf *cb, uint16_t v) {
    uint8_t b[2] = { (uint8_t)(v), (uint8_t)(v>>8) };
    return codebuf_emit_raw(cb, b, 2);
}
bool codebuf_emit_u32_le(CodeBuf *cb, uint32_t v) {
    uint8_t b[4] = { (uint8_t)v, (uint8_t)(v>>8), (uint8_t)(v>>16), (uint8_t)(v>>24) };
    return codebuf_emit_raw(cb, b, 4);
}
bool codebuf_emit_i32_le(CodeBuf *cb, int32_t v) {
    return codebuf_emit_u32_le(cb, (uint32_t)v);
}
bool codebuf_emit_u64_le(CodeBuf *cb, uint64_t v) {
    uint8_t b[8];
    for (int i=0;i<8;i++) b[i]=(uint8_t)(v>>(i*8));
    return codebuf_emit_raw(cb, b, 8);
}

bool codebuf_emit_op0(CodeBuf *cb, uint8_t opcode) {
    return codebuf_emit_u8(cb, opcode);
}
bool codebuf_emit_op1(CodeBuf *cb, uint8_t opcode, uint64_t v, size_t bytes) {
    if (!codebuf_emit_u8(cb, opcode)) return false;
    switch (bytes) {
        case 1: return codebuf_emit_u8(cb, (uint8_t)v);
        case 2: return codebuf_emit_u16_le(cb, (uint16_t)v);
        case 4: return codebuf_emit_u32_le(cb, (uint32_t)v);
        case 8: return codebuf_emit_u64_le(cb, (uint64_t)v);
        default: return false;
    }
}
bool codebuf_emit_op_jump(CodeBuf *cb, uint8_t opcode, int32_t rel) {
    if (!codebuf_emit_u8(cb, opcode)) return false;
    return codebuf_emit_i32_le(cb, rel);
}
