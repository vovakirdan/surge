#ifndef SURGE_CODEGEN_H
#define SURGE_CODEGEN_H

#include <stdint.h>
#include <stdbool.h>
#include "ast.h"
#include "sbc_writer.h"

typedef enum {
    CG_OK = 0,
    CG_ERR
} CgResult;

typedef struct {
    uint8_t *data;
    uint32_t len;
    uint32_t cap;
} CgBuf;

typedef struct {
    // простой map: имя -> локальный слот
    struct { char *name; uint16_t slot; } *locals;
    uint16_t nlocals, caplocals;
} CgLocals;

typedef struct {
    SbcWriter *w;
    uint32_t name_main_idx;
    CgBuf code;
    CgLocals locals;
} Codegen;

void cg_buf_init(CgBuf *b);
void cg_buf_free(CgBuf *b);
bool cg_emit_u8 (CgBuf *b, uint8_t v);
bool cg_emit_u16(CgBuf *b, uint16_t v);
bool cg_emit_u32(CgBuf *b, uint32_t v);
bool cg_emit_u64(CgBuf *b, uint64_t v);
bool cg_emit_f64(CgBuf *b, double d);

bool cg_locals_init(CgLocals *ls);
void cg_locals_free(CgLocals *ls);
bool cg_locals_put(CgLocals *ls, const char *name, uint16_t *out_slot); // alloc new slot
bool cg_locals_get(const CgLocals *ls, const char *name, uint16_t *out_slot);

CgResult surge_codegen_unit(SurgeAstUnit *unit, const char *out_path);

#endif // SURGE_CODEGEN_H
