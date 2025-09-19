#ifndef SURGE_SBC_WRITER_H
#define SURGE_SBC_WRITER_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "sbc.h"
typedef struct SbcWriter SbcWriter;

typedef struct SbcFuncInput {
    uint32_t name_idx;
    uint16_t arity;
    uint16_t nlocals;
    const uint8_t *code;
    uint32_t code_len;
    uint32_t flags;
} SbcFuncInput;

SbcWriter *sbc_writer_new(void);
void sbc_writer_free(SbcWriter *w);

uint32_t sbc_intern_string(SbcWriter *w, const char *s);
uint32_t sbc_intern_string_n(SbcWriter *w, const char *s, uint32_t len);
uint32_t sbc_add_const_i64(SbcWriter *w, int64_t value);
uint32_t sbc_add_const_f64(SbcWriter *w, double value);

void sbc_writer_set_global_count(SbcWriter *w, uint32_t count);

bool sbc_add_function(SbcWriter *w, const SbcFuncInput *fn);

bool sbc_write_to_file(SbcWriter *w, const char *path);

#endif /* SURGE_SBC_WRITER_H */
