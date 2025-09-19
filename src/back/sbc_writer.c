#include "sbc_writer.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    SbcConstKind kind;
    union {
        int64_t i64;
        double f64;
        struct {
            char *data;
            uint32_t len;
        } str;
    } u;
} SbcConstEntry;

typedef struct {
    uint32_t name_idx;
    uint16_t arity;
    uint16_t nlocals;
    uint32_t flags;
    uint8_t *code;
    uint32_t code_len;
} SbcFuncEntry;

struct SbcWriter {
    SbcConstEntry *consts;
    size_t const_count;
    size_t const_cap;

    SbcFuncEntry *funcs;
    size_t func_count;
    size_t func_cap;

    uint32_t global_count;
};

static void *xrealloc(void *ptr, size_t new_cap, size_t elem_size) {
    void *p = realloc(ptr, new_cap * elem_size);
    return p;
}

static bool ensure_const_capacity(SbcWriter *w, size_t needed) {
    if (needed <= w->const_cap) {
        return true;
    }
    size_t cap = w->const_cap ? w->const_cap * 2 : 8;
    if (cap < needed) {
        cap = needed;
    }
    SbcConstEntry *new_buf = (SbcConstEntry*)xrealloc(w->consts, cap, sizeof(SbcConstEntry));
    if (!new_buf) {
        return false;
    }
    w->consts = new_buf;
    w->const_cap = cap;
    return true;
}

static bool ensure_func_capacity(SbcWriter *w, size_t needed) {
    if (needed <= w->func_cap) {
        return true;
    }
    size_t cap = w->func_cap ? w->func_cap * 2 : 4;
    if (cap < needed) {
        cap = needed;
    }
    SbcFuncEntry *new_buf = (SbcFuncEntry*)xrealloc(w->funcs, cap, sizeof(SbcFuncEntry));
    if (!new_buf) {
        return false;
    }
    w->funcs = new_buf;
    w->func_cap = cap;
    return true;
}

SbcWriter *sbc_writer_new(void) {
    SbcWriter *w = (SbcWriter*)calloc(1, sizeof(SbcWriter));
    return w;
}

void sbc_writer_set_global_count(SbcWriter *w, uint32_t count) {
    if (!w) {
        return;
    }
    w->global_count = count;
}

void sbc_writer_free(SbcWriter *w) {
    if (!w) {
        return;
    }
    for (size_t i = 0; i < w->const_count; ++i) {
        if (w->consts[i].kind == SBC_CONST_STR) {
            free(w->consts[i].u.str.data);
        }
    }
    for (size_t i = 0; i < w->func_count; ++i) {
        free(w->funcs[i].code);
    }
    free(w->consts);
    free(w->funcs);
    free(w);
}

static uint32_t append_const(SbcWriter *w, SbcConstEntry entry) {
    if (!ensure_const_capacity(w, w->const_count + 1)) {
        return UINT32_MAX;
    }
    w->consts[w->const_count] = entry;
    return (uint32_t)w->const_count++;
}

uint32_t sbc_intern_string_n(SbcWriter *w, const char *s, uint32_t len) {
    if (!w || !s) {
        return UINT32_MAX;
    }
    for (size_t i = 0; i < w->const_count; ++i) {
        if (w->consts[i].kind == SBC_CONST_STR) {
            if (w->consts[i].u.str.len == len && memcmp(w->consts[i].u.str.data, s, len) == 0) {
                return (uint32_t)i;
            }
        }
    }

    SbcConstEntry entry = {
        .kind = SBC_CONST_STR,
        .u.str = {
            .data = (char*)malloc(len + 1u),
            .len = len
        }
    };
    if (!entry.u.str.data) {
        return UINT32_MAX;
    }
    memcpy(entry.u.str.data, s, len);
    entry.u.str.data[len] = '\0';

    return append_const(w, entry);
}

uint32_t sbc_intern_string(SbcWriter *w, const char *s) {
    if (!s) {
        return UINT32_MAX;
    }
    uint32_t len = (uint32_t)strlen(s);
    return sbc_intern_string_n(w, s, len);
}

uint32_t sbc_add_const_i64(SbcWriter *w, int64_t value) {
    if (!w) {
        return UINT32_MAX;
    }
    SbcConstEntry entry;
    entry.kind = SBC_CONST_I64;
    entry.u.i64 = value;
    return append_const(w, entry);
}

