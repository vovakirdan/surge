#ifndef SURGE_CODEBUF_H
#define SURGE_CODEBUF_H

#include <stddef.h>
#include <stdint.h>
#include <stdbool.h>

typedef struct {
    uint8_t *data;
    uint32_t len;
    uint32_t cap;
} CodeBuf;

void codebuf_init(CodeBuf *cb);
void codebuf_free(CodeBuf *cb);
bool codebuf_reserve(CodeBuf *cb, uint32_t add);
bool codebuf_emit_u8(CodeBuf *cb, uint8_t v);
bool codebuf_emit_u16_le(CodeBuf *cb, uint16_t v);
bool codebuf_emit_u32_le(CodeBuf *cb, uint32_t v);
bool codebuf_emit_i32_le(CodeBuf *cb, int32_t v);
bool codebuf_emit_u64_le(CodeBuf *cb, uint64_t v);

bool codebuf_emit_op0(CodeBuf *cb, uint8_t opcode);
bool codebuf_emit_op1(CodeBuf *cb, uint8_t opcode, uint64_t v, size_t bytes);
bool codebuf_emit_op_jump(CodeBuf *cb, uint8_t opcode, int32_t rel);
bool codebuf_emit_raw(CodeBuf *cb, const void *src, uint32_t n);

#endif
