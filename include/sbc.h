#ifndef SURGE_SBC_H
#define SURGE_SBC_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define SBC_MAGIC 0x30434253u /* "SBC0" in little-endian */
#define SBC_VERSION_MAJOR 0
#define SBC_VERSION_MINOR 1

static inline uint32_t sbc_align4(uint32_t x) {
    return (x + 3u) & ~3u;
}

typedef struct SbcHeader {
    uint32_t magic;
    uint16_t ver_major;
    uint16_t ver_minor;
    uint32_t off_const;
    uint32_t sz_const;
    uint32_t off_funcs;
    uint32_t sz_funcs;
    uint32_t off_code;
    uint32_t sz_code;
    uint32_t reserved;
} SbcHeader;

/*
 * Constant pool encoding (little-endian):
 * [u32 count]
 * repeat count times:
 *   [u8 kind]
 *   [payload...]
 * Payload layout:
 *   I64: [i64]
 *   F64: [f64]
 *   STR: [u32 byte_len][bytes][padding to 4]
 */

typedef enum SbcConstKind {
    SBC_CONST_I64 = 1,
    SBC_CONST_F64 = 2,
    SBC_CONST_STR = 3,
} SbcConstKind;

typedef struct SbcFuncDesc {
    uint32_t name_idx;   /* index into const pool (string) */
    uint16_t arity;      /* parameter count */
    uint16_t nlocals;    /* locals (excluding parameters) */
    uint32_t code_off;   /* offset within code section */
    uint32_t code_len;   /* length in bytes */
    uint32_t flags;      /* reserved for future use */
} SbcFuncDesc;

typedef struct SbcImage {
    SbcHeader hdr;
    uint8_t *blob;
    size_t blob_len;

    const uint8_t *const_sec;  /* pointer to start of const section (count) */
    const uint8_t *const_data; /* pointer to first entry */
    uint32_t const_count;

    SbcFuncDesc *funcs;
    uint32_t func_count;

    const uint8_t *code_sec;
} SbcImage;

bool sbc_load_from_file(const char *path, SbcImage *out_img);
void sbc_unload(SbcImage *img);

bool sbc_const_at(const SbcImage *img, uint32_t index, SbcConstKind *out_kind,
                  const void **out_ptr, uint32_t *out_len);

#endif /* SURGE_SBC_H */