uint32_t sbc_add_const_f64(SbcWriter *w, double value) {
    if (!w) {
        return UINT32_MAX;
    }
    SbcConstEntry entry;
    entry.kind = SBC_CONST_F64;
    entry.u.f64 = value;
    return append_const(w, entry);
}

bool sbc_add_function(SbcWriter *w, const SbcFuncInput *fn) {
    if (!w || !fn || !fn->code || fn->code_len == 0) {
        return false;
    }
    if (fn->name_idx == UINT32_MAX) {
        return false;
    }
    if (!ensure_func_capacity(w, w->func_count + 1)) {
        return false;
    }
    SbcFuncEntry entry;
    entry.name_idx = fn->name_idx;
    entry.arity = fn->arity;
    entry.nlocals = fn->nlocals;
    entry.flags = fn->flags;
    entry.code_len = fn->code_len;
    entry.code = (uint8_t*)malloc(fn->code_len);
    if (!entry.code) {
        return false;
    }
    memcpy(entry.code, fn->code, fn->code_len);
    w->funcs[w->func_count++] = entry;
    return true;
}

static bool write_u32_le(FILE *f, uint32_t v) {
    uint8_t buf[4] = {
        (uint8_t)(v & 0xFFu),
        (uint8_t)((v >> 8) & 0xFFu),
        (uint8_t)((v >> 16) & 0xFFu),
        (uint8_t)((v >> 24) & 0xFFu),
    };
    return fwrite(buf, 1, 4, f) == 4;
}

static bool write_u16_le(FILE *f, uint16_t v) {
    uint8_t buf[2] = {
        (uint8_t)(v & 0xFFu),
        (uint8_t)((v >> 8) & 0xFFu),
    };
    return fwrite(buf, 1, 2, f) == 2;
}

static bool write_u64_le(FILE *f, uint64_t v) {
    uint8_t buf[8];
    for (int i = 0; i < 8; ++i) {
        buf[i] = (uint8_t)((v >> (i * 8)) & 0xFFu);
    }
    return fwrite(buf, 1, 8, f) == 8;
}

static bool write_zero_bytes(FILE *f, uint32_t count) {
    static const uint8_t zero[16] = {0};
    while (count > 0) {
        uint32_t chunk = count > sizeof(zero) ? sizeof(zero) : count;
        if (fwrite(zero, 1, chunk, f) != chunk) {
            return false;
        }
        count -= chunk;
    }
    return true;
}

static uint32_t compute_const_section_size(const SbcWriter *w) {
    uint32_t size = 4u; /* count */
    for (size_t i = 0; i < w->const_count; ++i) {
        const SbcConstEntry *ce = &w->consts[i];
        size += 1u; /* kind */
        switch (ce->kind) {
            case SBC_CONST_I64:
            case SBC_CONST_F64:
                size += 8u;
                break;
            case SBC_CONST_STR:
                size += 4u; /* length */
                size += ce->u.str.len;
                break;
            default:
                break;
        }
        size = sbc_align4(size);
    }
    return size;
}

static uint32_t compute_code_size(const SbcWriter *w) {
    uint32_t size = 0;
    for (size_t i = 0; i < w->func_count; ++i) {
        size += w->funcs[i].code_len;
    }
    return size;
}

