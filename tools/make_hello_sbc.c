#include <stdio.h>
#include <stdlib.h>
#include "sbc_writer.h"
#include "opcodes.h"
#include "codebuf.h"

int main(int argc, char **argv) {
    const char *out = (argc > 1) ? argv[1] : "hello.sbc";

    SbcWriter *w = sbc_writer_new();
    if (!w) return 1;

    uint32_t name_main = sbc_intern_string(w, "main");
    if (name_main == UINT32_MAX) return 2;

    CodeBuf cb; codebuf_init(&cb);

    // PUSH_I64 3; PUSH_I64 4; ADD; RET
    codebuf_emit_op1(&cb, (uint8_t)SURGE_OP_PUSH_I64, 3, 8);
    codebuf_emit_op1(&cb, (uint8_t)SURGE_OP_PUSH_I64, 4, 8);
    codebuf_emit_op0(&cb, (uint8_t)SURGE_OP_ADD);
    codebuf_emit_op0(&cb, (uint8_t)SURGE_OP_RET);

    SbcFuncInput fn = {
        .name_idx = name_main,
        .arity = 0,
        .nlocals = 0,
        .code = cb.data,
        .code_len = cb.len,
        .flags = 0
    };
    if (!sbc_add_function(w, &fn)) return 3;

    if (!sbc_write_to_file(w, out)) return 4;

    codebuf_free(&cb);
    sbc_writer_free(w);

    printf("Wrote %s\n", out);
    return 0;
}