bool sbc_write_to_file(SbcWriter *w, const char *path) {
    if (!w || !path) {
        return false;
    }

    FILE *f = fopen(path, "wb");
    if (!f) {
        return false;
    }

    uint32_t const_size = compute_const_section_size(w);
    uint32_t func_size = (uint32_t)(w->func_count * sizeof(SbcFuncDesc));
    uint32_t code_size = compute_code_size(w);

    uint32_t offset = sizeof(SbcHeader);
    uint32_t off_const = sbc_align4(offset);
    offset = off_const + const_size;
    uint32_t off_funcs = sbc_align4(offset);
    offset = off_funcs + func_size;
    uint32_t off_code = sbc_align4(offset);
    uint32_t file_size = off_code + code_size;

    SbcHeader hdr = {
        .magic = SBC_MAGIC,
        .ver_major = SBC_VERSION_MAJOR,
        .ver_minor = SBC_VERSION_MINOR,
        .off_const = off_const,
        .sz_const = const_size,
        .off_funcs = off_funcs,
        .sz_funcs = func_size,
        .off_code = off_code,
        .sz_code = code_size,
        .global_count = w->global_count
    };

    if (fwrite(&hdr, 1, sizeof(SbcHeader), f) != sizeof(SbcHeader)) {
        fclose(f);
        return false;
    }

    uint32_t abs_offset = sizeof(SbcHeader);
    if (abs_offset < off_const) {
        if (!write_zero_bytes(f, off_const - abs_offset)) {
            fclose(f);
            return false;
        }
        abs_offset = off_const;
    }

    /* Constant section */
    if (!write_u32_le(f, (uint32_t)w->const_count)) {
        fclose(f);
        return false;
    }
    abs_offset += 4u;
    for (size_t i = 0; i < w->const_count; ++i) {
        const SbcConstEntry *ce = &w->consts[i];
        uint8_t kind = (uint8_t)ce->kind;
        if (fwrite(&kind, 1, 1, f) != 1) {
            fclose(f);
            return false;
        }
        abs_offset += 1u;
        switch (ce->kind) {
            case SBC_CONST_I64:
                if (!write_u64_le(f, (uint64_t)ce->u.i64)) { fclose(f); return false; }
                abs_offset += 8u;
                break;
            case SBC_CONST_F64: {
                uint64_t bits;
                memcpy(&bits, &ce->u.f64, sizeof(bits));
                if (!write_u64_le(f, bits)) { fclose(f); return false; }
                abs_offset += 8u;
                break;
            }
            case SBC_CONST_STR: {
                if (!write_u32_le(f, ce->u.str.len)) { fclose(f); return false; }
                abs_offset += 4u;
                if (fwrite(ce->u.str.data, 1, ce->u.str.len, f) != ce->u.str.len) {
                    fclose(f);
                    return false;
                }
                abs_offset += ce->u.str.len;
                break;
            }
            default:
                fclose(f);
                return false;
        }
        uint32_t aligned = sbc_align4(abs_offset);
        if (aligned > abs_offset) {
            if (!write_zero_bytes(f, aligned - abs_offset)) { fclose(f); return false; }
            abs_offset = aligned;
        }
    }

    uint32_t const_end = off_const + const_size;
    if (abs_offset < const_end) {
        if (!write_zero_bytes(f, const_end - abs_offset)) {
            fclose(f);
            return false;
        }
        abs_offset = const_end;
    } else if (abs_offset > const_end) {
        fclose(f);
        return false;
    }

    if (abs_offset < off_funcs) {
        if (!write_zero_bytes(f, off_funcs - abs_offset)) {
            fclose(f);
            return false;
        }
        abs_offset = off_funcs;
    } else if (abs_offset > off_funcs) {
        fclose(f);
        return false;
    }

    uint32_t code_cursor = 0;
    for (size_t i = 0; i < w->func_count; ++i) {
        const SbcFuncEntry *fn = &w->funcs[i];
        if (!write_u32_le(f, fn->name_idx)) { fclose(f); return false; }
        if (!write_u16_le(f, fn->arity)) { fclose(f); return false; }
        if (!write_u16_le(f, fn->nlocals)) { fclose(f); return false; }
        if (!write_u32_le(f, code_cursor)) { fclose(f); return false; }
        if (!write_u32_le(f, fn->code_len)) { fclose(f); return false; }
        if (!write_u32_le(f, fn->flags)) { fclose(f); return false; }
        code_cursor += fn->code_len;
    }

    abs_offset = off_funcs + func_size;
    if (abs_offset < off_code) {
        if (!write_zero_bytes(f, off_code - abs_offset)) {
            fclose(f);
            return false;
        }
        abs_offset = off_code;
    } else if (abs_offset > off_code) {
        fclose(f);
        return false;
    }

    for (size_t i = 0; i < w->func_count; ++i) {
        const SbcFuncEntry *fn = &w->funcs[i];
        if (fwrite(fn->code, 1, fn->code_len, f) != fn->code_len) {
            fclose(f);
            return false;
        }
    }

    if (fflush(f) != 0) {
        fclose(f);
        return false;
    }

    if (fclose(f) != 0) {
        return false;
    }

    (void)file_size; /* reserved for future checks */
    return true;
}
